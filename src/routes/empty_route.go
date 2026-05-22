package routes

import (
	networkutils "membox-serv/src/network_utils"
	"net/http"
)

type empty_routes_typ struct{}

var empty_route empty_routes_typ

func (rfrnc empty_routes_typ) Empty_route_byt(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload = []byte{}
	var err error

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		if stat_code == 0 {
			stat_code = 200
		}
		w.WriteHeader(stat_code)
		w.Write(payload)
	})()

}

func (rfrnc empty_routes_typ) Empty_route_json(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		networkutils.SendJson(payload, w)
	})()

}

func (rfrnc empty_routes_typ) Empty_route_postredirect(w http.ResponseWriter, r *http.Request) {
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
			stat_code = http.StatusCreated
		}

		w.WriteHeader(stat_code)
		w.Write([]byte("goto:" + redr_url))
	})()

}

func (rfrnc empty_routes_typ) Empty_route_getredirect(w http.ResponseWriter, r *http.Request) {
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

		http.Redirect(w, r, redr_url, stat_code)
	})()

}
