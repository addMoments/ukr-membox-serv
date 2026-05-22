package routes

import (
	"encoding/json"
	"errors"
	"io"
	db "membox-serv/src/db_layer"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type admin_partnership_routes_typ struct{}

var AdminPartnershipRoutes admin_partnership_routes_typ

type adminPartnershipCreateReq struct {
	Name        string   `json:"name"`
	Surname     string   `json:"surname"`
	CompanyName *string  `json:"company_name"`
	Phone       *string  `json:"phone"`
	Email       *string  `json:"email"`
	IsActive    *bool    `json:"is_active"`
	_           struct{} `json:"-"`
}

// List, soft-delete edilmemis partnership kayitlarini admin paneli icin dondurur.
func (apr admin_partnership_routes_typ) List(w http.ResponseWriter, r *http.Request) {
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

	rows, err := db.Query_all(sqlbuilder.BuildNamed(`
		SELECT
			`+adminPartnershipSelectColumns()+`
		FROM partnerships
		WHERE deleted_at IS NULL
		ORDER BY created_at DESC
	`, map[string]interface{}{}))
	if err != nil {
		err = utils.Tag_err("apl1", err)
		return
	}

	for _, row := range rows {
		payload = append(payload, partnershipRowPayload(row))
	}
}

// Get, tek bir aktif partnership kaydini admin detay ekrani icin dondurur.
func (apr admin_partnership_routes_typ) Get(w http.ResponseWriter, r *http.Request) {
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

	partnershipUID := strings.TrimSpace(mux.Vars(r)["partnershipUID"])
	if partnershipUID == "" {
		err = errors.New("partnership uid is required")
		statCode = http.StatusBadRequest
		return
	}

	row, err := db.Query_one(sqlbuilder.BuildNamed(`
		SELECT
			`+adminPartnershipSelectColumns()+`
		FROM partnerships
		WHERE uid = ${partnership_uid}
		  AND deleted_at IS NULL
		LIMIT 1
	`, map[string]interface{}{"partnership_uid": partnershipUID}))
	if err != nil {
		err = errors.New("partnership not found")
		statCode = http.StatusNotFound
		return
	}

	payload = partnershipRowPayload(row)
}

// Create, zorunlu kisi bilgileriyle yeni bir partnership kaydi olusturur.
func (apr admin_partnership_routes_typ) Create(w http.ResponseWriter, r *http.Request) {
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

	var req adminPartnershipCreateReq
	if err = json.Unmarshal(body, &req); err != nil {
		err = errors.New("invalid request body")
		statCode = http.StatusBadRequest
		return
	}

	name := strings.TrimSpace(req.Name)
	surname := strings.TrimSpace(req.Surname)
	if name == "" {
		err = errors.New("name is required")
		statCode = http.StatusBadRequest
		return
	}
	if surname == "" {
		err = errors.New("surname is required")
		statCode = http.StatusBadRequest
		return
	}

	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("partnerships")
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}
	ib.Cols("name", "surname", "company_name", "phone", "email", "is_active")
	ib.Values(
		name,
		surname,
		optionalTextValue(req.CompanyName),
		optionalTextValue(req.Phone),
		optionalTextValue(req.Email),
		isActive,
	)
	ib.SQL("RETURNING " + adminPartnershipReturningColumns())

	rows, err := db.Query_all(ib)
	if err != nil {
		err = utils.Tag_err("apc1", err)
		return
	}
	if len(rows) == 0 {
		err = errors.New("partnership could not be created")
		return
	}

	payload = partnershipRowPayload(rows[0])
}

