package routes

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/qr"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"
	"net/http"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type album_routes_typ struct{}

var AlbumRoutes album_routes_typ

// sendAlbumGuestError, misafir album uclarinin ortak hata sozlugu.
// Frontend kodlara gore ekran secer (kapali / passcode iste / passcode yanlis).
func sendAlbumGuestError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, dbscripts.ErrAlbumNotFound), errors.Is(err, dbscripts.ErrAlbumClosed):
		_ = networkutils.SendErrorJSON(w, http.StatusForbidden, "ALBUM_CLOSED", "This album is not available.")
	case errors.Is(err, dbscripts.ErrPasscodeRequired):
		_ = networkutils.SendErrorJSON(w, http.StatusForbidden, "PASSCODE_REQUIRED", "This album requires a passcode.")
	case errors.Is(err, dbscripts.ErrPasscodeInvalid):
		_ = networkutils.SendErrorJSON(w, http.StatusForbidden, "PASSCODE_INVALID", "The passcode is incorrect.")
	case errors.Is(err, dbscripts.ErrAlbumNotOpened):
		_ = networkutils.SendErrorJSON(w, http.StatusForbidden, "ALBUM_NOT_OPENED", "Open this album from its link first.")
	case errors.Is(err, dbscripts.ErrEventClosed):
		_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", "This event is closed.")
	default:
		return false
	}
	return true
}

// hostAlbum, host uclarinin ortak girisi: claims + event/album unpack + admin kontrolu +
// albumun bu etkinlige ait oldugu. Hata durumunda stat_code'u da doldurur.
func hostAlbum(r *http.Request) (album dbscripts.Album, eventUID string, eventPacked string, albumPacked string, statCode int, err error) {
	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok || claims.Role != "auth" {
		err = errors.New("unauthorized")
		statCode = http.StatusUnauthorized
		return
	}

	eventPacked = mux.Vars(r)["eventPackedUid"]
	eventUID, err = utils.UUID.UnpackUUID(eventPacked)
	if err != nil {
		err = utils.Tag_err("alr1", err)
		statCode = http.StatusBadRequest
		return
	}

	isAdmin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if err != nil {
		if !errors.Is(err, dbscripts.ErrEventClosed) {
			err = utils.Tag_err("alr2", err)
		}
		return
	}
	if !isAdmin {
		err = errors.New("forbidden")
		statCode = http.StatusForbidden
		return
	}

	albumPacked = mux.Vars(r)["albumPackedUid"]
	if albumPacked == "" {
		return
	}
	albumUID, err := utils.UUID.UnpackUUID(albumPacked)
	if err != nil {
		err = utils.Tag_err("alr3", err)
		statCode = http.StatusBadRequest
		return
	}

	album, err = dbscripts.Get_album(albumUID)
	if err != nil {
		if errors.Is(err, dbscripts.ErrAlbumNotFound) {
			statCode = http.StatusNotFound
		}
		return
	}
	if album.EventUID != eventUID {
		err = errors.New("album does not belong to this event")
		statCode = http.StatusForbidden
		return
	}
	return
}

// Delete, albumu soft-delete eder ve icindeki tum medyayi cope tasir (karar 3).
// General silinemez (409). Yanit: cope tasinan adet.
// DELETE /api/event/{eventPackedUid}/album/{albumPackedUid}
func (ar album_routes_typ) Delete(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if errors.Is(err, dbscripts.ErrDefaultAlbum) {
				_ = networkutils.SendErrorJSON(w, http.StatusConflict, "DEFAULT_ALBUM", "The default album cannot be deleted.")
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		networkutils.SendJson(payload, w)
	})()

	album, _, eventPacked, albumPacked, code, err := hostAlbum(r)
	if err != nil {
		stat_code = code
		return
	}
	if album.IsDefault {
		err = dbscripts.ErrDefaultAlbum
		return
	}
	if album.Deleted {
		payload = map[string]interface{}{"success": true, "already_deleted": true, "trashed_count": 0}
		return
	}

	// Iki UPDATE tek transaction'da: album silinmis ama medya acikta kalmasin (ya da tersi).
	tx, err := db.Db().Begin()
	if err != nil {
		err = utils.Tag_err("ald1", err)
		return
	}

	var trashed int
	row := tx.QueryRow(`
		WITH moved AS (
			UPDATE uploads SET trashed_at = NOW()
			WHERE album_uid = $1 AND trashed_at IS NULL
			RETURNING uid
		)
		SELECT COUNT(*) FROM moved
	`, album.UID)
	if err = row.Scan(&trashed); err != nil {
		tx.Rollback()
		err = utils.Tag_err("ald2", err)
		return
	}

	if _, err = tx.Exec(`UPDATE albums SET deleted_at = NOW() WHERE uid = $1 AND deleted_at IS NULL`, album.UID); err != nil {
		tx.Rollback()
		err = utils.Tag_err("ald3", err)
		return
	}

	if err = tx.Commit(); err != nil {
		err = utils.Tag_err("ald4", err)
		return
	}

	// QR dosyasi best-effort; yoksa/silinemezse islem basarili sayilir.
	if rmErr := s3wrap.Public_s3.Rm(qr.AlbumQRPath(eventPacked, albumPacked)); rmErr != nil {
		log.Printf("[album.delete] qr rm failed for %s: %v", albumPacked, rmErr)
	}

	log.Printf("[album.delete] event=%s album=%s trashed=%d", eventPacked, albumPacked, trashed)
	payload = map[string]interface{}{"success": true, "already_deleted": false, "trashed_count": trashed}
}

