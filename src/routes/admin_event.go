package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	eventcleanup "membox-serv/src/event_cleanup"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type admin_event_routes_typ struct{}

var AdminEventRoutes admin_event_routes_typ

// adminEventListLimit, tek sayfada donen event sayisi. Panelde arama var, sayfalama yok;
// liste bir ekrani asmasin diye ust sinir konuldu.
const adminEventListLimit = 200

// GET /api/admin/events?q=<arama>
// Ne: Admin panelinin event listesi.
// Nasil: Isim veya sahibinin e-postasi uzerinden arar; sahip e-postalari admins
//
//	dizisinden users tablosuna bakilarak toplanir.
//
// Neden: Panelde hic event ekrani yoktu; silme ve tarih duzeltme icin once
//
//	event'i bulmak gerekiyor.
func (a admin_event_routes_typ) List(w http.ResponseWriter, r *http.Request) {
	search := strings.TrimSpace(r.URL.Query().Get("q"))
	like := "%" + search + "%"

	bldr := sqlbuilder.BuildNamed(`
		SELECT
			e.uid::text,
			COALESCE(e.name, ''),
			e.status::text,
			COALESCE(e.activation_date::text, ''),
			COALESCE(e.active_until::text, ''),
			COALESCE(e.storage_until::text, ''),
			COALESCE(e.deleted_at::text, ''),
			COALESCE(e.created_at::text, ''),
			COALESCE((
				SELECT string_agg(u.mail, ', ' ORDER BY u.mail)
				FROM users u
				WHERE u.uid = ANY(e.admins)
			), '')
		FROM events e
		WHERE ${empty_search}
			OR e.name ILIKE ${name_like}
			OR EXISTS (
				SELECT 1 FROM users u
				WHERE u.uid = ANY(e.admins) AND u.mail ILIKE ${mail_like}
			)
		ORDER BY e.created_at DESC
		LIMIT ${row_limit}
	`, map[string]interface{}{
		"empty_search": search == "",
		"name_like":    like,
		"mail_like":    like,
		"row_limit":    adminEventListLimit,
	})

	rows, err := db.Query_all(bldr)
	if err != nil {
		http.Error(w, utils.Tag_err("ael1", err).Error(), http.StatusInternalServerError)
		return
	}

	events := []types.Js_object{}
	for _, row := range rows {
		events = append(events, types.Js_object{
			"uid":             string(row[0]),
			"name":            string(row[1]),
			"status":          string(row[2]),
			"activation_date": string(row[3]),
			"active_until":    string(row[4]),
			"storage_until":   string(row[5]),
			"deleted_at":      string(row[6]),
			"created_at":      string(row[7]),
			"admin_emails":    string(row[8]),
		})
	}

	networkutils.SendJson(events, w)
}

type adminEventUpdateReq struct {
	ActivationDate string `json:"activation_date"`
}

// PATCH /api/admin/events/{eventUID}
// Ne: Bir event'in aktivasyon tarihini gunceller.
// Nasil: Yalnizca activation_date yazilir; active_until'i DB trigger'i paketin
//
//	activation_days degerine gore kendisi yeniden hesaplar.
//
// Neden: Excel 2.1d. Onemli kisit: trigger, event bir kez aktive olduktan sonra
//
//	tarihi degistirmeyi reddediyor. O hatayi 409 olarak, sebebiyle birlikte
//	doneriz -- kullanici genel bir hata yerine nedenini gorsun.
func (a admin_event_routes_typ) UpdateActivationDate(w http.ResponseWriter, r *http.Request) {
	eventUID := mux.Vars(r)["eventUID"]

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	var req adminEventUpdateReq
	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.ActivationDate) == "" {
		http.Error(w, "activation_date is required", http.StatusBadRequest)
		return
	}

	parsed, err := parseAdminEventDate(strings.TrimSpace(req.ActivationDate))
	if err != nil {
		http.Error(w, "activation_date must be YYYY-MM-DD or RFC3339", http.StatusBadRequest)
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("events").Set(
		ub.Assign("activation_date", parsed),
	).Where(
		ub.Equal("uid", eventUID),
		ub.IsNull("deleted_at"),
	)
	ub.SQL("RETURNING activation_date::text, active_until::text")

	rows, err := db.Query_all(ub)
	if err != nil {
		// Trigger'in kendi mesajini kullaniciya aynen tasiriz; baska bir yerde
		// tekrarlanan bir metin tutmaktan iyidir.
		if strings.Contains(err.Error(), "Cannot modify activation_date") {
			http.Error(w, "this event has already been activated, so its activation date can no longer be changed", http.StatusConflict)
			return
		}
		http.Error(w, utils.Tag_err("ael2", err).Error(), http.StatusInternalServerError)
		return
	}
	if len(rows) == 0 {
		http.Error(w, "event not found or already deleted", http.StatusNotFound)
		return
	}

	fmt.Printf("[admin.event] activation_date updated event=%s to=%s\n", eventUID, parsed)

	networkutils.SendJson(types.Js_object{
		"activation_date": string(rows[0][0]),
		"active_until":    string(rows[0][1]),
	}, w)
}

// DELETE /api/admin/events/{eventUID}
// Ne: Bir event'i admin panelinden kapatir.
// Nasil: Event sahibinin kullandigi ayni fonksiyonu cagirir -- upload snapshot'i
//
//	alinir, event soft-delete edilir, medya kayitlari DB'den ve dosyalar
//	S3'ten temizlenir.
//
// Neden: Excel 2.15. Ayri bir silme mantigi yazmak iki yolun zamanla ayrilmasi
//
//	demek olurdu; snapshot'taki actor alani islemi kimin yaptigini zaten tutuyor.
func (a admin_event_routes_typ) Delete(w http.ResponseWriter, r *http.Request) {
	eventUID := mux.Vars(r)["eventUID"]

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	checkBldr := sqlbuilder.BuildNamed(`
		SELECT deleted_at IS NOT NULL
		FROM events
		WHERE uid = ${event_uid}
	`, map[string]interface{}{"event_uid": eventUID})

	res, err := db.Query_one(checkBldr)
	if err != nil {
		http.Error(w, "event not found", http.StatusNotFound)
		return
	}

	// Zaten kapatilmis event'te tekrar purge calistirmayiz; cagri basarili sayilir.
	if string(res[0]) == "true" || string(res[0]) == "t" {
		networkutils.SendJson(types.Js_object{"success": true, "already_closed": true}, w)
		return
	}

	fmt.Printf("[admin.event] deleting event=%s actor=%s\n", eventUID, claims.UserUID)

	if _, err := eventcleanup.PurgeUploadsAndSoftDeleteEvent(
		eventUID,
		claims.UserUID,
		eventcleanup.SnapshotReasonManualDelete,
	); err != nil {
		fmt.Printf("[admin.event] ERROR: delete failed event=%s err=%v\n", eventUID, err)
		http.Error(w, utils.Tag_err("ael3", err).Error(), http.StatusInternalServerError)
		return
	}

	networkutils.SendJson(types.Js_object{"success": true, "already_closed": false}, w)
}

// Panelden gelen tarih ya sade gun (date input) ya da tam zaman damgasi olabilir.
func parseAdminEventDate(raw string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t, nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t, nil
	}
	return time.Time{}, errors.New("unrecognised date format")
}
