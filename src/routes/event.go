package routes

import (
	"errors"
	"log"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	eventcleanup "membox-serv/src/event_cleanup"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/utils"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type event_routes_typ struct{}

var EventRoutes event_routes_typ

// Stats, event icin frontend'in kullanacagi contributor ve limit metriklerini dondurur.
func (er event_routes_typ) Stats(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(
					w,
					http.StatusGone,
					"EVENT_CLOSED",
					networkutils.EventClosedMessage(r),
				)
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

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("evs1", err)
		return
	}

	isAdmin := false
	isAdmin, err = dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("evs2", err)
		return
	}
	if !isAdmin {
		err = errors.New("unauthorized")
		stat_code = http.StatusForbidden
		return
	}

	guestLimit := 0
	guestLimit, err = dbscripts.Event_limit(eventUID, "guest_count")
	if err != nil {
		err = utils.Tag_err("evs3", err)
		return
	}

	contributorCount := 0
	contributorCount, err = dbscripts.Event_contributor_count(eventUID)
	if err != nil {
		err = utils.Tag_err("evs4", err)
		return
	}

	guestCount := 0
	guestCount, err = dbscripts.Event_guest_count(eventUID)
	if err != nil {
		err = utils.Tag_err("evs5", err)
		return
	}

	payload = map[string]interface{}{
		"contributor_count": contributorCount,
		"guest_count":       guestCount,
		"guest_limit":       guestLimit,
	}
	log.Printf(
		"[event.stats] event_packed_uid=%s event_uid=%s contributor_count=%d guest_count=%d guest_limit=%d",
		eventPackedUID,
		eventUID,
		contributorCount,
		guestCount,
		guestLimit,
	)
	stat_code = http.StatusOK
}

func (er event_routes_typ) Delete(w http.ResponseWriter, r *http.Request) {
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
	if !ok {
		err = errors.New("unauthorized")
		stat_code = http.StatusUnauthorized
		return
	}

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("evd1", err)
		return
	}

	activeAdminBldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*)
		FROM events
		WHERE uid = ${event_uid}
			AND ${user_uid}::uuid = ANY(admins)
			AND deleted_at IS NULL
	`, map[string]interface{}{
		"event_uid": eventUID,
		"user_uid":  claims.UserUID,
	})

	res, queryErr := db.Query_one(activeAdminBldr)
	if queryErr != nil {
		err = utils.Tag_err("evd2", queryErr)
		return
	}

	if string(res[0]) == "1" {
		// Manual delete artik ayni response sozlesmesini koruyarak upload
		// snapshot'i alir, medya satirlarini DB'den ve dosyalari S3'ten temizler.
		_, err = eventcleanup.PurgeUploadsAndSoftDeleteEvent(
			eventUID,
			claims.UserUID,
			eventcleanup.SnapshotReasonManualDelete,
		)
		if err != nil {
			err = utils.Tag_err("evd2.1", err)
			return
		}
		payload = map[string]interface{}{
			"success":        true,
			"already_closed": false,
		}
		stat_code = http.StatusOK
		return
	}
	alreadyClosedBldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*)
		FROM events
		WHERE uid = ${event_uid}
			AND ${user_uid}::uuid = ANY(admins)
			AND deleted_at IS NOT NULL
	`, map[string]interface{}{
		"event_uid": eventUID,
		"user_uid":  claims.UserUID,
	})

	res, queryErr = db.Query_one(alreadyClosedBldr)
	if queryErr != nil {
		err = utils.Tag_err("evd3", queryErr)
		return
	}

	if string(res[0]) == "1" {
		payload = map[string]interface{}{
			"success":        true,
			"already_closed": true,
		}
		stat_code = http.StatusOK
		return
	}

	err = errors.New("unauthorized")
	stat_code = http.StatusForbidden
}

