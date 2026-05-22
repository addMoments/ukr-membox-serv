package mockpaynet

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

type transaction_summary_data struct {
	ID                int     `json:"id"`
	XactID            string  `json:"xact_id"`
	XactDate          string  `json:"xact_date"`
	TransactionType   int     `json:"transaction_type"`
	PosType           int     `json:"pos_type"`
	AgentID           string  `json:"agent_id"`
	IsTds             bool    `json:"is_tds"`
	BankID            string  `json:"bank_id"`
	Instalment        int     `json:"instalment"`
	CardNo            string  `json:"card_no"`
	CardHolder        string  `json:"card_holder"`
	CardType          string  `json:"card_type"`
	Ratio             float64 `json:"ratio"`
	Amount            float64 `json:"amount"`
	NetAmount         float64 `json:"netAmount"`
	Comission         float64 `json:"comission"`
	ComissionTax      float64 `json:"comission_tax"`
	Currency          string  `json:"currency"`
	AuthorizationCode string  `json:"authorization_code"`
	ReferenceCode     string  `json:"reference_code"`
	OrderID           string  `json:"order_id"`
	IsSucceed         bool    `json:"is_succeed"`
	XactTransactionID string  `json:"xact_transaction_id"`
	Email             string  `json:"email"`
	Phone             string  `json:"phone"`
	Note              string  `json:"note"`
	AgentReference    string  `json:"agent_reference"`
	ObjectName        string  `json:"object_name"`
	Code              int     `json:"code"`
	Message           string  `json:"message"`
}

type Transaction_summary struct {
	Data       []transaction_summary_data `json:"Data"`
	ObjectName string                     `json:"object_name"`
	Code       int                        `json:"code"`
	Message    string                     `json:"message"`
}

func Get_transaction_summary(xact_id string) (sum Transaction_summary, err error) {
	is_success := true

	uid, err := strconv.Atoi(xact_id)
	if err != nil {
		return
	}
	r := Paylink_req{}

	fpath := filepath.Join(root, fmt.Sprintf("ok-%d.json", uid))
	_, err = os.Stat(fpath)
	if err != nil {
		is_success = false
		fpath = filepath.Join(root, fmt.Sprintf("err-%d.json", uid))
	}

	fmt.Println("pt", fpath)

	byt, err := os.ReadFile(fpath)
	if err != nil {
		return
	}

	err = json.Unmarshal(byt, &r)
	if err != nil {
		return
	}

	sum.Data = append(sum.Data, transaction_summary_data{
		ID:             uid,
		XactID:         xact_id,
		Amount:         r.Amount,
		NetAmount:      r.Amount,
		IsSucceed:      is_success,
		AgentReference: r.ReferenceNo,
	})

	sum.Code = 0
	sum.Message = "ok"
	sum.ObjectName = "Mock paynet api trans sum-" + xact_id

	if len(sum.Data) == 0 {
		err = fmt.Errorf("no len")
		return
	}

	return

}
