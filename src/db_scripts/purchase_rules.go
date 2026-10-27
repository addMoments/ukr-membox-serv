package dbscripts

import (
	db "membox-serv/src/db_layer"
	"membox-serv/src/utils"
	"strconv"

	"github.com/huandu/go-sqlbuilder"
)

// Disabled_product_ids, verilen urun id'lerinden admin'in kapattigi urunleri doner. /api/purchase
// bunlari reddeder: kapali urun magazada gorunmez, ama istek elle kurulursa yine satilirdi
// (ornek: yerlesimi beklenen 390 UAH'lik Hero paketi, Excel madde 6).
func Disabled_product_ids(productIDs []string) (disabledIDs []string, err error) {
	if len(productIDs) == 0 {
		return
	}
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("id").From("products").Where(
		sb.In("id", db.Interface_ar(productIDs)...),
		"is_enabled IS NOT TRUE",
	)
	rows, err := db.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("prr1", err)
		return
	}
	for _, row := range rows {
		disabledIDs = append(disabledIDs, string(row[0]))
	}
	return
}

// Special_package_with_ad_zone, sepette hem reklam alanini AddMoments'e veren bir urun (ozellik 5,
// ozel paketler) hem de host'a reklam alani satan bir urun (ozellik 4) var mi. Ikisi birlikte
// alinamaz: 5 onceliklidir, host'un odedigi reklam alani hic kullanilamazdi.
func Special_package_with_ad_zone(productIDs []string) (conflict bool, err error) {
	if len(productIDs) == 0 {
		return
	}
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select(
		"COUNT(*) FILTER (WHERE "+strconv.Itoa(FeatureAddmomentsAds)+" = ANY(granted_features))",
		"COUNT(*) FILTER (WHERE "+strconv.Itoa(FeatureAdvertorial)+" = ANY(granted_features))",
	).From("products").Where(sb.In("id", db.Interface_ar(productIDs)...))
	row, err := db.Query_one(sb)
	if err != nil {
		err = utils.Tag_err("prr2", err)
		return
	}
	conflict = string(row[0]) != "0" && string(row[1]) != "0"
	return
}
