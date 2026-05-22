package routes

import (
	"errors"
	"fmt"
	"membox-serv/src/auth"
	dbscripts "membox-serv/src/db_scripts"
	networkutils "membox-serv/src/network_utils"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"
	"net/http"

	"github.com/gorilla/mux"
)

func Calc_size_route(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		networkutils.SendJson(payload, w)
	})()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		return
	}

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("gu1", err)
		return
	}

	is_admin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("gu2", err)
		return
	}
	if !is_admin {
		err = errors.New("unauthorized")
		return
	}

	eventFolder := fmt.Sprintf("events/%s", eventPackedUID)

	size, err := s3wrap.Public_s3.Calc_size(eventFolder)
	if err != nil {
		err = utils.Tag_err("gu3", err)
		return
	}

	payload = map[string]interface{}{
		"size_mb": size,
	}
}
