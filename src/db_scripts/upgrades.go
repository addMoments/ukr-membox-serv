package dbscripts

import (
	"encoding/json"
	"errors"
	db "membox-serv/src/db_layer"
	"membox-serv/src/utils"
	"strconv"
	"strings"

	"github.com/huandu/go-sqlbuilder"
)

// Host panelinden paket yukseltme ve ek depolama (Excel 2.18). Model, tarih kurallari ve
// gecerli paketin tek kaynagi (event_core_product_uid) db-shell/misc/17-event-upgrades.sql'de.

var ErrUpgradeNotOffered = errors.New("this upgrade is not offered for the event's current package")
var ErrUpgradeWindowClosed = errors.New("the event's gallery period has ended")

// UpgradeOption, etkinligin mevcut paketine satilabilen bir yukseltme urunu.
type UpgradeOption struct {
	ID                 string  `json:"id"`
	Kind               string  `json:"kind"` // package | storage | time | upload
	Price              float64 `json:"price"`
	NameEN             string  `json:"display_name_en"`
	NameUK             string  `json:"display_name_uk"`
	DescriptionEN      string  `json:"display_description_en"`
	DescriptionUK      string  `json:"display_description_uk"`
	ToPackage          string  `json:"to_package,omitempty"`
	ExtraStorageGB     float64 `json:"extra_storage_gb,omitempty"`
	ExtraStorageMonths int     `json:"extra_storage_months,omitempty"`
	ExtraUploadMonths  int     `json:"extra_upload_months,omitempty"`
	// Paket yukseltmesinde hedef paket; host kartindaki "ne kazanirim" satirlari bundan uretilir.
	ToPackageInfo *PackageInfo `json:"to_package_info,omitempty"`
}

// PackageInfo, bir core paketin adlari, limitleri (products.options) ve ozellikleri
// (granted_features). Host karti mevcut ve hedef paketi karsilastirip farki gosterir; admin bir
// limiti degistirirse kart da onu gosterir.
type PackageInfo struct {
	ID       string          `json:"id"`
	NameEN   string          `json:"display_name_en"`
	NameUK   string          `json:"display_name_uk"`
	Options  json.RawMessage `json:"options"`
	Features []int           `json:"granted_features"`
}

