package networkutils

import (
	"encoding/json"
	"net/http"
	"strings"
)

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func SendErrorJSON(w http.ResponseWriter, status int, code string, message string) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(APIError{
		Code:    code,
		Message: message,
	})
}

func RequestLang(r *http.Request) string {
	acceptLanguage := r.Header.Get("Accept-Language")
	if acceptLanguage == "" {
		return "en"
	}

	base := strings.Split(strings.Split(acceptLanguage, ",")[0], "-")[0]
	base = strings.ToLower(strings.TrimSpace(base))
	if base == "uk" || base == "ua" {
		return "uk"
	}

	return "en"
}

func EventClosedMessage(r *http.Request) string {
	if RequestLang(r) == "uk" {
		return "Цю подію закрито."
	}
	return "This event is closed."
}
