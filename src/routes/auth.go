package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	"membox-serv/src/mycrypto"
	networkutils "membox-serv/src/network_utils"
	"net/http"

	"github.com/huandu/go-sqlbuilder"
)

type signin_routes_typ struct{}

var AuthRoutes signin_routes_typ

func (rfrnc signin_routes_typ) WhoAmI(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		networkutils.SendJson(payload, w)
	})()

	claims := r.Context().Value("claims").(auth.TokenClaims)
	payload = claims
	stat_code = 200

}

type signin_password_req struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (rfrnc signin_routes_typ) SigninPassword(w http.ResponseWriter, r *http.Request) {
	var redr_url string
	var err error
	var stat_code int

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		if stat_code == 0 {
			stat_code = http.StatusCreated
		}

		w.WriteHeader(stat_code)
		w.Write([]byte("goto:" + redr_url))
	})()

	byt, err := io.ReadAll(r.Body)
	if err != nil {
		return
	}

	var req signin_password_req
	err = json.Unmarshal(byt, &req)
	if err != nil {
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("credentials.value", "users.uid")
	sb.From("credentials")
	sb.Join("users", "credentials.user_uid = users.uid")
	sb.Where(
		sb.Equal("users.mail", req.Email),
		sb.Equal("credentials.type", "password"),
	)

	res, err := db.Query_one(sb)
	if err != nil {
		stat_code = 401
		err = errors.New("invalid credentials")
		return
	}

	ok := mycrypto.CheckPassword(string(res[0]), req.Password)
	if !ok {
		err = errors.New("invalid credentials")
		stat_code = 401
		return
	}

	_, _, err = auth.Authorize(w, r, "auth", string(res[1]), "")
	redr_url = "/events"

}

func (rfrnc signin_routes_typ) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		networkutils.SendJson(payload, w)
	})()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok || claims.Role != "auth" {
		err = errors.New("unauthorized")
		stat_code = 401
		return
	}

	err = dbscripts.RemoveUser(claims.UserUID)
	if err != nil {
		return
	}

	payload = map[string]interface{}{
		"success": true,
		"message": "account deleted",
	}
}

type changePasswordBody struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	ConfirmPassword string `json:"confirm_password"`
}

// ChangePassword, giris yapmis kullanicinin parolasini mevcut parolasini dogrulayarak degistirir.
// Musteri 2 Kasim (2.25): parola yalnizca e-postali sifirlama akisiyla degisiyordu. Kurallar o akisla
// ayni (en az 8 karakter, iki kez ayni). Yanlis parola 401 degil 400 doner: istemci 401'i oturum
// sorunu sayiyor. Oturum (token) degismez, kullanici bu cihazda girisli kalir.
func (rfrnc signin_routes_typ) ChangePassword(w http.ResponseWriter, r *http.Request) {
	fail := func(status int, code, message string) {
		_ = networkutils.SendErrorJSON(w, status, code, message)
	}

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok || claims.Role != "auth" {
		fail(http.StatusUnauthorized, "UNAUTHORIZED", "unauthorized")
		return
	}

	var req changePasswordBody
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		fail(http.StatusBadRequest, "BAD_REQUEST", "bad request")
		return
	}
	switch {
	case req.CurrentPassword == "" || req.NewPassword == "" || req.ConfirmPassword == "":
		fail(http.StatusBadRequest, "REQUIRED", "all fields are required")
		return
	case len(req.NewPassword) < 8:
		fail(http.StatusBadRequest, "PASSWORD_TOO_SHORT", "password must be at least 8 characters")
		return
	case req.NewPassword != req.ConfirmPassword:
		fail(http.StatusBadRequest, "PASSWORDS_DO_NOT_MATCH", "passwords do not match")
		return
	case req.NewPassword == req.CurrentPassword:
		fail(http.StatusBadRequest, "SAME_PASSWORD", "the new password is the same as the current one")
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("value").From("credentials").Where(
		sb.Equal("user_uid", claims.UserUID),
		sb.Equal("type", "password"),
	)
	rows, err := db.Query_all(sb)
	if err != nil {
		fmt.Printf("[auth.change_password] select: %v\n", err)
		fail(http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	if len(rows) == 0 || !mycrypto.CheckPassword(string(rows[0][0]), req.CurrentPassword) {
		fail(http.StatusBadRequest, "WRONG_PASSWORD", "the current password is not correct")
		return
	}

	hashed, err := mycrypto.HashPassword(req.NewPassword)
	if err != nil {
		fail(http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("credentials").
		Set(ub.Assign("value", hashed)).
		Where(ub.Equal("user_uid", claims.UserUID), ub.Equal("type", "password"))
	if err = db.Exec(ub); err != nil {
		fmt.Printf("[auth.change_password] update: %v\n", err)
		fail(http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}

	networkutils.SendJson(map[string]interface{}{"success": true}, w)
}