// AdjustQR, album QR'ini etkinlik QR'iyla ayni secenek govdesiyle (renk/sekil/logo) uretir.
// POST /api/qr/{eventPackedUid}/album/{albumPackedUid}
func (ar album_routes_typ) AdjustQR(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})()

	album, _, eventPacked, albumPacked, code, err := hostAlbum(r)
	if err != nil {
		stat_code = code
		return
	}
	if album.Deleted {
		err = errors.New("album has been deleted")
		stat_code = http.StatusGone
		return
	}

	req := defaultQRReq
	if r.ContentLength != 0 {
		if err = json.NewDecoder(r.Body).Decode(&req); err != nil {
			err = utils.Tag_err("alq1", err)
			stat_code = http.StatusBadRequest
			return
		}
	}

	opts, err := buildQROptions(req)
	if err != nil {
		return
	}

	if err = qr.UpdateAlbumQR(eventPacked, albumPacked, opts...); err != nil {
		err = utils.Tag_err("alq2", err)
		return
	}
}

// EnsureQRs, etkinligin QR dosyasi olmayan albumlerine varsayilan QR uretir.
// Idempotent; Albums sayfasi acilirken cagrilir (migration ile gelen General dahil).
// POST /api/event/{eventPackedUid}/albums/ensure-qr  -> {"generated": [packedAlbumUid...]}
func (ar album_routes_typ) EnsureQRs(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		networkutils.SendJson(payload, w)
	})()

	_, eventUID, eventPacked, _, code, err := hostAlbum(r)
	if err != nil {
		stat_code = code
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("uid").From("albums").Where(
		sb.Equal("event_uid", eventUID),
		sb.IsNull("deleted_at"),
	)
	rows, err := db.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("ale1", err)
		return
	}

	generated := []string{}
	for _, row := range rows {
		albumPacked, packErr := utils.UUID.PackUUID(string(row[0]))
		if packErr != nil {
			continue
		}
		exists, exErr := s3wrap.Public_s3.Exists(qr.AlbumQRPath(eventPacked, albumPacked))
		if exErr != nil {
			log.Printf("[album.ensure-qr] exists check failed for %s: %v", albumPacked, exErr)
			continue
		}
		if exists {
			continue
		}
		qrOpts, optErr := buildQROptions(defaultQRReq)
		if optErr != nil {
			err = optErr
			return
		}
		if genErr := qr.UpdateAlbumQR(eventPacked, albumPacked, qrOpts...); genErr != nil {
			log.Printf("[album.ensure-qr] generate failed for %s: %v", albumPacked, genErr)
			continue
		}
		generated = append(generated, albumPacked)
	}

	payload = map[string]interface{}{"generated": generated}
}

type guestOpenReq struct {
	Passcode string `json:"passcode"`
}

