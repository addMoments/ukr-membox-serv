package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	"membox-serv/src/env"
	"membox-serv/src/mycrypto"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/payments"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

// Host panelinden paket yukseltme ve ek depolama (Excel 2.18).
//
//	GET  /api/event/{eventPackedUid}/upgrades  -> gecerli paket + satilabilen secenekler
//	POST /api/event/{eventPackedUid}/upgrade   -> secilen yukseltme icin odeme acar
//
// Odeme normal siparisle ayni LiqPay akisindan gecer; fark siparisin etkinlige bagli olmasi
// (purchases.event_uid). Dogrulanmis odeme bildirimi geldiginde Payment_cb kayit linki
// uretmez, apply_event_upgrade ile yukseltmeyi uygular (payment-cb.go).

type upgrade_routes_typ struct{}

var UpgradeRoutes upgrade_routes_typ

type upgradeReq struct {
	ProductID  string `json:"product_id"`
	ProviderID string `json:"provider_id"`
}

// upgradeEventFromRequest, yol parametresindeki etkinligi cozer ve istegi yapanin o etkinligin
// admini oldugunu dogrular. Hata olursa cevabi kendisi yazar ve ok=false doner.
func upgradeEventFromRequest(w http.ResponseWriter, r *http.Request) (eventUID string, claims auth.TokenClaims, ok bool) {
	claims, ok = r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ok = false

	eventUID, err := utils.UUID.UnpackUUID(mux.Vars(r)["eventPackedUid"])
	if err != nil {
		http.Error(w, "invalid event", http.StatusBadRequest)
		return
	}

	isAdmin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
		return
	}
	if err != nil {
		http.Error(w, utils.Tag_err("upr1", err).Error(), http.StatusInternalServerError)
		return
	}
	if !isAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	ok = true
	return
}

// GET /api/event/{eventPackedUid}/upgrades
func (ur upgrade_routes_typ) Options(w http.ResponseWriter, r *http.Request) {
	eventUID, _, ok := upgradeEventFromRequest(w, r)
	if !ok {
		return
	}

	current, options, err := dbscripts.Event_upgrade_options(eventUID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	windowOpen, err := dbscripts.Event_upgrade_window_open(eventUID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("display_name_en", "display_name_uk").From("products").Where(sb.Equal("id", current))
	names, err := db.Query_one(sb)
	if err != nil {
		http.Error(w, utils.Tag_err("upr2", err).Error(), http.StatusInternalServerError)
		return
	}

	networkutils.SendJson(types.Js_object{
		"current_package": types.Js_object{
			"id":              current,
			"display_name_en": string(names[0]),
			"display_name_uk": string(names[1]),
		},
		// Galeri suresi bittiyse secenekler gosterilir ama satin alinamaz.
		"window_open": windowOpen,
		"options":     options,
	}, w)
}

// POST /api/event/{eventPackedUid}/upgrade
// Body: {"product_id": "upgrade_standard_plus", "provider_id": "liqpay"}
// Cevap odeme saglayicisinin formu (normal /api/purchase ile ayni bicim).
func (ur upgrade_routes_typ) Purchase(w http.ResponseWriter, r *http.Request) {
	eventUID, claims, ok := upgradeEventFromRequest(w, r)
	if !ok {
		return
	}

	var err error
	var stat_code int
	defer func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), stat_code)
		}
	}()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = utils.Tag_err("upp1", err)
		stat_code = http.StatusBadRequest
		return
	}
	var req upgradeReq
	if err = json.Unmarshal(body, &req); err != nil {
		err = errors.New("invalid request body")
		stat_code = http.StatusBadRequest
		return
	}
	if req.ProviderID == "" {
		req.ProviderID = "liqpay"
	}
	provider, found := payments.PaymentProviders[req.ProviderID]
	if !found {
		err = errors.New("unknown payment provider")
		stat_code = http.StatusBadRequest
		return
	}

	windowOpen, err := dbscripts.Event_upgrade_window_open(eventUID)
	if err != nil {
		return
	}
	if !windowOpen {
		_ = networkutils.SendErrorJSON(w, http.StatusConflict, "UPGRADE_WINDOW_CLOSED", dbscripts.ErrUpgradeWindowClosed.Error())
		return
	}

	// Secenek etkinligin bugunku paketine satiliyor olmali (Mini'ye Premium ek GB'si satilmaz,
	// Classic'e Mini -> Classic satilmaz). Liste ile ayni sorgu, ayni kural.
	_, options, err := dbscripts.Event_upgrade_options(eventUID)
	if err != nil {
		return
	}
	var option *dbscripts.UpgradeOption
	for i := range options {
		if options[i].ID == req.ProductID {
			option = &options[i]
			break
		}
	}
	if option == nil {
		_ = networkutils.SendErrorJSON(w, http.StatusConflict, "UPGRADE_NOT_OFFERED", dbscripts.ErrUpgradeNotOffered.Error())
		return
	}

	userSB := sqlbuilder.NewSelectBuilder()
	userSB.Select("COALESCE(mail, '')").From("users").Where(userSB.Equal("uid", claims.UserUID))
	userRow, err := db.Query_one(userSB)
	if err != nil {
		err = utils.Tag_err("upp2", err)
		return
	}
	email := string(userRow[0])

	cart := types.Cart{}
	cartUID, cartItems, err := cart.InsertQuantityMap(map[string]int{option.ID: 1})
	if err != nil {
		err = utils.Tag_err("upp3", err)
		return
	}

	productSB := sqlbuilder.NewSelectBuilder()
	productSB.Select("uid", "price").From("products").Where(productSB.Equal("id", option.ID))
	products, err := db.Query_all(productSB)
	if err != nil {
		err = utils.Tag_err("upp4", err)
		return
	}
	if err = snapshotCartItemPrices(cartUID, cartItems, products); err != nil {
		err = utils.Tag_err("upp5", err)
		return
	}

	purchaseInfo := types.Js_object{"email": email, "upgrade": option.ID}
	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("purchases")
	ib.Cols("purchase_info", "cart_uid", "provider", "buyer_uid", "event_uid", "gross_total", "discount_amount", "net_total")
	ib.Values(purchaseInfo.Json(), cartUID, provider.Name, claims.UserUID, eventUID, option.Price, 0, option.Price)
	ib.SQL("RETURNING uid")
	res, err := db.Query_one(ib)
	if err != nil {
		err = utils.Tag_err("upp6", err)
		return
	}
	purchaseUID := string(res[0])

	packedPurchaseUID, err := utils.UUID.PackUUID(purchaseUID)
	if err != nil {
		err = utils.Tag_err("upp7", err)
		return
	}
	encPackedPurchaseUID, err := mycrypto.Encrypt(packedPurchaseUID, []byte(env.Env().PaymentSecret))
	if err != nil {
		err = utils.Tag_err("upp8", err)
		return
	}

	name := option.NameEN
	if strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "uk") && option.NameUK != "" {
		name = option.NameUK
	}
	fmt.Printf("[upgrade.purchase] event=%s user=%s product=%s purchase=%s amount=%.2f\n",
		eventUID, claims.UserUID, option.ID, purchaseUID, option.Price)

	err = provider.OpenPayment(w, r, payments.PaymentRequest{
		Currency:               "UAH",
		Addcomission_to_amount: true,
		Note:                   "Addmoments: " + name,
		Amount:                 option.Price,
		Name:                   email,
		ReferenceNo:            encPackedPurchaseUID,
	})
}

