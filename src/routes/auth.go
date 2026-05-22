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
