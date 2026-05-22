package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	networkutils "membox-serv/src/network_utils"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	"strconv"
	"time"

	"github.com/gorilla/mux"
	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

type order_routes_typ struct{}

var OrderRoutes order_routes_typ

// isRestrictedOrderPanelUser, site order_admin rolundeki kullanicilari tespit eder.
// Super adminler tam finansal veriyi gormeye devam eder; sadece DB'deki order_admin rolu kisitlanir.
func isRestrictedOrderPanelUser(userUID string) (bool, error) {
	isSuperAdmin, err := auth.IsSuperAdmin(userUID)
	if err != nil {
		return false, err
	}
	if isSuperAdmin {
		return false, nil
	}

	isOrderAdmin, err := auth.IsPanelOrderAdmin(userUID)
	if err != nil {
		return false, err
	}
	return isOrderAdmin, nil
}

// stripOrderListForOrderAdmin, order_admin rolune gosterilmemesi gereken finansal alanlari siler.
// Bu kontrol backend tarafinda yapilir; sadece frontend'de gizlemek fiyat sizintisini engellemez.
func stripOrderListForOrderAdmin(payload []types.Js_object) {
	for _, order := range payload {
		delete(order, "total")
		delete(order, "gross_total")
		delete(order, "net_total")
		delete(order, "discount")
		delete(order, "discount_amount")
		delete(order, "promo_code_uid")
		delete(order, "promo_code_text_snapshot")
	}
}

// stripOrderDetailForOrderAdmin, siparis detayindaki fiyat/tutar alanlarini order_admin icin temizler.
// Operasyonel bilgiler korunur; urun ve satin alma fiyatlari response'tan cikartilir.
func stripOrderDetailForOrderAdmin(order types.Js_object) {
	delete(order, "total")
	delete(order, "gross_total")
	delete(order, "net_total")
	delete(order, "discount")
	delete(order, "discount_amount")
	delete(order, "promo_code_uid")
	delete(order, "promo_code_text_snapshot")
	delete(order, "payment_summary")

	items, ok := order["items"].([]types.Js_object)
	if !ok {
		return
	}
	for _, item := range items {
		delete(item, "total")
		delete(item, "unit_price")
		delete(item, "discount")
		delete(item, "discount_amount")

		product, ok := item["product"].(types.Js_object)
		if !ok {
			continue
		}
		delete(product, "price")
	}
}

// buildOrderAccountPayload, siparise bagli event icin Order Account metriklerini uretir.
// Aktif eventlerde canli uploads/S3 verisini kullanir; event medya purge ile
// kapatildiysa ayni frontend sozlesmesini korumak icin event_upload_snapshots
// kaydindaki son metriklere duser.
func buildOrderAccountPayload(purchaseUID string) (types.Js_object, error) {
	payload, found, err := buildActiveOrderAccountPayload(purchaseUID)
	if err != nil || found {
		return payload, err
	}
	return buildSnapshotOrderAccountPayload(purchaseUID)
}

func buildActiveOrderAccountPayload(purchaseUID string) (types.Js_object, bool, error) {
	eventBldr := sqlbuilder.BuildNamed(`
		SELECT
			uid::text,
			COALESCE(storage_until::text, '') AS storage_until
		FROM events
		WHERE purchase_uid = ${purchase_uid}
		  AND deleted_at IS NULL
		LIMIT 1
	`, map[string]interface{}{"purchase_uid": purchaseUID})

	eventRow, err := db.Query_one(eventBldr)
	if err != nil {
		if err.Error() == "empty row" {
			return nil, false, nil
		}
		return nil, false, err
	}

	eventUID := string(eventRow[0])
	eventPackedUID, err := utils.UUID.PackUUID(eventUID)
	if err != nil {
		return nil, false, err
	}

	totalGuest, err := dbscripts.Event_contributor_count(eventUID)
	if err != nil {
		return nil, false, err
	}

	guestLimit, err := dbscripts.Event_limit(eventUID, "guest_count")
	if err != nil {
		return nil, false, err
	}

	var totalSizeMB interface{}
	eventFolder := fmt.Sprintf("events/%s", eventPackedUID)
	if sizeMB, sizeErr := s3wrap.Public_s3.Calc_size(eventFolder); sizeErr == nil {
		totalSizeMB = sizeMB
	}

	return types.Js_object{
		"event_uid":          eventUID,
		"event_packed_uid":   eventPackedUID,
		"total_guest":        totalGuest,
		"guest_limit":        guestLimit,
		"total_size_mb":      totalSizeMB,
		"storage_expiration": string(eventRow[1]),
	}, true, nil
}

