package eventcleanup

import (
	"fmt"
	db "membox-serv/src/db_layer"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"
	"strings"
	"time"

	"github.com/huandu/go-sqlbuilder"
)

// ExportZipTTL, export zip'inin S3'te kalacagi sure.
// Neden: her export etkinligin tum medyasinin yeni bir kopyasi ve eskileri hic silinmiyordu; 40 GB'lik
// bir etkinlik 5 kez export edilince bucket'ta 200 GB zip birikti (Ekim 2026). Musteri istegi: zip'ler
// 3 gun sonra kendiliginden silinsin, yeni export alininca eskisi silinsin, etkinlik kapaninca hepsi silinsin.
const ExportZipTTL = 72 * time.Hour

// isExportZipKey: events/<etkinlik>/export-<rastgele>.zip. Medya bir seviye asagida
// (events/<etkinlik>/<yukleme>/<dosya>) durdugu icin misafirin "export-x.zip" adli dosyasi eslesmez.
func isExportZipKey(key string) bool {
	parts := strings.Split(strings.TrimPrefix(key, "/"), "/")
	return len(parts) == 3 && parts[0] == "events" &&
		strings.HasPrefix(parts[2], "export-") && strings.HasSuffix(parts[2], ".zip")
}

// DeleteEventExportZips, etkinligin klasorundeki butun export zip'lerini siler (etkinlik kapanirken).
func DeleteEventExportZips(eventUID string) (int, error) {
	packed, err := utils.UUID.PackUUID(eventUID)
	if err != nil {
		return 0, utils.Tag_err("ezip_pack", err)
	}
	keys, err := s3wrap.Public_s3.Ls_all("events/" + packed + "/export-")
	if err != nil {
		return 0, utils.Tag_err("ezip_ls_event", err)
	}
	return removeExportZips(keys)
}

// DeletePreviousExportZips, ayni kullanicinin ayni kapsamdaki (tum etkinlik ya da ayni album) onceki
// export zip'lerini siler; ekran yalnizca en son export'u gosterdigi icin eskilerine zaten ulasilamiyor.
// Yeni export'un job'u bu sirada hala 'running' oldugundan listeye girmez.
func DeletePreviousExportZips(userUID, eventUID, albumUID string) (int, error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT output->>'zip_path'
		FROM jobs
		WHERE name = 's3_export'
		  AND status = 'succeeded'
		  AND user_uid = ${user_uid}
		  AND input->>'event_uid' = ${event_uid}
		  AND COALESCE(input->>'album_uid', '') = ${album_uid}
		  AND COALESCE(output->>'zip_path', '') <> ''
		  AND output->>'zip_deleted_at' IS NULL
	`, map[string]interface{}{
		"user_uid":  userUID,
		"event_uid": eventUID,
		"album_uid": albumUID,
	})

	rows, err := db.Query_all(bldr)
	if err != nil {
		return 0, utils.Tag_err("ezip_previous", err)
	}
	keys := make([]string, 0, len(rows))
	for _, row := range rows {
		keys = append(keys, string(row[0]))
	}
	return removeExportZips(keys)
}

// DeleteExpiredExportZips, ExportZipTTL'i dolmus zip'leri bucket'i tarayarak siler; job satiri
// kalmamis eski zip'ler de boylece yakalanir.
func DeleteExpiredExportZips() (int, error) {
	return deleteExportZipsOlderThan(time.Now().Add(-ExportZipTTL))
}

func deleteExportZipsOlderThan(cutoff time.Time) (int, error) {
	objects, err := s3wrap.Public_s3.Ls_all_info("events/")
	if err != nil {
		return 0, utils.Tag_err("ezip_ls_all", err)
	}
	keys := []string{}
	for _, obj := range objects {
		if isExportZipKey(obj.Key) && obj.LastModified.Before(cutoff) {
			keys = append(keys, obj.Key)
		}
	}
	return removeExportZips(keys)
}

// removeExportZips, biri silinemese de digerlerine devam eder; ilk hatayi dondurur.
func removeExportZips(keys []string) (int, error) {
	removed := 0
	var firstErr error
	for _, key := range keys {
		if !isExportZipKey(key) {
			continue
		}
		if err := removeExportZip(key); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		removed++
	}
	return removed, firstErr
}

// removeExportZip, zip'i S3'ten siler ve ona isaret eden job'lara zip_deleted_at yazar; ekran bu alani
// gorunce indirme yerine yeniden export onerir. Job'larda yol "/events/..." diye bastaki egik cizgiyle
// duruyor, S3 anahtari ise onsuz.
func removeExportZip(key string) error {
	key = strings.TrimPrefix(key, "/")
	if err := s3wrap.Public_s3.Rm(key); err != nil {
		return utils.Tag_err("ezip_rm", fmt.Errorf("%s: %w", key, err))
	}

	bldr := sqlbuilder.BuildNamed(`
		UPDATE jobs
		SET output = output || jsonb_build_object('zip_deleted_at', NOW())
		WHERE name = 's3_export'
		  AND ltrim(output->>'zip_path', '/') = ${key}
		  AND output->>'zip_deleted_at' IS NULL
	`, map[string]interface{}{
		"key": key,
	})
	if err := db.Exec(bldr); err != nil {
		return utils.Tag_err("ezip_mark", fmt.Errorf("%s: %w", key, err))
	}
	return nil
}
