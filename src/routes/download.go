package routes

import (
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
	"time"

	s3wrap "membox-serv/src/s3-wrap"
)

// downloadFileName, istemcinin istedigi indirme adini basliga koymadan once temizler:
// yol parcalari, kontrol karakterleri ve asiri uzunluk atilir. Bos donerse baslik yazilmaz.
func downloadFileName(raw string) string {
	name := strings.Map(func(r rune) rune {
		if r < 32 || r == 127 {
			return -1
		}
		return r
	}, raw)
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" {
		return ""
	}
	if len(name) > 200 {
		name = name[len(name)-200:]
	}
	return name
}

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

	// Ne: filename verilirse imzali URL Content-Disposition: attachment tasir.
	// Neden: Frontend dosyayi fetch+blob ile bellege almak yerine tarayiciyi dogrudan bu URL'e
	//        gonderiyor (ilerleme cubugu, buyuk export'larda bellek yok); indirme adi buradan gelir.
	presigned, err := s3wrap.Public_s3.Get_presign(s3path, 60*time.Second, downloadFileName(r.URL.Query().Get("filename")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": presigned})
}