func buildSnapshotOrderAccountPayload(purchaseUID string) (types.Js_object, error) {
	snapshotBldr := sqlbuilder.BuildNamed(`
		SELECT
			e.uid::text,
			COALESCE(e.storage_until::text, '') AS storage_until,
			COALESCE(s.contributor_count::text, '0') AS contributor_count,
			COALESCE(s.total_upload_size_mb::text, '0') AS total_upload_size_mb
		FROM events e
		JOIN event_upload_snapshots s ON s.event_uid = e.uid
		WHERE e.purchase_uid = ${purchase_uid}
		  AND e.deleted_at IS NOT NULL
		ORDER BY s.captured_at DESC
		LIMIT 1
	`, map[string]interface{}{"purchase_uid": purchaseUID})

	eventRow, err := db.Query_one(snapshotBldr)
	if err != nil {
		if err.Error() == "empty row" {
			return nil, nil
		}
		return nil, err
	}

	eventUID := string(eventRow[0])
	eventPackedUID, err := utils.UUID.PackUUID(eventUID)
	if err != nil {
		return nil, err
	}

	guestLimit, err := dbscripts.Event_limit_including_closed(eventUID, "guest_count")
	if err != nil {
		return nil, err
	}

	totalGuest, err := strconv.Atoi(string(eventRow[2]))
	if err != nil {
		return nil, err
	}
	totalSizeMB, err := strconv.ParseFloat(string(eventRow[3]), 64)
	if err != nil {
		return nil, err
	}

	return types.Js_object{
		"event_uid":          eventUID,
		"event_packed_uid":   eventPackedUID,
		"total_guest":        totalGuest,
		"guest_limit":        guestLimit,
		"total_size_mb":      totalSizeMB,
		"storage_expiration": string(eventRow[1]),
	}, nil
}

// GET /api/admin/orders
func (o order_routes_typ) ListOrders(w http.ResponseWriter, r *http.Request) {
	var err error
	var stat_code int
	var payload []types.Js_object

	defer func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		networkutils.SendJson(payload, w)
	}()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		stat_code = http.StatusUnauthorized
		return
	}

	restrictedOrderPanelUser, err := isRestrictedOrderPanelUser(claims.UserUID)
	if err != nil {
		err = utils.Tag_err("lo0", err)
		return
	}

	bldr := sqlbuilder.BuildNamed(`
		SELECT
			p.uid,
			p.created_at,
			COALESCE(u.mail, p.purchase_info->>'email') AS buyer_email,
			COALESCE(u.name, '') AS buyer_name,
			COUNT(ci.product_uid) AS items_count,
			SUM(COALESCE(ci.unit_price_snapshot, pr.price) * ci.quantity) AS total,
			MIN(ci.status::text) AS worst_status,
			COALESCE(p.gross_total, SUM(COALESCE(ci.unit_price_snapshot, pr.price) * ci.quantity))::text AS gross_total,
			COALESCE(p.discount_amount, 0)::text AS discount_amount,
			COALESCE(p.net_total, SUM(COALESCE(ci.unit_price_snapshot, pr.price) * ci.quantity))::text AS net_total,
			COALESCE(p.promo_code_uid::text, '') AS promo_code_uid,
			COALESCE(p.promo_code_text_snapshot, '') AS promo_code_text_snapshot
		FROM purchases p
		LEFT JOIN users u ON p.buyer_uid = u.uid
		LEFT JOIN carts c ON p.cart_uid = c.uid
		LEFT JOIN cart_items ci ON ci.cart_uid = c.uid
		LEFT JOIN products pr ON ci.product_uid = pr.uid
		WHERE p.provider_id IS NOT NULL OR p.provider IS NOT NULL
		GROUP BY p.uid, u.mail, u.name, p.purchase_info, p.created_at
		ORDER BY p.created_at DESC
	`, map[string]interface{}{})

	rows, err := db.Query_all(bldr)
	if err != nil {
		err = utils.Tag_err("lo1", err)
		return
	}

	payload = []types.Js_object{}
	for _, row := range rows {
		payload = append(payload, types.Js_object{
			"uid":                      string(row[0]),
			"created_at":               string(row[1]),
			"buyer_email":              string(row[2]),
			"buyer_name":               string(row[3]),
			"items_count":              string(row[4]),
			"total":                    string(row[5]),
			"worst_status":             string(row[6]),
			"gross_total":              string(row[7]),
			"discount_amount":          string(row[8]),
			"net_total":                string(row[9]),
			"promo_code_uid":           emptyStringAsNil(row[10]),
			"promo_code_text_snapshot": emptyStringAsNil(row[11]),
		})
	}

	if restrictedOrderPanelUser {
		stripOrderListForOrderAdmin(payload)
	}
	stat_code = 200
}

