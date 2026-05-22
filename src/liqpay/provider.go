package liqpay

import (
	"bytes"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"membox-serv/src/payments"
	"membox-serv/src/types"
	"net/http"
	"net/url"
)

const ProviderName = "liqpay"

var pubKey, privKey string
var isSandbox bool

func NewProvider() payments.PaymentProvider {
	return payments.PaymentProvider{
		Name:           ProviderName,
		Init:           providerInit,
		OpenPayment:    providerOpenPayment,
		HandleCallback: providerHandleCallback,
	}
}

func providerInit(args types.Js_object) error {
	pk, ok := args["public_key"].(string)
	if !ok || pk == "" {
		return fmt.Errorf("liqpay: public_key not provided")
	}
	sk, ok := args["private_key"].(string)
	if !ok || sk == "" {
		return fmt.Errorf("liqpay: private_key not provided")
	}
	pubKey = pk
	privKey = sk
	if sb, ok := args["sandbox"].(bool); ok {
		isSandbox = sb
	}
	return nil
}

// sign computes base64(sha1(privKey + data + privKey))
func sign(data string) string {
	h := sha1.New()
	h.Write([]byte(privKey))
	h.Write([]byte(data))
	h.Write([]byte(privKey))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// encodeRequest JSON-marshals the map and Base64-encodes it
func encodeRequest(req map[string]interface{}) (string, error) {
	jsonBytes, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(jsonBytes), nil
}

func providerOpenPayment(w http.ResponseWriter, r *http.Request, req payments.PaymentRequest) error {
	callbackUrl, err := req.MakeUrl(ProviderName, "success")
	if err != nil {
		return fmt.Errorf("liqpay: failed to make callback url: %w", err)
	}

	resultUrl := payments.GetDomain() + "/checkout/pending/" + req.ReferenceNo

	sandbox := 0
	if isSandbox {
		sandbox = 1
	}

	desc := req.Note
	if len(desc) > 500 {
		desc = desc[:500]
	}

	liqReq := map[string]interface{}{
		"public_key":  pubKey,
		"version":     3,
		"action":      "pay",
		"amount":      req.Amount,
		"currency":    req.Currency,
		"description": desc,
		"order_id":    req.ReferenceNo,
		"server_url":  callbackUrl,
		"result_url":  resultUrl,
		"sandbox":     sandbox,
	}

	encodedData, err := encodeRequest(liqReq)
	if err != nil {
		return fmt.Errorf("liqpay: encode error: %w", err)
	}
	signature := sign(encodedData)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(map[string]string{
		"type":      "liqpay_form",
		"data":      encodedData,
		"signature": signature,
	})
}

func providerHandleCallback(w http.ResponseWriter, r *http.Request, pt payments.PaymentToken) (payments.PaymentRes, error) {
	// Read raw body first so we can log it and re-parse regardless of Content-Type
	bodyBytes, _ := io.ReadAll(r.Body)
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	fmt.Println("liqpay cb | content-type:", r.Header.Get("Content-Type"), "| raw body:", string(bodyBytes))

	// Parse as URL-encoded form values from the raw body directly
	var data, signature string
	if len(bodyBytes) > 0 {
		vals, parseErr := url.ParseQuery(string(bodyBytes))
		if parseErr == nil {
			data = vals.Get("data")
			signature = vals.Get("signature")
		}
	}
	// Fall back to standard form parsing (handles multipart etc.)
	if data == "" || signature == "" {
		r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		r.ParseMultipartForm(1 << 20)
		r.ParseForm()
		data = r.FormValue("data")
		signature = r.FormValue("signature")
	}
	fmt.Println("liqpay cb | data empty:", data == "", "| signature empty:", signature == "")

	if data == "" || signature == "" {
		// Browser GET from result_url redirect — no POST body, but the token already
		// encodes "success" status (set at URL creation time). Run Payment_cb to show
		// the user the success/notice screen. Idempotency guard in Payment_cb handles
		// the case where the server POST already ran first.
		if r.Method == "GET" {
			return payments.PaymentRes{
				ReferenceNo: pt.ReferenceNo,
				Provider:    ProviderName,
				ProviderId:  "browser_redirect",
			}, nil
		}
		return payments.PaymentRes{}, fmt.Errorf("liqpay cb: missing data or signature")
	}

	// Verify signature
	expected := sign(data)
	if expected != signature {
		return payments.PaymentRes{}, fmt.Errorf("liqpay cb: signature mismatch")
	}

	// Decode data
	jsonBytes, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return payments.PaymentRes{}, fmt.Errorf("liqpay cb: base64 decode error: %w", err)
	}

	var cbData struct {
		PaymentId int64   `json:"payment_id"`
		OrderId   string  `json:"order_id"`
		Status    string  `json:"status"`
		Amount    float64 `json:"amount"`
		Currency  string  `json:"currency"`
	}
	if err := json.Unmarshal(jsonBytes, &cbData); err != nil {
		return payments.PaymentRes{}, fmt.Errorf("liqpay cb: JSON unmarshal error: %w", err)
	}

	fmt.Println("liqpay cb | status:", cbData.Status, "| payment_id:", cbData.PaymentId, "| order_id:", cbData.OrderId, "| pt.ReferenceNo:", pt.ReferenceNo)

	if cbData.Status != "success" && cbData.Status != "sandbox" {
		// Return a failed result so Payment_cb can record it and the pending page can show failure
		fmt.Println("liqpay cb | non-success status:", cbData.Status)
		return payments.PaymentRes{
			ReferenceNo: pt.ReferenceNo,
			Provider:    ProviderName,
			ProviderId:  "failed:" + cbData.Status,
		}, nil
	}

	if cbData.OrderId != pt.ReferenceNo {
		fmt.Println("liqpay cb | order_id mismatch | cbData.OrderId:", cbData.OrderId, "| pt.ReferenceNo:", pt.ReferenceNo)
		return payments.PaymentRes{}, fmt.Errorf("liqpay cb: order_id mismatch")
	}

	return payments.PaymentRes{
		ReferenceNo: pt.ReferenceNo,
		Provider:    ProviderName,
		ProviderId:  fmt.Sprintf("%d", cbData.PaymentId),
		Amount:      cbData.Amount,
		Currency:    cbData.Currency,
	}, nil
}
