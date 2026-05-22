package routes

import (
	"net/http"
	"strings"

	networkutils "membox-serv/src/network_utils"
	storagecron "membox-serv/src/storage_cron"
)

type admin_storage_routes_typ struct{}

// AdminStorageRoutes, storage_until lifecycle araclarini export eder.
// Suan tek endpoint var: RunStorageCheck.
var AdminStorageRoutes admin_storage_routes_typ

// RunStorageCheck, T-14 uyari mailini ve T-0 soft-delete'i manuel tetikler.
//
// Erisim: SuperAdminMiddleware (env.AdminEmails listesindeki super-adminler).
// Query parametreleri:
//   - dry_run=true|1  : DB ve SMTP'ye dokunmadan sadece etkilenecek event
//                       sayilarini raporlar.
//   - dry_run=false   : (default) gercek kosturma; mail gonderir, deleted_at
//                       set eder.
//
// Cevap: storagecron.Result JSON'i (200) veya hata (500).
//
// Kullanim ornegi:
//   POST /api/admin/run-storage-check?dry_run=true
//   POST /api/admin/run-storage-check
func (rfrnc admin_storage_routes_typ) RunStorageCheck(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var err error
	var payload storagecron.Result

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		networkutils.SendJson(payload, w)
	})()

	dryRun := parseDryRun(r.URL.Query().Get("dry_run"))

	payload, err = storagecron.RunOnce(dryRun)
	if err != nil {
		stat_code = 500
		return
	}
}

func parseDryRun(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}
