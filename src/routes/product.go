package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	db "membox-serv/src/db_layer"
	db_layer "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	"membox-serv/src/env"
	"membox-serv/src/mycrypto"
	networkutils "membox-serv/src/network_utils"
	payments "membox-serv/src/payments"
	"membox-serv/src/promo"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	neturl "net/url"
	"path"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	jsonparser "github.com/buger/jsonparser"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type product_routes_typ struct{}

var ProductRoutes product_routes_typ

type Purchase_req struct {
	Provider_id   string         `json:"provider_id"`
	Purchase_info map[string]int `json:"purchase_info"`
	Email         string         `json:"email"`
	PromoCode     string         `json:"promo_code"`
	// buyer_configs: map of product_id -> JSON string of config fields
	Buyer_configs    map[string]string `json:"buyer_configs"`
	Shipping_address *types.Js_object  `json:"shipping_address"`
}

type Cart_item struct {
	Product_id string `json:"product_id"`
	Quantity   int    `json:"quantity"`
}

type adminProductUploadURLReq struct {
	ProductUID  string `json:"product_uid"`
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

type adminProductUploadURLRes struct {
	UploadURL       string            `json:"upload_url"`
	PublicURL       string            `json:"public_url"`
	Key             string            `json:"key"`
	ExpiresInSec    int               `json:"expires_in_sec"`
	RequiredHeaders map[string]string `json:"required_headers"`
}

const addonImageUploadMaxSizeBytes int64 = 5 * 1024 * 1024
const addonImagePresignTTL = 5 * time.Minute

var addonImageContentTypeToExts = map[string][]string{
	"image/jpeg": {"jpg", "jpeg"},
	"image/png":  {"png"},
	"image/webp": {"webp"},
}

// productRowToPayload, products satirini API response formatina cevirir.
func productRowToPayload(row [][]byte, keys []string, jsonKeys []string, pgArrayKeys []string, boolKeys []string) (types.Js_object, error) {
	product := types.Js_object{}
	for i := 0; i < len(keys); i++ {
		key := keys[i]
		val := string(row[i])

		if slices.Contains(jsonKeys, key) {
			options := types.Js_object{}
			if err := json.Unmarshal(row[i], &options); err != nil {
				return nil, err
			}
			product[key] = options
			continue
		}

		if slices.Contains(pgArrayKeys, key) {
			features := parsePgIntArray(val)
			product[key] = features
			if key == "granted_features" {
				// Feature booleanlari granted_features array'inden turetilir; PATCH de ayni kaynagi gunceller.
				product["voice_included"] = slices.Contains(features, dbscripts.FeatureVoice)
				product["advertorial_included"] = slices.Contains(features, dbscripts.FeatureAdvertorial)
				product["sponsored_included"] = slices.Contains(features, dbscripts.FeatureAdvertorial)
			}
			continue
		}

		if slices.Contains(boolKeys, key) {
			product[key] = val == "true"
			continue
		}

		product[key] = val
	}
	return product, nil
}

// parsePgIntArray, postgres int[] string degerini []int'e cevirir.
func parsePgIntArray(value string) []int {
	inner := strings.Trim(value, "{}")
	nums := []int{}
	if inner == "" {
		return nums
	}
	for _, s := range strings.Split(inner, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err == nil {
			nums = append(nums, n)
		}
	}
	return nums
}

// intArrayToPgRaw, []int degerini postgres int[] raw ifadesine cevirir.
func intArrayToPgRaw(values []int) string {
	if len(values) == 0 {
		return "ARRAY[]::int[]"
	}
	sort.Ints(values)
	parts := make([]string, 0, len(values))
	for _, v := range values {
		parts = append(parts, strconv.Itoa(v))
	}
	return "ARRAY[" + strings.Join(parts, ",") + "]::int[]"
}

// getFirstStringField, ilk bulunan string alanini dondurur.
func getFirstStringField(req types.Js_object, keys ...string) (string, bool) {
	for _, key := range keys {
		raw, ok := req[key]
		if !ok {
			continue
		}
		v, ok := raw.(string)
		if !ok {
			return "", false
		}
		return strings.TrimSpace(v), true
	}
	return "", false
}

// getFirstBoolField, ilk bulunan bool alanini dondurur.
func getFirstBoolField(req types.Js_object, keys ...string) (bool, bool) {
	for _, key := range keys {
		raw, ok := req[key]
		if !ok {
			continue
		}
		v, ok := raw.(bool)
		if !ok {
			return false, false
		}
		return v, true
	}
	return false, false
}

// getFirstIntField, ilk bulunan sayisal alanin int degerini dondurur.
func getFirstIntField(req types.Js_object, keys ...string) (int, bool, error) {
	for _, key := range keys {
		raw, ok := req[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case float64:
			return int(v), true, nil
		case int:
			return v, true, nil
		case string:
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil {
				return 0, true, err
			}
			return n, true, nil
		default:
			return 0, true, fmt.Errorf("invalid integer field: %s", key)
		}
	}
	return 0, false, nil
}

// getFirstFloatField, ilk bulunan sayisal alanin float degerini dondurur.
func getFirstFloatField(req types.Js_object, keys ...string) (float64, bool, error) {
	for _, key := range keys {
		raw, ok := req[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case float64:
			return v, true, nil
		case int:
			return float64(v), true, nil
		case string:
			n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil {
				return 0, true, err
			}
			return n, true, nil
		default:
			return 0, true, fmt.Errorf("invalid number field: %s", key)
		}
	}
	return 0, false, nil
}

// productDisplayFallback, S3 dil dosyasinda yeni urun id'si yoksa DB'deki
// display alanlarini kullanarak odeme notunun olusmaya devam etmesini saglar.
func productDisplayFallback(product [][]byte, lang string, name bool) string {
	productID := string(product[2])
	if name {
		if lang == "uk" && strings.TrimSpace(string(product[4])) != "" {
			return string(product[4])
		}
		if strings.TrimSpace(string(product[3])) != "" {
			return string(product[3])
		}
		return productID
	}

	if lang == "uk" && strings.TrimSpace(string(product[6])) != "" {
		return string(product[6])
	}
	if strings.TrimSpace(string(product[5])) != "" {
		return string(product[5])
	}
	return productID
}

// mergeGrantedFeature, verilen featureID'nin granted_features listesinde olup olmayacagini belirler.
// include=true ise feature eklenir, include=false ise feature listeden cikarilir.
func mergeGrantedFeature(features []int, featureID int, include bool) []int {
	hasFeature := slices.Contains(features, featureID)
	if include && !hasFeature {
		return append(features, featureID)
	}
	if !include && hasFeature {
		result := make([]int, 0, len(features))
		for _, f := range features {
			if f != featureID {
				result = append(result, f)
			}
		}
		return result
	}
	return features
}

// validateAddonImageURL, add-on gorsel URL'inin izinli S3 formatinda olup olmadigini dogrular.
func validateAddonImageURL(raw string) error {
	imageURL := strings.TrimSpace(raw)
	if imageURL == "" {
		return nil
	}

	parsed, err := neturl.Parse(imageURL)
	if err != nil {
		return errors.New("invalid image url")
	}
	if parsed.Scheme != "https" {
		return errors.New("image url must use https")
	}
	if parsed.Host != "memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com" {
		return errors.New("image url host must be memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com")
	}
	if !strings.HasPrefix(parsed.Path, "/addon_banner/") {
		return errors.New("image url path must start with /addon_banner/")
	}

	ext := strings.ToLower(strings.TrimPrefix(path.Ext(parsed.Path), "."))
	switch ext {
	case "jpg", "jpeg", "png", "webp":
		return nil
	default:
		return errors.New("image url extension must be jpg, jpeg, png or webp")
	}
}

// resolveAddonImageExt, dosya adi ve content-type bilgisinden guvenli uzantiyi belirler.
func resolveAddonImageExt(fileName string, contentType string) (string, error) {
	baseName := strings.TrimSpace(path.Base(fileName))
	if baseName == "" || baseName == "." || baseName == "/" {
		return "", errors.New("invalid file_name")
	}
	if strings.Contains(baseName, "/") || strings.Contains(baseName, "\\") {
		return "", errors.New("file_name must not contain path separators")
	}

	ext := strings.TrimPrefix(strings.ToLower(path.Ext(baseName)), ".")
	if ext == "" {
		return "", errors.New("file_name must include extension")
	}

	allowedExts, ok := addonImageContentTypeToExts[contentType]
	if !ok {
		return "", errors.New("unsupported content_type")
	}
	if !slices.Contains(allowedExts, ext) {
		return "", errors.New("file extension does not match content_type")
	}

	return ext, nil
}

// snapshotCartItemPrices, satin alim anindaki urun fiyatlarini cart_items satirina sabitler.
func snapshotCartItemPrices(cartUID string, cartItems []types.CartItem, products [][][]byte) error {
	priceByProductUID := map[string]float64{}
	for _, product := range products {
		price, err := strconv.ParseFloat(string(product[1]), 64)
		if err != nil {
			return err
		}
		priceByProductUID[string(product[0])] = price
	}

	for _, item := range cartItems {
		price, ok := priceByProductUID[item.ProductUID]
		if !ok {
			return fmt.Errorf("product not found for snapshot: %s", item.ProductUID)
		}

		ub := sqlbuilder.NewUpdateBuilder()
		ub.Update("cart_items")
		ub.Set(ub.Assign("unit_price_snapshot", price))
		ub.Where(
			ub.Equal("cart_uid", cartUID),
			ub.Equal("product_uid", item.ProductUID),
			ub.IsNull("unit_price_snapshot"),
		)

		if err := db.Exec(ub); err != nil {
			return err
		}
	}

	return nil
}

// loadPromoPartnershipSnapshot, purchase aninda promo partner bilgisini sabitler.
func loadPromoPartnershipSnapshot(promoCodeUID string) (interface{}, interface{}, error) {
	if strings.TrimSpace(promoCodeUID) == "" {
		return nil, nil, nil
	}

	row, err := db.Query_one(sqlbuilder.BuildNamed(`
		SELECT
			COALESCE(pc.partnership_uid::text, '') AS partnership_uid,
			COALESCE(p.name, '') AS name,
			COALESCE(p.surname, '') AS surname,
			COALESCE(p.company_name, '') AS company_name,
			COALESCE(p.phone, '') AS phone,
			COALESCE(p.email, '') AS email
		FROM promo_codes pc
		LEFT JOIN partnerships p ON p.uid = pc.partnership_uid
		WHERE pc.uid = ${promo_code_uid}
		LIMIT 1
	`, map[string]interface{}{"promo_code_uid": promoCodeUID}))
	if err != nil {
		return nil, nil, err
	}
	if string(row[0]) == "" {
		return nil, nil, nil
	}

	snapshot := types.Js_object{
		"uid":          string(row[0]),
		"name":         string(row[1]),
		"surname":      string(row[2]),
		"company_name": emptyStringAsNil(row[3]),
		"phone":        emptyStringAsNil(row[4]),
		"email":        emptyStringAsNil(row[5]),
	}
	return string(row[0]), snapshot.Json(), nil
}

func (rfrnc product_routes_typ) GetProducts(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload []types.Js_object
	var err error

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

	keys := []string{
		"id",
		"price",
		"options",
		"priority",
		"fullfillment_type",
		"granted_features",
		"is_add_on",
		"is_enabled",
		"display_name_en",
		"display_name_uk",
		"display_description_en",
		"display_description_uk",
		"display_bullets_en",
		"display_bullets_uk",
	}
	jsonKeys := []string{"options"}
	pgArrayKeys := []string{"granted_features"}
	boolKeys := []string{"is_add_on", "is_enabled"}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select(keys...)
	sb.From("products")
	sb.Where(sb.GreaterThan("priority", 0), sb.Equal("is_enabled", true))
	sb.OrderByDesc("priority")

	res, err := db_layer.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("mce1", err)
		return
	}

	for _, row := range res {
		product, convErr := productRowToPayload(row, keys, jsonKeys, pgArrayKeys, boolKeys)
		if convErr != nil {
			err = utils.Tag_err("mce2", convErr)
			fmt.Println("error: ", err)
			return
		}
		payload = append(payload, product)
	}

	stat_code = 200
	return
}

