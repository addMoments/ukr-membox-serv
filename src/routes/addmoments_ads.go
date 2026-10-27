package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	db "membox-serv/src/db_layer"
	networkutils "membox-serv/src/network_utils"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"

	"github.com/google/uuid"
	"github.com/huandu/go-sqlbuilder"
)

// AddMoments reklami (Excel madde 6): ozel paketli etkinliklerin (ozellik 5) misafir sayfasinda
// gosterilen reklam. Tum bu etkinliklerde ayni; super admin admin urun listesinden duzenler,
// host goremez ve degistiremez (musteri 26 Ekim: "Addmoments admin owns access to this area").
// Kayit site_settings.addmoments_ads'te (18-special-packages.sql), bicim etkinlik reklamiyla ayni.

type addmoments_ads_routes_typ struct{}

var AddmomentsAdsRoutes addmoments_ads_routes_typ

const addmomentsAdsKey = "addmoments_ads"

var errAdsManagedByAddmoments = errors.New("the advertising area of this event is managed by AddMoments")

// fetchAddmomentsAdsConfig, kayitli AddMoments reklamini okur; satir yoksa reklamsiz config doner.
func fetchAddmomentsAdsConfig() (types.Js_object, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("value")
	sb.From("site_settings")
	sb.Where(sb.Equal("key", addmomentsAdsKey))

	rows, err := db.Query_all(sb)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return defaultAdvertorialConfig(), nil
	}
	return parseAdvertorialConfig(rows[0][0]), nil
}

// AdminGet, super admin icin AddMoments reklamini doner.
// GET /api/admin/addmoments-ads
func (ar addmoments_ads_routes_typ) AdminGet(w http.ResponseWriter, r *http.Request) {
	cfg, err := fetchAddmomentsAdsConfig()
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

// AdminPatch, AddMoments reklamini kaydeder. Dogrulama etkinlik reklamiyla ayni
// (layout/hucre sayisi, gorsel bizim S3 advertorial/ klasorunden, link http/https).
// PATCH /api/admin/addmoments-ads
func (ar addmoments_ads_routes_typ) AdminPatch(w http.ResponseWriter, r *http.Request) {
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
	ib.Values(addmomentsAdsKey, normalizedConfig.Json(), sqlbuilder.Raw("NOW()"))
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
// POST /api/admin/addmoments-ads/upload-url
func (ar addmoments_ads_routes_typ) AdminUploadURL(w http.ResponseWriter, r *http.Request) {
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