// Update, partnership alanlarini kismi gunceller; name ve surname bos birakilamaz.
func (apr admin_partnership_routes_typ) Update(w http.ResponseWriter, r *http.Request) {
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

	partnershipUID := strings.TrimSpace(mux.Vars(r)["partnershipUID"])
	if partnershipUID == "" {
		err = errors.New("partnership uid is required")
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

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("partnerships")
	assignments := []string{}

	if rawName, ok := raw["name"]; ok {
		name, parseErr := requiredTextFromRaw(rawName, "name")
		if parseErr != nil {
			err = parseErr
			statCode = http.StatusBadRequest
			return
		}
		assignments = append(assignments, ub.Assign("name", name))
	}

	if rawSurname, ok := raw["surname"]; ok {
		surname, parseErr := requiredTextFromRaw(rawSurname, "surname")
		if parseErr != nil {
			err = parseErr
			statCode = http.StatusBadRequest
			return
		}
		assignments = append(assignments, ub.Assign("surname", surname))
	}

	for _, field := range []string{"company_name", "phone", "email"} {
		if rawValue, ok := raw[field]; ok {
			value, parseErr := optionalTextFromRaw(rawValue, field)
			if parseErr != nil {
				err = parseErr
				statCode = http.StatusBadRequest
				return
			}
			assignments = append(assignments, ub.Assign(field, value))
		}
	}

	if rawIsActive, ok := raw["is_active"]; ok {
		var isActive bool
		if err = json.Unmarshal(rawIsActive, &isActive); err != nil {
			err = errors.New("is_active must be a boolean")
			statCode = http.StatusBadRequest
			return
		}
		assignments = append(assignments, ub.Assign("is_active", isActive))
	}

	if len(assignments) == 0 {
		err = errors.New("no fields to update")
		statCode = http.StatusBadRequest
		return
	}
	assignments = append(assignments, ub.Assign("updated_at", sqlbuilder.Raw("NOW()")))

	ub.Set(assignments...)
	ub.Where(ub.Equal("uid", partnershipUID), ub.IsNull("deleted_at"))
	ub.SQL("RETURNING " + adminPartnershipReturningColumns())

	rows, err := db.Query_all(ub)
	if err != nil {
		err = utils.Tag_err("apu1", err)
		return
	}
	if len(rows) == 0 {
		err = errors.New("partnership not found")
		statCode = http.StatusNotFound
		return
	}

	payload = partnershipRowPayload(rows[0])
}

// Delete, partnership kaydini silmek yerine pasife alir; eski rapor referanslari korunur.
func (apr admin_partnership_routes_typ) Delete(w http.ResponseWriter, r *http.Request) {
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

	partnershipUID := strings.TrimSpace(mux.Vars(r)["partnershipUID"])
	if partnershipUID == "" {
		err = errors.New("partnership uid is required")
		statCode = http.StatusBadRequest
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("partnerships").Set(
		ub.Assign("updated_at", sqlbuilder.Raw("NOW()")),
		ub.Assign("is_active", false),
	).Where(
		ub.Equal("uid", partnershipUID),
		ub.IsNull("deleted_at"),
	)
	ub.SQL("RETURNING " + adminPartnershipReturningColumns())

	rows, err := db.Query_all(ub)
	if err != nil {
		err = utils.Tag_err("apd1", err)
		return
	}
	if len(rows) == 0 {
		err = errors.New("partnership not found")
		statCode = http.StatusNotFound
		return
	}

	payload = partnershipRowPayload(rows[0])
}

// adminPartnershipReturningColumns, CRUD cevaplarinda ortak donen temel kolon listesini tek yerde tutar.
func adminPartnershipReturningColumns() string {
	return `
		uid::text,
		name,
		surname,
		COALESCE(company_name, '') AS company_name,
		COALESCE(phone, '') AS phone,
		COALESCE(email, '') AS email,
		is_active::text,
		created_at::text,
		updated_at::text,
		COALESCE(deleted_at::text, '') AS deleted_at
	`
}

// adminPartnershipSelectColumns, liste/detay ekranlari icin temel kolonlara satis metriklerini ekler.
func adminPartnershipSelectColumns() string {
	return adminPartnershipReturningColumns() + `,
		(
			SELECT COUNT(*)::text
			FROM promo_codes pc
			WHERE pc.partnership_uid = partnerships.uid
			  AND pc.deleted_at IS NULL
		) AS promo_count,
		(
			SELECT COUNT(*)::text
			FROM purchases pu
			LEFT JOIN promo_codes pc ON pc.uid = pu.promo_code_uid
			WHERE pu.provider_id IS NOT NULL
			  AND pu.provider_id NOT LIKE 'failed:%'
			  AND COALESCE(pu.promo_partnership_uid, pc.partnership_uid) = partnerships.uid
		) AS usage_count,
		(
			SELECT COALESCE(SUM(pu.gross_total), 0)::text
			FROM purchases pu
			LEFT JOIN promo_codes pc ON pc.uid = pu.promo_code_uid
			WHERE pu.provider_id IS NOT NULL
			  AND pu.provider_id NOT LIKE 'failed:%'
			  AND COALESCE(pu.promo_partnership_uid, pc.partnership_uid) = partnerships.uid
		) AS gross_total,
		(
			SELECT COALESCE(SUM(pu.discount_amount), 0)::text
			FROM purchases pu
			LEFT JOIN promo_codes pc ON pc.uid = pu.promo_code_uid
			WHERE pu.provider_id IS NOT NULL
			  AND pu.provider_id NOT LIKE 'failed:%'
			  AND COALESCE(pu.promo_partnership_uid, pc.partnership_uid) = partnerships.uid
		) AS discount_total,
		(
			SELECT COALESCE(SUM(pu.net_total), 0)::text
			FROM purchases pu
			LEFT JOIN promo_codes pc ON pc.uid = pu.promo_code_uid
			WHERE pu.provider_id IS NOT NULL
			  AND pu.provider_id NOT LIKE 'failed:%'
			  AND COALESCE(pu.promo_partnership_uid, pc.partnership_uid) = partnerships.uid
		) AS net_total,
		COALESCE((
			SELECT MIN(pu.created_at)::text
			FROM purchases pu
			LEFT JOIN promo_codes pc ON pc.uid = pu.promo_code_uid
			WHERE pu.provider_id IS NOT NULL
			  AND pu.provider_id NOT LIKE 'failed:%'
			  AND COALESCE(pu.promo_partnership_uid, pc.partnership_uid) = partnerships.uid
		), '') AS first_used_at,
		COALESCE((
			SELECT MAX(pu.created_at)::text
			FROM purchases pu
			LEFT JOIN promo_codes pc ON pc.uid = pu.promo_code_uid
			WHERE pu.provider_id IS NOT NULL
			  AND pu.provider_id NOT LIKE 'failed:%'
			  AND COALESCE(pu.promo_partnership_uid, pc.partnership_uid) = partnerships.uid
		), '') AS last_used_at
	`
}

// partnershipRowPayload, DB satirini admin panelinin bekledigi JSON sekline cevirir.
func partnershipRowPayload(row [][]byte) types.Js_object {
	payload := types.Js_object{
		"uid":          string(row[0]),
		"name":         string(row[1]),
		"surname":      string(row[2]),
		"company_name": emptyStringAsNil(row[3]),
		"phone":        emptyStringAsNil(row[4]),
		"email":        emptyStringAsNil(row[5]),
		"is_active":    parseTextBool(row[6]),
		"created_at":   string(row[7]),
		"updated_at":   string(row[8]),
		"deleted_at":   emptyStringAsNil(row[9]),
	}
	payload["metrics"] = partnershipMetricsPayload(row)
	return payload
}

// partnershipMetricsPayload, metrik kolonlari yoksa yeni CRUD cevaplari icin sifir deger uretir.
func partnershipMetricsPayload(row [][]byte) types.Js_object {
	if len(row) < 17 {
		return types.Js_object{
			"promo_count":    0,
			"usage_count":    0,
			"gross_total":    0,
			"discount_total": 0,
			"net_total":      0,
			"first_used_at":  nil,
			"last_used_at":   nil,
		}
	}
	return types.Js_object{
		"promo_count":    mustParseInt(row[10]),
		"usage_count":    mustParseInt(row[11]),
		"gross_total":    mustParseFloat(row[12]),
		"discount_total": mustParseFloat(row[13]),
		"net_total":      mustParseFloat(row[14]),
		"first_used_at":  emptyStringAsNil(row[15]),
		"last_used_at":   emptyStringAsNil(row[16]),
	}
}

// optionalTextValue, create istegindeki bos opsiyonel metinleri DB NULL degerine cevirir.
func optionalTextValue(value *string) interface{} {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}

// requiredTextFromRaw, PATCH icin zorunlu metin alanlarini trimleyip dogrular.
func requiredTextFromRaw(raw json.RawMessage, fieldName string) (string, error) {
	if isJSONNull(raw) {
		return "", fmtFieldRequired(fieldName)
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", fmtFieldMustBeString(fieldName)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmtFieldRequired(fieldName)
	}
	return value, nil
}

// optionalTextFromRaw, PATCH icin opsiyonel metinlerde null veya bos string ile temizlemeyi destekler.
func optionalTextFromRaw(raw json.RawMessage, fieldName string) (interface{}, error) {
	if isJSONNull(raw) {
		return sqlbuilder.Raw("NULL"), nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, fmtFieldMustBeString(fieldName)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return sqlbuilder.Raw("NULL"), nil
	}
	return value, nil
}

// fmtFieldRequired, alan bazli zorunluluk hatalarini tutarli formatta uretir.
func fmtFieldRequired(fieldName string) error {
	return errors.New(fieldName + " is required")
}

// fmtFieldMustBeString, alan bazli tip hatalarini tutarli formatta uretir.
func fmtFieldMustBeString(fieldName string) error {
	return errors.New(fieldName + " must be a string")
}
