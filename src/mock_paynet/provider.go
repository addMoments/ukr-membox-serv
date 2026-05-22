package mockpaynet

import (
	"encoding/json"
	"fmt"
	"membox-serv/src/payments"
	"membox-serv/src/types"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gorilla/mux"
)

const ProviderName = "mock_paynet"

func NewProvider() payments.PaymentProvider {
	return payments.PaymentProvider{
		Name:           ProviderName,
		Init:           providerInit,
		OpenPayment:    providerOpenPayment,
		HandleCallback: providerHandleCallback,
	}
}

func providerInit(args types.Js_object) error {
	rootFolder, ok := args["root_folder"].(string)
	if !ok {
		return fmt.Errorf("mock_paynet: root_folder not provided or invalid")
	}

	router, ok := args["router"].(*mux.Router)
	if !ok || router == nil {
		return fmt.Errorf("mock_paynet: router not provided or invalid")
	}

	Init(rootFolder, router)
	return nil
}

func providerOpenPayment(w http.ResponseWriter, r *http.Request, req payments.PaymentRequest) error {
	fmt.Println("providerOpenPayment: ", req)
	successUrl, err := req.MakeUrl(ProviderName, "success")
	if err != nil {
		return fmt.Errorf("mock_paynet: failed to make success url: %w", err)
	}

	errorUrl, err := req.MakeUrl(ProviderName, "error")
	if err != nil {
		return fmt.Errorf("mock_paynet: failed to make error url: %w", err)
	}

	plReq := Paylink_req{
		NameSurname:            req.Name + " " + req.Surname,
		Amount:                 req.Amount,
		Note:                   req.Note,
		ReferenceNo:            req.ReferenceNo,
		SucceedURL:             successUrl,
		ErrorURL:               errorUrl,
		Addcomission_to_amount: req.Addcomission_to_amount,
	}

	res, err := plReq.Do()
	if err != nil {
		return fmt.Errorf("mock_paynet: failed to create paylink: %w", err)
	}

	fmt.Println("res: ", res.URL)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"url": res.URL})
	return nil
}

func providerHandleCallback(w http.ResponseWriter, r *http.Request, pt payments.PaymentToken) (payments.PaymentRes, error) {
	id := r.URL.Query().Get("id")
	if id == "" {
		return payments.PaymentRes{}, fmt.Errorf("mock_paynet: xact_id not provided")
	}

	byt, err := os.ReadFile(filepath.Join(root, fmt.Sprintf("%s.json", id)))
	if err != nil {
		return payments.PaymentRes{}, fmt.Errorf("mock_paynet: failed to read file: %w", err)
	}

	var plReq Paylink_req
	fmt.Println("byt: ", string(byt))
	err = json.Unmarshal(byt, &plReq)
	if err != nil {
		return payments.PaymentRes{}, fmt.Errorf("mock_paynet: failed to unmarshal: %w", err)
	}

	return payments.PaymentRes{
		ReferenceNo: plReq.ReferenceNo,
		Provider:    ProviderName,
		ProviderId:  id,
		Amount:      plReq.Amount,
	}, nil
}
