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

type upgradeItem struct {
	ProductID string `json:"product_id"`
	Quantity  int    `json:"quantity"`
}

type upgradeReq struct {
	Items      []upgradeItem `json:"items"`
	ProductID  string        `json:"product_id"` // tek kalem, adet 1 (ilk bicim)
	ProviderID string        `json:"provider_id"`
}

// Premium'un +6 ay / +25 GB / +1 ay yuklemesi ayni odemede birden fazla alinabilir (musteri 22 Ekim: "Yes, it is
// possible"); ust sinir yanlislikla buyuk tutar acilmasin diye.
const maxUpgradeQuantity = 10

// upgradeSelection, istekteki kalemleri etkinlige satilabilen seceneklerle eslestirir.
// Kural: paket yukseltmesi tek basina ve tek adet; storage/time/upload kalemleri birlikte ve
// 1..maxUpgradeQuantity adet. Hata kodu bos degilse istek reddedilir.
func upgradeSelection(req upgradeReq, options []dbscripts.UpgradeOption) (selected []dbscripts.UpgradeOption, quantities map[string]int, total float64, code string, msg string) {
	items := req.Items
	if len(items) == 0 && req.ProductID != "" {
		items = []upgradeItem{{ProductID: req.ProductID, Quantity: 1}}
	}
	if len(items) == 0 {
		return nil, nil, 0, "UPGRADE_EMPTY", "no upgrade selected"
	}

	byID := map[string]dbscripts.UpgradeOption{}
	for _, option := range options {
		byID[option.ID] = option
	}
	quantities = map[string]int{}
	hasPackage := false
	for _, item := range items {
		option, offered := byID[item.ProductID]
		if !offered {
			return nil, nil, 0, "UPGRADE_NOT_OFFERED", dbscripts.ErrUpgradeNotOffered.Error()
		}
		if _, dup := quantities[item.ProductID]; dup {
			return nil, nil, 0, "UPGRADE_INVALID", "each upgrade can be listed once"
		}
		if item.Quantity < 1 || item.Quantity > maxUpgradeQuantity {
			return nil, nil, 0, "UPGRADE_INVALID", fmt.Sprintf("quantity must be between 1 and %d", maxUpgradeQuantity)
		}
		if option.Kind == "package" {
			hasPackage = true
		}
		quantities[item.ProductID] = item.Quantity
		selected = append(selected, option)
		total += option.Price * float64(item.Quantity)
	}
	if hasPackage && (len(items) != 1 || items[0].Quantity != 1) {
		return nil, nil, 0, "UPGRADE_INVALID", "a package upgrade is bought on its own, once"
	}
	return selected, quantities, total, "", ""
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

	currentPackage, err := dbscripts.Package_info(current)
	if err != nil {
		http.Error(w, utils.Tag_err("upr2", err).Error(), http.StatusInternalServerError)
		return
	}

	networkutils.SendJson(types.Js_object{
		"current_package": currentPackage,
		// Galeri suresi bittiyse secenekler gosterilir ama satin alinamaz.
		"window_open":  windowOpen,
		"options":      options,
		"max_quantity": maxUpgradeQuantity,
	}, w)
}

// POST /api/event/{eventPackedUid}/upgrade
// Body: {"items": [{"product_id": "premium_extra_storage", "quantity": 2}, ...], "provider_id": "liqpay"}
//
//	ya da tek kalem icin {"product_id": "upgrade_standard_plus"}
//
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
	selected, quantities, total, code, msg := upgradeSelection(req, options)
	if code == "UPGRADE_NOT_OFFERED" {
		_ = networkutils.SendErrorJSON(w, http.StatusConflict, code, msg)
		return
	}
	if code != "" {
		_ = networkutils.SendErrorJSON(w, http.StatusBadRequest, code, msg)
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
	cartUID, cartItems, err := cart.InsertQuantityMap(quantities)
	if err != nil {
		err = utils.Tag_err("upp3", err)
		return
	}

	productIDs := []string{}
	for id := range quantities {
		productIDs = append(productIDs, id)
	}
	productSB := sqlbuilder.NewSelectBuilder()
	productSB.Select("uid", "price").From("products").Where(productSB.In("id", db.Interface_ar(productIDs)...))
	products, err := db.Query_all(productSB)
	if err != nil {
		err = utils.Tag_err("upp4", err)
		return
	}
	if err = snapshotCartItemPrices(cartUID, cartItems, products); err != nil {
		err = utils.Tag_err("upp5", err)
		return
	}

	upgradeInfo := types.Js_object{}
	for id, quantity := range quantities {
		upgradeInfo[id] = quantity
	}
	purchaseInfo := types.Js_object{"email": email, "upgrade": upgradeInfo}
	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("purchases")
	ib.Cols("purchase_info", "cart_uid", "provider", "buyer_uid", "event_uid", "gross_total", "discount_amount", "net_total")
	ib.Values(purchaseInfo.Json(), cartUID, provider.Name, claims.UserUID, eventUID, total, 0, total)
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

	ukrainian := strings.HasPrefix(strings.ToLower(r.Header.Get("Accept-Language")), "uk")
	names := []string{}
	for _, option := range selected {
		name := option.NameEN
		if ukrainian && option.NameUK != "" {
			name = option.NameUK
		}
		if quantities[option.ID] > 1 {
			name += fmt.Sprintf(" × %d", quantities[option.ID])
		}
		names = append(names, name)
	}
	fmt.Printf("[upgrade.purchase] event=%s user=%s items=%v purchase=%s amount=%.2f\n",
		eventUID, claims.UserUID, quantities, purchaseUID, total)

	err = provider.OpenPayment(w, r, payments.PaymentRequest{
		Currency:               "UAH",
		Addcomission_to_amount: true,
		Note:                   "Addmoments: " + strings.Join(names, ", "),
		Amount:                 total,
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
