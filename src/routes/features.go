package routes

import (
	"encoding/json"
	"errors"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/utils"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type features_routes_typ struct{}

var FeaturesRoute features_routes_typ

// eventFeatures queries the union of all granted_features integers across all
// products in the event's purchase cart.
func eventFeatures(eventUID string) (featureIDs []int, err error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT DISTINCT unnest(p.granted_features) AS feature_id
		FROM events e
		INNER JOIN purchases pu ON e.purchase_uid = pu.uid
		INNER JOIN cart_items ci ON pu.cart_uid = ci.cart_uid
		INNER JOIN products p ON ci.product_uid = p.uid
		WHERE e.uid = ${event_uid}
		  AND e.deleted_at IS NULL
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	rows, err := db.Query_all(bldr)
	if err != nil {
		return
	}

	featureIDs = []int{}
	for _, row := range rows {
		var id int
		var strVal = string(row[0])
		for _, b := range strVal {
			if b >= '0' && b <= '9' {
				id = id*10 + int(b-'0')
			}
		}
		featureIDs = append(featureIDs, id)
	}
	return
}

// Private returns the feature IDs for an event. Requires auth JWT and admin role.
// GET /api/event/:eventPackedUid/features
func (fr features_routes_typ) Private(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var err error

	defer func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
		}
	}()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		stat_code = http.StatusUnauthorized
		return
	}

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("feat1", err)
		return
	}

	is_admin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("feat2", err)
		return
	}
	if !is_admin {
		err = errors.New("unauthorized")
		stat_code = http.StatusForbidden
		return
	}

	featureIDs, err := eventFeatures(eventUID)
	if err != nil {
		err = utils.Tag_err("feat3", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(featureIDs)
}

// Public returns the feature IDs for an event. No auth required (used by guest pages).
// GET /api/event/:eventPackedUid/public-features
func (fr features_routes_typ) Public(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var err error

	defer func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
		}
	}()

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("pfeat1", err)
		return
	}

	featureIDs, err := eventFeatures(eventUID)
	if err != nil {
		err = utils.Tag_err("pfeat2", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(featureIDs)
}
