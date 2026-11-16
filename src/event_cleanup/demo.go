package eventcleanup

import (
	"fmt"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"
	"time"

	"github.com/huandu/go-sqlbuilder"
)

// DemoGuestUploadTTL, demo etkinliginde misafir yuklemelerinin silinmeden once kalacagi sure.
// Musteri 14 Kasim: "Uploaded photos except us will be deleted after 1 hour."
const DemoGuestUploadTTL = time.Hour

// DeleteExpiredDemoGuestUploads, demo etkinliklerinde sahip ve ortak yoneticiler disindakilerin
// DemoGuestUploadTTL'den eski yuklemelerini (foto, video, ses, yazi; copte olsun olmasin) kalici siler.
// storage_cron birkac dakikada bir cagirir. Silinen satir sayisini doner.
//
// Neden hepsi: demo linki herkese acik; ziyaretcinin biraktigi yazi ya da ses de bir saat sonra
// sonraki ziyaretcilere gorunmemeli. Sahibin yuklemeleri (ornek fotograflar) hic silinmez.
func DeleteExpiredDemoGuestUploads() (int, error) {
	eventUIDs, err := dbscripts.Demo_event_uids()
	if err != nil {
		return 0, err
	}
	total := 0
	for _, eventUID := range eventUIDs {
		keep, err := auth.EventAdminParticipantUIDs(eventUID)
		if err != nil {
			return total, err
		}
		if len(keep) == 0 {
			// Sahibi okunamayan etkinlikte kimin yuklemesi oldugu bilinemez; hicbir sey silinmez.
			fmt.Printf("[demo] event %s has no admins, skipped\n", eventUID)
			continue
		}
		dlb := sqlbuilder.NewDeleteBuilder()
		dlb.DeleteFrom("uploads").Where(
			dlb.Equal("event_uid", eventUID),
			dlb.NotIn("client_uid", db.Interface_ar(keep)...),
			// created_at zaman dilimsiz ve varsayilani CURRENT_TIMESTAMP (oturum saati); karsilastirma
			// da ayni saatle yapilsin diye sinir SQL'de hesaplanir.
			fmt.Sprintf("created_at < LOCALTIMESTAMP - INTERVAL '%d seconds'", int(DemoGuestUploadTTL.Seconds())),
		)
		dlb.SQL("RETURNING value, upload_type::text")
		n, err := deleteRowsWithFiles(dlb, "demo_expired")
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// deleteRowsWithFiles, deleteTrashRows gibi satirlari siler ve dosyalarini kaldirir; farki yazi
// kayitlarini da kapsamasi: onlarin value alani dosya yolu degil metin, S3'e gidilmez.
func deleteRowsWithFiles(dlb *sqlbuilder.DeleteBuilder, tag string) (int, error) {
	rows, err := db.Query_all(dlb)
	if err != nil {
		return 0, utils.Tag_err(tag, err)
	}
	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) < 2 || string(row[1]) == "text" {
			continue
		}
		if p := string(row[0]); p != "" {
			paths = append(paths, p)
		}
	}
	covers, err := albumCoverPaths(paths)
	if err != nil {
		fmt.Printf("[demo] %s: album covers lookup failed, S3 files kept: %v\n", tag, err)
		return len(rows), nil
	}
	for _, p := range paths {
		if covers[p] {
			continue
		}
		if rmErr := s3wrap.Public_s3.Rm(p); rmErr != nil {
			fmt.Printf("[demo] %s: S3 deletion failed for %s: %v\n", tag, p, rmErr)
		}
	}
	return len(rows), nil
}
