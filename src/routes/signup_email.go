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
	"membox-serv/src/qr"
	"membox-serv/src/utils"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type signup_email_routes_typ struct{}

var SignupEmailRoutes signup_email_routes_typ

func (r signup_email_routes_typ) Get(w http.ResponseWriter, req *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			networkutils.SendJson(map[string]interface{}{
				"valid": false,
				"error": err.Error(),
			}, w)
			return
		}

		networkutils.SendJson(payload, w)
	})()

	vars := mux.Vars(req)
	token := vars["token"]

	pt, ownerEmail, err := validateSignupToken(token)
	if err != nil {
		return
	}

	fmt.Println(ownerEmail)

	exists, err := auth.EmailExists(ownerEmail)
	if err != nil {
		fmt.Println(err)
		return
	}
	payload = map[string]interface{}{
		"valid":        true,
		"reference_no": pt.ReferenceNo,
		"owner_email":  ownerEmail,
		"email_exists": exists,
	}
}

type signupEmailPostReq struct {
	Email           string `json:"email"`
	Name            string `json:"name"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
	Agreed          bool   `json:"agreed"`
}

func (r signup_email_routes_typ) Post(w http.ResponseWriter, req *http.Request) {
	var redr_url string
	var err error
	var statusCode int

	defer (func() {
		if err != nil {
			if statusCode == 0 {
				statusCode = 500
			}
			http.Error(w, err.Error(), statusCode)
			return
		}

		if statusCode == 0 {
			statusCode = http.StatusCreated
		}

		w.WriteHeader(statusCode)
		w.Write([]byte("goto:" + redr_url))
	})()

	// 1. Extract token from URL
	vars := mux.Vars(req)
	token := vars["token"]

	// 2. Validate signup token
	pt, ownerEmail, err := validateSignupToken(token)
	if err != nil {
		statusCode = 400
		return
	}

	exists, err := auth.EmailExists(ownerEmail)
	if err != nil {
		statusCode = 400
		return
	}
	if exists {
		statusCode = 400
		return
	}

	// 3. Parse JSON body
	body, err := io.ReadAll(req.Body)
	if err != nil {
		statusCode = 400
		return
	}

	var postReq signupEmailPostReq
	err = json.Unmarshal(body, &postReq)
	if err != nil {
		statusCode = 400
		return
	}

	// 4. Validate fields
	if postReq.Email != ownerEmail {
		statusCode = 400
		err = errors.New("email does not match the owner email")
		return
	}

	if !postReq.Agreed {
		statusCode = 400
		err = errors.New("you must agree to the terms and conditions")
		return
	}

	if postReq.Password != postReq.ConfirmPassword {
		statusCode = 400
		err = errors.New("passwords do not match")
		return
	}

	if postReq.Email == "" || postReq.Name == "" || postReq.Password == "" {
		statusCode = 400
		err = errors.New("email, name, and password are required")
		return
	}

	// 5. Hash password
	hashedPassword, err := mycrypto.HashPassword(postReq.Password)
	if err != nil {
		return
	}

	userUid, err := dbscripts.CreateUser(postReq.Name, postReq.Email, "password", hashedPassword, true)
	if err != nil {
		statusCode = 409 // Conflict - likely duplicate email
		return
	}

	if strings.HasPrefix(pt.Status, "c:") {
		// collaborator invite link.
		eventUid, err := utils.UUID.UnpackUUID(pt.ReferenceNo)
		if err != nil {
			return
		}

		ub := sqlbuilder.NewUpdateBuilder()
		ub.Update("events").Set(
			ub.Assign("admins", sqlbuilder.Raw(fmt.Sprintf("array_append(admins, '%s'::uuid)", userUid))),
		).Where(ub.Equal("uid", eventUid))
		err = db.Exec(ub)
		if err != nil {
			err = errors.New("failed to update event admins")
			return
		}

		_, _, err = auth.Authorize(w, req, "auth", userUid, "")
		if err != nil {
			return
		}

		redr_url = "/events"
		return
	}

	unpackedPurchaseUID, err := utils.UUID.UnpackUUID(pt.ReferenceNo)
	if err != nil {
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("purchases").Set(
		ub.Assign("buyer_uid", userUid),
	).Where(ub.Equal("uid", unpackedPurchaseUID))
	err = db.Exec(ub)
	if err != nil {
		return
	}

	// activation_date bilerek bos birakilir: host dugun tarihine gore kendisi secer.
	// NULL kalinca trigger active_until/storage_until'i de hesaplamaz, misafir kapisi
	// da kapali kalir (tarih yoksa "etkinlik henuz baslamadi" gosterilir).
	status := "paid"

	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("events")
	ib.Cols("admins", "purchase_uid", "status")
	ib.Values(
		fmt.Sprintf(`{%s}`, userUid),
		unpackedPurchaseUID,
		status,
	)
	ib.SQL("RETURNING uid")

	res, err := db.Query_one(ib)
	if err != nil {
		fmt.Println("error inserting event", err)
		return
	}

	eventUID := string(res[0])
	fmt.Println("eventUID", eventUID)

	// 8. Authorize user
	_, _, err = auth.Authorize(w, req, "auth", userUid, "")
	if err != nil {
		return
	}

	packedEventUID, err := utils.UUID.PackUUID(eventUID)
	if err != nil {
		return
	}

	qr.UpdateEventQR(packedEventUID)

	redr_url = "/events"
}

type attachEmailPostReq struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (r signup_email_routes_typ) Attach(w http.ResponseWriter, req *http.Request) {
	var redr_url string
	var err error
	var statusCode int

	defer (func() {
		if err != nil {
			if statusCode == 0 {
				statusCode = 500
			}
			http.Error(w, err.Error(), statusCode)
			return
		}
		if statusCode == 0 {
			statusCode = http.StatusCreated
		}
		w.WriteHeader(statusCode)
		w.Write([]byte("goto:" + redr_url))
	})()

	vars := mux.Vars(req)
	token := vars["token"]
	pt, ownerEmail, err := validateSignupToken(token)
	if err != nil {
		statusCode = 400
		return
	}

	body, err := io.ReadAll(req.Body)
	if err != nil {
		statusCode = 400
		return
	}
	var postReq attachEmailPostReq
	err = json.Unmarshal(body, &postReq)
	if err != nil {
		statusCode = 400
		return
	}

	if postReq.Email != ownerEmail {
		statusCode = 400
		err = errors.New("email does not match token")
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("credentials.value", "users.uid")
	sb.From("credentials")
	sb.Join("users", "credentials.user_uid = users.uid")
	sb.Where(
		sb.Equal("users.mail", postReq.Email),
		sb.Equal("credentials.type", "password"),
	)
	res, err := db.Query_one(sb)
	if err != nil {
		statusCode = 401
		err = errors.New("invalid credentials")
		return
	}

	ok := mycrypto.CheckPassword(string(res[0]), postReq.Password)
	if !ok {
		statusCode = 401
		err = errors.New("invalid credentials")
		return
	}
	userUid := string(res[1])

	if strings.HasPrefix(pt.Status, "c:") {
		eventUid, err := utils.UUID.UnpackUUID(pt.ReferenceNo)
		if err != nil {
			return
		}

		ub := sqlbuilder.NewUpdateBuilder()
		ub.Update("events").Set(
			ub.Assign("admins", sqlbuilder.Raw(fmt.Sprintf("array_append(admins, '%s'::uuid)", userUid))),
		).Where(
			ub.Equal("uid", eventUid),
			fmt.Sprintf("NOT ('%s'::uuid = ANY(admins))", userUid),
		)
		err = db.Exec(ub)
		if err != nil {
			err = errors.New("failed to update event admins")
			return
		}

		_, _, err = auth.Authorize(w, req, "auth", userUid, "")
		if err != nil {
			return
		}
		redr_url = "/events"
		return
	}

	unpackedPurchaseUID, err := utils.UUID.UnpackUUID(pt.ReferenceNo)
	if err != nil {
		return
	}

	checkSB := sqlbuilder.NewSelectBuilder()
	checkSB.Select("uid").From("events").Where(
		checkSB.Equal("purchase_uid", unpackedPurchaseUID),
		checkSB.IsNull("deleted_at"),
	)
	existingRes, checkErr := db.Query_one(checkSB)
	if checkErr == nil && len(existingRes) > 0 && string(existingRes[0]) != "" {
		_, _, err = auth.Authorize(w, req, "auth", userUid, "")
		if err != nil {
			return
		}
		redr_url = "/events"
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("purchases").Set(ub.Assign("buyer_uid", userUid)).Where(ub.Equal("uid", unpackedPurchaseUID))
	err = db.Exec(ub)
	if err != nil {
		return
	}

	// Bkz. Post: activation_date bilerek bos birakilir, host kendisi secer.
	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("events")
	ib.Cols("admins", "purchase_uid", "status")
	ib.Values(fmt.Sprintf(`{%s}`, userUid), unpackedPurchaseUID, "paid")
	ib.SQL("RETURNING uid")

	eventRes, err := db.Query_one(ib)
	if err != nil {
		return
	}

	_, _, err = auth.Authorize(w, req, "auth", userUid, "")
	if err != nil {
		return
	}

	packedEventUID, err := utils.UUID.PackUUID(string(eventRes[0]))
	if err != nil {
		return
	}
	qr.UpdateEventQR(packedEventUID)

	redr_url = "/events"
}

// validateSignupToken decrypts and validates a signup token.
// The token's Status field must be in "m:<owneremail>" format (ready to consume).
// Returns the decrypted PaymentToken and owner email if valid, or an error if invalid.
func validateSignupToken(encToken string) (pt payments.PaymentToken, ownerEmail string, err error) {
	// Decrypt the token using PaymentSecret
	err = pt.Decrypt(env.Env().PaymentSecret, encToken)
	if err != nil {
		return pt, "", errors.New("invalid token: decryption failed")
	}

	if pt.Provider != "admt_payment" {
		return pt, "", errors.New("invalid token: provider not supported")
	}

	// Parse Status: must be "m:<owneremail>" format
	if !(strings.HasPrefix(pt.Status, "m:") || strings.HasPrefix(pt.Status, "c:")) {
		return pt, "", errors.New("invalid token: status format invalid")
	}

	// Extract prefix (m: or c:)
	prefix := strings.Split(pt.Status, ":")[0]
	ownerEmail = strings.TrimPrefix(pt.Status, prefix+":")
	if ownerEmail == "" {
		return pt, "", errors.New("invalid token: owner email missing")
	}

	return pt, ownerEmail, nil
}
