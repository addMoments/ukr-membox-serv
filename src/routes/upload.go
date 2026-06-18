package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	networkutils "membox-serv/src/network_utils"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"
	"net/http"
	"path"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type upload_routes_typ struct{}

var UploadRoutes upload_routes_typ

type presignRes struct {
	UpUrl    string `json:"upUrl"`
	FilePath string `json:"filePath"`
}

var utypes = []string{"photo", "video", "voice"}

func presignMany(clientFileNames []string, pathF func(string) string) (payload map[string]presignRes, err error) {
	payload = make(map[string]presignRes)
	reqData := clientFileNames
	s3serv := s3wrap.Public_s3

	for i := 0; i < len(reqData); i++ {
		curr := presignRes{}

		fpath := pathF(reqData[i])
		curr.UpUrl, err = s3serv.Store_presign(fpath, time.Minute)
		if err != nil {
			return
		}

		curr.FilePath = fpath

		payload[reqData[i]] = curr

	}

	return

}

func (ur upload_routes_typ) Upload(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			// Contributor limiti dolduysa frontendin ayirt edebilmesi icin
			// standart bir hata kodu ve mesaj dondur.
			if errors.Is(err, dbscripts.ErrGuestLimitReached) {
				_ = networkutils.SendErrorJSON(
					w,
					http.StatusForbidden,
					"CONTRIBUTOR_LIMIT_REACHED",
					"Contributor limit reached for this event.",
				)
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
	if !ok || claims.Role != "auth" {
		err = errors.New("unauthorized")
		return
	}

	fNames := []string{}
	err = json.NewDecoder(r.Body).Decode(&fNames)
	if err != nil {
		err = utils.Tag_err("gu2", err)
		return
	}

	purpose := mux.Vars(r)["purpose"]
	switch purpose {
	case "qr_logo":
		fallthrough
	case "event_image":
		if len(fNames) != 1 {
			err = errors.New("only one file is allowed")
			return
		}
		break

	default:
		err = errors.New("invalid upload purpose")
		return
	}

	var pathF func(string) string
	fileUUids := make(map[string]string)

	for _, fileName := range fNames {
		uuid := uuid.New().String()
		packedUUID, err := utils.UUID.PackUUID(uuid)
		if err != nil {
			err = utils.Tag_err("gu21", err)
			return
		}
		fileUUids[fileName] = packedUUID
	}

	userPackedUID, err := utils.UUID.PackUUID(claims.UserUID)
	if err != nil {
		err = utils.Tag_err("gu22", err)
		return
	}

	pathF = func(fileName string) string {
		ext := path.Ext(fileName)
		return fmt.Sprintf("/uload/%s/%s/%s%s", userPackedUID, purpose, fileUUids[fileName], ext)
	}

	payload, err = presignMany(fNames, pathF)

}

func (ur upload_routes_typ) GuestUpload(w http.ResponseWriter, r *http.Request) {
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

	})()

	fmt.Println("GuestUpload", r.Context().Value("claims"))

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized22")
		return
	}

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("gu1", err)
		return
	}

	utype := mux.Vars(r)["utype"]
	if !slices.Contains(utypes, utype) {
		err = errors.New("invalid upload type")
		return
	}

	if utype == "voice" {
		has_voice, err := dbscripts.Has_feature(eventUID, dbscripts.FeatureVoice)
		if err != nil {
			stat_code = http.StatusForbidden
			err = utils.Tag_err("gu1.5", err)
			return
		}
		if !has_voice {
			err = errors.New("feature not purchased")
			stat_code = http.StatusForbidden
			return
		}
	}

	reqData := []string{}
	err = json.NewDecoder(r.Body).Decode(&reqData)
	if err != nil {
		err = utils.Tag_err("gu2", err)
		return
	}

	err = dbscripts.Check_media_limit(eventUID, len(reqData))
	if err != nil {
		stat_code = http.StatusForbidden
		err = utils.Tag_err("gu2.1", err)
		return
	}

	// Paylasim aninda limiti contributor bazinda kontrol et.
	err = dbscripts.Check_contributor_limit_for_upload(eventUID, claims.UserUID)
	if err != nil {
		stat_code = http.StatusForbidden
		err = utils.Tag_err("gu2.2", err)
		return
	}

	uuidMap := make(map[string]string)
	packedUUIDMap := make(map[string]string)

	for _, fileName := range reqData {
		uuid := uuid.New().String()
		uuidMap[fileName] = uuid
		packedUUID, err := utils.UUID.PackUUID(uuid)
		if err != nil {
			err = utils.Tag_err("gu21", err)
			return
		}
		packedUUIDMap[fileName] = packedUUID
	}

	pathF := func(fileName string) string {
		return fmt.Sprintf("/events/%s/%s/%s", eventPackedUID, packedUUIDMap[fileName], fileName)
	}

	payload, err = presignMany(reqData, pathF)
	if err != nil {
		err = utils.Tag_err("gu3", err)
		return
	}

	err = networkutils.SendJson(payload, w)
	if err != nil {
		err = utils.Tag_err("gu3.1", err)
		return
	}

	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("uploads")
	ib.Cols(
		"uid",
		"upload_type",
		"client_uid",
		"event_uid",
		"value",
	)

	for i := 0; i < len(reqData); i++ {
		ib.Values(
			uuidMap[reqData[i]],
			utype,
			claims.UserUID,
			eventUID,
			pathF(reqData[i]),
		)
	}

	err = db.Exec(ib)

}