// GET /api/admin/orders/{purchaseUID}
func (o order_routes_typ) GetOrder(w http.ResponseWriter, r *http.Request) {
	var err error
	var stat_code int

	defer func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
		}
	}()

	purchaseUID := mux.Vars(r)["purchaseUID"]

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		stat_code = http.StatusUnauthorized
		return
	}

	restrictedOrderPanelUser, err := isRestrictedOrderPanelUser(claims.UserUID)
	if err != nil {
		err = utils.Tag_err("go0", err)
		return
	}

	// Fetch purchase + buyer info
	purchaseBldr := sqlbuilder.BuildNamed(`
		SELECT
			p.uid,
			p.created_at,
			COALESCE(u.mail, p.purchase_info->>'email') AS buyer_email,
			COALESCE(u.name, '') AS buyer_name,
			p.provider,
			COALESCE(p.purchase_info->>'shipping_address', '') AS shipping_address,
			COALESCE(p.gross_total::text, '') AS gross_total,
			COALESCE(p.discount_amount::text, '') AS discount_amount,
			COALESCE(p.net_total::text, '') AS net_total,
			COALESCE(p.promo_code_uid::text, '') AS promo_code_uid,
			COALESCE(p.promo_code_text_snapshot, '') AS promo_code_text_snapshot
		FROM purchases p
		LEFT JOIN users u ON p.buyer_uid = u.uid
		WHERE p.uid = ${purchase_uid}
	`, map[string]interface{}{"purchase_uid": purchaseUID})

	purchaseRow, err := db.Query_one(purchaseBldr)
	if err != nil {
		err = utils.Tag_err("go1", err)
		return
	}

	// Fetch cart items with product info
	itemsBldr := sqlbuilder.BuildNamed(`
		SELECT
			ci.uid,
			ci.cart_uid,
			ci.quantity,
			ci.status,
			ci.note,
			ci.admin_note,
			ci.tracking_number,
			ci.carrier,
			ci.buyer_config,
			ci.shipped_at,
			ci.fulfilled_at,
			ci.created_at,
			pr.id,
			COALESCE(ci.unit_price_snapshot, pr.price)::text,
			pr.options,
			pr.fullfillment_type,
			ci.np_waybill_ref
		FROM purchases p
		JOIN carts c ON p.cart_uid = c.uid
		JOIN cart_items ci ON ci.cart_uid = c.uid
		JOIN products pr ON ci.product_uid = pr.uid
		WHERE p.uid = ${purchase_uid}
	`, map[string]interface{}{"purchase_uid": purchaseUID})

	itemRows, err := db.Query_all(itemsBldr)
	if err != nil {
		err = utils.Tag_err("go2", err)
		return
	}

	items := []types.Js_object{}
	for _, row := range itemRows {
		var buyerConfig types.Js_object
		json.Unmarshal(row[8], &buyerConfig)

		var productOptions types.Js_object
		json.Unmarshal(row[14], &productOptions)

		items = append(items, types.Js_object{
			"uid":             string(row[0]),
			"cart_uid":        string(row[1]),
			"quantity":        string(row[2]),
			"status":          string(row[3]),
			"note":            string(row[4]),
			"admin_note":      string(row[5]),
			"tracking_number": string(row[6]),
			"carrier":         string(row[7]),
			"buyer_config":    buyerConfig,
			"shipped_at":      string(row[9]),
			"fulfilled_at":    string(row[10]),
			"created_at":      string(row[11]),
			"np_waybill_ref":  string(row[16]),
			"product": types.Js_object{
				"id":                string(row[12]),
				"price":             string(row[13]),
				"options":           productOptions,
				"fullfillment_type": string(row[15]),
			},
		})
	}

	var shippingAddress interface{}
	if sa := string(purchaseRow[5]); sa != "" {
		var saObj types.Js_object
		if json.Unmarshal([]byte(sa), &saObj) == nil {
			shippingAddress = saObj
		}
	}

	paymentGross := string(purchaseRow[6])
	paymentDiscount := string(purchaseRow[7])
	paymentNet := string(purchaseRow[8])
	if paymentGross == "" || paymentDiscount == "" || paymentNet == "" {
		grossTotal := 0.0
		for _, item := range items {
			quantity, _ := strconv.ParseFloat(fmt.Sprintf("%v", item["quantity"]), 64)
			product, _ := item["product"].(types.Js_object)
			unitPrice, _ := strconv.ParseFloat(fmt.Sprintf("%v", product["price"]), 64)
			grossTotal += quantity * unitPrice
		}
		paymentGross = fmt.Sprintf("%.2f", grossTotal)
		paymentDiscount = "0"
		paymentNet = fmt.Sprintf("%.2f", grossTotal)
	}

	paymentSummary := types.Js_object{
		"gross_total":              paymentGross,
		"discount_amount":          paymentDiscount,
		"net_total":                paymentNet,
		"promo_code_uid":           emptyStringAsNil(purchaseRow[9]),
		"promo_code_text_snapshot": emptyStringAsNil(purchaseRow[10]),
	}

	order := types.Js_object{
		"uid":              string(purchaseRow[0]),
		"created_at":       string(purchaseRow[1]),
		"buyer_email":      string(purchaseRow[2]),
		"buyer_name":       string(purchaseRow[3]),
		"provider":         string(purchaseRow[4]),
		"shipping_address": shippingAddress,
		"items":            items,
		"payment_summary":  paymentSummary,
	}

	orderAccount, err := buildOrderAccountPayload(purchaseUID)
	if err != nil {
		err = utils.Tag_err("go3", err)
		return
	}
	order["order_account"] = orderAccount

	if restrictedOrderPanelUser {
		stripOrderDetailForOrderAdmin(order)
	}
	stat_code = 200
	networkutils.SendJson(order, w)
}

