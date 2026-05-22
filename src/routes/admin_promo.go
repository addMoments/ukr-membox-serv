package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	db "membox-serv/src/db_layer"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/promo"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type admin_promo_routes_typ struct{}

var AdminPromoRoutes admin_promo_routes_typ

type adminPromoCreateReq struct {
	Code            string   `json:"code"`
	DiscountValue   float64  `json:"discount_value"`
	ValidFrom       *string  `json:"valid_from"`
	ValidUntil      *string  `json:"valid_until"`
	UsageLimitTotal *int     `json:"usage_limit_total"`
	IsActive        *bool    `json:"is_active"`
	DiscountType    string   `json:"discount_type"`
	_               struct{} `json:"-"`
}

// List, soft-delete edilmemis promo kodlarini super admin paneli icin dondurur.
// include_deleted=true yalniz audit/geri alma ihtiyaci icin silinenleri de dahil eder.
func (apr admin_promo_routes_typ) List(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int
	payload := []types.Js_object{}

	defer func() {
		if err != nil {
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
			return
		}
		networkutils.SendJson(payload, w)
	}()

	whereDeleted := "WHERE deleted_at IS NULL"
	if strings.EqualFold(r.URL.Query().Get("include_deleted"), "true") {
		whereDeleted = ""
	}

	rows, err := db.Query_all(sqlbuilder.BuildNamed(fmt.Sprintf(`
		SELECT
			uid::text,
			code,
			discount_type,
			discount_value::text,
			valid_from::text,
			COALESCE(valid_until::text, '') AS valid_until,
			COALESCE(usage_limit_total::text, '') AS usage_limit_total,
			usage_count::text,
			is_active::text,
			COALESCE(deactivated_at::text, '') AS deactivated_at,
			COALESCE(deactivated_reason, '') AS deactivated_reason,
			COALESCE(deleted_at::text, '') AS deleted_at,
			created_at::text
		FROM promo_codes
		%s
		ORDER BY created_at DESC
	`, whereDeleted), map[string]interface{}{}))
	if err != nil {
		err = utils.Tag_err("apl1", err)
		return
	}

	for _, row := range rows {
		payload = append(payload, promoRowPayload(row))
	}
}

// Report, promo bazli basarili satis metriklerini purchase snapshotlarindan okur.
// Promo tanimi sonradan degisse veya soft-delete edilse bile rapor odeme anindaki
// gross/discount/net degerlerini korur.
func (apr admin_promo_routes_typ) Report(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int
	payload := []types.Js_object{}

	defer func() {
		if err != nil {
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
			return
		}
		networkutils.SendJson(payload, w)
	}()

	filters := []string{
		"p.provider_id IS NOT NULL",
		"p.provider_id NOT LIKE 'failed:%'",
		"p.promo_code_uid IS NOT NULL",
	}
	args := map[string]interface{}{}

	if rawFrom := strings.TrimSpace(r.URL.Query().Get("from")); rawFrom != "" {
		fromTime, parseErr := parsePromoTime(rawFrom, "from")
		if parseErr != nil {
			err = parseErr
			statCode = http.StatusBadRequest
			return
		}
		args["from"] = fromTime
		filters = append(filters, "p.created_at >= ${from}")
	}

	if rawTo := strings.TrimSpace(r.URL.Query().Get("to")); rawTo != "" {
		toTime, parseErr := parsePromoTime(rawTo, "to")
		if parseErr != nil {
			err = parseErr
			statCode = http.StatusBadRequest
			return
		}
		args["to"] = toTime
		filters = append(filters, "p.created_at <= ${to}")
	}

	rows, err := db.Query_all(sqlbuilder.BuildNamed(fmt.Sprintf(`
		SELECT
			p.promo_code_uid::text,
			COALESCE(p.promo_code_text_snapshot, pc.code, '') AS promo_code,
			COUNT(*)::text AS usage_count,
			COALESCE(SUM(p.gross_total), 0)::text AS gross_total,
			COALESCE(SUM(p.discount_amount), 0)::text AS discount_total,
			COALESCE(SUM(p.net_total), 0)::text AS net_total,
			MIN(p.created_at)::text AS first_used_at,
			MAX(p.created_at)::text AS last_used_at
		FROM purchases p
		LEFT JOIN promo_codes pc ON pc.uid = p.promo_code_uid
		WHERE %s
		GROUP BY p.promo_code_uid, COALESCE(p.promo_code_text_snapshot, pc.code, '')
		ORDER BY usage_count::int DESC, last_used_at DESC
	`, strings.Join(filters, " AND ")), args))
	if err != nil {
		err = utils.Tag_err("apr1", err)
		return
	}

	for _, row := range rows {
		payload = append(payload, types.Js_object{
			"promo_code_uid": string(row[0]),
			"promo_code":     string(row[1]),
			"usage_count":    mustParseInt(row[2]),
			"gross_total":    mustParseFloat(row[3]),
			"discount_total": mustParseFloat(row[4]),
			"net_total":      mustParseFloat(row[5]),
			"first_used_at":  string(row[6]),
			"last_used_at":   string(row[7]),
		})
	}
}

