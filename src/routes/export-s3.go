package routes

import (
	"errors"
	"fmt"
	"io"
	"log"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"path"
	"strings"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
	"github.com/xuri/excelize/v2"
)

func Export_s3(input types.Js_object, user_uid string) (output types.Js_object, err error) {
	log.Printf("[export-s3] starting export for user %s", user_uid)
	output = types.Js_object{}

	event_uid, ok := input["event_uid"].(string)
	if !ok {
		err = fmt.Errorf("event_uid is required")
		log.Printf("[export-s3] error: event_uid is required")
		return
	}

	log.Printf("[export-s3] exporting event %s", event_uid)

	packedEventUID, err := utils.UUID.PackUUID(event_uid)
	if err != nil {
		log.Printf("[export-s3] failed to pack event UUID: %v", err)
		return
	}

	is_admin, err := dbscripts.Is_events_admin(event_uid, user_uid)
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

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select(
		"uploads.uid",
		"uploads.upload_type",
		"uploads.client_uid",
		"uploads.created_at",
		"uploads.value",
		"participants.name",
	).From("uploads").Join(
		"participants",
		"uploads.client_uid = participants.uid",
	).Where(
		sb.Equal("uploads.event_uid", event_uid),
		sb.IsNull("uploads.trashed_at"),
	)

	uploads, err := db.Query_all(sb)
	if err != nil {
		log.Printf("[export-s3] failed to query uploads: %v", err)
		return
	}

	log.Printf("[export-s3] found %d uploads to export", len(uploads))

	uploadsExcel := excelize.NewFile()
	uploadsSheet := "uploads"
	uploadsExcel.NewSheet(uploadsSheet)
	uploadsExcel.DeleteSheet("Sheet1")

	guestsExcel := excelize.NewFile()
	guestsSheet := "guests"
	guestsExcel.NewSheet(guestsSheet)
	guestsExcel.DeleteSheet("Sheet1")

	err = utils.WriteRow(uploadsExcel, uploadsSheet, []interface{}{"Upload ID", "Guest Name", "Value", "Type", "Guest ID", "Created At"})
	if err != nil {
		log.Printf("[export-s3] failed to write Excel header row: %v", err)
		return
	}

	randUUID := uuid.New().String()
	packedRandUUID, err := utils.UUID.PackUUID(randUUID)
	if err != nil {
		log.Printf("[export-s3] failed to pack random UUID: %v", err)
		return
	}

	exportPath := fmt.Sprintf("/events/%s/export-%s.zip", packedEventUID, packedRandUUID)
	log.Printf("[export-s3] creating zip at %s", exportPath)
	zipStreamer := s3wrap.Public_s3.New_zip_streamer(exportPath)
	defer zipStreamer.Close()

	simpleUidMap := make(map[string]map[string]string)
	simpleUidSeq := make(map[string]int)

	simplifyUid := func(uid string, namespace string) string {
		namespaceMap, ok := simpleUidMap[namespace]
		if !ok {
			namespaceMap = make(map[string]string)
			simpleUidMap[namespace] = namespaceMap
		}

		if _, ok := namespaceMap[uid]; ok {
			return namespaceMap[uid]
		}

		simpleUid := fmt.Sprintf("%05d", simpleUidSeq[namespace]+1)
		namespaceMap[uid] = simpleUid
		simpleUidSeq[namespace] = simpleUidSeq[namespace] + 1
		return simpleUid
	}

	for i, uploadRow := range uploads {
		uploadUID := string(uploadRow[0])
		uploadType := string(uploadRow[1])
		clientUID := string(uploadRow[2])
		createdAt := string(uploadRow[3])
		value := string(uploadRow[4])
		guestName := string(uploadRow[5])

		log.Printf("[export-s3] processing upload %d/%d: %s (type: %s)", i+1, len(uploads), uploadUID, uploadType)

		if uploadType != "text" {
			var file io.ReadCloser
			file, err = s3wrap.Public_s3.Get(value)
			if err != nil {
				if aerr, ok := err.(awserr.Error); ok && aerr.Code() == s3.ErrCodeNoSuchKey {
					log.Printf("[export-s3] skipping missing S3 file %s (NoSuchKey)", value)
					err = nil
					continue
				}
				log.Printf("[export-s3] failed to get file from S3 %s: %v", value, err)
				return
			}
			defer file.Close()

			written := int64(0)

			fileName := path.Base(value)
			zipPath := fmt.Sprintf("/uploads/%s-%s", simplifyUid(uploadUID, "uploads"), fileName)
			written, err = zipStreamer.Add_file(zipPath, file)
			if err != nil {
				log.Printf("[export-s3] failed to add file to zip %s: %v", zipPath, err)
				return
			}

			log.Printf("[export-s3] added file to zip %s: %d bytes", zipPath, written)

			value = zipPath
		}

		if strings.HasPrefix(guestName, "guest-") {
			guestName = fmt.Sprintf("%s-anon", simplifyUid(clientUID, "guests"))
		}

		utils.WriteRow(uploadsExcel, uploadsSheet, []interface{}{simplifyUid(uploadUID, "uploads"), guestName, value, uploadType, simplifyUid(clientUID, "guests"), createdAt})
	}

	log.Printf("[export-s3] writing uploads.xlsx to zip")
	w, err := zipStreamer.Open_writer("/uploads.xlsx")
	if err != nil {
		log.Printf("[export-s3] failed to open writer for uploads.xlsx: %v", err)
		return
	}
	defer w.Close()

	_, err = uploadsExcel.WriteTo(w)
	if err != nil {
		log.Printf("[export-s3] failed to write Excel file: %v", err)
		w.CloseWithError(err)
		return
	}

	sb = sqlbuilder.NewSelectBuilder()
	sb.Select("image").From("events").Where(
		sb.Equal("uid", event_uid),
		sb.IsNull("deleted_at"),
	)

	res, err := db.Query_one(sb)
	if err != nil {
		log.Printf("[export-s3] failed to query event image: %v", err)
		return
	}

	bannerPath := string(res[0])
	if bannerPath == "" {
		log.Printf("[export-s3] skipping banner: event has no image")
	} else {
		reader, bannerErr := s3wrap.Public_s3.Get(bannerPath)
		if bannerErr != nil {
			if aerr, ok := bannerErr.(awserr.Error); ok && aerr.Code() == s3.ErrCodeNoSuchKey {
				log.Printf("[export-s3] skipping missing banner image %s (NoSuchKey)", bannerPath)
			} else {
				log.Printf("[export-s3] failed to get event image: %v", bannerErr)
			}
		} else {
			defer reader.Close()
			zipStreamer.Add_file(fmt.Sprintf("/banner%s", path.Ext(bannerPath)), reader)
		}
	}

	log.Printf("[export-s3] export completed successfully: %s", s3wrap.Public_s3.Url(exportPath))
	output["zip_path"] = exportPath
	return
}
