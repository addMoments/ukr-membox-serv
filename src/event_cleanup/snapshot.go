package eventcleanup

import (
	"encoding/json"
	"fmt"
	db "membox-serv/src/db_layer"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"
	"strconv"

	"github.com/huandu/go-sqlbuilder"
)

const (
	SnapshotReasonManualDelete   = "manual_delete"
	SnapshotReasonStorageExpired = "storage_expired"
)

type UploadSnapshot struct {
	UID               string
	EventUID          string
	Reason            string
	GuestCount        int
	ContributorCount  int
	UploadCountTotal  int
	PhotoCount        int
	VideoCount        int
	VoiceCount        int
	TextCount         int
	TotalUploadSizeMB float64
	FirstUploadAt     string
	LastUploadAt      string
	MediaPaths        []string
	PurgeStartedAt    string
	PurgeFinishedAt   string
	PurgeError        string
}

// CreateEventUploadSnapshot, medya purge baslamadan once upload bazli son
// analitik durumu kalici tabloya yazar. Bu fonksiyon bilerek event'i silmez,
// upload kaydi silmez ve S3 objesi silmez; sadece ilerideki purge adimlari icin
// raporlama verisini ve retry edilecek medya path'lerini guvenceye alir.
func CreateEventUploadSnapshot(eventUID string, reason string) (*UploadSnapshot, bool, error) {
	if reason != SnapshotReasonManualDelete && reason != SnapshotReasonStorageExpired {
		return nil, false, fmt.Errorf("invalid snapshot reason: %s", reason)
	}

	existing, exists, err := getExistingSnapshot(eventUID)
	if err != nil {
		return nil, false, err
	}
	if exists {
		return existing, false, nil
	}

	snapshot, err := buildUploadSnapshot(eventUID, reason)
	if err != nil {
		return nil, false, err
	}

	mediaPathsJSON, err := json.Marshal(snapshot.MediaPaths)
	if err != nil {
		return nil, false, utils.Tag_err("eus_json_paths", err)
	}

	bldr := sqlbuilder.BuildNamed(`
		INSERT INTO event_upload_snapshots (
			event_uid,
			reason,
			guest_count,
			contributor_count,
			upload_count_total,
			photo_count,
			video_count,
			voice_count,
			text_count,
			total_upload_size_mb,
			first_upload_at,
			last_upload_at,
			media_paths
		)
		VALUES (
			${event_uid},
			${reason},
			${guest_count},
			${contributor_count},
			${upload_count_total},
			${photo_count},
			${video_count},
			${voice_count},
			${text_count},
			${total_upload_size_mb},
			NULLIF(${first_upload_at}, '')::timestamptz,
			NULLIF(${last_upload_at}, '')::timestamptz,
			${media_paths}::jsonb
		)
		ON CONFLICT (event_uid) DO NOTHING
		RETURNING uid::text
	`, map[string]interface{}{
		"event_uid":            snapshot.EventUID,
		"reason":               snapshot.Reason,
		"guest_count":          snapshot.GuestCount,
		"contributor_count":    snapshot.ContributorCount,
		"upload_count_total":   snapshot.UploadCountTotal,
		"photo_count":          snapshot.PhotoCount,
		"video_count":          snapshot.VideoCount,
		"voice_count":          snapshot.VoiceCount,
		"text_count":           snapshot.TextCount,
		"total_upload_size_mb": snapshot.TotalUploadSizeMB,
		"first_upload_at":      snapshot.FirstUploadAt,
		"last_upload_at":       snapshot.LastUploadAt,
		"media_paths":          string(mediaPathsJSON),
	})

	row, err := db.Query_one(bldr)
	if err != nil {
		if err.Error() == "empty row" {
			existing, exists, existingErr := getExistingSnapshot(eventUID)
			if existingErr != nil {
				return nil, false, existingErr
			}
			if exists {
				return existing, false, nil
			}
		}
		return nil, false, utils.Tag_err("eus_insert", err)
	}

	snapshot.UID = string(row[0])
	return snapshot, true, nil
}

func getExistingSnapshot(eventUID string) (*UploadSnapshot, bool, error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT
			uid::text,
			event_uid::text,
			reason,
			guest_count::text,
			contributor_count::text,
			upload_count_total::text,
			photo_count::text,
			video_count::text,
			voice_count::text,
			text_count::text,
			COALESCE(total_upload_size_mb::text, '0'),
			COALESCE(first_upload_at::text, ''),
			COALESCE(last_upload_at::text, ''),
			media_paths::text,
			COALESCE(purge_started_at::text, ''),
			COALESCE(purge_finished_at::text, ''),
			COALESCE(purge_error, '')
		FROM event_upload_snapshots
		WHERE event_uid = ${event_uid}
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	row, err := db.Query_one(bldr)
	if err != nil {
		if err.Error() == "empty row" {
			return nil, false, nil
		}
		return nil, false, utils.Tag_err("eus_existing", err)
	}

	snapshot, err := snapshotFromRow(row)
	if err != nil {
		return nil, false, err
	}
	return snapshot, true, nil
}

func buildUploadSnapshot(eventUID string, reason string) (*UploadSnapshot, error) {
	stats, err := queryUploadStats(eventUID)
	if err != nil {
		return nil, err
	}

	guestCount, err := queryGuestCount(eventUID)
	if err != nil {
		return nil, err
	}
	stats.GuestCount = guestCount

	mediaPaths, err := queryMediaPaths(eventUID)
	if err != nil {
		return nil, err
	}
	stats.MediaPaths = mediaPaths

	totalSizeMB, err := calcMediaSizeMB(mediaPaths)
	if err != nil {
		return nil, err
	}
	stats.TotalUploadSizeMB = totalSizeMB

	stats.EventUID = eventUID
	stats.Reason = reason
	return stats, nil
}

