// Package storagecron handles event lifecycle automation:
//   - T-14 warning mails (host'a "14 gun icinde silinecek" uyarisi)
//   - T-0  soft-delete (storage_until gectiginde deleted_at doldurulur)
//
// Tasarim notlari:
//   - RunOnce idempotent: storage_warning_mail_sent_at ve deleted_at flag'leri
//     sayesinde tekrar koşturmak ek yan etki yaratmaz.
//   - dry_run: ilk prod tetiklerinde verilerin etkilenmemesi icin write/SMTP
//     calismayan inceleme modu. Sayilari rapor eder, calismayi simule eder.
//   - Periyodik tick (24 saatlik) Init icinde varsayilan olarak DEVRE DISI.
//     Manuel admin endpoint ile birkac kez koşturulup dogrulandiktan sonra
//     ilgili blok aciliarak gercek zamana alinir.
package storagecron

import (
	"fmt"
	"sync"
	"time"
)

// Result, tek bir RunOnce cikti raporu. Admin endpoint'inden JSON olarak doner.
type Result struct {
	DryRun           bool      `json:"dry_run"`
	WarningMailsSent int       `json:"warning_mails_sent"`
	WarningFailures  []string  `json:"warning_failures"` // event UID'leri
	SoftDeletedCount int       `json:"soft_deleted_count"`
	StartedAt        time.Time `json:"started_at"`
	FinishedAt       time.Time `json:"finished_at"`
}

// runMutex, ayni anda iki RunOnce calisirsa (admin double-click veya cron
// + manuel tetik cakismasi) seri hale getirir.
var runMutex sync.Mutex

// Init, main.go'dan cagirilir. Servis acilisinda bir kez RunOnce calistirir,
// ardindan 24 saatlik tick ile periyodik kosturma yapar.
// Manuel admin endpoint'i (RunStorageCheck) ayrica fail-safe olarak duruyor.
func Init() {
	fmt.Println("[storage_cron] initialised; tick=24h, boot run scheduled")

	go func() {
		if _, err := RunOnce(false); err != nil {
			fmt.Printf("[storage_cron] boot run error: %v\n", err)
		}
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := RunOnce(false); err != nil {
				fmt.Printf("[storage_cron] tick run error: %v\n", err)
			}
		}
	}()
}

// RunOnce, uyari mail job'unu ve soft-delete job'unu sirayla calistirir.
// dryRun=true ise yazma/SMTP calismaz; sadece kac event etkilenecekti raporlar.
func RunOnce(dryRun bool) (Result, error) {
	runMutex.Lock()
	defer runMutex.Unlock()

	res := Result{DryRun: dryRun, StartedAt: time.Now()}

	mailSent, mailFails, err := sendWarningMails(dryRun)
	if err != nil {
		res.FinishedAt = time.Now()
		return res, fmt.Errorf("warning mails: %w", err)
	}
	res.WarningMailsSent = mailSent
	res.WarningFailures = mailFails

	deleted, err := softDeleteExpired(dryRun)
	if err != nil {
		res.FinishedAt = time.Now()
		return res, fmt.Errorf("soft delete: %w", err)
	}
	res.SoftDeletedCount = deleted

	res.FinishedAt = time.Now()
	fmt.Printf(
		"[storage_cron] run done dry=%v mails=%d failures=%d soft_deleted=%d duration=%s\n",
		dryRun, res.WarningMailsSent, len(res.WarningFailures), res.SoftDeletedCount,
		res.FinishedAt.Sub(res.StartedAt),
	)
	return res, nil
}

// Job implementasyonlari:
//   - sendWarningMails  -> warn_mail.go
//   - softDeleteExpired -> soft_delete.go