// Create, yeni promo kodunu olusturur. valid_from gonderilmezse DB default'u
// CURRENT_TIMESTAMP basar; boylece admin tarih secmezse kod hemen baslar.
func (apr admin_promo_routes_typ) Create(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int
	var payload types.Js_object

	defer func() {
		if err != nil {
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
			return
		}
		networkutils.SendJson(payload, w)
	}()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		statCode = http.StatusBadRequest
		return
	}

	var req adminPromoCreateReq
	if err = json.Unmarshal(body, &req); err != nil {
		err = errors.New("invalid request body")
		statCode = http.StatusBadRequest
		return
	}

	code := promo.NormalizeCode(req.Code)
	if code == "" {
		err = errors.New("code is required")
		statCode = http.StatusBadRequest
		return
	}
	if req.DiscountType != "" && req.DiscountType != "percent" {
		err = errors.New("discount_type must be percent")
		statCode = http.StatusBadRequest
		return
	}
	if req.DiscountValue <= 0 || req.DiscountValue > 100 {
		err = errors.New("discount_value must be between 0 and 100")
		statCode = http.StatusBadRequest
		return
	}
	if req.UsageLimitTotal != nil && *req.UsageLimitTotal <= 0 {
		err = errors.New("usage_limit_total must be greater than 0")
		statCode = http.StatusBadRequest
		return
	}

	var validFrom interface{}
	if req.ValidFrom != nil && strings.TrimSpace(*req.ValidFrom) != "" {
		validFrom, err = parsePromoTime(*req.ValidFrom, "valid_from")
		if err != nil {
			statCode = http.StatusBadRequest
			return
		}
	}

	var validUntil interface{}
	if req.ValidUntil != nil && strings.TrimSpace(*req.ValidUntil) != "" {
		validUntil, err = parsePromoTime(*req.ValidUntil, "valid_until")
		if err != nil {
			statCode = http.StatusBadRequest
			return
		}
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	var usageLimit interface{}
	if req.UsageLimitTotal != nil {
		usageLimit = *req.UsageLimitTotal
	}

	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("promo_codes")
	cols := []string{"code", "discount_type", "discount_value", "valid_until", "usage_limit_total", "is_active"}
	values := []interface{}{code, "percent", req.DiscountValue, validUntil, usageLimit, isActive}
	if validFrom != nil {
		cols = append(cols, "valid_from")
		values = append(values, validFrom)
	}
	if !isActive {
		cols = append(cols, "deactivated_at", "deactivated_reason")
		values = append(values, sqlbuilder.Raw("NOW()"), "manual")
	}
	ib.Cols(cols...)
	ib.Values(values...)
	ib.SQL("RETURNING " + adminPromoReturningColumns())

	rows, err := db.Query_all(ib)
	if err != nil {
		err = utils.Tag_err("apc1", err)
		statCode = http.StatusConflict
		return
	}
	if len(rows) == 0 {
		err = errors.New("promo could not be created")
		return
	}
	payload = promoRowPayload(rows[0])
}

