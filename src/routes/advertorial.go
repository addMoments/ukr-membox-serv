package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"membox-serv/src/auth"
	db "membox-serv/src/db_layer"
	dbscripts "membox-serv/src/db_scripts"
	networkutils "membox-serv/src/network_utils"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	neturl "net/url"
	"path"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/huandu/go-sqlbuilder"
)

type advertorial_routes_typ struct{}

var AdvertorialRoutes advertorial_routes_typ

type advertorialCellPayload struct {
	Index    int    `json:"index"`
	ImageURL string `json:"image_url"`
	LinkURL  string `json:"link_url,omitempty"`
}

type advertorialConfigPayload struct {
	Layout string                   `json:"layout"`
	Cells  []advertorialCellPayload `json:"cells"`
}

type advertorialUploadURLReq struct {
	FileName    string `json:"file_name"`
	ContentType string `json:"content_type"`
	SizeBytes   int64  `json:"size_bytes"`
}

// defaultAdvertorialConfig, eventte henuz ayar yoksa reklam alani ayirmayan
// bos bir config dondurur. Frontend "none" layout'unda guest'te render etmez.
func defaultAdvertorialConfig() types.Js_object {
	return types.Js_object{
		"layout": "none",
		"cells":  []types.Js_object{},
	}
}

// expectedAdvertorialCellCount, secilen layout icin beklenen hucre sayisini dondurur.
func expectedAdvertorialCellCount(layout string) (count int, ok bool) {
	switch layout {
	case "none":
		return 0, true
	case "single", "1x1":
		return 1, true
	case "2x1", "1x2":
		return 2, true
	case "2x2":
		return 4, true
	default:
		return 0, false
	}
}