// AdminListProducts, super admin icin urunleri display alanlariyla listeler.
func (rfrnc product_routes_typ) AdminListProducts(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload []types.Js_object
	var err error

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

	keys := []string{
		"uid",
		"id",
		"price",
		"options",
		"priority",
		"fullfillment_type",
		"granted_features",
		"is_add_on",
		"is_enabled",
		"display_name_en",
		"display_name_uk",
		"display_description_en",
		"display_description_uk",
		"display_bullets_en",
		"display_bullets_uk",
	}
	jsonKeys := []string{"options"}
	pgArrayKeys := []string{"granted_features"}
	boolKeys := []string{"is_add_on", "is_enabled"}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select(keys...)
	sb.From("products")
	sb.OrderByDesc("priority")

	res, err := db_layer.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("alp1", err)
		return
	}

	payload = make([]types.Js_object, 0, len(res))
	for _, row := range res {
		product, convErr := productRowToPayload(row, keys, jsonKeys, pgArrayKeys, boolKeys)
		if convErr != nil {
			err = utils.Tag_err("alp2", convErr)
			return
		}
		payload = append(payload, product)
	}

	stat_code = 200
}

// AdminCreateAddonImageUploadURL, add-on urunler icin S3 upload URL'i uretir.
func (rfrnc product_routes_typ) AdminCreateAddonImageUploadURL(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload interface{}
	var err error

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

	req := adminProductUploadURLReq{}
	if decErr := json.NewDecoder(r.Body).Decode(&req); decErr != nil {
		stat_code = 400
		err = errors.New("invalid request body")
		return
	}

	req.ProductUID = strings.TrimSpace(req.ProductUID)
	req.FileName = strings.TrimSpace(req.FileName)
	req.ContentType = strings.TrimSpace(req.ContentType)

	if req.ProductUID == "" {
		stat_code = 400
		err = errors.New("product_uid is required")
		return
	}
	if req.FileName == "" {
		stat_code = 400
		err = errors.New("file_name is required")
		return
	}
	if req.ContentType == "" {
		stat_code = 400
		err = errors.New("content_type is required")
		return
	}
	if req.SizeBytes <= 0 {
		stat_code = 400
		err = errors.New("size_bytes must be > 0")
		return
	}
	if req.SizeBytes > addonImageUploadMaxSizeBytes {
		stat_code = 400
		err = fmt.Errorf("size_bytes must be <= %d", addonImageUploadMaxSizeBytes)
		return
	}

	ext, extErr := resolveAddonImageExt(req.FileName, req.ContentType)
	if extErr != nil {
		stat_code = 400
		err = extErr
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("is_add_on")
	sb.From("products")
	sb.Where(sb.Equal("uid", req.ProductUID))

	row, dbErr := db.Query_one(sb)
	if dbErr != nil {
		stat_code = 404
		err = errors.New("product not found")
		return
	}

	if string(row[0]) != "true" {
		stat_code = http.StatusUnprocessableEntity
		err = errors.New("image upload is only allowed for add-on products")
		return
	}

	key := fmt.Sprintf("addon_banner/%s/%s.%s", req.ProductUID, uuid.NewString(), ext)

	uploadURL, signErr := s3wrap.Public_s3.Store_presign_with_content_type(key, req.ContentType, addonImagePresignTTL)
	if signErr != nil {
		err = utils.Tag_err("acaiu1", signErr)
		return
	}

	publicURL := s3wrap.Public_s3.Url(key)
	if urlErr := validateAddonImageURL(publicURL); urlErr != nil {
		err = utils.Tag_err("acaiu2", urlErr)
		return
	}

	payload = adminProductUploadURLRes{
		UploadURL:    uploadURL,
		PublicURL:    publicURL,
		Key:          key,
		ExpiresInSec: int(addonImagePresignTTL / time.Second),
		RequiredHeaders: map[string]string{
			"Content-Type": req.ContentType,
		},
	}
}

// AdminUpdateProduct, admin panelden gelen izinli alanlari gunceller.
func (rfrnc product_routes_typ) AdminUpdateProduct(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var err error

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
		}
	})()

	productUID := mux.Vars(r)["productUid"]
	if strings.TrimSpace(productUID) == "" {
		stat_code = 400
		err = errors.New("product uid is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = utils.Tag_err("aup1", err)
		return
	}

	req := types.Js_object{}
	if err = json.Unmarshal(body, &req); err != nil {
		err = utils.Tag_err("aup2", err)
		stat_code = 400
		return
	}

	if _, hasID := req["id"]; hasID {
		stat_code = 400
		err = errors.New("product_id is immutable and cannot be updated")
		return
	}
	if _, hasProductID := req["product_id"]; hasProductID {
		stat_code = 400
		err = errors.New("product_id is immutable and cannot be updated")
		return
	}

	selectKeys := []string{
		"uid",
		"id",
		"price",
		"options",
		"priority",
		"fullfillment_type",
		"granted_features",
		"is_add_on",
		"is_enabled",
		"display_name_en",
		"display_name_uk",
		"display_description_en",
		"display_description_uk",
		"display_bullets_en",
		"display_bullets_uk",
	}
	jsonKeys := []string{"options"}
	pgArrayKeys := []string{"granted_features"}
	boolKeys := []string{"is_add_on", "is_enabled"}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select(selectKeys...)
	sb.From("products")
	sb.Where(sb.Equal("uid", productUID))

	row, err := db.Query_one(sb)
	if err != nil {
		stat_code = 404
		err = utils.Tag_err("aup3", err)
		return
	}

	currentProduct, convErr := productRowToPayload(row, selectKeys, jsonKeys, pgArrayKeys, boolKeys)
	if convErr != nil {
		err = utils.Tag_err("aup4", convErr)
		return
	}

	options := types.Js_object{}
	if opt, ok := currentProduct["options"].(types.Js_object); ok {
		options = opt
	}

	grantedFeatures := []int{}
	if gf, ok := currentProduct["granted_features"].([]int); ok {
		grantedFeatures = append(grantedFeatures, gf...)
	}
	isAddOn, _ := currentProduct["is_add_on"].(bool)

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("products")
	assignments := []string{}

	if priceVal, hasPrice, parseErr := getFirstFloatField(req, "price", "package_price", "add_on_price"); hasPrice {
		if parseErr != nil {
			stat_code = 400
			err = parseErr
			return
		}
		if priceVal < 0 {
			stat_code = 400
			err = errors.New("price must be >= 0")
			return
		}
		assignments = append(assignments, ub.Assign("price", priceVal))
	}

	displayNameEN, hasDisplayNameEN := getFirstStringField(req, "display_name_en")
	displayNameUK, hasDisplayNameUK := getFirstStringField(req, "display_name_uk")
	commonName, hasCommonName := getFirstStringField(req, "name", "package_name", "add_on_name")
	if hasCommonName {
		if !hasDisplayNameEN {
			displayNameEN = commonName
			hasDisplayNameEN = true
		}
		if !hasDisplayNameUK {
			displayNameUK = commonName
			hasDisplayNameUK = true
		}
	}
	if hasDisplayNameEN {
		assignments = append(assignments, ub.Assign("display_name_en", displayNameEN))
	}
	if hasDisplayNameUK {
		assignments = append(assignments, ub.Assign("display_name_uk", displayNameUK))
	}

	displayDescEN, hasDisplayDescEN := getFirstStringField(req, "display_description_en")
	displayDescUK, hasDisplayDescUK := getFirstStringField(req, "display_description_uk")
	commonDesc, hasCommonDesc := getFirstStringField(req, "description", "package_description", "add_on_description")
	if hasCommonDesc {
		if !hasDisplayDescEN {
			displayDescEN = commonDesc
			hasDisplayDescEN = true
		}
		if !hasDisplayDescUK {
			displayDescUK = commonDesc
			hasDisplayDescUK = true
		}
	}
	if hasDisplayDescEN {
		assignments = append(assignments, ub.Assign("display_description_en", displayDescEN))
	}
	if hasDisplayDescUK {
		assignments = append(assignments, ub.Assign("display_description_uk", displayDescUK))
	}

	// Kartta gosterilecek satirlar admin tarafindan manuel yazilir.
	displayBulletsEN, hasDisplayBulletsEN := getFirstStringField(req, "display_bullets_en")
	displayBulletsUK, hasDisplayBulletsUK := getFirstStringField(req, "display_bullets_uk")
	commonBullets, hasCommonBullets := getFirstStringField(req, "display_bullets")
	if hasCommonBullets {
		if !hasDisplayBulletsEN {
			displayBulletsEN = commonBullets
			hasDisplayBulletsEN = true
		}
		if !hasDisplayBulletsUK {
			displayBulletsUK = commonBullets
			hasDisplayBulletsUK = true
		}
	}
	if hasDisplayBulletsEN {
		assignments = append(assignments, ub.Assign("display_bullets_en", displayBulletsEN))
	}
	if hasDisplayBulletsUK {
		assignments = append(assignments, ub.Assign("display_bullets_uk", displayBulletsUK))
	}

	// Add-on aktif/pasif kontrolu sadece add-on urunler icin aciktir.
	if rawEnabled, hasEnabled := req["is_enabled"]; hasEnabled {
		isEnabled, ok := rawEnabled.(bool)
		if !ok {
			stat_code = 400
			err = errors.New("is_enabled must be a boolean")
			return
		}
		if !isAddOn {
			stat_code = http.StatusUnprocessableEntity
			err = errors.New("is_enabled can only be updated for add-on products")
			return
		}
		assignments = append(assignments, ub.Assign("is_enabled", isEnabled))
	}

	// Add-on kart gorseli yalnizca add-on urunlerde guncellenebilir.
	optionsFromReq := types.Js_object{}
	if rawOptions, hasOptions := req["options"]; hasOptions {
		switch v := rawOptions.(type) {
		case map[string]interface{}:
			optionsFromReq = types.Js_object(v)
		case types.Js_object:
			optionsFromReq = v
		default:
			stat_code = 400
			err = errors.New("options must be an object")
			return
		}
	}

	optionsChanged := false
	if rawImage, hasImage := optionsFromReq["image"]; hasImage {
		imageURL, ok := rawImage.(string)
		if !ok {
			stat_code = 400
			err = errors.New("options.image must be a string")
			return
		}
		if !isAddOn {
			stat_code = http.StatusUnprocessableEntity
			err = errors.New("options.image can only be updated for add-on products")
			return
		}

		imageURL = strings.TrimSpace(imageURL)
		if imageURL == "" {
			delete(options, "image")
			optionsChanged = true
		} else {
			if err = validateAddonImageURL(imageURL); err != nil {
				stat_code = 400
				return
			}

			// Yanlis/eksik upload URL'siyle DB'ye kirik link yazilmamasi icin
			// ilgili S3 objesinin varligini kontrol ediyoruz.
			imagePath, decodeErr := s3wrap.Public_s3.Decode_url(imageURL)
			if decodeErr != nil {
				stat_code = 400
				err = errors.New("invalid image url for this s3 bucket")
				return
			}
			imagePath = strings.TrimPrefix(imagePath, "/")
			exists, existsErr := s3wrap.Public_s3.Exists(imagePath)
			if existsErr != nil {
				err = utils.Tag_err("aup8", existsErr)
				return
			}
			if !exists {
				stat_code = http.StatusUnprocessableEntity
				err = errors.New("options.image object not found in s3")
				return
			}

			options["image"] = imageURL
			optionsChanged = true
		}
	}

	if rawMobileImage, hasMobileImage := optionsFromReq["mobile_image"]; hasMobileImage {
		mobileImageURL, ok := rawMobileImage.(string)
		if !ok {
			stat_code = 400
			err = errors.New("options.mobile_image must be a string")
			return
		}
		if !isAddOn {
			stat_code = http.StatusUnprocessableEntity
			err = errors.New("options.mobile_image can only be updated for add-on products")
			return
		}

		mobileImageURL = strings.TrimSpace(mobileImageURL)
		if mobileImageURL == "" {
			delete(options, "mobile_image")
			optionsChanged = true
		} else {
			if err = validateAddonImageURL(mobileImageURL); err != nil {
				stat_code = 400
				return
			}

			// Mobil gorsel de ayni add-on S3 upload akisini kullanir; kirik link
			// yazmamak icin desktop image ile ayni bucket/object kontrolunden geciriyoruz.
			mobileImagePath, decodeErr := s3wrap.Public_s3.Decode_url(mobileImageURL)
			if decodeErr != nil {
				stat_code = 400
				err = errors.New("invalid mobile_image url for this s3 bucket")
				return
			}
			mobileImagePath = strings.TrimPrefix(mobileImagePath, "/")
			exists, existsErr := s3wrap.Public_s3.Exists(mobileImagePath)
			if existsErr != nil {
				err = utils.Tag_err("aup9", existsErr)
				return
			}
			if !exists {
				stat_code = http.StatusUnprocessableEntity
				err = errors.New("options.mobile_image object not found in s3")
				return
			}

			options["mobile_image"] = mobileImageURL
			optionsChanged = true
		}
	}

	if guestCount, hasGuest, parseErr := getFirstIntField(req, "guest_count"); hasGuest {
		if parseErr != nil {
			stat_code = 400
			err = parseErr
			return
		}
		if guestCount < -1 {
			stat_code = 400
			err = errors.New("guest_count must be >= -1")
			return
		}
		options["guest_count"] = guestCount
		optionsChanged = true
	}

	if mediaCount, hasMedia, parseErr := getFirstIntField(req, "media_count"); hasMedia {
		if parseErr != nil {
			stat_code = 400
			err = parseErr
			return
		}
		if mediaCount < -1 {
			stat_code = 400
			err = errors.New("media_count must be >= -1")
			return
		}
		options["media_count"] = mediaCount
		optionsChanged = true
	}

	if activationDays, hasActivation, parseErr := getFirstIntField(req, "activation_period_days", "activation_days"); hasActivation {
		if parseErr != nil {
			stat_code = 400
			err = parseErr
			return
		}
		if activationDays <= 0 {
			stat_code = 400
			err = errors.New("activation_period_days must be > 0")
			return
		}
		options["activation_days"] = activationDays
		optionsChanged = true
	}

	if storageDays, hasStorage, parseErr := getFirstIntField(req, "storage_period_days", "storage_days"); hasStorage {
		if parseErr != nil {
			stat_code = 400
			err = parseErr
			return
		}
		if storageDays <= 0 {
			stat_code = 400
			err = errors.New("storage_period_days must be > 0")
			return
		}
		options["storage_days"] = storageDays
		optionsChanged = true
	}

	if optionsChanged {
		assignments = append(assignments, ub.Assign("options", options.Json()))
	}

	grantedFeaturesChanged := false

	// voice_included, urunun voice feature hakkini granted_features listesinde ac/kapa olarak yonetir.
	if voiceIncluded, hasVoice := getFirstBoolField(req, "voice_included"); hasVoice {
		grantedFeatures = mergeGrantedFeature(grantedFeatures, dbscripts.FeatureVoice, voiceIncluded)
		grantedFeaturesChanged = true
	}

	// advertorial_included/sponsored_included, reklam alani feature hakkini granted_features listesinde ac/kapa olarak yonetir.
	if advertorialIncluded, hasAdvertorial := getFirstBoolField(req, "advertorial_included", "sponsored_included"); hasAdvertorial {
		grantedFeatures = mergeGrantedFeature(grantedFeatures, dbscripts.FeatureAdvertorial, advertorialIncluded)
		grantedFeaturesChanged = true
	}

	if grantedFeaturesChanged {
		assignments = append(assignments, ub.Assign("granted_features", sqlbuilder.Raw(intArrayToPgRaw(grantedFeatures))))
	}

	if len(assignments) == 0 {
		stat_code = 200
		networkutils.SendJson(currentProduct, w)
		return
	}

	ub.Set(assignments...)
	ub.Where(ub.Equal("uid", productUID))

	if err = db.Exec(ub); err != nil {
		err = utils.Tag_err("aup5", err)
		return
	}

	refetchSB := sqlbuilder.NewSelectBuilder()
	refetchSB.Select(selectKeys...)
	refetchSB.From("products")
	refetchSB.Where(refetchSB.Equal("uid", productUID))

	updatedRow, err := db.Query_one(refetchSB)
	if err != nil {
		err = utils.Tag_err("aup6", err)
		return
	}

	updatedProduct, convErr := productRowToPayload(updatedRow, selectKeys, jsonKeys, pgArrayKeys, boolKeys)
	if convErr != nil {
		err = utils.Tag_err("aup7", convErr)
		return
	}

	stat_code = 200
	networkutils.SendJson(updatedProduct, w)
}

