package dbscripts

import (
	"errors"
	db "membox-serv/src/db_layer"
	"membox-serv/src/utils"

	"github.com/huandu/go-sqlbuilder"
)

// Album, albums tablosunun handler'larin ihtiyac duydugu kolonlari.
type Album struct {
	UID              string
	EventUID         string
	Name             string
	Privacy          string
	Passcode         string
	GuestUpload      bool
	GuestView        bool
	GuestDownloadAll bool
	IsDefault        bool
	Deleted          bool
}

var ErrAlbumNotFound = errors.New("album not found")

// ErrAlbumClosed: album silinmis ya da misafir yuklemesi kapali -> misafire hic gorunmez (karar 12).
var ErrAlbumClosed = errors.New("album is closed")

// ErrPasscodeRequired / ErrPasscodeInvalid: protected album, token'da acik degil.
var ErrPasscodeRequired = errors.New("passcode required")
var ErrPasscodeInvalid = errors.New("passcode invalid")

// ErrAlbumNotOpened: private album linkten acilmadan (token'da yokken) kullanilmak istendi.
var ErrAlbumNotOpened = errors.New("album not opened")

// ErrDefaultAlbum: General silinemez.
var ErrDefaultAlbum = errors.New("default album cannot be deleted")

func Get_album(albumUID string) (album Album, err error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT uid, event_uid, name, privacy, COALESCE(passcode, ''),
		       guest_upload, guest_view, guest_download_all, is_default,
		       (deleted_at IS NOT NULL)
		FROM albums
		WHERE uid = ${album_uid}
	`, map[string]interface{}{"album_uid": albumUID})

	rows, err := db.Query_all(bldr)
	if err != nil {
		err = utils.Tag_err("alb1", err)
		return
	}
	if len(rows) == 0 {
		err = ErrAlbumNotFound
		return
	}
	r := rows[0]
	album = Album{
		UID:              string(r[0]),
		EventUID:         string(r[1]),
		Name:             string(r[2]),
		Privacy:          string(r[3]),
		Passcode:         string(r[4]),
		GuestUpload:      pgBool(r[5]),
		GuestView:        pgBool(r[6]),
		GuestDownloadAll: pgBool(r[7]),
		IsDefault:        pgBool(r[8]),
		Deleted:          pgBool(r[9]),
	}
	return
}

// Default_album, etkinligin General albumunu dondurur; migration/trigger'a ragmen
// yoksa (ornegin cok eski bir yolla acilmis etkinlik) olusturur.
func Default_album(eventUID string) (albumUID string, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("uid").From("albums").Where(
		sb.Equal("event_uid", eventUID),
		sb.Equal("is_default", true),
		sb.IsNull("deleted_at"),
	)
	rows, err := db.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("alb2", err)
		return
	}
	if len(rows) > 0 {
		albumUID = string(rows[0][0])
		return
	}

	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("albums")
	ib.Cols("event_uid", "name", "is_default")
	ib.Values(eventUID, "General", true)
	ib.SQL("ON CONFLICT DO NOTHING RETURNING uid")
	res, err := db.Query_one(ib)
	if err != nil {
		// Yaris: baska bir istek ayni anda yaratmis olabilir; tekrar oku.
		rows, err2 := db.Query_all(sb)
		if err2 == nil && len(rows) > 0 {
			return string(rows[0][0]), nil
		}
		err = utils.Tag_err("alb3", err)
		return
	}
	albumUID = string(res[0])
	return
}

// Event_guest_gallery, etkinlik duzeyi galeri anahtarini (settings.guest_gallery) okur.
// Anahtar yoksa FALSE: mevcut etkinliklerde misafir galeri gormeye devam etmez.
func Event_guest_gallery(eventUID string) (enabled bool, err error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT COALESCE((settings->>'guest_gallery')::boolean, FALSE)
		FROM events
		WHERE uid = ${event_uid}
	`, map[string]interface{}{"event_uid": eventUID})
	res, err := db.Query_one(bldr)
	if err != nil {
		err = utils.Tag_err("alb4", err)
		return
	}
	enabled = pgBool(res[0])
	return
}

// Guest_album_access, misafirin albumu gorup goremeyecegini tek yerde karara baglar.
// RLS'teki albums_guest_select ile birebir ayni kural:
//
//	silinmemis + yukleme acik + (public | token'da acik)
//
// Protected album token'da yoksa ErrPasscodeRequired, private ise ErrAlbumNotOpened doner;
// ikisi de "open" ucundan gecilerek asilir.
func Guest_album_access(album Album, eventUID string, openedAlbums []string) error {
	if album.Deleted || album.EventUID != eventUID || !album.GuestUpload {
		return ErrAlbumClosed
	}
	if album.Privacy == "public" {
		return nil
	}
	for _, a := range openedAlbums {
		if a == album.UID {
			return nil
		}
	}
	if album.Privacy == "protected" {
		return ErrPasscodeRequired
	}
	return ErrAlbumNotOpened
}

// Trash_album_uploads, albumdeki cope atilmamis tum medyayi cope tasir ve sayisini doner.
func Trash_album_uploads(albumUID string) (count int, err error) {
	bldr := sqlbuilder.BuildNamed(`
		WITH moved AS (
			UPDATE uploads
			SET trashed_at = NOW()
			WHERE album_uid = ${album_uid}
			  AND trashed_at IS NULL
			RETURNING uid
		)
		SELECT COUNT(*) FROM moved
	`, map[string]interface{}{"album_uid": albumUID})
	res, err := db.Query_one(bldr)
	if err != nil {
		err = utils.Tag_err("alb5", err)
		return
	}
	count = pgInt(res[0])
	return
}

func pgBool(b []byte) bool {
	s := string(b)
	return s == "true" || s == "t" || s == "1"
}

func pgInt(b []byte) int {
	n := 0
	for _, c := range string(b) {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}