// isHTTPURL, url'in mutlak ve http/https semali olup olmadigini kontrol eder.
func isHTTPURL(raw string) bool {
	parsed, err := neturl.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil {
		return false
	}
	if !parsed.IsAbs() {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}

// validateAdvertorialImageURL, reklam gorselinin bizim S3 advertorial
// klasorunden geldigini dogrular; boylece guest tarafinda rastgele host render edilmez.
func validateAdvertorialImageURL(raw string) error {
	imageURL := strings.TrimSpace(raw)
	if imageURL == "" {
		return errors.New("image_url is required")
	}

	parsed, err := neturl.Parse(imageURL)
	if err != nil {
		return errors.New("invalid image_url")
	}
	if parsed.Scheme != "https" {
		return errors.New("image_url must use https")
	}
	if parsed.Host != "memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com" {
		return errors.New("image_url host must be memboxpub-qo1gff2e.s3.eu-north-1.amazonaws.com")
	}
	if !strings.HasPrefix(parsed.Path, "/advertorial/") {
		return errors.New("image_url path must start with /advertorial/")
	}

	ext := strings.ToLower(strings.TrimPrefix(path.Ext(parsed.Path), "."))
	switch ext {
	case "jpg", "jpeg", "png", "webp":
		return nil
	default:
		return errors.New("image_url extension must be jpg, jpeg, png or webp")
	}
}

// normalizeAndValidateAdvertorialConfig, PATCH ile gelen layout/cells payloadini
// dogrular ve DB'ye yazmaya hazir normalize bir JSON obje uretir.
func normalizeAndValidateAdvertorialConfig(req advertorialConfigPayload) (types.Js_object, error) {
	layout := strings.TrimSpace(strings.ToLower(req.Layout))
	expectedCount, ok := expectedAdvertorialCellCount(layout)
	if !ok {
		return nil, errors.New("invalid layout")
	}
	if len(req.Cells) != expectedCount {
		return nil, errors.New("cells length does not match layout")
	}

	seenIndexes := map[int]bool{}
	normalizedCells := make([]types.Js_object, 0, len(req.Cells))

	for _, cell := range req.Cells {
		if cell.Index < 0 || cell.Index >= expectedCount {
			return nil, errors.New("invalid cell index")
		}
		if seenIndexes[cell.Index] {
			return nil, errors.New("duplicate cell index")
		}
		seenIndexes[cell.Index] = true

		imageURL := strings.TrimSpace(cell.ImageURL)
		if imageURL == "" {
			return nil, errors.New("image_url is required")
		}
		if err := validateAdvertorialImageURL(imageURL); err != nil {
			return nil, err
		}

		linkURL := strings.TrimSpace(cell.LinkURL)
		if linkURL != "" && !isHTTPURL(linkURL) {
			return nil, errors.New("link_url must be a valid http/https url")
		}

		normalizedCell := types.Js_object{
			"index":     cell.Index,
			"image_url": imageURL,
		}
		if linkURL != "" {
			normalizedCell["link_url"] = linkURL
		} else {
			normalizedCell["link_url"] = ""
		}

		normalizedCells = append(normalizedCells, normalizedCell)
	}

	sort.Slice(normalizedCells, func(i int, j int) bool {
		return normalizedCells[i]["index"].(int) < normalizedCells[j]["index"].(int)
	})

	return types.Js_object{
		"layout": layout,
		"cells":  normalizedCells,
	}, nil
}

// fetchEventAdvertorialConfig, event kaydindaki advertorial_config kolonunu okur.
// Bos veya parse edilemeyen degerlerde varsayilan config'e geri duser.
func fetchEventAdvertorialConfig(eventUID string) (types.Js_object, error) {
	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("advertorial_config")
	sb.From("events")
	sb.Where(
		sb.Equal("uid", eventUID),
		sb.IsNull("deleted_at"),
	)

	row, err := db.Query_one(sb)
	if err != nil {
		return nil, err
	}

	cfg := defaultAdvertorialConfig()
	raw := strings.TrimSpace(string(row[0]))
	if raw == "" || raw == "null" {
		return cfg, nil
	}

	parsed := types.Js_object{}
	if err := json.Unmarshal(row[0], &parsed); err != nil {
		return cfg, nil
	}

	if layoutRaw, ok := parsed["layout"].(string); ok && layoutRaw != "" {
		cfg["layout"] = strings.TrimSpace(strings.ToLower(layoutRaw))
	}
	if cellsRaw, ok := parsed["cells"].([]interface{}); ok {
		normalizedCells := make([]types.Js_object, 0, len(cellsRaw))
		for _, c := range cellsRaw {
			if m, ok := c.(map[string]interface{}); ok {
				normalizedCells = append(normalizedCells, types.Js_object(m))
				continue
			}
			if m, ok := c.(types.Js_object); ok {
				normalizedCells = append(normalizedCells, m)
			}
		}
		cfg["cells"] = normalizedCells
	}

	return cfg, nil
}

// PrivateUploadURL, event adminine reklam gorseli icin presigned S3 upload URL'i uretir.
// Upload tamamlaninca response'taki public_url, advertorial_config.cells[].image_url olarak kaydedilir.
func (ar advertorial_routes_typ) PrivateUploadURL(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int

	defer func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
		}
	}()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		statCode = http.StatusUnauthorized
		return
	}

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("aru1", err)
		statCode = http.StatusBadRequest
		return
	}

	isAdmin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("aru2", err)
		return
	}
	if !isAdmin {
		err = errors.New("forbidden")
		statCode = http.StatusForbidden
		return
	}

	hasAdvertorial, err := dbscripts.Has_feature(eventUID, dbscripts.FeatureAdvertorial)
	if err != nil {
		err = utils.Tag_err("aru3", err)
		return
	}
	if !hasAdvertorial {
		err = errors.New("feature not purchased")
		statCode = http.StatusForbidden
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = utils.Tag_err("aru4", err)
		statCode = http.StatusBadRequest
		return
	}

	var req advertorialUploadURLReq
	if err = json.Unmarshal(body, &req); err != nil {
		err = errors.New("invalid request body")
		statCode = http.StatusBadRequest
		return
	}
	if req.FileName == "" {
		err = errors.New("file_name is required")
		statCode = http.StatusBadRequest
		return
	}
	if req.ContentType == "" {
		err = errors.New("content_type is required")
		statCode = http.StatusBadRequest
		return
	}
	if req.SizeBytes <= 0 {
		err = errors.New("size_bytes must be > 0")
		statCode = http.StatusBadRequest
		return
	}
	if req.SizeBytes > addonImageUploadMaxSizeBytes {
		err = fmt.Errorf("size_bytes must be <= %d", addonImageUploadMaxSizeBytes)
		statCode = http.StatusBadRequest
		return
	}

	ext, extErr := resolveAddonImageExt(req.FileName, req.ContentType)
	if extErr != nil {
		err = extErr
		statCode = http.StatusBadRequest
		return
	}

	key := fmt.Sprintf("advertorial/%s/%s.%s", eventUID, uuid.NewString(), ext)
	uploadURL, signErr := s3wrap.Public_s3.Store_presign_with_content_type(key, req.ContentType, addonImagePresignTTL)
	if signErr != nil {
		err = utils.Tag_err("aru5", signErr)
		return
	}

	publicURL := s3wrap.Public_s3.Url(key)
	if urlErr := validateAdvertorialImageURL(publicURL); urlErr != nil {
		err = utils.Tag_err("aru6", urlErr)
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

// PrivateGet, event admininin reklam alani ayarini gormesini saglar.
// Ozellik satin alinmadiysa enabled=false dondurur.
func (ar advertorial_routes_typ) PrivateGet(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int

	defer func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
		}
	}()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		statCode = http.StatusUnauthorized
		return
	}

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("arg1", err)
		statCode = http.StatusBadRequest
		return
	}

	isAdmin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("arg2", err)
		return
	}
	if !isAdmin {
		err = errors.New("forbidden")
		statCode = http.StatusForbidden
		return
	}

	hasAdvertorial, err := dbscripts.Has_feature(eventUID, dbscripts.FeatureAdvertorial)
	if err != nil {
		err = utils.Tag_err("arg3", err)
		return
	}

	cfg, err := fetchEventAdvertorialConfig(eventUID)
	if err != nil {
		err = utils.Tag_err("arg4", err)
		return
	}

	_ = networkutils.SendJson(types.Js_object{
		"enabled": hasAdvertorial,
		"config":  cfg,
	}, w)
}