// upgradeReturnURL, yukseltme odemesinden sonra hostun dondugu etkinlik sayfasi; done ise
// sayfa "yukseltme tamamlandi" bildirimini gosterir (?upgrade=done).
func upgradeReturnURL(eventUID string, done bool) string {
	packed, err := utils.UUID.PackUUID(eventUID)
	if err != nil {
		return "/events"
	}
	if !done {
		return "/event/" + packed
	}
	return "/event/" + packed + "?upgrade=done"
}

// POST /api/admin/orders/{purchaseUID}/apply-upgrade
// Odemesi dogrulanmis ama uygulanamamis (Payment_cb'de hata) bir yukseltmeyi yeniden uygular.
func (o order_routes_typ) ApplyUpgrade(w http.ResponseWriter, r *http.Request) {
	purchaseUID := mux.Vars(r)["purchaseUID"]

	eventUID, appliedAt, err := dbscripts.Purchase_upgrade_state(purchaseUID)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	if eventUID == "" {
		http.Error(w, "this order is not an event upgrade", http.StatusConflict)
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COALESCE(provider_id, '')").From("purchases").Where(sb.Equal("uid", purchaseUID))
	res, err := db.Query_one(sb)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	providerID := string(res[0])
	if providerID == "" || strings.HasPrefix(providerID, "failed:") {
		http.Error(w, "payment has not been confirmed for this order", http.StatusConflict)
		return
	}

	if appliedAt == "" {
		if _, err = dbscripts.Apply_event_upgrade(purchaseUID); err != nil {
			fmt.Printf("[order.apply-upgrade] ERROR: purchase=%s err=%v\n", purchaseUID, err)
			http.Error(w, "the upgrade could not be applied", http.StatusInternalServerError)
			return
		}
		_, appliedAt, _ = dbscripts.Purchase_upgrade_state(purchaseUID)
		fmt.Printf("[order.apply-upgrade] purchase=%s event=%s applied\n", purchaseUID, eventUID)
	}

	networkutils.SendJson(types.Js_object{"ok": true, "event_uid": eventUID, "upgrade_applied_at": appliedAt}, w)
}