// PATCH /api/admin/orders/items/{cartItemUID}
func (o order_routes_typ) UpdateItem(w http.ResponseWriter, r *http.Request) {
	var err error
	var stat_code int

	defer func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
		}
	}()

	cartItemUID := mux.Vars(r)["cartItemUID"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = utils.Tag_err("ui1", err)
		return
	}

	var req types.Js_object
	err = json.Unmarshal(body, &req)
	if err != nil {
		err = utils.Tag_err("ui2", err)
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("cart_items")
	assigns := []string{}

	if status, ok := req["status"].(string); ok && status != "" {
		assigns = append(assigns, ub.Assign("status", status))
		if status == "shipped" {
			assigns = append(assigns, ub.Assign("shipped_at", time.Now()))
		} else if status == "fulfilled" {
			assigns = append(assigns, ub.Assign("fulfilled_at", time.Now()))
		}
	}
	if tracking, ok := req["tracking_number"].(string); ok {
		assigns = append(assigns, ub.Assign("tracking_number", tracking))
	}
	if carrier, ok := req["carrier"].(string); ok {
		assigns = append(assigns, ub.Assign("carrier", carrier))
	}
	if note, ok := req["admin_note"].(string); ok {
		assigns = append(assigns, ub.Assign("admin_note", note))
	}

	if len(assigns) == 0 {
		stat_code = 200
		return
	}

	ub.Set(assigns...)
	ub.Where(ub.Equal("uid", cartItemUID))

	err = db.Exec(ub)
	if err != nil {
		err = utils.Tag_err("ui3", err)
		return
	}

	stat_code = 200
	w.WriteHeader(200)
}