// PrivatePatch, event admininin reklam alani ayarini gunceller.
// Bu endpoint sadece advertorial feature satin alinmis eventlerde calisir.
func (ar advertorial_routes_typ) PrivatePatch(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int

	defer func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
		}
	}()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		statCode = http.StatusUnauthorized
		return
	}

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("arp1", err)
		statCode = http.StatusBadRequest
		return
	}

	isAdmin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("arp2", err)
		return
	}
	if !isAdmin {
		err = errors.New("forbidden")
		statCode = http.StatusForbidden
		return
	}

	hasAdvertorial, err := dbscripts.Has_feature(eventUID, dbscripts.FeatureAdvertorial)
	if err != nil {
		err = utils.Tag_err("arp3", err)
		return
	}
	if !hasAdvertorial {
		err = errors.New("feature not purchased")
		statCode = http.StatusForbidden
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		err = utils.Tag_err("arp4", err)
		statCode = http.StatusBadRequest
		return
	}

	var req advertorialConfigPayload
	if err = json.Unmarshal(body, &req); err != nil {
		err = errors.New("invalid request body")
		statCode = http.StatusBadRequest
		return
	}

	normalizedConfig, valErr := normalizeAndValidateAdvertorialConfig(req)
	if valErr != nil {
		err = valErr
		statCode = http.StatusBadRequest
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("events")
	ub.Set(ub.Assign("advertorial_config", normalizedConfig.Json()))
	ub.Where(
		ub.Equal("uid", eventUID),
		ub.IsNull("deleted_at"),
	)

	if err = db.Exec(ub); err != nil {
		err = utils.Tag_err("arp5", err)
		return
	}

	_ = networkutils.SendJson(types.Js_object{
		"enabled": true,
		"config":  normalizedConfig,
	}, w)
}

// PublicGet, guest sayfasi icin reklam alani config'ini dondurur.
// Feature yoksa enabled=false dondurulur; frontend bu durumda alani render etmez.
func (ar advertorial_routes_typ) PublicGet(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int

	defer func() {
		if err != nil {
			if statCode == 0 {
				statCode = http.StatusInternalServerError
			}
			http.Error(w, err.Error(), statCode)
		}
	}()

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("apg1", err)
		statCode = http.StatusBadRequest
		return
	}

	hasAdvertorial, err := dbscripts.Has_feature(eventUID, dbscripts.FeatureAdvertorial)
	if err != nil {
		err = utils.Tag_err("apg2", err)
		return
	}

	if !hasAdvertorial {
		_ = networkutils.SendJson(types.Js_object{
			"enabled": false,
			"config":  defaultAdvertorialConfig(),
		}, w)
		return
	}

	cfg, err := fetchEventAdvertorialConfig(eventUID)
	if err != nil {
		err = utils.Tag_err("apg3", err)
		return
	}

	_ = networkutils.SendJson(types.Js_object{
		"enabled": true,
		"config":  cfg,
	}, w)
}