// SimulatePaymentSuccess is a DEV-ONLY endpoint that manually sets provider_id
// on a purchase, simulating what the LiqPay server callback would do.
// Register this route only when is_live == false.
func (rfrnc product_routes_typ) SimulatePaymentSuccess(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	encPackedUID := vars["encPackedUID"]

	packedUID, err := mycrypto.Decrypt(encPackedUID, []byte(env.Env().PaymentSecret))
	if err != nil {
		http.Error(w, "invalid token", http.StatusBadRequest)
		return
	}

	purchaseUID, err := utils.UUID.UnpackUUID(packedUID)
	if err != nil {
		http.Error(w, "invalid token", http.StatusBadRequest)
		return
	}

	fmt.Println("simulate-success | purchaseUID:", purchaseUID)

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("promo_code_uid").From("purchases").Where(sb.Equal("uid", purchaseUID))
	promoRow, err := db.Query_one(sb)
	if err != nil {
		fmt.Println("simulate-success | promo read error:", err)
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	promoCodeUID := string(promoRow[0])

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("purchases").Set(
		ub.Assign("provider_id", "dev_simulated_"+purchaseUID),
		ub.Assign("provider", "dev"),
	).Where(ub.Equal("uid", purchaseUID), ub.IsNull("provider_id"))
	ub.SQL("RETURNING uid")
	updateRows, err := db.Query_all(ub)
	if err != nil {
		fmt.Println("simulate-success | db error:", err)
		http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if len(updateRows) == 0 {
		fmt.Println("simulate-success | already processed, skipping promo usage increment")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true,"already_processed":true}`))
		return
	}

	if promoCodeUID != "" {
		// DEV simulate endpoint'i gercek callback'i bypass ettigi icin, testte
		// usage_count davranisini dogru gormek adina ayni idempotent artisi burada da yapar.
		promoUB := sqlbuilder.NewUpdateBuilder()
		promoUB.Update("promo_codes").Set(
			promoUB.Assign("usage_count", sqlbuilder.Raw("usage_count + 1")),
		).Where(promoUB.Equal("uid", promoCodeUID))
		if promoErr := db.Exec(promoUB); promoErr != nil {
			fmt.Println("simulate-success | promo usage error:", promoErr)
			http.Error(w, "db error: "+promoErr.Error(), http.StatusInternalServerError)
			return
		}
	}
	fmt.Println("simulate-success | done")

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"ok":true}`))
}

