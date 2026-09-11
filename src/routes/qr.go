package routes

import (
	"encoding/json"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"membox-serv/src/auth"
	dbscripts "membox-serv/src/db_scripts"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/qr"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/yeqown/go-qrcode/writer/standard"
	"golang.org/x/image/draw"
)

type adjust_qr_req struct {
	BgColor string `json:"bgColor"`
	FgColor string `json:"fgColor"`
	Shape   string `json:"shape"`
	Logo    string `json:"logo"`
}

func Adjust_event_qr(w http.ResponseWriter, r *http.Request) {
	var stat_code = 0
	var payload = []byte{}
	var err error

	defer (func() {
		if err != nil {
			if errors.Is(err, dbscripts.ErrEventClosed) {
				_ = networkutils.SendErrorJSON(w, http.StatusGone, "EVENT_CLOSED", networkutils.EventClosedMessage(r))
				return
			}
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}
		if stat_code == 0 {
			stat_code = 200
		}
		w.WriteHeader(stat_code)
		w.Write(payload)
	})()

	claims, ok := r.Context().Value("claims").(auth.TokenClaims)
	if !ok {
		err = errors.New("unauthorized")
		return
	}

	eventPackedUID := mux.Vars(r)["eventPackedUid"]
	eventUID, err := utils.UUID.UnpackUUID(eventPackedUID)
	if err != nil {
		err = utils.Tag_err("mce1", err)
		return
	}

	is_admin, err := dbscripts.Is_events_admin(eventUID, claims.UserUID)
	if errors.Is(err, dbscripts.ErrEventClosed) {
		return
	}
	if err != nil {
		err = utils.Tag_err("gu2", err)
		return
	}
	if !is_admin {
		err = errors.New("unauthorized")
		return
	}

	var req adjust_qr_req
	err = json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		err = utils.Tag_err("mce2", err)
		return
	}

	opts := []standard.ImageOption{
		standard.WithFgColorRGBHex(req.FgColor),
		standard.WithBgColorRGBHex(req.BgColor),
	}

	if req.Shape != "rectangle" {
		shape, ok := qr.QrShapes[req.Shape]
		if !ok {
			err = errors.New("invalid shape")
			return
		}
		opts = append(opts, shape)
	}

	if req.Logo != "" {
		var logo io.ReadCloser
		logo, err = s3wrap.Public_s3.Get(req.Logo)
		if err != nil {
			err = utils.Tag_err("mce4", err)
			return
		}
		defer logo.Close()

		var img image.Image
		img, _, err = image.Decode(logo)
		if err != nil {
			err = utils.Tag_err("mce4.1", err)
			return
		}

		// Scale logo to reasonable size for QR code (max 150x150)
		bounds := img.Bounds()
		maxSize := 164
		w, h := bounds.Dx(), bounds.Dy()
		if w > maxSize || h > maxSize {
			scale := float64(maxSize) / float64(max(w, h))
			newW := int(float64(w) * scale)
			newH := int(float64(h) * scale)
			scaled := image.NewRGBA(image.Rect(0, 0, newW, newH))
			draw.CatmullRom.Scale(scaled, scaled.Bounds(), img, bounds, draw.Over, nil)
			img = scaled
		}
		// svg?

		opts = append(opts, standard.WithLogoImage(img))
		opts = append(opts, standard.WithLogoSafeZone())
	}

	err = qr.UpdateEventQR(eventPackedUID, opts...)
	if err != nil {
		err = utils.Tag_err("mce5", err)
		return
	}

	payload = []byte("ok")
	return

}
