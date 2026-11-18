package routes

import (
	"encoding/json"
	"fmt"
	"io"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	"membox-serv/src/mycrypto"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/huandu/go-sqlbuilder"
)

// Free kayit (musteri 18 Kasim 2026): header'daki "Get started free" dogrudan kayit sayfasini acar.
// Bugune kadar hesap yalnizca odenmis bir siparisin kayit linkiyle (/signup/<token>) aciliyordu;
// burada kayit formu tek adimda free paket siparisini acar, odenmis sayar, hesabi olusturur ve
// hostu yeni etkinligine sokar. Odeme ekrani ve kayit linki e-postasi yok.
//
// Kurallar Services & Prices'taki free siparisle ayni: urun admin'den kapaliysa satilmaz, e-posta
// basina bir free paket (freePackageUsedByEmail). Bu e-postayla bir hesap zaten varsa kayit
// yapilmaz, host giris yapmaya yonlendirilir.

// freePackageProductID, 22-free-package.sql'in ekledigi urun.
const freePackageProductID = "free"

type signupFreeReq struct {
	Email           string `json:"email"`
	Name            string `json:"name"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	Agreed          bool   `json:"agreed"`
}

// Ayni IP'den saatte en fazla signupFreePerHour kayit denemesi (basarili ya da degil). Her free
// etkinlik depolama maliyeti; e-posta siniri farkli adreslerle asilabildigi icin kaba bir fren.
// Sunucu tek surec, sayac bellekte; yeniden baslatinca sifirlanir.
const signupFreePerHour = 10

var signupFreeAttempts = struct {
	sync.Mutex
	byIP map[string][]time.Time
}{byIP: map[string][]time.Time{}}

func signupFreeAllowed(ip string, now time.Time) bool {
	signupFreeAttempts.Lock()
	defer signupFreeAttempts.Unlock()
	cutoff := now.Add(-time.Hour)
	for key, times := range signupFreeAttempts.byIP {
		kept := times[:0]
		for _, at := range times {
			if at.After(cutoff) {
				kept = append(kept, at)
			}
		}
		if len(kept) == 0 {
			delete(signupFreeAttempts.byIP, key)
		} else {
			signupFreeAttempts.byIP[key] = kept
		}
	}
	if len(signupFreeAttempts.byIP[ip]) >= signupFreePerHour {
		return false
	}
	signupFreeAttempts.byIP[ip] = append(signupFreeAttempts.byIP[ip], now)
	return true
}

// emailHasAccount, buyuk/kucuk harf ve bosluk farki gozetmeden bu e-postayla bir hesap var mi.
func emailHasAccount(email string) (bool, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("count(*)").From("users").Where(
		sb.Equal("lower(btrim(mail))", strings.ToLower(strings.TrimSpace(email))),
	)
	row, err := db.Query_one(sb)
	if err != nil {
		return false, err
	}
	return string(row[0]) != "0", nil
}

// Free, POST /auth/signup/free. Basarida 201 "goto:/events" ve X-Auth-Token (token'li kayitla ayni).
// Hata kodlari: INVALID_INPUT, TOO_MANY_ATTEMPTS, ACCOUNT_EXISTS, FREE_PACKAGE_UNAVAILABLE,
// FREE_PACKAGE_ALREADY_USED.
func (r signup_email_routes_typ) Free(w http.ResponseWriter, req *http.Request) {
	var err error
	var redr_url string

	defer (func() {
		if err != nil {
			fmt.Println("signup free | error:", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if redr_url != "" {
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte("goto:" + redr_url))
		}
	})()

	fail := func(status int, code, message string) {
		_ = networkutils.SendErrorJSON(w, status, code, message)
	}

	if !signupFreeAllowed(auth.GetClientIP(req), time.Now()) {
		fail(http.StatusTooManyRequests, "TOO_MANY_ATTEMPTS", "too many sign ups from this network, try again later")
		return
	}

	body, err := io.ReadAll(io.LimitReader(req.Body, 1<<16))
	if err != nil {
		err = utils.Tag_err("sfr1", err)
		return
	}
	var in signupFreeReq
	if json.Unmarshal(body, &in) != nil {
		fail(http.StatusBadRequest, "INVALID_INPUT", "invalid request")
		return
	}
	in.Email = strings.TrimSpace(in.Email)
	in.Name = strings.TrimSpace(in.Name)

	at := strings.Index(in.Email, "@")
	switch {
	case in.Email == "" || in.Name == "" || in.Password == "":
		fail(http.StatusBadRequest, "INVALID_INPUT", "email, name, and password are required")
		return
	case at < 1 || at == len(in.Email)-1 || strings.ContainsAny(in.Email, " \t\r\n"):
		fail(http.StatusBadRequest, "INVALID_INPUT", "invalid email")
		return
	case in.Password != in.ConfirmPassword:
		fail(http.StatusBadRequest, "INVALID_INPUT", "passwords do not match")
		return
	case !in.Agreed:
		fail(http.StatusBadRequest, "INVALID_INPUT", "you must agree to the terms and conditions")
		return
	}

	exists, err := emailHasAccount(in.Email)
	if err != nil {
		err = utils.Tag_err("sfr2", err)
		return
	}
	if exists {
		fail(http.StatusConflict, "ACCOUNT_EXISTS", "an account with this email already exists, please sign in")
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("uid", "price", "id").From("products").Where(
		sb.Equal("id", freePackageProductID),
		"is_enabled",
		"NOT is_add_on",
		"price = 0",
	)
	products, err := db.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("sfr3", err)
		return
	}
	if len(products) == 0 {
		fail(http.StatusServiceUnavailable, "FREE_PACKAGE_UNAVAILABLE", "the free package is not available right now")
		return
	}

	used, err := freePackageUsedByEmail(in.Email)
	if err != nil {
		err = utils.Tag_err("sfr4", err)
		return
	}
	if used {
		fail(http.StatusConflict, "FREE_PACKAGE_ALREADY_USED", "the free package can be used once per email")
		return
	}

	// Siparis once odenmemis (provider_id bos) yazilir. Hesap olusturulamazsa (ayni anda ayni
	// e-postayla ikinci istek) siparis odenmemis kalir ve e-postanin free hakkini yakmaz.
	cart := types.Cart{}
	cartUID, cartItems, err := cart.InsertQuantityMapWithConfigs(map[string]int{freePackageProductID: 1}, nil)
	if err != nil {
		err = utils.Tag_err("sfr5", err)
		return
	}
	if err = snapshotCartItemPrices(cartUID, cartItems, products); err != nil {
		err = utils.Tag_err("sfr6", err)
		return
	}

	purchaseInfo := types.Js_object{"email": in.Email, "source": "signup_free"}
	purchaseInfoRaw := []byte(purchaseInfo.Json())
	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("purchases")
	ib.Cols("purchase_info", "cart_uid", "provider", "gross_total", "discount_amount", "net_total")
	ib.Values(purchaseInfo.Json(), cartUID, "free", 0, 0, 0)
	ib.SQL("RETURNING uid")
	res, err := db.Query_one(ib)
	if err != nil {
		err = utils.Tag_err("sfr7", err)
		return
	}
	purchaseUID := string(res[0])

	hashedPassword, err := mycrypto.HashPassword(in.Password)
	if err != nil {
		err = utils.Tag_err("sfr8", err)
		return
	}
	userUid, err := dbscripts.CreateUser(in.Name, in.Email, "password", hashedPassword, true)
	if err != nil {
		fmt.Println("signup free | create user:", err)
		err = nil
		fail(http.StatusConflict, "ACCOUNT_EXISTS", "an account with this email already exists, please sign in")
		return
	}

	if err = recordFreePayment(purchaseUID, ""); err != nil {
		err = utils.Tag_err("sfr9", err)
		return
	}
	if err = settleCartItems(cartUID, purchaseInfoRaw); err != nil {
		err = utils.Tag_err("sfr10", err)
		return
	}
	eventUID, err := openPurchasedEvent(userUid, purchaseUID)
	if err != nil {
		err = utils.Tag_err("sfr11", err)
		return
	}
	fmt.Println("signup free | event", eventUID, "purchase", purchaseUID)

	if _, _, err = auth.Authorize(w, req, "auth", userUid, ""); err != nil {
		err = utils.Tag_err("sfr12", err)
		return
	}
	redr_url = "/events"
}
