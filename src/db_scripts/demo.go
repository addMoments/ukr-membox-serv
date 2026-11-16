package dbscripts

import (
	"encoding/json"
	db "membox-serv/src/db_layer"
	"membox-serv/src/utils"
	"strings"

	"github.com/huandu/go-sqlbuilder"
)

// Demo etkinligi (musteri 14 Kasim): potansiyel musterilere gosterilen, linki her zaman acik ornek etkinlik.
//
// Hangi etkinliklerin demo oldugu site_settings 'demo_events' anahtarinda bir uid dizisi olarak durur
// (23-demo-event.sql). site_settings PostgREST rollerine kapali; host settings alanini yazabildigi icin
// isaret orada tutulmadi, yoksa herhangi bir host kendi etkinligini demo yapabilirdi.
//
// Demo kurallari:
//   - etkinligin sahibi ve ortak yoneticileri disindakilerin yuklemeleri (foto, video, ses, yazi)
//     DemoGuestUploadTTL sonra kalici silinir (event_cleanup.DeleteExpiredDemoGuestUploads);
//   - sahip olmayan bir ziyaretci ayni anda en fazla DemoGuestMediaLimit dosya tutabilir;
//   - linkin hep acik kalmasi tarihle saglanir: migration active_until/storage_until'i ileri atar.

const demoEventsKey = "demo_events"

// DemoGuestMediaLimit, demo etkinliginde sahip olmayan bir ziyaretcinin tutabilecegi foto/video/ses sayisi.
// Musteri: "Cant upload more than 30 photos." Dosyalar bir saatte silindigi icin hak bir saatte yenilenir.
const DemoGuestMediaLimit = 30

// Demo_event_uids, demo isaretli etkinliklerin uid'lerini doner (anahtar yoksa bos).
func Demo_event_uids() ([]string, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("value::text").From("site_settings").Where(sb.Equal("key", demoEventsKey))
	rows, err := db.Query_all(sb)
	if err != nil {
		return nil, utils.Tag_err("demo1", err)
	}
	if len(rows) == 0 || len(rows[0]) == 0 {
		return nil, nil
	}
	var uids []string
	if err := json.Unmarshal(rows[0][0], &uids); err != nil {
		return nil, utils.Tag_err("demo2", err)
	}
	return uids, nil
}

// Is_demo_event, etkinligin demo isaretli olup olmadigini doner.
func Is_demo_event(eventUID string) (bool, error) {
	uids, err := Demo_event_uids()
	if err != nil {
		return false, err
	}
	for _, uid := range uids {
		if strings.EqualFold(strings.TrimSpace(uid), eventUID) {
			return true, nil
		}
	}
	return false, nil
}

// Event_admins, etkinligin sahip ve ortak yonetici kullanici uid'lerini doner.
func Event_admins(eventUID string) ([]string, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("unnest(admins)::text").From("events").Where(sb.Equal("uid", eventUID))
	rows, err := db.Query_all(sb)
	if err != nil {
		return nil, utils.Tag_err("demo3", err)
	}
	admins := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) > 0 && len(row[0]) > 0 {
			admins = append(admins, string(row[0]))
		}
	}
	return admins, nil
}

// Check_demo_guest_limit, demo etkinliginde sahip olmayan ziyaretcinin yeni dosyalarla
// DemoGuestMediaLimit'i asip asmayacagina bakar; asarsa ErrGuestMediaLimitReached doner
// (frontend bu kodu "yukleyebileceginiz dosya sayisina ulastiniz" diye gosteriyor).
func Check_demo_guest_limit(eventUID string, clientUID string, newCount int) error {
	usedCount, _, err := Guest_upload_usage(eventUID, clientUID)
	if err != nil {
		return err
	}
	if usedCount+newCount > DemoGuestMediaLimit {
		return ErrGuestMediaLimitReached
	}
	return nil
}