// GET /api/event/{eventPackedUID}/order
func (o order_routes_typ) GetMyOrder(w http.ResponseWriter, r *http.Request) {
	var err error
	var stat_code int

	defer func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
		}
	}()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		stat_code = 401
		return
	}

	eventPackedUID := mux.Vars(r)["eventPackedUID"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("gmo1", err)
		return
	}

	isAdmin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil || !isAdmin {
		err = errors.New("forbidden")
		stat_code = 403
		return
	}

	itemsBldr := sqlbuilder.BuildNamed(`
		SELECT
			ci.uid,
			ci.cart_uid,
			ci.quantity,
			ci.status,
			ci.tracking_number,
			ci.carrier,
			ci.buyer_config,
			ci.created_at,
			pr.id,
			COALESCE(ci.unit_price_snapshot, pr.price)::text,
			pr.options,
			pr.fullfillment_type,
			pr.granted_features::text
		FROM events e
		JOIN purchases pu ON e.purchase_uid = pu.uid
		JOIN carts c ON pu.cart_uid = c.uid
		JOIN cart_items ci ON ci.cart_uid = c.uid
		JOIN products pr ON ci.product_uid = pr.uid
		WHERE e.uid = ${event_uid} AND e.deleted_at IS NULL AND pr.is_add_on = TRUE
	`, map[string]interface{}{"event_uid": eventUID})

	itemRows, err := db.Query_all(itemsBldr)
	if err != nil {
		err = utils.Tag_err("gmo2", err)
		return
	}

	items := []types.Js_object{}
	for _, row := range itemRows {
		var buyerConfig types.Js_object
		json.Unmarshal(row[6], &buyerConfig)

		var productOptions types.Js_object
		json.Unmarshal(row[10], &productOptions)

		items = append(items, types.Js_object{
			"uid":             string(row[0]),
			"cart_uid":        string(row[1]),
			"quantity":        string(row[2]),
			"status":          string(row[3]),
			"tracking_number": string(row[4]),
			"carrier":         string(row[5]),
			"buyer_config":    buyerConfig,
			"created_at":      string(row[7]),
			"product": types.Js_object{
				"id":                string(row[8]),
				"price":             string(row[9]),
				"options":           productOptions,
				"fullfillment_type": string(row[11]),
				"granted_features":  string(row[12]),
			},
		})
	}

	stat_code = 200
	networkutils.SendJson(types.Js_object{"items": items}, w)
}

// PATCH /api/event/{eventPackedUID}/order/items/{cartItemUID}
func (o order_routes_typ) SubmitBuyerConfig(w http.ResponseWriter, r *http.Request) {
	var err error
	var stat_code int

	defer func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
		}
	}()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		stat_code = 401
		return
	}

	vars := mux.Vars(r)
	eventPackedUID := vars["eventPackedUID"]
	cartItemUID := vars["cartItemUID"]

	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("sbc1", err)
		return
	}

	isAdmin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil || !isAdmin {
		err = errors.New("forbidden")
		stat_code = 403
		return
	}

	// Verify cart item belongs to this event and is in client-action status
	verifyBldr := sqlbuilder.BuildNamed(`
		SELECT ci.status
		FROM events e
		JOIN purchases pu ON e.purchase_uid = pu.uid
		JOIN carts c ON pu.cart_uid = c.uid
		JOIN cart_items ci ON ci.cart_uid = c.uid
		WHERE e.uid = ${event_uid} AND e.deleted_at IS NULL AND ci.uid = ${cart_item_uid}
	`, map[string]interface{}{
		"event_uid":     eventUID,
		"cart_item_uid": cartItemUID,
	})

	statusRow, err := db.Query_one(verifyBldr)
	if err != nil {
		err = utils.Tag_err("sbc2", err)
		return
	}

	currentStatus := string(statusRow[0])
	if currentStatus != "client-action" {
		err = errors.New("item is not awaiting configuration")
		stat_code = 400
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = utils.Tag_err("sbc3", err)
		return
	}

	var req struct {
		BuyerConfig types.Js_object `json:"buyer_config"`
	}
	err = json.Unmarshal(body, &req)
	if err != nil {
		err = utils.Tag_err("sbc4", err)
		return
	}

	configJson, err := json.Marshal(req.BuyerConfig)
	if err != nil {
		err = utils.Tag_err("sbc5", err)
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("cart_items")
	ub.Set(
		ub.Assign("buyer_config", string(configJson)),
		ub.Assign("status", "admin-action"),
	)
	ub.Where(ub.Equal("uid", cartItemUID))

	err = db.Exec(ub)
	if err != nil {
		err = utils.Tag_err("sbc6", err)
		return
	}

	// Fetch shipping address + product options and auto-create NP waybill (non-fatal)
	go func() {
		saBldr := sqlbuilder.BuildNamed(`
			SELECT pu.purchase_info, pr.options
			FROM events e
			JOIN purchases pu ON e.purchase_uid = pu.uid
			JOIN carts c ON pu.cart_uid = c.uid
			JOIN cart_items ci ON ci.cart_uid = c.uid
			JOIN products pr ON ci.product_uid = pr.uid
			WHERE e.uid = ${event_uid} AND e.deleted_at IS NULL AND ci.uid = ${cart_item_uid}
			LIMIT 1
		`, map[string]interface{}{
			"event_uid":     eventUID,
			"cart_item_uid": cartItemUID,
		})
		row, fetchErr := db.Query_one(saBldr)
		if fetchErr != nil {
			return
		}
		shippingAddr := shippingAddressFromPurchaseInfo(row[0])
		var bc map[string]interface{}
		json.Unmarshal(configJson, &bc)
		var po map[string]interface{}
		json.Unmarshal(row[1], &po)
		tryCreateNPWaybill(cartItemUID, shippingAddr, bc, po)
	}()

	stat_code = 200
	w.WriteHeader(200)
}

