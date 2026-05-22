package routes

import (
	"errors"
	"fmt"
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
	case 'c':
		err = errors.New("collaborator invitation not implemented")
		return
	}

}
