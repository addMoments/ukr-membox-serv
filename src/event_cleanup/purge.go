package eventcleanup

import (
	"fmt"
	db "membox-serv/src/db_layer"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"

	"github.com/huandu/go-sqlbuilder"
)

// PurgeUploadsAndSoftDeleteEvent, event kapatma akisini tek merkezde toplar:
// once upload snapshot'ini garantiye alir, sonra event'i soft-delete eder,
// photo/video/voice upload kayitlarini DB'den siler ve ayni medya path'lerini
// S3'ten temizler. Route veya cron bu fonksiyona baglanana kadar mevcut sistem
// davranisi degismez.
func PurgeUploadsAndSoftDeleteEvent(eventUID string, actorUserUID string, reason string) (*UploadSnapshot, error) {
	// actorUserUID manuel delete log/audit icin imzada tutuluyor; yetki kontrolu
	// route katmaninda yapilacak, cron ise bos actor ile cagirabilir.
	_ = actorUserUID

	snapshot, _, err := CreateEventUploadSnapshot(eventUID, reason)
	if err != nil {
		return nil, err
	}
	if snapshot.PurgeFinishedAt != "" {
		return snapshot, nil
	}

	if err = markPurgeStarted(eventUID); err != nil {
		return nil, err
	}

	if err = softDeleteEvent(eventUID); err != nil {
		_ = markPurgeFailed(eventUID, err)
		return nil, err
	}

	if err = deleteMediaUploadRows(eventUID); err != nil {
		_ = markPurgeFailed(eventUID, err)
		return nil, err
	}

	if err = deleteS3Media(snapshot.MediaPaths); err != nil {
		_ = markPurgeFailed(eventUID, err)
		return nil, err
	}

	if err = markPurgeFinished(eventUID); err != nil {
		return nil, err
	}

	finishedSnapshot, exists, err := getExistingSnapshot(eventUID)
	if err != nil {
		return nil, err
	}
	if exists {
		return finishedSnapshot, nil
	}

	return snapshot, nil
}

func markPurgeStarted(eventUID string) error {
	bldr := sqlbuilder.BuildNamed(`
		UPDATE event_upload_snapshots
		SET purge_started_at = COALESCE(purge_started_at, NOW()),
		    purge_error = NULL
		WHERE event_uid = ${event_uid}
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	if err := db.Exec(bldr); err != nil {
		return utils.Tag_err("eup_started", err)
	}
	return nil
}

func softDeleteEvent(eventUID string) error {
	bldr := sqlbuilder.BuildNamed(`
		UPDATE events
		SET deleted_at = COALESCE(deleted_at, NOW())
		WHERE uid = ${event_uid}
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	if err := db.Exec(bldr); err != nil {
		return utils.Tag_err("eup_soft_delete", err)
	}
	return nil
}

func deleteMediaUploadRows(eventUID string) error {
	bldr := sqlbuilder.BuildNamed(`
		DELETE FROM uploads
		WHERE event_uid = ${event_uid}
		  AND upload_type IN ('photo', 'video', 'voice')
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	if err := db.Exec(bldr); err != nil {
		return utils.Tag_err("eup_delete_uploads", err)
	}
	return nil
}

func deleteS3Media(mediaPaths []string) error {
	for _, mediaPath := range mediaPaths {
		if mediaPath == "" {
			continue
		}
		if err := s3wrap.Public_s3.Rm(mediaPath); err != nil {
			return utils.Tag_err("eup_s3_rm", fmt.Errorf("%s: %w", mediaPath, err))
		}
	}
	return nil
}

func markPurgeFinished(eventUID string) error {
	bldr := sqlbuilder.BuildNamed(`
		UPDATE event_upload_snapshots
		SET purge_finished_at = NOW(),
		    purge_error = NULL
		WHERE event_uid = ${event_uid}
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	if err := db.Exec(bldr); err != nil {
		return utils.Tag_err("eup_finished", err)
	}
	return nil
}

func markPurgeFailed(eventUID string, purgeErr error) error {
	bldr := sqlbuilder.BuildNamed(`
		UPDATE event_upload_snapshots
		SET purge_error = ${purge_error}
		WHERE event_uid = ${event_uid}
	`, map[string]interface{}{
		"event_uid":   eventUID,
		"purge_error": purgeErr.Error(),
	})

	if err := db.Exec(bldr); err != nil {
		return utils.Tag_err("eup_failed", err)
	}
	return nil
}