// GuestOpen, misafirin bir albumu linkten acmasi. Public albumde yalnizca kontrol eder;
// private albumu token'a isler; protected albumde passcode ister/dogrular ve token'a isler.
// Basarida yeni token X-Auth-Token basliginda gelir (gerekiyorsa).
// POST /api/guest/album/{albumPackedUid}/open  {passcode?}
func (ar album_routes_typ) GuestOpen(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if sendAlbumGuestError(w, err) {
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		networkutils.SendJson(payload, w)
	})()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		stat_code = http.StatusUnauthorized
		return
	}

	eventUID, err := utils.UUID.UnpackUUID(r.Header.Get("X-Event"))
	if err != nil {
		err = utils.Tag_err("alo1", err)
		stat_code = http.StatusBadRequest
		return
	}

	albumUID, err := utils.UUID.UnpackUUID(mux.Vars(r)["albumPackedUid"])
	if err != nil {
		err = utils.Tag_err("alo2", err)
		stat_code = http.StatusBadRequest
		return
	}

	album, err := dbscripts.Get_album(albumUID)
	if err != nil {
		return
	}

	accessErr := dbscripts.Guest_album_access(album, eventUID, claims.Al)
	switch {
	case accessErr == nil:
		// Zaten acik (public ya da token'da). Token'a dokunmaya gerek yok.
	case errors.Is(accessErr, dbscripts.ErrAlbumNotOpened):
		// Private: linki bilen acar, passcode yok.
	case errors.Is(accessErr, dbscripts.ErrPasscodeRequired):
		var req guestOpenReq
		if r.ContentLength != 0 {
			if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
				err = utils.Tag_err("alo3", decErr)
				stat_code = http.StatusBadRequest
				return
			}
		}
		if strings.TrimSpace(req.Passcode) == "" {
			err = dbscripts.ErrPasscodeRequired
			return
		}
		if strings.TrimSpace(req.Passcode) != album.Passcode {
			log.Printf("[album.open] wrong passcode event=%s album=%s ip=%s", eventUID, albumUID, auth.GetClientIP(r))
			err = dbscripts.ErrPasscodeInvalid
			return
		}
	default:
		err = accessErr
		return
	}

	// Private/protected: token'a isle. Role auth olan (giris yapmis) ziyaretci icin token
	// yenilenemez; o durumda album yalnizca Go uclarindan gorunur, PostgREST listesinde cikmaz.
	if accessErr != nil && claims.Role == "webanon" {
		if claims, _, err = auth.ReissueGuestToken(w, claims, eventUID, []string{albumUID}); err != nil {
			err = utils.Tag_err("alo4", err)
			return
		}
	}

	payload = map[string]interface{}{
		"ok":         true,
		"album_uid":  album.UID,
		"guest_view": album.GuestView,
	}
}

// GuestZip, gorunur bir albumun tum medyasini sikistirmadan zip olarak akitir (karar 4).
// Kontrol: album misafire acik + guest_download_all + etkinlik guest_gallery + album guest_view.
// GET /api/guest/album/{albumPackedUid}/zip
func (ar album_routes_typ) GuestZip(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var err error
	headersSent := false

	defer (func() {
		if err != nil && !headersSent {
			if sendAlbumGuestError(w, err) {
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
	})()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		stat_code = http.StatusUnauthorized
		return
	}

	eventUID, err := utils.UUID.UnpackUUID(r.Header.Get("X-Event"))
	if err != nil {
		err = utils.Tag_err("alz1", err)
		stat_code = http.StatusBadRequest
		return
	}

	albumUID, err := utils.UUID.UnpackUUID(mux.Vars(r)["albumPackedUid"])
	if err != nil {
		err = utils.Tag_err("alz2", err)
		stat_code = http.StatusBadRequest
		return
	}

	album, err := dbscripts.Get_album(albumUID)
	if err != nil {
		return
	}
	if err = dbscripts.Guest_album_access(album, eventUID, claims.Al); err != nil {
		return
	}

	galleryOn, err := dbscripts.Event_guest_gallery(eventUID)
	if err != nil {
		return
	}
	if !galleryOn || !album.GuestView || !album.GuestDownloadAll {
		err = dbscripts.ErrAlbumClosed
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("uid", "value").From("uploads").Where(
		sb.Equal("album_uid", albumUID),
		sb.IsNull("trashed_at"),
		sb.In("upload_type", "photo", "video"),
	).OrderBy("created_at ASC")
	rows, err := db.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("alz3", err)
		return
	}

	safeName := strings.Map(func(c rune) rune {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			return c
		}
		return '-'
	}, album.Name)
	if safeName == "" {
		safeName = "album"
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.zip"`, safeName))
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	headersSent = true

	zw := zip.NewWriter(w)
	flusher, _ := w.(http.Flusher)
	added := 0
	for i, row := range rows {
		value := string(row[1])
		file, getErr := s3wrap.Public_s3.Get(value)
		if getErr != nil {
			if aerr, ok := getErr.(awserr.Error); ok && aerr.Code() == s3.ErrCodeNoSuchKey {
				continue
			}
			log.Printf("[album.zip] s3 get failed %s: %v", value, getErr)
			continue
		}

		// Store: sikistirma yok; medya zaten sikisik, CPU harcamaya deger degil.
		entry, createErr := zw.CreateHeader(&zip.FileHeader{
			Name:   fmt.Sprintf("%03d-%s", i+1, path.Base(value)),
			Method: zip.Store,
		})
		if createErr != nil {
			file.Close()
			log.Printf("[album.zip] zip entry failed: %v", createErr)
			break
		}
		if _, copyErr := io.Copy(entry, file); copyErr != nil {
			file.Close()
			// Istemci koptu ya da S3 kesildi; devam etmenin anlami yok.
			log.Printf("[album.zip] copy failed for %s: %v", value, copyErr)
			break
		}
		file.Close()
		added++
		if flusher != nil {
			flusher.Flush()
		}
	}

	if closeErr := zw.Close(); closeErr != nil {
		log.Printf("[album.zip] close failed: %v", closeErr)
	}
	log.Printf("[album.zip] event=%s album=%s files=%d", eventUID, albumUID, added)
}
