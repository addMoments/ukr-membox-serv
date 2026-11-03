package eventcleanup

import (
	"fmt"
	db "membox-serv/src/db_layer"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"
	"time"

	"github.com/huandu/go-sqlbuilder"
)

// TrashTTL, cope atilan foto ve videolarin kalici silinmeden once copte kalacagi sure.
// Neden: musteri 2 Kasim (2.26) "Trash Bin: auto delete after 2 weeks". Cop yalnizca foto/video:
// misafir defterinde gizlenen ses ve yazili mesajlar da trashed_at tasiyor ama onlar "gizli",
// cop degil; host onlari Guestbook sayfasindan geri acabilir, o yuzden burada hic silinmez.
const TrashTTL = 14 * 24 * time.Hour

// DeleteTrashedMedia, bir etkinligin copteki foto/videolarini kalici siler; uploadUIDs bossa tumunu.
// Copte olmayan ya da baska etkinlige ait uid'ler sessizce atlanir. Silinen satir sayisini doner.
func DeleteTrashedMedia(eventUID string, uploadUIDs []string) (int, error) {
	dlb := sqlbuilder.NewDeleteBuilder()
	dlb.DeleteFrom("uploads").Where(
		dlb.Equal("event_uid", eventUID),
		dlb.IsNotNull("trashed_at"),
		dlb.In("upload_type", "photo", "video"),
	)
	if len(uploadUIDs) > 0 {
		dlb.Where(dlb.In("uid", db.Interface_ar(uploadUIDs)...))
	}
	dlb.SQL("RETURNING value")
	return deleteTrashRows(dlb, "etr_delete")
}

// DeleteExpiredTrash, TrashTTL'den uzun suredir copte duran foto/videolari butun etkinliklerde siler.
// storage_cron saatlik cagirir.
func DeleteExpiredTrash() (int, error) {
	dlb := sqlbuilder.NewDeleteBuilder()
	dlb.DeleteFrom("uploads").Where(
		dlb.IsNotNull("trashed_at"),
		dlb.LessEqualThan("trashed_at", time.Now().Add(-TrashTTL)),
		dlb.In("upload_type", "photo", "video"),
	)
	dlb.SQL("RETURNING value")
	return deleteTrashRows(dlb, "etr_expired")
}

// deleteTrashRows satirlari siler, sonra dosyalarini S3'ten kaldirir. Bir albume kapak yapilmis
// fotografin dosyasi (albums.cover ayni yolu gosterir) S3'te birakilir, yoksa kapak kirilirdi.
// S3 hatasi islemi durdurmaz: satir silindi, dosya en kotu ihtimalle sahipsiz kalir (tek silmeyle ayni).
func deleteTrashRows(dlb *sqlbuilder.DeleteBuilder, tag string) (int, error) {
	rows, err := db.Query_all(dlb)
	if err != nil {
		return 0, utils.Tag_err(tag, err)
	}

	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		if p := string(row[0]); p != "" {
			paths = append(paths, p)
		}
	}
	covers, err := albumCoverPaths(paths)
	if err != nil {
		// Kapaklar okunamadiysa hicbir dosyaya dokunma; satirlar zaten silindi.
		fmt.Printf("[trash] %s: album covers lookup failed, S3 files kept: %v\n", tag, err)
		return len(rows), nil
	}
	for _, p := range paths {
		if covers[p] {
			continue
		}
		if rmErr := s3wrap.Public_s3.Rm(p); rmErr != nil {
			fmt.Printf("[trash] %s: S3 deletion failed for %s: %v\n", tag, p, rmErr)
		}
	}
	return len(rows), nil
}

func albumCoverPaths(paths []string) (map[string]bool, error) {
	covers := map[string]bool{}
	if len(paths) == 0 {
		return covers, nil
	}
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("DISTINCT cover").From("albums").Where(sb.In("cover", db.Interface_ar(paths)...))
	rows, err := db.Query_all(sb)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		covers[string(row[0])] = true
	}
	return covers, nil
}
