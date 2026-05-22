package dbscripts

import (
	"errors"
	db "membox-serv/src/db_layer"
	"strconv"

	"github.com/huandu/go-sqlbuilder"
)

// Has_feature returns true if any product in the event's purchase cart
// has featureID in its granted_features array.
func Has_feature(eventUID string, featureID int) (has bool, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("events e")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "purchases pu", "e.purchase_uid = pu.uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "cart_items ci", "pu.cart_uid = ci.cart_uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "products p", "ci.product_uid = p.uid")
	sb.Where(
		sb.Equal("e.uid", eventUID),
		sb.IsNull("e.deleted_at"),
		sb.Var(featureID)+" = ANY(p.granted_features)",
	)

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	has = string(res[0]) != "0"
	return
}

var ErrLimitReached = errors.New("limit reached")
var ErrGuestLimitReached = errors.New("guest limit reached")
var ErrMediaLimitReached = errors.New("media limit reached")

func Event_tier(eventUID string) (tier string, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("p.id").From("events e")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "purchases pu", "e.purchase_uid = pu.uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "cart_items ci", "pu.cart_uid = ci.cart_uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "products p", "ci.product_uid = p.uid")
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

func Event_limit(eventUID string, limitName string) (limit int, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("p.options->>" + sb.Var(limitName)).From("events e")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "purchases pu", "e.purchase_uid = pu.uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "cart_items ci", "pu.cart_uid = ci.cart_uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "products p", "ci.product_uid = p.uid")
	sb.Where(
		sb.Equal("e.uid", eventUID),
		sb.IsNull("e.deleted_at"),
		sb.Equal("p.is_add_on", false),
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
	sb.JoinWithOption(sqlbuilder.InnerJoin, "purchases pu", "e.purchase_uid = pu.uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "cart_items ci", "pu.cart_uid = ci.cart_uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "products p", "ci.product_uid = p.uid")
	sb.Where(
		sb.Equal("e.uid", eventUID),
		sb.Equal("p.is_add_on", false),
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

func Event_media_count(eventUID string) (count int, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COUNT(*)").From("uploads")
	sb.Where(sb.Equal("event_uid", eventUID))

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
	)

	res, err := db.Query_one(sb)
	if err != nil {
		return
	}

	count, err = strconv.Atoi(string(res[0]))
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
