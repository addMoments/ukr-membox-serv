package storagecron

import (
	"fmt"
	"io"
	"strings"
	"time"

	db "membox-serv/src/db_layer"
	"membox-serv/src/env"
	sendemail "membox-serv/src/send_email"
	"membox-serv/src/utils"

	"github.com/huandu/go-sqlbuilder"
)

// frontendBaseURL, mail butonundan acilacak FE event sayfasinin kok URL'i.
// Dev modunda lokal FE'ye, prod modunda canli domain'e isaret eder. Ayni pattern
// password_reset.go icinde de kullaniliyor.
func frontendBaseURL() string {
	if env.Is_live() {
		return "https://addmoments.com.ua"
	}
	return "http://localhost:3000"
}

// warnCandidate, uyari mailinin atilacagi bir adayi temsil eder.
type warnCandidate struct {
	UID          string
	Name         string
	StorageUntil time.Time
	AdminMails   []string
}

// sendWarningMails, T-14 esiginde olan eventleri bulur ve host'lara uyari maili
// gonderir. Idempotent: storage_warning_mail_sent_at IS NULL filtresi sayesinde
// her event icin yalnizca bir kez tetiklenir.
//
// Geri donuş: count = basariyla mail atilan event sayisi, failures = mail veya
// flag update'i basarisiz olan event UID'leri.
func sendWarningMails(dryRun bool) (int, []string, error) {
	candidates, err := fetchWarnCandidates()
	if err != nil {
		return 0, nil, err
	}

	count := 0
	failures := []string{}

	for _, c := range candidates {
		if len(c.AdminMails) == 0 {
			fmt.Printf("[storage_cron] event=%s has no admin mails, skipping (state untouched)\n", c.UID)
			continue
		}

		if dryRun {
			fmt.Printf("[storage_cron][DRY] would mail event=%s name=%q to=%v storage_until=%s\n",
				c.UID, c.Name, c.AdminMails, c.StorageUntil.UTC().Format(time.RFC3339))
			count++
			continue
		}

		if err := sendOneWarning(c); err != nil {
			fmt.Printf("[storage_cron] mail send failed event=%s err=%v (will retry next run)\n", c.UID, err)
			failures = append(failures, c.UID)
			continue
		}

		if err := markWarnSent(c.UID); err != nil {
			// Mail gitti ama flag yazilamadi. Bir sonraki cycle'da tekrar
			// mail gidebilir (rare). Logla, basariyi say.
			fmt.Printf("[storage_cron] WARN mail sent but flag update failed event=%s err=%v\n", c.UID, err)
		}
		count++
	}

	return count, failures, nil
}

func fetchWarnCandidates() ([]warnCandidate, error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT
			e.uid::text,
			e.name,
			e.storage_until,
			COALESCE(
				string_agg(u.mail, ',') FILTER (WHERE u.mail IS NOT NULL AND u.mail <> ''),
				''
			) AS admin_mails
		FROM events e
		LEFT JOIN users u ON u.uid = ANY(e.admins)
		WHERE e.deleted_at                   IS NULL
		  AND e.storage_until                IS NOT NULL
		  AND e.storage_warning_mail_sent_at IS NULL
		  AND e.storage_until > NOW()
		  AND (e.storage_until - interval '14 days') <= NOW()
		GROUP BY e.uid, e.name, e.storage_until
	`, nil)

	rows, err := db.Query_all(bldr)
	if err != nil {
		return nil, utils.Tag_err("warn_q1", err)
	}

	out := make([]warnCandidate, 0, len(rows))
	for _, row := range rows {
		c := warnCandidate{
			UID:  string(row[0]),
			Name: string(row[1]),
		}

		if len(row[2]) > 0 {
			t, err := time.Parse(time.RFC3339, string(row[2]))
			if err != nil {
				// Postgres bazen 'YYYY-MM-DD HH:MM:SS+TZ' formatinda doner;
				// fallback olarak alternatif layout'u dene.
				t, err = time.Parse("2006-01-02 15:04:05.999999-07", string(row[2]))
				if err != nil {
					return nil, utils.Tag_err("warn_q2_parse_storage_until", err)
				}
			}
			c.StorageUntil = t
		}

		if mails := strings.TrimSpace(string(row[3])); mails != "" {
			parts := strings.Split(mails, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					c.AdminMails = append(c.AdminMails, p)
				}
			}
		}

		out = append(out, c)
	}

	return out, nil
}

func sendOneWarning(c warnCandidate) error {
	packedUID, err := utils.UUID.PackUUID(c.UID)
	if err != nil {
		return utils.Tag_err("warn_pack", err)
	}

	expDate := c.StorageUntil.UTC().Format("2006-01-02")
	eventURL := fmt.Sprintf("%s/event/%s?prompt=extend", frontendBaseURL(), packedUID)

	subject := "Your event will be deleted in 14 days"

	return sendemail.Info_mail.Send(c.AdminMails, subject, func(w io.WriteCloser) {
		sendemail.Write_html(w, "Your event storage is about to expire", []string{
			fmt.Sprintf(
				`Your event "%s" will be permanently deleted on <b>%s</b>. After this date, all photos, videos and content will be removed.`,
				c.Name, expDate,
			),
			"You can extend the storage period by 30 days with one click. Note: this is a one-time, free extension.",
			sendemail.Button(eventURL, "Extend by 1 month"),
			"If the button doesn't work, copy and paste this link into your browser:<br>" + eventURL,
			"If you do not extend, your event will be removed automatically.",
		})
	}, nil)
}

func markWarnSent(eventUID string) error {
	bldr := sqlbuilder.BuildNamed(`
		UPDATE events
		SET storage_warning_mail_sent_at = NOW()
		WHERE uid = ${uid}
		  AND storage_warning_mail_sent_at IS NULL
	`, map[string]interface{}{"uid": eventUID})

	return db.Exec(bldr)
}