// Update, promo temel alanlarini kismi gunceller. valid_until ve usage_limit_total
// JSON null ile temizlenebilir; valid_from zorunlu oldugu icin null kabul edilmez.
func (apr admin_promo_routes_typ) Update(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int
	var payload types.Js_object

	defer func() {
		if err != nil {
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
			return
		}
		networkutils.SendJson(payload, w)
	}()

	promoUID := strings.TrimSpace(mux.Vars(r)["promoUID"])
	if promoUID == "" {
		err = errors.New("promo uid is required")
		statCode = http.StatusBadRequest
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		statCode = http.StatusBadRequest
		return
	}

	var raw map[string]json.RawMessage
	if err = json.Unmarshal(body, &raw); err != nil {
		err = errors.New("invalid request body")
		statCode = http.StatusBadRequest
		return
	}

	assignments := []string{}
	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("promo_codes")

	if rawCode, ok := raw["code"]; ok {
		var codeValue string
		if err = json.Unmarshal(rawCode, &codeValue); err != nil {
			err = errors.New("code must be a string")
			statCode = http.StatusBadRequest
			return
		}
		code := promo.NormalizeCode(codeValue)
		if code == "" {
			err = errors.New("code is required")
			statCode = http.StatusBadRequest
			return
		}
		assignments = append(assignments, ub.Assign("code", code))
	}

	if rawDiscountType, ok := raw["discount_type"]; ok {
		var discountType string
		if err = json.Unmarshal(rawDiscountType, &discountType); err != nil || discountType != "percent" {
			err = errors.New("discount_type must be percent")
			statCode = http.StatusBadRequest
			return
		}
		assignments = append(assignments, ub.Assign("discount_type", "percent"))
	}

	if rawDiscountValue, ok := raw["discount_value"]; ok {
		var discountValue float64
		if err = json.Unmarshal(rawDiscountValue, &discountValue); err != nil {
			err = errors.New("discount_value must be a number")
			statCode = http.StatusBadRequest
			return
		}
		if discountValue <= 0 || discountValue > 100 {
			err = errors.New("discount_value must be between 0 and 100")
			statCode = http.StatusBadRequest
			return
		}
		assignments = append(assignments, ub.Assign("discount_value", discountValue))
	}

	if rawValidFrom, ok := raw["valid_from"]; ok {
		if isJSONNull(rawValidFrom) {
			err = errors.New("valid_from cannot be null")
			statCode = http.StatusBadRequest
			return
		}
		var validFromText string
		if err = json.Unmarshal(rawValidFrom, &validFromText); err != nil {
			err = errors.New("valid_from must be a timestamp string")
			statCode = http.StatusBadRequest
			return
		}
		validFrom, parseErr := parsePromoTime(validFromText, "valid_from")
		if parseErr != nil {
			err = parseErr
			statCode = http.StatusBadRequest
			return
		}
		assignments = append(assignments, ub.Assign("valid_from", validFrom))
	}

	if rawValidUntil, ok := raw["valid_until"]; ok {
		if isJSONNull(rawValidUntil) {
			assignments = append(assignments, ub.Assign("valid_until", sqlbuilder.Raw("NULL")))
		} else {
			var validUntilText string
			if err = json.Unmarshal(rawValidUntil, &validUntilText); err != nil {
				err = errors.New("valid_until must be a timestamp string or null")
				statCode = http.StatusBadRequest
				return
			}
			validUntil, parseErr := parsePromoTime(validUntilText, "valid_until")
			if parseErr != nil {
				err = parseErr
				statCode = http.StatusBadRequest
				return
			}
			assignments = append(assignments, ub.Assign("valid_until", validUntil))
		}
	}

	if rawUsageLimit, ok := raw["usage_limit_total"]; ok {
		if isJSONNull(rawUsageLimit) {
			assignments = append(assignments, ub.Assign("usage_limit_total", sqlbuilder.Raw("NULL")))
		} else {
			var usageLimit int
			if err = json.Unmarshal(rawUsageLimit, &usageLimit); err != nil {
				err = errors.New("usage_limit_total must be a number or null")
				statCode = http.StatusBadRequest
				return
			}
			if usageLimit <= 0 {
				err = errors.New("usage_limit_total must be greater than 0")
				statCode = http.StatusBadRequest
				return
			}
			assignments = append(assignments, ub.Assign("usage_limit_total", usageLimit))
		}
	}

	if len(assignments) == 0 {
		err = errors.New("no fields to update")
		statCode = http.StatusBadRequest
		return
	}

	ub.Set(assignments...)
	ub.Where(ub.Equal("uid", promoUID), ub.IsNull("deleted_at"))
	ub.SQL("RETURNING " + adminPromoReturningColumns())
	rows, err := db.Query_all(ub)
	if err != nil {
		err = utils.Tag_err("apu1", err)
		return
	}
	if len(rows) == 0 {
		err = errors.New("promo not found")
		statCode = http.StatusNotFound
		return
	}
	payload = promoRowPayload(rows[0])
}

// Enable, manuel kapatilmis uygun bir kodu tekrar aktif eder. Suresi gecmis
// veya toplam limiti dolmus kodlar once tarih/limit guncellenmeden aktiflenmez.
func (apr admin_promo_routes_typ) Enable(w http.ResponseWriter, r *http.Request) {
	apr.setActiveState(w, r, true)
}

// Disable, super adminin kodu manuel olarak pasife almasini saglar.
func (apr admin_promo_routes_typ) Disable(w http.ResponseWriter, r *http.Request) {
	apr.setActiveState(w, r, false)
}

