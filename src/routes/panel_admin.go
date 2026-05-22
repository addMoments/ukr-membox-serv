package routes

import (
	"encoding/json"
	"errors"
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

	"github.com/gorilla/mux"
	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

type panel_admin_routes_typ struct{}

var PanelAdminRoutes panel_admin_routes_typ

type panelAdminCreateReq struct {
	Email           string `json:"email"`
	Name            string `json:"name"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	Role            string `json:"role"`
}

// List, super admin panelinde DB tabanli site adminlerini listeler.
// Mevcut env.admin_emails super adminleri burada zorunlu olarak listelenmez; bu endpoint DB rollerini yonetir.
func (par panel_admin_routes_typ) List(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int
	var payload []types.Js_object

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

	bldr := sqlbuilder.BuildNamed(`
		SELECT
			pa.user_uid::text,
			u.mail,
			COALESCE(u.name, '') AS name,
			pa.role,
			pa.created_at::text,
			COALESCE(pa.created_by_uid::text, '') AS created_by_uid,
			COALESCE(created_by.mail, '') AS created_by_email
		FROM panel_admins pa
		JOIN users u ON u.uid = pa.user_uid
		LEFT JOIN users created_by ON created_by.uid = pa.created_by_uid
		WHERE pa.deleted_at IS NULL
		ORDER BY pa.created_at DESC
	`, map[string]interface{}{})

	rows, err := db.Query_all(bldr)
	if err != nil {
		err = utils.Tag_err("pal1", err)
		return
	}

	payload = []types.Js_object{}
	for _, row := range rows {
		payload = append(payload, types.Js_object{
			"user_uid":         string(row[0]),
			"email":            string(row[1]),
			"name":             string(row[2]),
			"role":             string(row[3]),
			"created_at":       string(row[4]),
			"created_by_uid":   string(row[5]),
			"created_by_email": string(row[6]),
		})
	}
	statCode = http.StatusOK
}

// Create, super admin tarafindan site order admin yetkisi verir.
// Mevcut kullanicinin aktif eventi varsa credential korunur; event yoksa girilen sifre panel hesabi icin guncellenir.
func (par panel_admin_routes_typ) Create(w http.ResponseWriter, r *http.Request) {
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

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		statCode = http.StatusUnauthorized
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = utils.Tag_err("pac1", err)
		statCode = http.StatusBadRequest
		return
	}

	var req panelAdminCreateReq
	if err = json.Unmarshal(body, &req); err != nil {
		err = errors.New("invalid request body")
		statCode = http.StatusBadRequest
		return
	}

	email := strings.TrimSpace(req.Email)
	name := strings.TrimSpace(req.Name)
	password := strings.TrimSpace(req.Password)
	confirmPassword := strings.TrimSpace(req.ConfirmPassword)
	role := strings.TrimSpace(req.Role)
	if role == "" {
		role = auth.PanelRoleOrderAdmin
	}
	if email == "" {
		err = errors.New("email is required")
		statCode = http.StatusBadRequest
		return
	}
	if name == "" {
		err = errors.New("name is required")
		statCode = http.StatusBadRequest
		return
	}
	if role != auth.PanelRoleOrderAdmin {
		err = errors.New("only order_admin can be created from this endpoint")
		statCode = http.StatusBadRequest
		return
	}

	userLookupBldr := sqlbuilder.BuildNamed(`
		SELECT uid::text, COALESCE(name, '') AS name
		FROM users
		WHERE LOWER(mail) = LOWER(${email})
		LIMIT 1
	`, map[string]interface{}{"email": email})

	userUID := ""
	payloadName := name
	existingUserRow, lookupErr := db.Query_one(userLookupBldr)
	if lookupErr != nil && lookupErr.Error() != "empty row" {
		err = utils.Tag_err("pac2", lookupErr)
		return
	}
	isExistingUser := lookupErr == nil
	if isExistingUser {
		userUID = string(existingUserRow[0])
		payloadName = string(existingUserRow[1])
	} else {
		// Yeni hesaplarda sifre ilk credential olur.
		if password == "" || confirmPassword == "" {
			err = errors.New("password and confirm_password are required")
			statCode = http.StatusBadRequest
			return
		}
		if password != confirmPassword {
			err = errors.New("passwords do not match")
			statCode = http.StatusBadRequest
			return
		}

		hashedPassword, hashErr := mycrypto.HashPassword(password)
		if hashErr != nil {
			err = utils.Tag_err("pac3", hashErr)
			return
		}

		userUID, err = dbscripts.CreateUser(name, email, "password", hashedPassword, true)
		if err != nil {
			err = utils.Tag_err("pac4", err)
			statCode = http.StatusConflict
			return
		}
	}

	activeAdminBldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*)
		FROM panel_admins
		WHERE user_uid = ${user_uid}
		  AND deleted_at IS NULL
	`, map[string]interface{}{"user_uid": userUID})

	activeAdminRow, err := db.Query_one(activeAdminBldr)
	if err != nil {
		err = utils.Tag_err("pac4.1", err)
		return
	}
	if string(activeAdminRow[0]) != "0" {
		err = errors.New("user already exists")
		statCode = http.StatusConflict
		return
	}

	if isExistingUser {
		hasActiveEventBldr := sqlbuilder.BuildNamed(`
			SELECT COUNT(*)
			FROM events
			WHERE ${user_uid}::uuid = ANY(admins)
			  AND deleted_at IS NULL
		`, map[string]interface{}{"user_uid": userUID})

		hasActiveEventRow, eventErr := db.Query_one(hasActiveEventBldr)
		if eventErr != nil {
			err = utils.Tag_err("pac4.2", eventErr)
			return
		}

		if string(hasActiveEventRow[0]) == "0" {
			// Eventsiz mevcut kullanici panel hesabi gibi davranir; super adminin girdigi sifre gecerlidir.
			if password == "" || confirmPassword == "" {
				err = errors.New("password and confirm_password are required")
				statCode = http.StatusBadRequest
				return
			}
			if password != confirmPassword {
				err = errors.New("passwords do not match")
				statCode = http.StatusBadRequest
				return
			}

			hashedPassword, hashErr := mycrypto.HashPassword(password)
			if hashErr != nil {
				err = utils.Tag_err("pac4.3", hashErr)
				return
			}

			updateCredentialBldr := sqlbuilder.BuildNamed(`
				UPDATE credentials
				SET value = ${credential_value}
				WHERE user_uid = ${user_uid}
				  AND type = 'password'
				RETURNING user_uid::text
			`, map[string]interface{}{
				"user_uid":          userUID,
				"credential_value": hashedPassword,
			})
			_, credentialErr := db.Query_one(updateCredentialBldr)
			if credentialErr != nil && credentialErr.Error() != "empty row" {
				err = utils.Tag_err("pac4.4", credentialErr)
				return
			}
			if credentialErr != nil {
				insertCredentialBldr := sqlbuilder.BuildNamed(`
					INSERT INTO credentials (user_uid, type, value)
					VALUES (${user_uid}, 'password', ${credential_value})
					RETURNING user_uid::text
				`, map[string]interface{}{
					"user_uid":          userUID,
					"credential_value": hashedPassword,
				})
				if _, err = db.Query_one(insertCredentialBldr); err != nil {
					err = utils.Tag_err("pac4.5", err)
					return
				}
			}
		}
	}

	insertBldr := sqlbuilder.BuildNamed(`
		INSERT INTO panel_admins (user_uid, role, created_by_uid)
		VALUES (${user_uid}, ${role}, ${created_by_uid})
		ON CONFLICT (user_uid) DO UPDATE
			SET role = EXCLUDED.role,
				created_by_uid = EXCLUDED.created_by_uid,
				deleted_at = NULL,
				deleted_by_uid = NULL
		RETURNING created_at::text
	`, map[string]interface{}{
		"user_uid":       userUID,
		"role":           role,
		"created_by_uid": claims.UserUID,
	})

	createdRow, err := db.Query_one(insertBldr)
	if err != nil {
		err = utils.Tag_err("pac5", err)
		return
	}

	payload = types.Js_object{
		"user_uid":   userUID,
		"email":      email,
		"name":       payloadName,
		"role":       role,
		"created_at": string(createdRow[0]),
	}
	statCode = http.StatusOK
}

