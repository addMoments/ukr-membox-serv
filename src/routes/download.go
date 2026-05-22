package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	s3wrap "membox-serv/src/s3-wrap"
)

func DownloadProxy(w http.ResponseWriter, r *http.Request) {
	s3url := r.URL.Query().Get("url")
	if s3url == "" {
		http.Error(w, "missing url param", http.StatusBadRequest)
		return
	}

	s3path, err := s3wrap.Public_s3.Decode_url(s3url)
	if err != nil {
		http.Error(w, errors.New("invalid s3 url").Error(), http.StatusBadRequest)
		return
	}

	presigned, err := s3wrap.Public_s3.Get_presign(s3path, 60*time.Second)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": presigned})
}
