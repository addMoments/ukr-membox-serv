package eventcleanup

import (
	"encoding/json"
	db "membox-serv/src/db_layer"
	"membox-serv/src/utils"

	"github.com/huandu/go-sqlbuilder"
)

// RetryFailedEventUploadPurges, DB/S3 atomik olmadigi icin yarida kalmis medya
// temizliklerini tekrar dener. Fonksiyon sadece snapshot'ta sakli media_paths
// uzerinden S3 silme yapar; event, participants veya uploads tablolarina
// dokunmaz. Bir route veya cron tarafindan cagrilmadikca mevcut davranisi
// degistirmez.
func RetryFailedEventUploadPurges() (int, error) {
	rows, err := queryFailedPurgeSnapshots()
	if err != nil {
		return 0, err
	}

	retried := 0
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}

		eventUID := string(row[0])
		mediaPaths := []string{}
		if len(row[1]) > 0 {
			if err := json.Unmarshal(row[1], &mediaPaths); err != nil {
				return retried, utils.Tag_err("eur_parse_media_paths", err)
			}
		}

		if err := deleteS3Media(mediaPaths); err != nil {
			_ = markPurgeFailed(eventUID, err)
			return retried, err
		}
		if err := markPurgeFinished(eventUID); err != nil {
			return retried, err
		}
		retried++
	}

	return retried, nil
}

func queryFailedPurgeSnapshots() ([][][]byte, error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT event_uid::text, media_paths::text
		FROM event_upload_snapshots
		WHERE purge_finished_at IS NULL
		  AND purge_error IS NOT NULL
		ORDER BY purge_started_at ASC NULLS FIRST, captured_at ASC
	`, nil)

	rows, err := db.Query_all(bldr)
	if err != nil {
		return nil, utils.Tag_err("eur_failed_snapshots", err)
	}
	return rows, nil
}
