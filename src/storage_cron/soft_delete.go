package storagecron

import (
	"fmt"

	db "membox-serv/src/db_layer"
	eventcleanup "membox-serv/src/event_cleanup"
	"membox-serv/src/utils"

	"github.com/huandu/go-sqlbuilder"
)

const enableExpiredEventMediaPurge = true

// softDeleteExpired, storage_until'i gecmis ve henuz silinmemis eventleri
// kapatir. Risk azaltmak icin cron medya purge yolu feature flag arkasindadir:
// flag kapaliyken eski tek UPDATE davranisi aynen korunur; flag acilinca ayni
// eventler ortak snapshot + DB/S3 medya purge fonksiyonundan gecirilir.
//
// dryRun=true: WHERE clausu ile ayni filtreyi SELECT olarak calistirir, sayar,
// hicbir sey yazmaz.
func softDeleteExpired(dryRun bool) (int, error) {
	if dryRun {
		return countExpired()
	}
	if enableExpiredEventMediaPurge {
		return purgeExpired()
	}

	bldr := sqlbuilder.BuildNamed(`
		UPDATE events
		SET deleted_at = NOW()
		WHERE deleted_at    IS NULL
		  AND storage_until IS NOT NULL
		  AND storage_until <= NOW()
		RETURNING uid::text
	`, nil)

	rows, err := db.Query_all(bldr)
	if err != nil {
		return 0, utils.Tag_err("soft_delete_q1", err)
	}

	for _, row := range rows {
		fmt.Printf("[storage_cron] soft-deleted event=%s\n", string(row[0]))
	}

	return len(rows), nil
}

func purgeExpired() (int, error) {
	rows, err := queryExpiredEvents()
	if err != nil {
		return 0, err
	}

	for _, row := range rows {
		eventUID := string(row[0])
		_, err = eventcleanup.PurgeUploadsAndSoftDeleteEvent(
			eventUID,
			"",
			eventcleanup.SnapshotReasonStorageExpired,
		)
		if err != nil {
			return 0, utils.Tag_err("soft_delete_purge_q1", err)
		}
		fmt.Printf("[storage_cron] purged and soft-deleted event=%s\n", eventUID)
	}

	return len(rows), nil
}

func queryExpiredEvents() ([][][]byte, error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT uid::text
		FROM events
		WHERE deleted_at    IS NULL
		  AND storage_until IS NOT NULL
		  AND storage_until <= NOW()
		ORDER BY storage_until ASC, uid ASC
	`, nil)

	rows, err := db.Query_all(bldr)
	if err != nil {
		return nil, utils.Tag_err("soft_delete_expired_q1", err)
	}
	return rows, nil
}

func countExpired() (int, error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*)
		FROM events
		WHERE deleted_at    IS NULL
		  AND storage_until IS NOT NULL
		  AND storage_until <= NOW()
	`, nil)

	row, err := db.Query_one(bldr)
	if err != nil {
		return 0, utils.Tag_err("soft_delete_count_q1", err)
	}
	if len(row) == 0 || len(row[0]) == 0 {
		return 0, nil
	}

	var n int
	if _, err := fmt.Sscanf(string(row[0]), "%d", &n); err != nil {
		return 0, utils.Tag_err("soft_delete_count_parse", err)
	}

	return n, nil
}
