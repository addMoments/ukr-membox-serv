package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	networkutils "membox-serv/src/network_utils"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	"regexp"

	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
)

// AddMoments reklami (Excel madde 6): reklam alani AddMoments'in olan etkinliklerin misafir sayfasinda
// gosterilen reklam. Super admin admin urun listesinden duzenler, host goremez ve degistiremez
// (musteri 26 Ekim: "Addmoments admin owns access to this area").
// Musteri 28 Ekim: paket gruplarina gore ayri reklam ("Ads for School / Hero / Mini"). Grup urunde
// (options.addmoments_ads_set), reklam site_settings 'addmoments_ads:<grup>'te (20-special-upgrades-and-ad-sets.sql);
// bicim etkinlik reklamiyla ayni.

type addmoments_ads_routes_typ struct{}

var AddmomentsAdsRoutes addmoments_ads_routes_typ

// Grubu olmayan paket (20'den once) eski tek satiri okur.
const addmomentsAdsKey = "addmoments_ads"

var errAdsManagedByAddmoments = errors.New("the advertising area of this event is managed by AddMoments")

var addmomentsAdsSetPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

func addmomentsAdsKeyFor(set string) string {
	if set == "" {
		return addmomentsAdsKey
	}
	return addmomentsAdsKey + ":" + set
}

// fetchAddmomentsAdsConfig, grubun kayitli AddMoments reklamini okur; satir yoksa reklamsiz config doner.
func fetchAddmomentsAdsConfig(set string) (types.Js_object, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("value")
	sb.From("site_settings")
	sb.Where(sb.Equal("key", addmomentsAdsKeyFor(set)))

	rows, err := db.Query_all(sb)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return defaultAdvertorialConfig(), nil
	}
	return parseAdvertorialConfig(rows[0][0]), nil
}

// eventAddmomentsAds, etkinligin reklam alani AddMoments'in mi ve oyleyse hangi reklam gosterilir.
// Ozellik 5'li pakette (ozel paketler) alan her zaman AddMoments'in, reklam henuz bos olsa da. Ozellik 5'siz
// ama reklam grubu olan pakette (Mini, musteri 28 Ekim: "we plan to place our ads to Mini packages also")
// yalniz admin grubun reklamini kurduysa ve etkinlik kendi reklam alanini (ozellik 4) almadiysa: parasini
// odeyen hostun reklami onde, admin kurana kadar Mini etkinliklerinde hicbir sey degismez.
func eventAddmomentsAds(eventUID string) (cfg types.Js_object, managed bool, err error) {
	owned, err := dbscripts.Has_feature(eventUID, dbscripts.FeatureAddmomentsAds)
	if err != nil {
		return
	}
	set, err := dbscripts.Event_addmoments_ads_set(eventUID)
	if err != nil {
		return
	}
	if !owned {
		if set == "" {
			return
		}
		hasOwnZone, zoneErr := dbscripts.Has_feature(eventUID, dbscripts.FeatureAdvertorial)
		if zoneErr != nil || hasOwnZone {
			err = zoneErr
			return
		}
	}
	cfg, err = fetchAddmomentsAdsConfig(set)
	if err != nil {
		return
	}
	if !owned && cfg["layout"] == "none" {
		cfg = nil
		return
	}
	managed = true
	return
}

// adminAddmomentsAdsSet, admin isteginin ?set= grubunu dogrular: bir urunun kullandigi grup olmali.
func adminAddmomentsAdsSet(w http.ResponseWriter, r *http.Request) (set string, ok bool) {
	set = r.URL.Query().Get("set")
	if !addmomentsAdsSetPattern.MatchString(set) {
		http.Error(w, "set is required", http.StatusBadRequest)
		return
	}
	exists, err := dbscripts.Addmoments_ads_set_exists(set)
	if err != nil {
		http.Error(w, utils.Tag_err("aas1", err).Error(), http.StatusInternalServerError)
		return
	}
	if !exists {
		http.Error(w, "unknown ads set", http.StatusNotFound)
		return
	}
	ok = true
	return
}

