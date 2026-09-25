package dbscripts

import (
	db "membox-serv/src/db_layer"
	"membox-serv/src/utils"
	"strings"
	"time"

	"github.com/huandu/go-sqlbuilder"
)

// Misafir yuklemesinin S3'e ulastigi an: uploads.received_at (db-shell/misc/12-upload-received.sql).
//
// Satir presign aninda yaziliyor ama dosya tarayicidan dogrudan S3'e gidiyor; PUT yarida kalirsa
// (otobusteki mobil ag, 25 Eylul) sunucu bunu gormuyordu ve galeri olmayan dosyayi gri kutu olarak
// gosteriyordu. Artik presign satiri received_at NULL yazar; misafir PUT'tan sonra /confirm'u
// cagirir ya da dakikalik tarama (upload_receipt paketi) dosyayi S3'te gorunce doldurur.
// NULL satiri PostgREST rolleri hic gormez (RESTRICTIVE politika); membox-serv RLS'ten muaf
// oldugu icin kendi sorgulari asagidaki kurali acikca yazar.

// UploadCountsSQL, kotalarin ve katilimci sayiminin hangi satirlari saydigini tanimlar:
// S3'te dogrulanmis olanlar ve son bir saatte presign edilip henuz dogrulanmamis olanlar.
//
// Neden bir saat: dogrulanmamis satir ya hala yukleniyordur ya da yarida kalmistir. Yuklenmekte
// olani saymazsak ayni anda acilan presign'lar kotayi asabilir; yarim kalani sonsuza kadar
// sayarsak basarisiz denemeler misafirin 100 dosya / 4 GB hakkini yer. Bir saat sonra satir
// yarim kalmis sayilir; dosya yine de gelirse tarama onu dogrular ve tekrar sayilir.
// Ayni kural copten geri alma trigger'inda da var (check_upload_restore_quota); biri degisirse
// digeri de degismeli.
const UploadCountsSQL = "(received_at IS NOT NULL OR created_at > LOCALTIMESTAMP - INTERVAL '1 hour')"

// PendingUpload, henuz S3'te dogrulanmamis ya da yeni dogrulanmis bir yukleme satiri.
type PendingUpload struct {
	UID      string
	Value    string
	Received bool
}

// Guest_upload_by_path, misafirin kendi yuklemesini S3 yoluyla bulur. Yol bu etkinlige ve bu
// misafire ait degilse found=false; yani bir misafir baskasinin satirini dogrulayamaz ya da silemez.
func Guest_upload_by_path(eventUID string, clientUID string, value string) (u PendingUpload, found bool, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("uid", "value", "received_at IS NOT NULL").From("uploads").Where(
		sb.Equal("event_uid", eventUID),
		sb.Equal("client_uid", clientUID),
		sb.Equal("value", value),
	)

	rows, err := db.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("urc1", err)
		return
	}
	if len(rows) == 0 {
		return
	}

	u = PendingUpload{
		UID:      string(rows[0][0]),
		Value:    string(rows[0][1]),
		Received: pgBool(rows[0][2]),
	}
	return u, true, nil
}

// Mark_upload_received, satiri gorunur yapar ve boyutu S3'teki gercek boyutla degistirir.
// Presign'daki boyut tarayicinin beyaniydi; kota artik olculen bayti sayar.
func Mark_upload_received(uploadUID string, size int64) error {
	bldr := sqlbuilder.BuildNamed(`
		UPDATE uploads
		SET received_at = now(), size_bytes = ${size}
		WHERE uid = ${uid} AND received_at IS NULL
	`, map[string]interface{}{"uid": uploadUID, "size": size})

	if err := db.Exec(bldr); err != nil {
		return utils.Tag_err("urc2", err)
	}
	return nil
}

// Drop_pending_upload, dosyasi hic gelmeyecegi kesinlesmis satiri siler. Dogrulanmis satira
// dokunmaz; tarama ile /confirm ayni satir icin yarissa bile gorunur bir yukleme silinmez.
func Drop_pending_upload(uploadUID string) error {
	bldr := sqlbuilder.BuildNamed(`
		DELETE FROM uploads
		WHERE uid = ${uid} AND received_at IS NULL
	`, map[string]interface{}{"uid": uploadUID})

	if err := db.Exec(bldr); err != nil {
		return utils.Tag_err("urc3", err)
	}
	return nil
}

// Pending_uploads, en az minAge once ve en fazla maxAge once presign edilmis, henuz
// dogrulanmamis medya satirlarini eskiden yeniye doner.
func Pending_uploads(minAge time.Duration, maxAge time.Duration, limit int) (list []PendingUpload, err error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT uid, value
		FROM uploads
		WHERE received_at IS NULL
		  AND upload_type IN ('photo', 'video', 'voice')
		  AND created_at < LOCALTIMESTAMP - make_interval(secs => ${min_age})
		  AND created_at > LOCALTIMESTAMP - make_interval(secs => ${max_age})
		ORDER BY created_at ASC
		LIMIT ${limit}
	`, map[string]interface{}{
		"min_age": minAge.Seconds(),
		"max_age": maxAge.Seconds(),
		"limit":   limit,
	})

	rows, err := db.Query_all(bldr)
	if err != nil {
		err = utils.Tag_err("urc4", err)
		return
	}

	for _, row := range rows {
		list = append(list, PendingUpload{
			UID:   strings.TrimSpace(string(row[0])),
			Value: string(row[1]),
		})
	}
	return
}