// POST /api/admin/orders/items/{cartItemUID}/retry-waybill
func (o order_routes_typ) RetryWaybill(w http.ResponseWriter, r *http.Request) {
	cartItemUID := mux.Vars(r)["cartItemUID"]
	if err := retryWaybillForItem(cartItemUID); err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.WriteHeader(200)
	w.Write([]byte("waybill created successfully"))
}

// GET /api/admin/check
func (o order_routes_typ) AdminCheck(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	encToken, err := auth.GetToken(r)
	if err != nil {
		w.WriteHeader(200)
		w.Write([]byte(`{"is_admin":false,"is_super_admin":false,"is_order_admin":false,"was_panel_admin":false,"has_active_event":false}`))
		return
	}
	claims, err := auth.ValidateToken(encToken, auth.GetClientIP(r))
	if err != nil {
		w.WriteHeader(200)
		w.Write([]byte(`{"is_admin":false,"is_super_admin":false,"is_order_admin":false,"was_panel_admin":false,"has_active_event":false}`))
		return
	}

	// AdminCheck, frontend'in site panel menulerini ve revoke edilmis admin yonlendirmesini hesaplamasi icin rol bilgisi dondurur.
	// is_admin geriye uyumluluk icin korunur; super veya aktif order admin olan herkes icin true olur.
	isSuperAdmin, _ := auth.IsSuperAdmin(claims.UserUID)
	isOrderAdmin := false
	if !isSuperAdmin {
		isOrderAdmin, _ = auth.IsPanelOrderAdmin(claims.UserUID)
	}

	wasPanelAdmin := false
	wasPanelAdminBldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*)
		FROM panel_admins
		WHERE user_uid = ${user_uid}
	`, map[string]interface{}{"user_uid": claims.UserUID})
	if wasPanelAdminRow, panelErr := db.Query_one(wasPanelAdminBldr); panelErr == nil {
		wasPanelAdmin = string(wasPanelAdminRow[0]) != "0"
	}

	hasActiveEvent := false
	hasActiveEventBldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*)
		FROM events
		WHERE ${user_uid}::uuid = ANY(admins)
		  AND deleted_at IS NULL
	`, map[string]interface{}{"user_uid": claims.UserUID})
	if hasActiveEventRow, eventErr := db.Query_one(hasActiveEventBldr); eventErr == nil {
		hasActiveEvent = string(hasActiveEventRow[0]) != "0"
	}

	w.WriteHeader(200)
	networkutils.SendJson(types.Js_object{
		"is_admin":         isSuperAdmin || isOrderAdmin,
		"is_super_admin":   isSuperAdmin,
		"is_order_admin":   isOrderAdmin,
		"was_panel_admin":  wasPanelAdmin,
		"has_active_event": hasActiveEvent,
	}, w)
}
