package dbscripts

import (
	"errors"
	db "membox-serv/src/db_layer"
	"membox-serv/src/utils"
	"strconv"
	"strings"

	"github.com/huandu/go-sqlbuilder"
)

// Yukleme limitleri (Excel madde 2.17).
//
// Dort limit var; hepsi core paketin products.options alanindan okunur ve admin
// panelinden degistirilebilir. -1 ya da tanimsiz = sinirsiz.
//
//	media_count       — etkinlik basina medya adedi   (eskiden beri var)
//	storage_gb        — etkinlik basina toplam boyut  (yeni)
//	guest_media_count — misafir basina medya adedi    (yeni)
//	guest_storage_gb  — misafir basina toplam boyut   (yeni)
//
// Boyut hesabi uploads.size_bytes uzerinden yapilir; cope atilmis satirlar da sayilir,
// cunku dosya kalici silinene kadar S3'te durmaya devam eder.

var ErrStorageLimitReached = errors.New("storage limit reached")
var ErrGuestMediaLimitReached = errors.New("guest media limit reached")
var ErrGuestStorageLimitReached = errors.New("guest storage limit reached")

const bytesPerGB = 1024 * 1024 * 1024

// Event_option_number, core paketin options alanindaki sayisal bir secenegi okur.
// Anahtar yoksa, bos ya da sayiya cevrilemiyorsa "tanimsiz" doner; cagiran taraf
// bunu sinirsiz olarak yorumlar. Boylece eski paketlerde yeni anahtarlar hata uretmez.
func Event_option_number(eventUID string, key string) (value float64, defined bool, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("p.options->>" + sb.Var(key)).From("events e")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "purchases pu", "e.purchase_uid = pu.uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "cart_items ci", "pu.cart_uid = ci.cart_uid")
	sb.JoinWithOption(sqlbuilder.InnerJoin, "products p", "ci.product_uid = p.uid")
	sb.Where(
		sb.Equal("e.uid", eventUID),
		sb.IsNull("e.deleted_at"),
		sb.Equal("p.is_add_on", false),
	)

	rows, err := db.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("lim1", err)
		return
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return
	}

	raw := strings.TrimSpace(string(rows[0][0]))
	if raw == "" {
		return
	}

	value, parseErr := strconv.ParseFloat(raw, 64)
	if parseErr != nil {
		return 0, false, nil
	}
	return value, true, nil
}

// Event_storage_bytes, etkinligin tum medyasinin toplam boyutunu doner.
//
// Yalnizca gercek dosyalar (photo/video/voice) sayilir. Metin kayitlari disarida:
// misafir onlari PostgREST uzerinden dogrudan yaziyor ve size_bytes alanini kendisi
// doldurabilirdi; toplam disinda tutulunca uydurma bir boyutla kotayi sisirmek mumkun degil.
func Event_storage_bytes(eventUID string) (bytes int64, err error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT COALESCE(SUM(size_bytes), 0)
		FROM uploads
		WHERE event_uid = ${event_uid}
		  AND upload_type IN ('photo', 'video', 'voice')
	`, map[string]interface{}{"event_uid": eventUID})

	res, err := db.Query_one(bldr)
	if err != nil {
		err = utils.Tag_err("lim2", err)
		return
	}
	bytes, _ = strconv.ParseInt(strings.TrimSpace(string(res[0])), 10, 64)
	return
}

// Guest_upload_usage, tek bir misafirin bu etkinlikteki medya adedini ve toplam boyutunu doner.
func Guest_upload_usage(eventUID string, clientUID string) (count int, bytes int64, err error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*), COALESCE(SUM(size_bytes), 0)
		FROM uploads
		WHERE event_uid = ${event_uid}
		  AND client_uid = ${client_uid}
		  AND upload_type IN ('photo', 'video', 'voice')
	`, map[string]interface{}{"event_uid": eventUID, "client_uid": clientUID})

	res, err := db.Query_one(bldr)
	if err != nil {
		err = utils.Tag_err("lim3", err)
		return
	}
	count, _ = strconv.Atoi(strings.TrimSpace(string(res[0])))
	bytes, _ = strconv.ParseInt(strings.TrimSpace(string(res[1])), 10, 64)
	return
}

// Check_upload_limits, bir yukleme partisi kabul edilmeden once dort limiti de kontrol eder.
// newCount yeni dosya adedi, newBytes ise istemcinin bildirdigi toplam boyuttur.
// Limit asilirsa yukaridaki tipli hatalardan biri doner; cagiran taraf hata koduna cevirir.
func Check_upload_limits(eventUID string, clientUID string, newCount int, newBytes int64) error {
	// 1. Etkinlik basina medya adedi (mevcut davranis korunuyor).
	if err := Check_media_limit(eventUID, newCount); err != nil {
		return err
	}

	// 2. Etkinlik basina depolama.
	if limitGB, defined, err := Event_option_number(eventUID, "storage_gb"); err != nil {
		return err
	} else if defined && limitGB >= 0 {
		used, err := Event_storage_bytes(eventUID)
		if err != nil {
			return err
		}
		if used+newBytes > int64(limitGB*bytesPerGB) {
			return ErrStorageLimitReached
		}
	}

	// 3 ve 4. Misafir basina adet ve boyut. Ikisi de tanimsizsa sorgu hic calismaz.
	mediaLimit, mediaDefined, err := Event_option_number(eventUID, "guest_media_count")
	if err != nil {
		return err
	}
	storageLimitGB, storageDefined, err := Event_option_number(eventUID, "guest_storage_gb")
	if err != nil {
		return err
	}
	if (!mediaDefined || mediaLimit < 0) && (!storageDefined || storageLimitGB < 0) {
		return nil
	}

	usedCount, usedBytes, err := Guest_upload_usage(eventUID, clientUID)
	if err != nil {
		return err
	}
	if mediaDefined && mediaLimit >= 0 && float64(usedCount+newCount) > mediaLimit {
		return ErrGuestMediaLimitReached
	}
	if storageDefined && storageLimitGB >= 0 && usedBytes+newBytes > int64(storageLimitGB*bytesPerGB) {
		return ErrGuestStorageLimitReached
	}

	return nil
}