func queryUploadStats(eventUID string) (*UploadSnapshot, error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT
			COUNT(*)::text AS upload_count_total,
			COUNT(*) FILTER (WHERE upload_type = 'photo')::text AS photo_count,
			COUNT(*) FILTER (WHERE upload_type = 'video')::text AS video_count,
			COUNT(*) FILTER (WHERE upload_type = 'voice')::text AS voice_count,
			COUNT(*) FILTER (WHERE upload_type = 'text')::text AS text_count,
			COUNT(DISTINCT client_uid) FILTER (WHERE trashed_at IS NULL)::text AS contributor_count,
			COALESCE(MIN(created_at)::text, '') AS first_upload_at,
			COALESCE(MAX(created_at)::text, '') AS last_upload_at
		FROM uploads
		WHERE event_uid = ${event_uid}
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	row, err := db.Query_one(bldr)
	if err != nil {
		return nil, utils.Tag_err("eus_stats", err)
	}

	uploadCountTotal, err := parseIntCol(row, 0, "upload_count_total")
	if err != nil {
		return nil, err
	}
	photoCount, err := parseIntCol(row, 1, "photo_count")
	if err != nil {
		return nil, err
	}
	videoCount, err := parseIntCol(row, 2, "video_count")
	if err != nil {
		return nil, err
	}
	voiceCount, err := parseIntCol(row, 3, "voice_count")
	if err != nil {
		return nil, err
	}
	textCount, err := parseIntCol(row, 4, "text_count")
	if err != nil {
		return nil, err
	}
	contributorCount, err := parseIntCol(row, 5, "contributor_count")
	if err != nil {
		return nil, err
	}

	return &UploadSnapshot{
		UploadCountTotal: uploadCountTotal,
		PhotoCount:       photoCount,
		VideoCount:       videoCount,
		VoiceCount:       voiceCount,
		TextCount:        textCount,
		ContributorCount: contributorCount,
		FirstUploadAt:    string(row[6]),
		LastUploadAt:     string(row[7]),
	}, nil
}

func queryGuestCount(eventUID string) (int, error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*)::text
		FROM participants
		WHERE event_uid = ${event_uid}
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	row, err := db.Query_one(bldr)
	if err != nil {
		return 0, utils.Tag_err("eus_guest_count", err)
	}
	return parseIntCol(row, 0, "guest_count")
}

func queryMediaPaths(eventUID string) ([]string, error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT value
		FROM uploads
		WHERE event_uid = ${event_uid}
		  AND upload_type IN ('photo', 'video', 'voice')
		ORDER BY created_at ASC, uid ASC
	`, map[string]interface{}{
		"event_uid": eventUID,
	})

	rows, err := db.Query_all(bldr)
	if err != nil {
		return nil, utils.Tag_err("eus_media_paths", err)
	}

	paths := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		paths = append(paths, string(row[0]))
	}
	return paths, nil
}

func calcMediaSizeMB(mediaPaths []string) (float64, error) {
	total := 0.0
	for _, mediaPath := range mediaPaths {
		sizeMB, _, err := s3wrap.Public_s3.Calc_object_size(mediaPath)
		if err != nil {
			return 0, utils.Tag_err("eus_media_size", err)
		}
		total += sizeMB
	}
	return total, nil
}

func snapshotFromRow(row [][]byte) (*UploadSnapshot, error) {
	guestCount, err := parseIntCol(row, 3, "guest_count")
	if err != nil {
		return nil, err
	}
	contributorCount, err := parseIntCol(row, 4, "contributor_count")
	if err != nil {
		return nil, err
	}
	uploadCountTotal, err := parseIntCol(row, 5, "upload_count_total")
	if err != nil {
		return nil, err
	}
	photoCount, err := parseIntCol(row, 6, "photo_count")
	if err != nil {
		return nil, err
	}
	videoCount, err := parseIntCol(row, 7, "video_count")
	if err != nil {
		return nil, err
	}
	voiceCount, err := parseIntCol(row, 8, "voice_count")
	if err != nil {
		return nil, err
	}
	textCount, err := parseIntCol(row, 9, "text_count")
	if err != nil {
		return nil, err
	}
	totalUploadSizeMB, err := strconv.ParseFloat(string(row[10]), 64)
	if err != nil {
		return nil, utils.Tag_err("eus_parse_total_upload_size_mb", err)
	}

	mediaPaths := []string{}
	if len(row[13]) > 0 {
		if err := json.Unmarshal(row[13], &mediaPaths); err != nil {
			return nil, utils.Tag_err("eus_parse_media_paths", err)
		}
	}

	return &UploadSnapshot{
		UID:               string(row[0]),
		EventUID:          string(row[1]),
		Reason:            string(row[2]),
		GuestCount:        guestCount,
		ContributorCount:  contributorCount,
		UploadCountTotal:  uploadCountTotal,
		PhotoCount:        photoCount,
		VideoCount:        videoCount,
		VoiceCount:        voiceCount,
		TextCount:         textCount,
		TotalUploadSizeMB: totalUploadSizeMB,
		FirstUploadAt:     string(row[11]),
		LastUploadAt:      string(row[12]),
		MediaPaths:        mediaPaths,
		PurgeStartedAt:    string(row[14]),
		PurgeFinishedAt:   string(row[15]),
		PurgeError:        string(row[16]),
	}, nil
}

func parseIntCol(row [][]byte, idx int, name string) (int, error) {
	val, err := strconv.Atoi(string(row[idx]))
	if err != nil {
		return 0, utils.Tag_err("eus_parse_"+name, err)
	}
	return val, nil
}