// AdminGet, super admin icin bir grubun AddMoments reklamini doner.
// GET /api/admin/addmoments-ads?set=school
func (ar addmoments_ads_routes_typ) AdminGet(w http.ResponseWriter, r *http.Request) {
	set, ok := adminAddmomentsAdsSet(w, r)
	if !ok {
		return
	}
	cfg, err := fetchAddmomentsAdsConfig(set)
	if err != nil {
		http.Error(w, utils.Tag_err("aag1", err).Error(), http.StatusInternalServerError)
		return
	}
	_ = networkutils.SendJson(types.Js_object{
		"enabled":    true,
		"managed_by": "addmoments",
		"config":     cfg,
	}, w)
}

// AdminPatch, bir grubun AddMoments reklamini kaydeder. Dogrulama etkinlik reklamiyla ayni
// (layout/hucre sayisi, gorsel bizim S3 advertorial/ klasorunden, link http/https).
// PATCH /api/admin/addmoments-ads?set=school
func (ar addmoments_ads_routes_typ) AdminPatch(w http.ResponseWriter, r *http.Request) {
	set, ok := adminAddmomentsAdsSet(w, r)
	if !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var req advertorialConfigPayload
	if err = json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	normalizedConfig, valErr := normalizeAndValidateAdvertorialConfig(req)
	if valErr != nil {
		http.Error(w, valErr.Error(), http.StatusBadRequest)
		return
	}

	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("site_settings").Cols("key", "value", "updated_at")
	ib.Values(addmomentsAdsKeyFor(set), normalizedConfig.Json(), sqlbuilder.Raw("NOW()"))
	ib.SQL("ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = EXCLUDED.updated_at")
	if err = db.Exec(ib); err != nil {
		http.Error(w, utils.Tag_err("aap1", err).Error(), http.StatusInternalServerError)
		return
	}

	_ = networkutils.SendJson(types.Js_object{
		"enabled":    true,
		"managed_by": "addmoments",
		"config":     normalizedConfig,
	}, w)
}

// AdminUploadURL, AddMoments reklam gorseli icin presigned S3 upload URL'i uretir.
// Anahtar advertorial/addmoments/ altinda: misafir tarafindaki gorsel dogrulamasi ayni klasoru bekler.
// POST /api/admin/addmoments-ads/upload-url?set=school
func (ar addmoments_ads_routes_typ) AdminUploadURL(w http.ResponseWriter, r *http.Request) {
	if _, ok := adminAddmomentsAdsSet(w, r); !ok {
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var req advertorialUploadURLReq
	if err = json.Unmarshal(body, &req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.FileName == "" {
		http.Error(w, "file_name is required", http.StatusBadRequest)
		return
	}
	if req.ContentType == "" {
		http.Error(w, "content_type is required", http.StatusBadRequest)
		return
	}
	if req.SizeBytes <= 0 {
		http.Error(w, "size_bytes must be > 0", http.StatusBadRequest)
		return
	}
	if req.SizeBytes > addonImageUploadMaxSizeBytes {
		http.Error(w, fmt.Sprintf("size_bytes must be <= %d", addonImageUploadMaxSizeBytes), http.StatusBadRequest)
		return
	}

	ext, extErr := resolveAddonImageExt(req.FileName, req.ContentType)
	if extErr != nil {
		http.Error(w, extErr.Error(), http.StatusBadRequest)
		return
	}

	key := fmt.Sprintf("advertorial/addmoments/%s.%s", uuid.NewString(), ext)
	uploadURL, signErr := s3wrap.Public_s3.Store_presign_with_content_type(key, req.ContentType, addonImagePresignTTL)
	if signErr != nil {
		http.Error(w, utils.Tag_err("aau1", signErr).Error(), http.StatusInternalServerError)
		return
	}

	publicURL := s3wrap.Public_s3.Url(key)
	if urlErr := validateAdvertorialImageURL(publicURL); urlErr != nil {
		http.Error(w, utils.Tag_err("aau2", urlErr).Error(), http.StatusInternalServerError)
		return
	}

	_ = networkutils.SendJson(adminProductUploadURLRes{
		UploadURL:    uploadURL,
		PublicURL:    publicURL,
		Key:          key,
		ExpiresInSec: int(addonImagePresignTTL.Seconds()),
		RequiredHeaders: map[string]string{
			"Content-Type": req.ContentType,
		},
	}, w)
}