// Package_info, id'si verilen urunun PackageInfo'su.
func Package_info(productID string) (info PackageInfo, err error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT id, COALESCE(display_name_en, ''), COALESCE(display_name_uk, ''),
		       COALESCE(options, '{}')::text, COALESCE(array_to_json(granted_features), '[]')::text
		  FROM products WHERE id = ${id}
	`, map[string]interface{}{"id": productID})
	res, err := db.Query_one(bldr)
	if err != nil {
		err = utils.Tag_err("upg7", err)
		return
	}
	info = PackageInfo{
		ID:      string(res[0]),
		NameEN:  string(res[1]),
		NameUK:  string(res[2]),
		Options: json.RawMessage(append([]byte(nil), res[3]...)),
	}
	if err = json.Unmarshal(res[4], &info.Features); err != nil {
		err = utils.Tag_err("upg8", err)
	}
	return
}

// Event_upgrade_options, etkinligin gecerli paketine (yukseltme dahil) satilabilen acik
// yukseltme urunlerini fiyata gore sirali doner. Paket yukseltmesinin hedefi core paket
// olarak var olmali; admin hedef paketi silerse secenek listeden duser. Ek GB yalnizca yukleme
// suresi bitmemisken (active_until gelecekte ya da tarih henuz secilmemis; musteri 23 Ekim:
// "Extra 25 GB will be unvisible after upload period is over"). Ek yukleme ayi yukleme bittikten
// sonra da satilir (musteri 27 Ekim: "1 month Link extension ... should be" visible); link
// satin alindigi andan acilir (19-upload-extension-after-end.sql, upgrade_extra_dates).
func Event_upgrade_options(eventUID string) (current string, options []UpgradeOption, err error) {
	current, err = Event_tier(eventUID)
	if err != nil {
		err = utils.Tag_err("upg1", err)
		return
	}

	bldr := sqlbuilder.BuildNamed(`
		SELECT p.id, p.options->>'upgrade_kind', p.price::text,
		       p.display_name_en, p.display_name_uk, p.display_description_en, p.display_description_uk,
		       COALESCE(p.options->>'upgrade_to', ''),
		       COALESCE(p.options->>'extra_storage_gb', ''),
		       COALESCE(p.options->>'extra_storage_months', ''),
		       COALESCE(p.options->>'extra_upload_months', '')
		  FROM products p
		 WHERE p.is_enabled
		   AND p.options->>'upgrade_kind' IN ('package', 'storage', 'time', 'upload')
		   AND COALESCE(p.options->>'upgrade_from', p.options->>'upgrade_for') = ${current}
		   AND (p.options->>'upgrade_kind' <> 'package'
		        OR EXISTS (SELECT 1 FROM products tp
		                    WHERE tp.id = p.options->>'upgrade_to' AND tp.is_add_on = FALSE))
		   AND (p.options->>'upgrade_kind' <> 'storage'
		        OR EXISTS (SELECT 1 FROM events e
		                    WHERE e.uid = ${event_uid}
		                      AND (e.active_until IS NULL OR e.active_until > NOW())))
		 ORDER BY p.price ASC, p.id ASC
	`, map[string]interface{}{"current": current, "event_uid": eventUID})

	rows, err := db.Query_all(bldr)
	if err != nil {
		err = utils.Tag_err("upg2", err)
		return
	}

	options = []UpgradeOption{}
	for _, row := range rows {
		price, _ := strconv.ParseFloat(string(row[2]), 64)
		gb, _ := strconv.ParseFloat(strings.TrimSpace(string(row[8])), 64)
		months, _ := strconv.Atoi(strings.TrimSpace(string(row[9])))
		uploadMonths, _ := strconv.Atoi(strings.TrimSpace(string(row[10])))
		options = append(options, UpgradeOption{
			ID:                 string(row[0]),
			Kind:               string(row[1]),
			Price:              price,
			NameEN:             string(row[3]),
			NameUK:             string(row[4]),
			DescriptionEN:      string(row[5]),
			DescriptionUK:      string(row[6]),
			ToPackage:          string(row[7]),
			ExtraStorageGB:     gb,
			ExtraStorageMonths: months,
			ExtraUploadMonths:  uploadMonths,
		})
	}
	for i := range options {
		if options[i].Kind != "package" {
			continue
		}
		info, infoErr := Package_info(options[i].ToPackage)
		if infoErr != nil {
			err = infoErr
			return
		}
		options[i].ToPackageInfo = &info
	}
	return
}

// Event_upgrade_window_open, yukseltmenin satilabilecegi donem: galeri suresi bitmemis
// (storage_until gelecekte) ya da host tarihi henuz secmemis (storage_until bos).
// Silinmis etkinlik Is_events_admin'de zaten ErrEventClosed doner.
func Event_upgrade_window_open(eventUID string) (open bool, err error) {
	bldr := sqlbuilder.BuildNamed(`
		SELECT COUNT(*) FROM events
		 WHERE uid = ${event_uid}
		   AND deleted_at IS NULL
		   AND (storage_until IS NULL OR storage_until > NOW())
	`, map[string]interface{}{"event_uid": eventUID})
	res, err := db.Query_one(bldr)
	if err != nil {
		err = utils.Tag_err("upg3", err)
		return
	}
	open = string(res[0]) != "0"
	return
}

// Apply_event_upgrade, odemesi dogrulanmis bir yukseltme siparisini uygular ve etkinligi doner.
// Ikinci cagri hicbir sey degistirmez (SQL tarafinda upgrade_applied_at kilidi).
func Apply_event_upgrade(purchaseUID string) (eventUID string, err error) {
	bldr := sqlbuilder.BuildNamed(`SELECT apply_event_upgrade(${purchase_uid})::text`,
		map[string]interface{}{"purchase_uid": purchaseUID})
	res, err := db.Query_one(bldr)
	if err != nil {
		err = utils.Tag_err("upg4", err)
		return
	}
	eventUID = string(res[0])
	return
}

// Purchase_upgrade_state, siparis bir yukseltmeyse bagli etkinligi ve uygulanma zamanini doner;
// normal siparislerde ikisi de bos.
func Purchase_upgrade_state(purchaseUID string) (eventUID string, appliedAt string, err error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("COALESCE(event_uid::text, '')", "COALESCE(upgrade_applied_at::text, '')").
		From("purchases").Where(sb.Equal("uid", purchaseUID))
	res, err := db.Query_one(sb)
	if err != nil {
		err = utils.Tag_err("upg5", err)
		return
	}
	eventUID = string(res[0])
	appliedAt = string(res[1])
	return
}

// Upgrade_product_ids, verilen urun id'lerinden yukseltme urunu olanlari doner. Normal
// /api/purchase bunlari reddeder: yukseltme yalnizca etkinlik sayfasindan, etkinlige bagli alinir.
func Upgrade_product_ids(productIDs []string) (upgradeIDs []string, err error) {
	if len(productIDs) == 0 {
		return
	}
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("id").From("products").Where(
		sb.In("id", db.Interface_ar(productIDs)...),
		"options->>'upgrade_kind' IS NOT NULL",
	)
	rows, err := db.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("upg6", err)
		return
	}
	for _, row := range rows {
		upgradeIDs = append(upgradeIDs, string(row[0]))
	}
	return
}
