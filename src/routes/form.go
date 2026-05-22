package routes

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/gorilla/mux"
)

var formNames = []string{"newsletter"}

func Form_route(w http.ResponseWriter, r *http.Request) {
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

	formName := mux.Vars(r)["formName"]
	if !slices.Contains(formNames, formName) {
		err = fmt.Errorf("frmrt1")
		return
	}

	data := make(map[string]string)

	err = json.NewDecoder(r.Body).Decode(&data)
	if err != nil {
		err = fmt.Errorf("frmrt2")
		return
	}

	payload = []byte("ok")

}
