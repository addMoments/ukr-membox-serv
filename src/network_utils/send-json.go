package networkutils

import (
	"encoding/json"
	"net/http"
)

func SendJson(payload interface{}, w http.ResponseWriter) error {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(payload)
}
