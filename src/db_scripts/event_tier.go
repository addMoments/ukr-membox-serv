package dbscripts

import (
	"errors"
	db "membox-serv/src/db_layer"
	"strconv"

	"github.com/huandu/go-sqlbuilder"
)

// Has_feature returns true if any product in the event's purchase cart, or the
// package the event was upgraded to (event_core_product_uid), has featureID in its
// granted_features array. Bir Classic etkinlik Premium'a gecince reklam alani acilir.
func Has_feature(eventUID string, featureID int) (has bool, err error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*) FROM events e
		WHERE e.uid = ${event_uid}
		  AND e.deleted_at IS NULL
		  AND (
		        EXISTS (SELECT 1
		                  FROM purchases pu
		                  JOIN cart_items ci ON pu.cart_uid = ci.cart_uid
		                  JOIN products p    ON ci.product_uid = p.uid
		                 WHERE pu.uid = e.purchase_uid
		                   AND ${feature_id} = ANY(p.granted_features))
		     OR EXISTS (SELECT 1 FROM products p
		                 WHERE p.uid = event_core_product_uid(e.uid)
		                   AND ${feature_id} = ANY(p.granted_features))
		  )
	`, map[string]interface{}{"event_uid": eventUID, "feature_id": featureID})

	res, err := db.Query_one(bldr)
	if err != nil {
		return
	}

	has = string(res[0]) != "0"
	return
}

var ErrLimitReached = errors.New("limit reached")
var ErrGuestLimitReached = errors.New("guest limit reached")
var ErrMediaLimitReached = errors.New("media limit reached")

// Event_tier, etkinligin gecerli core paketinin id'si (yukseltme dahil).
func Event_tier(eventUID string) (tier string, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("p.id").From("events e")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "products p", "p.uid = event_core_product_uid(e.uid)")
	sb.Where(
		sb.Equal("e.uid", eventUID),
		sb.IsNull("e.deleted_at"),
	)

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	tier = string(res[0])
	return
}

// Event_limit, etkinligin gecerli core paketindeki (yukseltme dahil) tamsayi limiti okur.
func Event_limit(eventUID string, limitName string) (limit int, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("p.options->>" + sb.Var(limitName)).From("events e")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "products p", "p.uid = event_core_product_uid(e.uid)")
	sb.Where(
		sb.Equal("e.uid", eventUID),
		sb.IsNull("e.deleted_at"),
	)

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	limit, err = strconv.Atoi(string(res[0]))
	return
}

// Event_limit_including_closed, soft-delete edilmis eventlerin order-account
// snapshot fallback'inde paket limitini okuyabilmek icin deleted_at filtresi
// olmadan core paket options degerini dondurur.
func Event_limit_including_closed(eventUID string, limitName string) (limit int, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("p.options->>" + sb.Var(limitName)).From("events e")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "products p", "p.uid = event_core_product_uid(e.uid)")
	sb.Where(
		sb.Equal("e.uid", eventUID),
	)

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	limit, err = strconv.Atoi(string(res[0]))
	return
}

func Event_guest_count(eventUID string) (count int, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("participants")
	sb.Where(sb.Equal("event_uid", eventUID))

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	count, err = strconv.Atoi(string(res[0]))
	return
}

// Event_media_count, etkinligin paket medya kotasina sayilan dosya adedini doner.
//
// Yalnizca galeriye giren gercek medya sayilir:
//   * upload_type: photo/video. Guestbook kayitlari (text, voice) medya kotasina girmez —
//     text satirlarini misafir PostgREST uzerinden dogrudan yaziyor, yani hicbir limit
//     kontrolunden gecmeden "fotograf ve video" kotasini tuketebiliyordu.
//   * trashed_at IS NULL: cope atilan medya kotayi bosaltir. Boyut sayaclari da
//     (Event_storage_bytes, Guest_upload_usage) ayni kurali izler, yani ayni cope
//     dort limit de ayni cevabi verir. Gerekcesi ve geri alma korumasi limits.go'da.
func Event_media_count(eventUID string) (count int, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("uploads")
	sb.Where(
		sb.Equal("event_uid", eventUID),
		sb.In("upload_type", "photo", "video"),
		sb.IsNull("trashed_at"),
		UploadCountsSQL,
	)

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	count, err = strconv.Atoi(string(res[0]))
	return
}

func Event_contributor_count(eventUID string) (count int, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(DISTINCT client_uid)").From("uploads")
	sb.Where(
		sb.Equal("event_uid", eventUID),
		sb.IsNull("trashed_at"),
		UploadCountsSQL,
	)

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	count, err = strconv.Atoi(string(res[0]))
	return
}

// Participant_in_event, participant kaydinin bu event'e ait olup olmadigini doner.
func Participant_in_event(participantUID string, eventUID string) (inEvent bool, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("participants")
	sb.Where(sb.Equal("uid", participantUID), sb.Equal("event_uid", eventUID))

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	inEvent = string(res[0]) != "0"
	return
}

// Has_contributed, ilgili katilimcinin bu eventte silinmemis en az bir
// paylasimi olup olmadigini doner.
func Has_contributed(eventUID string, clientUID string) (hasContributed bool, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("uploads")
	sb.Where(
		sb.Equal("event_uid", eventUID),
		sb.Equal("client_uid", clientUID),
		sb.IsNull("trashed_at"),
		UploadCountsSQL,
	)

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	hasContributed = string(res[0]) != "0"
	return
}

// Check_contributor_limit_for_new_guest, daha once guest kimligi olusmamis
// yeni bir kullanici event sayfasini acarken contributor limitini kontrol eder.
func Check_contributor_limit_for_new_guest(eventUID string) error {
	limit, err := Event_limit(eventUID, "guest_count")
	if err != nil {
		return err
	}

	if limit == -1 {
		return nil
	}

	contributorCount, err := Event_contributor_count(eventUID)
	if err != nil {
		return err
	}

	if contributorCount >= limit {
		return ErrGuestLimitReached
	}

	return nil
}

// Check_contributor_limit_for_upload, mevcut contributorlarin paylasima devam
// etmesine izin verir; limit doluysa yalnizca yeni contributoru engeller.
func Check_contributor_limit_for_upload(eventUID string, clientUID string) error {
	limit, err := Event_limit(eventUID, "guest_count")
	if err != nil {
		return err
	}

	if limit == -1 {
		return nil
	}

	hasContributed, err := Has_contributed(eventUID, clientUID)
	if err != nil {
		return err
	}
	if hasContributed {
		return nil
	}

	contributorCount, err := Event_contributor_count(eventUID)
	if err != nil {
		return err
	}

	if contributorCount >= limit {
		return ErrGuestLimitReached
	}

	return nil
}

/*
Check_guest_limit eski (participants bazli) limit kontroludur.
Contributor bazli yeni akisa gecildigi icin aktif olarak kullanilmiyor.

func Check_guest_limit(eventUID string) error {
	limit, err := Event_limit(eventUID, "guest_count")
	if err != nil {
		return err
	}

	if limit == -1 {
		return nil
	}

	count, err := Event_guest_count(eventUID)
	if err != nil {
		return err
	}

	contributorCount, err := Event_contributor_count(eventUID)
	if err != nil {
		return err
	}

	log.Printf(
		"[guest-limit] event_uid=%s guest_count=%d contributor_count=%d guest_limit=%d",
		eventUID,
		count,
		contributorCount,
		limit,
	)

	if count >= limit {
		return ErrGuestLimitReached
	}

	return nil
}
*/

func Check_media_limit(eventUID string, newUploads int) error {
	limit, err := Event_limit(eventUID, "media_count")
	if err != nil {
		return err
	}

	if limit == -1 {
		return nil
	}

	count, err := Event_media_count(eventUID)
	if err != nil {
		return err
	}

	if count+newUploads > limit {
		return ErrMediaLimitReached
	}

	return nil
}