// Delete, promo kodunu fiziksel silmez; purchase raporlari eski referansi
// okuyabilsin diye soft delete yapar.
func (apr admin_promo_routes_typ) Delete(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int
	var payload types.Js_object

	defer func() {
		if err != nil {
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
			return
		}
		networkutils.SendJson(payload, w)
	}()

	promoUID := strings.TrimSpace(mux.Vars(r)["promoUID"])
	if promoUID == "" {
		err = errors.New("promo uid is required")
		statCode = http.StatusBadRequest
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("promo_codes").Set(
		ub.Assign("is_active", false),
		ub.Assign("deactivated_at", sqlbuilder.Raw("NOW()")),
		ub.Assign("deactivated_reason", "deleted"),
		ub.Assign("deleted_at", sqlbuilder.Raw("NOW()")),
	).Where(
		ub.Equal("uid", promoUID),
		ub.IsNull("deleted_at"),
	)
	ub.SQL("RETURNING " + adminPromoReturningColumns())
	rows, err := db.Query_all(ub)
	if err != nil {
		err = utils.Tag_err("apd1", err)
		return
	}
	if len(rows) == 0 {
		err = errors.New("promo not found")
		statCode = http.StatusNotFound
		return
	}
	payload = promoRowPayload(rows[0])
}

func (apr admin_promo_routes_typ) setActiveState(w http.ResponseWriter, r *http.Request, active bool) {
	var err error
	var statCode int
	var payload types.Js_object

	defer func() {
		if err != nil {
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
			return
		}
		networkutils.SendJson(payload, w)
	}()

	promoUID := strings.TrimSpace(mux.Vars(r)["promoUID"])
	if promoUID == "" {
		err = errors.New("promo uid is required")
		statCode = http.StatusBadRequest
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("promo_codes")
	if active {
		ub.Set(
			ub.Assign("is_active", true),
			ub.Assign("deactivated_at", sqlbuilder.Raw("NULL")),
			ub.Assign("deactivated_reason", sqlbuilder.Raw("NULL")),
		)
		ub.Where(
			ub.Equal("uid", promoUID),
			ub.IsNull("deleted_at"),
			"(valid_until IS NULL OR valid_until >= NOW())",
			"(usage_limit_total IS NULL OR usage_count < usage_limit_total)",
		)
	} else {
		ub.Set(
			ub.Assign("is_active", false),
			ub.Assign("deactivated_at", sqlbuilder.Raw("NOW()")),
			ub.Assign("deactivated_reason", "manual"),
		)
		ub.Where(ub.Equal("uid", promoUID), ub.IsNull("deleted_at"))
	}
	ub.SQL("RETURNING " + adminPromoReturningColumns())

	rows, err := db.Query_all(ub)
	if err != nil {
		err = utils.Tag_err("aps1", err)
		return
	}
	if len(rows) == 0 {
		if active {
			err = errors.New("promo cannot be enabled; update date or usage limit first")
			statCode = http.StatusConflict
		} else {
			err = errors.New("promo not found")
			statCode = http.StatusNotFound
		}
		return
	}
	payload = promoRowPayload(rows[0])
	if returnedUID, ok := payload["uid"].(string); !ok || !strings.EqualFold(returnedUID, promoUID) {
		err = errors.New("promo state response uid mismatch")
		statCode = http.StatusInternalServerError
		return
	}
}

func adminPromoReturningColumns() string {
	return `
		uid::text,
		code,
		discount_type,
		discount_value::text,
		valid_from::text,
		COALESCE(valid_until::text, '') AS valid_until,
		COALESCE(usage_limit_total::text, '') AS usage_limit_total,
		usage_count::text,
		is_active::text,
		COALESCE(deactivated_at::text, '') AS deactivated_at,
		COALESCE(deactivated_reason, '') AS deactivated_reason,
		COALESCE(deleted_at::text, '') AS deleted_at,
		created_at::text
	`
}

func promoRowPayload(row [][]byte) types.Js_object {
	return types.Js_object{
		"uid":                string(row[0]),
		"code":               string(row[1]),
		"discount_type":      string(row[2]),
		"discount_value":     mustParseFloat(row[3]),
		"valid_from":         string(row[4]),
		"valid_until":        emptyStringAsNil(row[5]),
		"usage_limit_total":  emptyStringAsNilInt(row[6]),
		"usage_count":        mustParseInt(row[7]),
		"is_active":          parseTextBool(row[8]),
		"deactivated_at":     emptyStringAsNil(row[9]),
		"deactivated_reason": emptyStringAsNil(row[10]),
		"deleted_at":         emptyStringAsNil(row[11]),
		"created_at":         string(row[12]),
	}
}

func parsePromoTime(raw string, fieldName string) (time.Time, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return time.Time{}, fmt.Errorf("%s is required", fieldName)
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be RFC3339 timestamp", fieldName)
	}
	return parsed, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return strings.EqualFold(strings.TrimSpace(string(raw)), "null")
}

func emptyStringAsNil(value []byte) interface{} {
	if string(value) == "" {
		return nil
	}
	return string(value)
}

func emptyStringAsNilInt(value []byte) interface{} {
	if string(value) == "" {
		return nil
	}
	return mustParseInt(value)
}

func mustParseFloat(value []byte) float64 {
	parsed, _ := strconv.ParseFloat(string(value), 64)
	return parsed
}

func mustParseInt(value []byte) int {
	parsed, _ := strconv.Atoi(string(value))
	return parsed
}

func parseTextBool(value []byte) bool {
	return string(value) == "true" || string(value) == "t"
}
