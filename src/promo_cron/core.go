// Package promocron keeps promo_codes state in sync with time and usage limits.
//
// Apply/purchase validation is still the source of truth for checkout safety.
// This cron only makes admin-visible state consistent by deactivating promos
// that can no longer be used.
package promocron

import (
	"fmt"
	db "membox-serv/src/db_layer"
	"strconv"
	"sync"
	"time"

	"github.com/huandu/go-sqlbuilder"
)

type Result struct {
	DryRun                     bool      `json:"dry_run"`
	ExpiredDeactivatedCount    int       `json:"expired_deactivated_count"`
	UsageLimitDeactivatedCount int       `json:"usage_limit_deactivated_count"`
	StartedAt                  time.Time `json:"started_at"`
	FinishedAt                 time.Time `json:"finished_at"`
}

var runMutex sync.Mutex

// Init starts an independent 5 minute promo cleanup ticker.
// It mirrors the existing storage cron style but stays separate so promo state
// changes cannot affect event lifecycle jobs.
func Init() {
	fmt.Println("[promo_cron] initialised; tick=5m, boot run scheduled")

	go func() {
		if _, err := RunOnce(false); err != nil {
			fmt.Printf("[promo_cron] boot run error: %v\n", err)
		}

		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := RunOnce(false); err != nil {
				fmt.Printf("[promo_cron] tick run error: %v\n", err)
			}
		}
	}()
}

// RunOnce deactivates active promos that are expired or have reached their
// total usage limit. The updates are idempotent because they only touch
// is_active=true, non-deleted rows.
func RunOnce(dryRun bool) (Result, error) {
	runMutex.Lock()
	defer runMutex.Unlock()

	res := Result{DryRun: dryRun, StartedAt: time.Now()}

	expiredCount, err := deactivateExpired(dryRun)
	if err != nil {
		res.FinishedAt = time.Now()
		return res, fmt.Errorf("expired cleanup: %w", err)
	}
	res.ExpiredDeactivatedCount = expiredCount

	usageLimitCount, err := deactivateUsageLimitReached(dryRun)
	if err != nil {
		res.FinishedAt = time.Now()
		return res, fmt.Errorf("usage limit cleanup: %w", err)
	}
	res.UsageLimitDeactivatedCount = usageLimitCount

	res.FinishedAt = time.Now()
	fmt.Printf(
		"[promo_cron] run done dry=%v expired=%d usage_limit=%d duration=%s\n",
		dryRun,
		res.ExpiredDeactivatedCount,
		res.UsageLimitDeactivatedCount,
		res.FinishedAt.Sub(res.StartedAt),
	)
	return res, nil
}

func deactivateExpired(dryRun bool) (int, error) {
	if dryRun {
		return countMatchingPromos(`
			valid_until IS NOT NULL
			AND valid_until < NOW()
		`)
	}

	return updateMatchingPromos(
		"expired",
		`
			valid_until IS NOT NULL
			AND valid_until < NOW()
		`,
	)
}

func deactivateUsageLimitReached(dryRun bool) (int, error) {
	if dryRun {
		return countMatchingPromos(`
			usage_limit_total IS NOT NULL
			AND usage_count >= usage_limit_total
		`)
	}

	return updateMatchingPromos(
		"usage_limit_reached",
		`
			usage_limit_total IS NOT NULL
			AND usage_count >= usage_limit_total
		`,
	)
}

func countMatchingPromos(extraWhere string) (int, error) {
	bldr := sqlbuilder.BuildNamed(fmt.Sprintf(`
		SELECT COUNT(*)::text
		FROM promo_codes
		WHERE is_active = TRUE
		  AND deleted_at IS NULL
		  AND %s
	`, extraWhere), map[string]interface{}{})

	row, err := db.Query_one(bldr)
	if err != nil {
		return 0, err
	}

	count, err := strconv.Atoi(string(row[0]))
	if err != nil {
		return 0, err
	}
	return count, nil
}

func updateMatchingPromos(reason string, extraWhere string) (int, error) {
	bldr := sqlbuilder.BuildNamed(fmt.Sprintf(`
		UPDATE promo_codes
		SET is_active = FALSE,
		    deactivated_at = COALESCE(deactivated_at, NOW()),
		    deactivated_reason = ${reason}
		WHERE is_active = TRUE
		  AND deleted_at IS NULL
		  AND %s
		RETURNING uid
	`, extraWhere), map[string]interface{}{
		"reason": reason,
	})

	rows, err := db.Query_all(bldr)
	if err != nil {
		return 0, err
	}
	return len(rows), nil
}
