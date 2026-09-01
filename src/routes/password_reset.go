package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	"membox-serv/src/env"
	"membox-serv/src/mycrypto"
	sendemail "membox-serv/src/send_email"
	"net/http"
	"time"

	"github.com/huandu/go-sqlbuilder"
)

type password_reset_routes_typ struct{}

var PasswordResetRoutes password_reset_routes_typ

// resetTokenLife, sifre sifirlama linkinin gecerlilik suresi.
// 15 dakikaydi; ukr.net gibi saglayicilarda teslimat gecikince kullanicilar linki
// suresi dolmus buluyor ve tekrar istek atiyordu. Sureyi 60 dakikaya cikardik.
// E-posta metni ve /recover sayfasindaki bilgilendirme bu degerle ayni kalmali.
const resetTokenLife = 60 * time.Minute

type passwordResetRequestBody struct {
	Email string `json:"email"`
}

func (rfrnc password_reset_routes_typ) Request(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var req passwordResetRequestBody
	if err := json.Unmarshal(body, &req); err != nil || req.Email == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	// Look up user — always return 200 to prevent enumeration
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("uid").From("users").Where(sb.Equal("mail", req.Email))
	res, err := db.Query_one(sb)
	if err != nil {
		// User not found — return 200 silently
		w.WriteHeader(http.StatusOK)
		return
	}
	userUID := string(res[0])

	token := mycrypto.Rand_Str(32, "")
	expiresAt := time.Now().Add(resetTokenLife).Format(time.RFC3339)

	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("password_reset_tokens")
	ib.Cols("token", "user_uid", "expires_at")
	ib.Values(token, userUID, expiresAt)
	if err := db.Exec(ib); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	siteRoot := "https://addmoments.com.ua"
	if !env.Is_live() {
		siteRoot = "http://localhost:3000"
	}
	resetLink := siteRoot + "/reset-password/" + token

	go func() {
		mailErr := sendemail.Info_mail.Send([]string{req.Email}, "Reset your Add Moments password", func(w io.WriteCloser) {
			sendemail.Write_html(w, "Reset your password", []string{
				"We received a request to reset your password. Click the button below to choose a new one. This link expires in 60 minutes.",
				sendemail.Button(resetLink, "Reset Password"),
				"If you didn't request this, you can safely ignore this email. Your password won't change.",
				"If the button doesn't work, copy and paste this link into your browser:<br>" + resetLink,
			})
		}, nil)
		if mailErr != nil {
			fmt.Printf("[password_reset] ERROR: failed to send reset email to=%s err=%v\n", req.Email, mailErr)
		}
	}()

	w.WriteHeader(http.StatusOK)
}

type passwordResetConfirmBody struct {
	Token           string `json:"token"`
	Password        string `json:"password"`
	ConfirmPassword string `json:"confirm_password"`
}

func (rfrnc password_reset_routes_typ) Confirm(w http.ResponseWriter, r *http.Request) {
	var redr_url string
	var err error
	var statusCode int

	defer func() {
		if err != nil {
			if statusCode == 0 {
				statusCode = http.StatusBadRequest
			}
			http.Error(w, err.Error(), statusCode)
			return
		}
		if statusCode == 0 {
			statusCode = http.StatusCreated
		}
		w.WriteHeader(statusCode)
		w.Write([]byte("goto:" + redr_url))
	}()

	body, readErr := io.ReadAll(r.Body)
	if readErr != nil {
		err = errors.New("bad request")
		return
	}

	var req passwordResetConfirmBody
	if jsonErr := json.Unmarshal(body, &req); jsonErr != nil {
		err = errors.New("bad request")
		return
	}

	if req.Token == "" || req.Password == "" || req.ConfirmPassword == "" {
		err = errors.New("all fields are required")
		return
	}

	if req.Password != req.ConfirmPassword {
		err = errors.New("passwords do not match")
		return
	}

	if len(req.Password) < 8 {
		err = errors.New("password must be at least 8 characters")
		return
	}

	// Look up token: must exist, unused, not expired
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("user_uid", "expires_at", "used_at").
		From("password_reset_tokens").
		Where(sb.Equal("token", req.Token))
	res, dbErr := db.Query_one(sb)
	if dbErr != nil {
		err = errors.New("invalid or expired token")
		return
	}

	if len(res[2]) > 0 {
		err = errors.New("invalid or expired token")
		return
	}

	var expiresAt time.Time
	var parseErr error
	for _, layout := range []string{time.RFC3339, "2006-01-02 15:04:05-07:00", "2006-01-02 15:04:05+00:00", "2006-01-02T15:04:05Z"} {
		expiresAt, parseErr = time.Parse(layout, string(res[1]))
		if parseErr == nil {
			break
		}
	}
	if parseErr != nil || time.Now().After(expiresAt) {
		err = errors.New("invalid or expired token")
		return
	}

	userUID := string(res[0])

	hashedPassword, hashErr := mycrypto.HashPassword(req.Password)
	if hashErr != nil {
		err = errors.New("internal error")
		statusCode = http.StatusInternalServerError
		return
	}

	// Update the user's password credential
	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("credentials").
		Set(ub.Assign("value", hashedPassword)).
		Where(ub.Equal("user_uid", userUID), ub.Equal("type", "password"))
	if execErr := db.Exec(ub); execErr != nil {
		err = errors.New("internal error")
		statusCode = http.StatusInternalServerError
		return
	}

	// Mark token as used
	ub2 := sqlbuilder.NewUpdateBuilder()
	ub2.Update("password_reset_tokens").
		Set(ub2.Assign("used_at", time.Now().Format(time.RFC3339))).
		Where(ub2.Equal("token", req.Token))
	if execErr := db.Exec(ub2); execErr != nil {
		err = errors.New("internal error")
		statusCode = http.StatusInternalServerError
		return
	}

	// Auto-login
	_, _, err = auth.Authorize(w, r, "auth", userUID, "")
	if err != nil {
		statusCode = http.StatusInternalServerError
		return
	}

	redr_url = "/events"
}