// ExtendStorage, event sahibinin storage_until tarihini bir kereye mahsus
// 30 gun ileri atmasini saglar. Tek seferlik hak: storage_extended_at NULL
// degilse 409 doner. Mail bildirimi ve UI modal'i bu alana bakarak kararlarini
// verir; bir daha modal acilmaz, bir daha mail gonderilmez.
func (er event_routes_typ) ExtendStorage(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(
					w,
					http.StatusGone,
					"EVENT_CLOSED",
					networkutils.EventClosedMessage(r),
				)
				return
			}
			if stat_code == 0 {
				stat_code = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		networkutils.SendJson(payload, w)
	})()

	// 1. Auth: middleware tarafindan zaten kontrol edildi, burada claims'i okuyoruz.
	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		stat_code = http.StatusUnauthorized
		return
	}

	// 2. URL'deki packed UID'i ic UUID'e cevir.
	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("ext1", err)
		stat_code = http.StatusBadRequest
		return
	}

	// 3. Admin kontrolu. Is_events_admin zaten soft-delete edilmis event'lar icin
	// ErrEventClosed firlatir; defer onu yakalayip 410 Gone'a cevirir.
	isAdmin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("ext2", err)
		return
	}
	if !isAdmin {
		err = errors.New("forbidden")
		stat_code = http.StatusForbidden
		return
	}

	// 4. Mevcut storage_until ve storage_extended_at degerlerini cek.
	// Burada kontrol etmemizin sebebi: race condition'a girmeden once
	// kullaniciya hangi nedenle reddedildigini anlamli kodlarla bildirmek.
	checkSB := sqlbuilder.NewSelectBuilder()
	checkSB.Select("storage_until", "storage_extended_at").From("events").
		Where(checkSB.Equal("uid", eventUID))
	row, err := db.Query_one(checkSB)
	if err != nil {
		err = utils.Tag_err("ext3", err)
		return
	}

	storageUntilStr := string(row[0])
	storageExtendedAtStr := string(row[1])

	// 4a. Tek seferlik hak kullanildi mi?
	if storageExtendedAtStr != "" {
		_ = networkutils.SendErrorJSON(
			w,
			http.StatusConflict,
			"ALREADY_EXTENDED",
			"Storage already extended for this event.",
		)
		err = errors.New("already extended")
		stat_code = http.StatusConflict
		return
	}

	// 4b. storage_until zaten gecmis mi? Bu durumda cron job'in soft-delete
	// yapmasini bekliyoruz; uzatma artik mumkun degil.
	if storageUntilStr == "" {
		err = utils.Tag_err("ext4", errors.New("storage_until is null"))
		return
	}

	// 5. Atomik update: WHERE clausu icindeki storage_extended_at IS NULL kontrolu
	// race condition'a karsi sigortadir. Eger iki request ayni anda gelirse
	// sadece biri satira dokunabilir; digeri 0 satir gunceller ve 409 doner.
	// storage_until <= NOW() kontrolu ek sigorta: cron tetiklendi ama henuz
	// deleted_at set etmediyse uzatma yine reddedilir.
	updateBldr := sqlbuilder.BuildNamed(`
		UPDATE events
		SET storage_until        = storage_until + interval '30 days',
		    storage_extended_at  = NOW()
		WHERE uid = ${event_uid}
		  AND storage_extended_at IS NULL
		  AND storage_until > NOW()
		  AND deleted_at IS NULL
		RETURNING storage_until
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	updateRes, err := db.Query_one(updateBldr)
	if err != nil {
		// "empty row" => RETURNING yok demek, yani WHERE eslesmedi: yarista kaybettik
		// ya da arada storage_until gecti. Kullaniciya 409 mantikli; ama "expired"
		// daha dogru bir mesaj olabilir, gercek nedeni bilmiyoruz, 409 ile sadelestiriyoruz.
		if err.Error() == "empty row" {
			_ = networkutils.SendErrorJSON(
				w,
				http.StatusConflict,
				"EXTEND_REJECTED",
				"Extension could not be applied (already extended or expired).",
			)
			err = errors.New("extend rejected")
			stat_code = http.StatusConflict
			return
		}
		err = utils.Tag_err("ext5", err)
		return
	}

	newStorageUntil := string(updateRes[0])

	log.Printf(
		"[event.extend-storage] event_uid=%s user_uid=%s new_storage_until=%s",
		eventUID,
		claims.UserUID,
		newStorageUntil,
	)

	payload = map[string]interface{}{
		"storage_until": newStorageUntil,
	}
	stat_code = http.StatusOK
}