func (rfrnc product_routes_typ) PurchaseStatus(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	encPackedUID := vars["encPackedUID"]

	packedUID, err := mycrypto.Decrypt(encPackedUID, []byte(env.Env().PaymentSecret))
	if err != nil {
		http.Error(w, "invalid token", http.StatusBadRequest)
		return
	}

	purchaseUID, err := utils.UUID.UnpackUUID(packedUID)
	if err != nil {
		http.Error(w, "invalid token", http.StatusBadRequest)
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("provider_id", "purchase_info", "COALESCE(net_total, 0)::text").From("purchases").Where(sb.Equal("uid", purchaseUID))
	res, err := db.Query_one(sb)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	providerID := string(res[0])
	// Not yet processed by server callback
	if providerID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{"status":"pending"}`))
		return
	}

	// Payment was recorded as failed
	if strings.HasPrefix(providerID, "failed:") {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "failed"})
		return
	}

	// Build signup token same way as payment-cb.go
	purchaseInfo := types.Js_object{}
	if err := json.Unmarshal(res[1], &purchaseInfo); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	decEmail, _ := purchaseInfo["email"].(string)
	netTotal, err := strconv.ParseFloat(string(res[2]), 64)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	pt := payments.PaymentToken{
		ReferenceNo: packedUID,
		Status:      "m:" + decEmail,
		Provider:    "admt_payment",
	}
	encPaymentTkn, err := pt.Encrypt(env.Env().PaymentSecret)
	if err != nil {
		http.Error(w, "token error", http.StatusInternalServerError)
		return
	}

	server_url := "https://" + env.Env().ServRoot
	signup_url := server_url + "/signup/" + encPaymentTkn

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(types.Js_object{
		"status":     "success",
		"signup_url": signup_url,
		"email":      decEmail,
		"net_total":  netTotal,
	})
}

func (rfrnc product_routes_typ) Purchase(w http.ResponseWriter, r *http.Request) {
	var err error
	var stat_code int

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			fmt.Println("error: ", err)
			http.Error(w, err.Error(), stat_code)
			return
		}
	})()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = utils.Tag_err("mce1", err)
		return
	}

	var req Purchase_req
	err = json.Unmarshal(body, &req)
	if err != nil {
		err = utils.Tag_err("mce2", err)
		return
	}

	provider, ok := payments.PaymentProviders[req.Provider_id]
	if !ok {
		providers := []string{}
		for provider := range payments.PaymentProviders {
			providers = append(providers, provider)
		}
		err = errors.New("provider not found: " + strings.Join(providers, ", ") + ", but got: " + req.Provider_id)
		return
	}

	fmt.Println("provider found: ", provider.Name, provider, req)

	lang := "en"
	AcceptLanguage := r.Header.Get("Accept-Language")
	if AcceptLanguage != "" {
		lang = strings.Split(strings.Split(AcceptLanguage, ",")[0], "-")[0]
	}

	fmt.Println("AcceptLanguage: ", AcceptLanguage, lang)

	cart := types.Cart{}
	cartUID, cartItems, err := cart.InsertQuantityMapWithConfigs(req.Purchase_info, req.Buyer_configs)
	if err != nil {
		err = utils.Tag_err("mce4", err)
		return
	}

	getByt := func(url string) (byt []byte, err error) {
		res, err := http.Get(url)
		if err != nil {
			return
		}
		defer res.Body.Close()
		return io.ReadAll(res.Body)
	}

	fmt.Println("0", s3wrap.Public_s3.Url("/ui/assets/lang/"+lang+".json"))

	langByt, err := getByt(s3wrap.Public_s3.Url("/ui/assets/lang/" + lang + ".json"))
	if err != nil {
		err = utils.Tag_err("mce3", err)
		return
	}
	if len(langByt) == 0 || langByt[0] != '{' {
		langByt, err = getByt(s3wrap.Public_s3.Url("/ui/assets/lang/en.json"))
		if err != nil {
			err = utils.Tag_err("mce3b", err)
			return
		}
	}

	fmt.Println("3")

	note := "Addmoments Purchase:\n"
	totalPrice := 0.0
	paymentAmount := 0.0
	var promoQuote promo.Quote
	var promoPartnershipUID interface{}
	var promoPartnershipSnapshot interface{}
	hasPromoCode := promo.NormalizeCode(req.PromoCode) != ""

	product_uids := []string{}
	for _, row := range cartItems {
		product_uids = append(product_uids, row.ProductUID)
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select(
		"uid",
		"price",
		"id",
		"display_name_en",
		"display_name_uk",
		"display_description_en",
		"display_description_uk",
	).From("products").Where(sb.In("uid", db_layer.Interface_ar(product_uids)...))
	products, err := db_layer.Query_all(sb)
	if err != nil {
		err = utils.Tag_err("mce4", err)
		return
	}

	// Satin alim tutarliligi icin, o andaki fiyatlari cart item satirlarina sabitle.
	err = snapshotCartItemPrices(cartUID, cartItems, products)
	if err != nil {
		err = utils.Tag_err("mce4s", err)
		return
	}

	for _, item := range cartItems {
		product_uid := item.ProductUID
		product_idx := slices.IndexFunc(products, func(product [][]byte) bool {
			return string(product[0]) == product_uid
		})
		if product_idx == -1 {
			err = utils.Tag_err("mce4", fmt.Errorf("product not found: %s", product_uid))
			return
		}
		product := products[product_idx]
		product_id := string(product[2])
		quantity := item.Quantity
		price, err := strconv.ParseFloat(string(product[1]), 64)
		if err != nil {
			err = utils.Tag_err("mce4", err)
			return
		}

		var prodName string
		var prodDesc string
		fmt.Println("langByt: ", string(langByt))
		prodName, err = jsonparser.GetString(langByt, "products", product_id, "name")
		if err != nil {
			prodName = productDisplayFallback(product, lang, true)
		}
		prodDesc, err = jsonparser.GetString(langByt, "products", product_id, "description")
		if err != nil {
			prodDesc = productDisplayFallback(product, lang, false)
		}

		note += fmt.Sprintf("%s: %dx %s\n", prodName, quantity, prodDesc)

		totalPrice += price * float64(quantity)
	}
	paymentAmount = totalPrice

	// Promo code tamamen opsiyoneldir: bos gelirse eski fiyat akisi aynen korunur.
	// Dolu gelirse checkout preview'e guvenmeyip payment olusturmadan once tekrar validate ederiz.
	if hasPromoCode {
		promoQuote, err = promo.Validate(req.PromoCode, req.Purchase_info)
		if err != nil {
			if code, ok := promo.ErrorCodeOf(err); ok {
				_ = networkutils.SendErrorJSON(w, http.StatusBadRequest, string(code), err.Error())
				err = nil
				return
			}
			err = utils.Tag_err("mce4p", err)
			return
		}
		paymentAmount = promoQuote.NetTotal
		promoPartnershipUID, promoPartnershipSnapshot, err = loadPromoPartnershipSnapshot(promoQuote.PromoCodeUID)
		if err != nil {
			err = utils.Tag_err("mce4ps", err)
			return
		}
	}

	purchaseInfo := types.Js_object{"email": req.Email}
	if req.Shipping_address != nil {
		purchaseInfo["shipping_address"] = req.Shipping_address
	}
	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("purchases")
	if hasPromoCode {
		ib.Cols(
			"purchase_info",
			"cart_uid",
			"provider",
			"promo_code_uid",
			"promo_code_text_snapshot",
			"promo_partnership_uid",
			"promo_partnership_snapshot",
			"gross_total",
			"discount_amount",
			"net_total",
		)
		ib.Values(
			purchaseInfo.Json(),
			cartUID,
			provider.Name,
			promoQuote.PromoCodeUID,
			promoQuote.PromoCodeTextSnapshot,
			promoPartnershipUID,
			promoPartnershipSnapshot,
			promoQuote.GrossTotal,
			promoQuote.DiscountAmount,
			promoQuote.NetTotal,
		)
	} else {
		// Promosuz yeni purchase'larda da toplam snapshotlarini dolduruyoruz.
		// Eski odeme davranisi degismez; sadece raporlar icin net/gross alanlari hazir olur.
		ib.Cols("purchase_info", "cart_uid", "provider", "gross_total", "discount_amount", "net_total")
		ib.Values(purchaseInfo.Json(), cartUID, provider.Name, totalPrice, 0, totalPrice)
	}
	ib.SQL("RETURNING uid")
	res, err := db.Query_one(ib)
	if err != nil {
		err = utils.Tag_err("pcb4", err)
		return
	}
	purchaseUID := string(res[0])

	packedPurchaseUID, err := utils.UUID.PackUUID(purchaseUID)
	if err != nil {
		err = utils.Tag_err("mce7", err)
		return
	}

	encPackedPurchaseUID, err := mycrypto.Encrypt(packedPurchaseUID, []byte(env.Env().PaymentSecret))
	if err != nil {
		err = utils.Tag_err("mce8", err)
		return
	}

	paymentreq := payments.PaymentRequest{
		Currency:               "UAH",
		Addcomission_to_amount: true,
		Note:                   note,
		Amount:                 paymentAmount,
		Name:                   req.Email,
		ReferenceNo:            encPackedPurchaseUID,
	}

	fmt.Println("paymentreq: ", paymentreq, paymentreq.ReferenceNo)

	err = provider.OpenPayment(w, r, paymentreq)

}