// Delete, DB tabanli panel admin yetkisini revoke eder.
// Satir silinmez; deleted_at gecmisi login yonlendirme kararinda kullanilir.
// Env.admin_emails icindeki super adminler DB'de tutulmadigi icin bu endpoint onlari silemez.
func (par panel_admin_routes_typ) Delete(w http.ResponseWriter, r *http.Request) {
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

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		statCode = http.StatusUnauthorized
		return
	}

	userUID := mux.Vars(r)["userUID"]
	if strings.TrimSpace(userUID) == "" {
		err = errors.New("user uid is required")
		statCode = http.StatusBadRequest
		return
	}

	deleteBldr := sqlbuilder.BuildNamed(`
		UPDATE panel_admins
		SET deleted_at = NOW(),
			deleted_by_uid = ${deleted_by_uid}
		WHERE user_uid = ${user_uid}
		  AND deleted_at IS NULL
		RETURNING user_uid::text
	`, map[string]interface{}{
		"user_uid":       userUID,
		"deleted_by_uid": claims.UserUID,
	})

	deletedRow, err := db.Query_one(deleteBldr)
	if err != nil {
		err = errors.New("panel admin not found")
		statCode = http.StatusNotFound
		return
	}

	payload = types.Js_object{
		"deleted":  true,
		"user_uid": string(deletedRow[0]),
	}
	statCode = http.StatusOK
}
