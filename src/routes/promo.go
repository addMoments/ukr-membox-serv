package routes

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	networkutils "membox-serv/src/network_utils"
	"membox-serv/src/promo"
	"net/http"
)

type promo_routes_typ struct{}

var PromoRoutes promo_routes_typ

type promoValidateReq struct {
	PromoCode    string         `json:"promo_code"`
	PurchaseInfo map[string]int `json:"purchase_info"`
}

// Validate, checkout ekranindaki "Apply promo" onizlemesini hesaplar.
// Bu endpoint hicbir DB yazimi yapmaz; asil guvenlik icin purchase aninda
// ayni promo.Validate helper'i tekrar calistirilacaktir.
func (rfrnc promo_routes_typ) Validate(w http.ResponseWriter, r *http.Request) {
	var err error
	var statCode int

	defer func() {
		if err == nil {
			return
		}
		if statCode == 0 {
			statCode = http.StatusInternalServerError
		}
		fmt.Println("promo validate error:", err)
		http.Error(w, err.Error(), statCode)
	}()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		statCode = http.StatusBadRequest
		return
	}

	req := promoValidateReq{}
	if err = json.Unmarshal(body, &req); err != nil {
		statCode = http.StatusBadRequest
		return
	}

	quote, validationErr := promo.Validate(req.PromoCode, req.PurchaseInfo)
	if validationErr != nil {
		if code, ok := promo.ErrorCodeOf(validationErr); ok {
			err = networkutils.SendErrorJSON(w, http.StatusBadRequest, string(code), validationErr.Error())
			return
		}
		err = validationErr
		return
	}

	if quote.PromoCodeUID == "" {
		err = errors.New("promo validation returned empty promo uid")
		return
	}

	err = networkutils.SendJson(quote, w)
}
