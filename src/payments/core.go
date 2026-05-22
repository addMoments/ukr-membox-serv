package payments

import (
	"errors"
	"fmt"
	"membox-serv/src/mycrypto"
	"membox-serv/src/types"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
)

var currentDomainAndProtocol string

func GetDomain() string {
	return currentDomainAndProtocol
}
var currentSecret string
var PaymentProviders = map[string]PaymentProvider{}

const tokenDelimiter = "\x1E"

type PaymentRequest struct {
	Name                   string  `json:"name"`
	Surname                string  `json:"surname"`
	Amount                 float64 `json:"amount"`
	Note                   string  `json:"note"`
	ReferenceNo            string  `json:"reference_no"`
	Addcomission_to_amount bool    `json:"addcomission_to_amount"`
	Currency               string  `json:"currency"`
}

type PaymentRes struct {
	ReferenceNo string  `json:"reference_no"`
	Provider    string  `json:"provider"`
	ProviderId  string  `json:"provider_id"`
	Amount      float64 `json:"amount"`
	Currency    string  `json:"currency"`
}

type PaymentToken struct {
	ReferenceNo string
	Provider    string
	Status      string
}

type PaymentProvider struct {
	Name           string
	OpenPayment    func(w http.ResponseWriter, r *http.Request, req PaymentRequest) error
	HandleCallback func(w http.ResponseWriter, r *http.Request, pt PaymentToken) (PaymentRes, error)
	Init           func(types.Js_object) error
}

func (pr PaymentRequest) MakeUrl(provider, status string) (enc string, err error) {
	pt := PaymentToken{
		ReferenceNo: pr.ReferenceNo,
		Provider:    provider,
		Status:      status,
	}
	enc, err = pt.Encrypt(currentSecret)
	if err != nil {
		return
	}
	return fmt.Sprintf("%s/api/payments/%s", currentDomainAndProtocol, enc), nil
}

func (pt PaymentToken) Encrypt(secret string) (enc string, err error) {
	return mycrypto.Encrypt(fmt.Sprintf(`%s%s%s%s%s`, pt.ReferenceNo, tokenDelimiter, pt.Provider, tokenDelimiter, pt.Status), []byte(secret))
}

func (pt *PaymentToken) Decrypt(secret string, enc string) (err error) {
	dec, err := mycrypto.Decrypt(enc, []byte(secret))
	if err != nil {
		return
	}
	parts := strings.Split(dec, tokenDelimiter)
	if len(parts) != 3 {
		return errors.New("invalid payment token")
	}
	pt.ReferenceNo = parts[0]
	pt.Provider = parts[1]
	pt.Status = parts[2]
	return
}

func Init(
	initArgs map[string]types.Js_object,
	providers map[string]PaymentProvider,
	secret string,
	domainAndProtocol string,
	r *mux.Router,
	onPayment func(w http.ResponseWriter, r *http.Request, res PaymentRes, pt PaymentToken),
) (err error) {
	PaymentProviders = providers
	currentSecret = secret
	currentDomainAndProtocol = domainAndProtocol
	for name, provider := range PaymentProviders {
		args, ok := initArgs[name]
		if !ok {
			return fmt.Errorf("provider %s not found in init args", name)
		}
		err = provider.Init(args)
		if err != nil {
			return fmt.Errorf("provider %s init failed: %w", name, err)
		}
	}

	r.HandleFunc("/api/payments/{tkn}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		tkn := vars["tkn"]
		fmt.Println("payment callback hit | method:", r.Method, "| tkn:", tkn)
		var pt PaymentToken
		var err error
		err = pt.Decrypt(secret, tkn)
		if err != nil {
			fmt.Println("payment callback decrypt error:", err)
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		fmt.Println("payment callback decrypted | provider:", pt.Provider, "| status:", pt.Status)
		provider, ok := PaymentProviders[pt.Provider]
		if !ok {
			http.Error(w, fmt.Sprintf("provider %s not found", pt.Provider), http.StatusBadRequest)
			return
		}
		res, err := provider.HandleCallback(w, r, pt)
		if err != nil {
			fmt.Println("payment callback handler error:", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		onPayment(w, r, res, pt)
	}).Methods("GET", "POST")

	return nil
}