// Delete permanently deletes an upload from the database and S3.
// Requires the user to be an admin of the event associated with the upload.
func (ur upload_routes_typ) Delete(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(
					w,
					http.StatusGone,
					"EVENT_CLOSED",
					networkutils.EventClosedMessage(r),
				)
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
	if !ok || claims.Role != "auth" {
		err = errors.New("unauthorized")
		stat_code = http.StatusUnauthorized
		return
	}

	uploadPackedUID := mux.Vars(r)["uploadPackedUid"]
	uploadUID, err := utils.UUID.UnpackUUID(uploadPackedUID)
	if err != nil {
		err = utils.Tag_err("ud1", err)
		return
	}

	// Query the upload to get its event_uid and S3 path
	uploadBldr := sqlbuilder.BuildNamed(`
		SELECT event_uid, value
		FROM uploads
		WHERE uid = ${upload_uid}
	`, map[string]interface{}{
		"upload_uid": uploadUID,
	})

	uploadRes, queryErr := db.Query_one(uploadBldr)
	if queryErr != nil {
		err = utils.Tag_err("ud2", queryErr)
		return
	}

	if len(uploadRes) == 0 {
		payload = map[string]interface{}{
			"success": true,
			"already_deleted": true,
		}
		stat_code = http.StatusOK
		return
	}

	if string(uploadRes[0]) == "" {
		err = errors.New("upload not found")
		stat_code = http.StatusNotFound
		return
	}

	eventUID := string(uploadRes[0])
	s3Path := string(uploadRes[1])

	// Verify user is admin of this event
	isAdmin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("ud3", err)
		return
	}
	if !isAdmin {
		err = errors.New("forbidden")
		stat_code = http.StatusForbidden
		return
	}

	// Delete the upload from the database
	deleteBldr := sqlbuilder.BuildNamed(`
		DELETE FROM uploads
		WHERE uid = ${upload_uid}
	`, map[string]interface{}{
		"upload_uid": uploadUID,
	})

	if err = db.Exec(deleteBldr); err != nil {
		err = utils.Tag_err("ud4", err)
		return
	}

	// Delete from S3 (fire and forget - log error but don't fail the operation)
	if s3Path != "" {
		if s3Err := s3wrap.Public_s3.Rm(s3Path); s3Err != nil {
			// Log the error but don't fail the operation
			fmt.Printf("[upload.delete] S3 deletion failed for %s: %v\n", s3Path, s3Err)
		}
	}

	payload = map[string]interface{}{
		"success": true,
	}
	stat_code = http.StatusOK
}
