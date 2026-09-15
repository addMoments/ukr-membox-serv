package routes

import (
	"errors"
	"fmt"
	dbscripts "membox-serv/src/db_scripts"
	"membox-serv/src/utils"
	"net/http"

	"github.com/gorilla/mux"
)

type linkHandlerTyp struct{}

var LinkHandler linkHandlerTyp

func (lh linkHandlerTyp) HandleLink(w http.ResponseWriter, r *http.Request) {
	var is_temp bool
	var redr_url string
	var err error
	var stat_code int

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		if stat_code == 0 {
			if is_temp {
				stat_code = http.StatusTemporaryRedirect
			} else {
				stat_code = http.StatusPermanentRedirect
			}
		}

		if redr_url == "" {
			redr_url = "/"
		}

		http.Redirect(w, r, redr_url, stat_code)
	})()

	path := mux.Vars(r)["path"]
	fmt.Println("path", path)

	if len(path) < 2 {
		return
	}

	switch path[0] {
	case 'q':
		packedUUID := path[1:]
		redr_url = "/guest/" + packedUUID
		is_temp = false
	case 'a':
		// Album linki: /l/a<packedAlbumUid> -> /guest/<packedEvent>/album/<packedAlbum>.
		// Etkinlik URL'de ikinci segment olmak zorunda: frontend X-Event basligini oradan
		// aliyor (client/core.ts guestPackedEventUid). Kapali/yuklemesi kapali album da
		// yonlendirilir; SPA "album kapali" ekranini gosterir, host acinca ayni link calisir.
		packedAlbum := path[1:]
		var albumUID string
		albumUID, err = utils.UUID.UnpackUUID(packedAlbum)
		if err != nil {
			err = nil
			return
		}
		var album dbscripts.Album
		album, err = dbscripts.Get_album(albumUID)
		if err != nil {
			err = nil
			return
		}
		var packedEvent string
		packedEvent, err = utils.UUID.PackUUID(album.EventUID)
		if err != nil {
			err = nil
			return
		}
		redr_url = "/guest/" + packedEvent + "/album/" + packedAlbum
		// Album silinip yeniden acilamaz ama etkinlik kapanabilir; kalici redirect cache'i
		// sorun olmasin diye gecici.
		is_temp = true
	case 'c':
		err = errors.New("collaborator invitation not implemented")
		return
	}

}
