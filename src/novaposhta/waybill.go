package novaposhta

import "encoding/json"

// SenderConfig holds all pre-configured sender details from env.
type SenderConfig struct {
	APIKey          string
	SenderRef       string
	SenderContactRef string
	SenderAddressRef string
	SenderCityRef   string
	SenderPhone     string
}

// CreateWaybillInput is the recipient + shipment data needed to create a waybill.
type CreateWaybillInput struct {
	Sender SenderConfig

	// Recipient fields
	RecipientCityRef      string // from NP searchSettlements → DeliveryCity
	RecipientWarehouseRef string // from NP getWarehouses → Ref
	RecipientPhone        string // 380XXXXXXXXX
	RecipientName         string // full name — used to create recipient counterparty

	// Shipment details — leave zero to use values from ProductOptions or built-in defaults
	Description string
	// ProductOptions is the raw options JSONB from the products table.
	// Supported keys: weight (kg), width (cm), length (cm), height (cm), cost (UAH), description
	ProductOptions map[string]interface{}
}

// CreateWaybillResult holds the key fields from a successful waybill creation.
type CreateWaybillResult struct {
	Ref            string `json:"Ref"`             // waybill UUID
	IntDocNumber   string `json:"IntDocNumber"`    // human-readable tracking number
	EstimatedDeliveryDate string `json:"EstimatedDeliveryDate"`
}

type recipientRefs struct {
	CounterpartyRef string
	ContactRef      string
}

// createRecipient creates a PrivatePerson counterparty and returns their Ref + contact Ref.
func createRecipient(apiKey, cityRef, phone, fullName string) (*recipientRefs, error) {
	// Split full name into first/last/middle (NP wants them separate)
	parts := splitName(fullName)

	data, err := call(apiKey, "Counterparty", "save", map[string]any{
		"FirstName":        parts[0],
		"LastName":         parts[1],
		"MiddleName":       parts[2],
		"Phone":            phone,
		"Email":            "",
		"CounterpartyType": "PrivatePerson",
		"CounterpartyProperty": "Recipient",
		"CityRef":          cityRef,
	})
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	var cp struct {
		Ref             string `json:"Ref"`
		ContactPerson   struct {
			Data []struct {
				Ref string `json:"Ref"`
			} `json:"data"`
		} `json:"ContactPerson"`
	}
	if err := json.Unmarshal(data[0], &cp); err != nil {
		return nil, err
	}

	contactRef := ""
	if len(cp.ContactPerson.Data) > 0 {
		contactRef = cp.ContactPerson.Data[0].Ref
	}

	return &recipientRefs{CounterpartyRef: cp.Ref, ContactRef: contactRef}, nil
}

// splitName splits a full name into [firstName, lastName, middleName].
// We ask users to enter "First Last" or "First Last Middle".
// If only one word, use it as both first and last name.
func splitName(fullName string) [3]string {
	var parts [3]string
	words := []string{}
	start := 0
	for i := 0; i <= len(fullName); i++ {
		if i == len(fullName) || fullName[i] == ' ' {
			word := fullName[start:i]
			if word != "" {
				words = append(words, word)
			}
			start = i + 1
		}
	}
	switch len(words) {
	case 0:
		parts[0] = "Невідомо"
		parts[1] = "Невідомо"
	case 1:
		parts[0] = words[0] // FirstName
		parts[1] = words[0] // LastName (duplicate — NP requires both)
	case 2:
		parts[0] = words[0] // FirstName
		parts[1] = words[1] // LastName
	default:
		parts[0] = words[0] // FirstName
		parts[1] = words[1] // LastName
		parts[2] = words[2] // MiddleName
	}
	return parts
}

// optFloat extracts a float64 from a map, trying both float64 and json.Number/string forms.
func optFloat(m map[string]interface{}, key string, def float64) float64 {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch val := v.(type) {
	case float64:
		if val > 0 {
			return val
		}
	case int:
		if val > 0 {
			return float64(val)
		}
	}
	return def
}

// CreateWaybill creates an express waybill (branch-to-branch, WarehouseWarehouse).
// It first creates a recipient counterparty, then creates the document.
// Shipment dimensions/weight/cost are read from inp.ProductOptions:
//   weight (kg), width (cm), length (cm), height (cm), cost (UAH)
// Falls back to A3-paper defaults if not set.
func CreateWaybill(inp CreateWaybillInput) (*CreateWaybillResult, error) {
	po := inp.ProductOptions

	weight := optFloat(po, "weight", 0.5)
	width  := optFloat(po, "width",  5)
	length := optFloat(po, "length", 42)
	height := optFloat(po, "height", 30)
	cost   := optFloat(po, "cost",   1)

	description := inp.Description
	if description == "" {
		if po != nil {
			if d, ok := po["description"].(string); ok && d != "" {
				description = d
			}
		}
	}
	if description == "" {
		description = "Товар"
	}

	// Step 1: create recipient counterparty to get Ref + ContactRef
	recipient, err := createRecipient(inp.Sender.APIKey, inp.RecipientCityRef, inp.RecipientPhone, inp.RecipientName)
	if err != nil {
		return nil, err
	}
	if recipient == nil || recipient.CounterpartyRef == "" {
		return nil, nil
	}

	// Step 2: create the waybill
	data, err := call(inp.Sender.APIKey, "InternetDocument", "save", map[string]any{
		"PayerType":        "Sender",
		"PaymentMethod":    "Cash",
		"CargoType":        "Parcel",
		"ServiceType":      "WarehouseWarehouse",
		"Weight":           weight,
		"SeatsAmount":      1,
		"Description":      description,
		"Cost":             cost,
		"CitySender":       inp.Sender.SenderCityRef,
		"Sender":           inp.Sender.SenderRef,
		"SenderAddress":    inp.Sender.SenderAddressRef,
		"ContactSender":    inp.Sender.SenderContactRef,
		"SendersPhone":     inp.Sender.SenderPhone,
		"CityRecipient":    inp.RecipientCityRef,
		"RecipientAddress": inp.RecipientWarehouseRef,
		"RecipientsPhone":  inp.RecipientPhone,
		"Recipient":        recipient.CounterpartyRef,
		"ContactRecipient": recipient.ContactRef,
		"OptionsSeat": []map[string]any{
			{
				"volumetricVolume": 1,
				"volumetricWidth":  width,
				"volumetricLength": length,
				"volumetricHeight": height,
				"weight":           weight,
			},
		},
	})
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}

	var result CreateWaybillResult
	if err := json.Unmarshal(data[0], &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// TrackingStatus is the status of a single tracked shipment.
type TrackingStatus struct {
	Number          string `json:"Number"`
	Status          string `json:"Status"`
	StatusCode      string `json:"StatusCode"`
	ScheduledDeliveryDate string `json:"ScheduledDeliveryDate"`
	ActualDeliveryDate    string `json:"ActualDeliveryDate"`
	CityRecipient   string `json:"CityRecipient"`
	WarehouseRecipient string `json:"WarehouseRecipient"`
}

// TrackWaybills returns tracking status for up to 100 waybill numbers.
func TrackWaybills(apiKey string, numbers []string) ([]TrackingStatus, error) {
	docs := make([]map[string]string, len(numbers))
	for i, n := range numbers {
		docs[i] = map[string]string{"DocumentNumber": n}
	}

	data, err := call(apiKey, "TrackingDocumentGeneral", "getStatusDocuments", map[string]any{
		"Documents": docs,
	})
	if err != nil {
		return nil, err
	}

	var results []TrackingStatus
	for _, item := range data {
		var s TrackingStatus
		if err := json.Unmarshal(item, &s); err != nil {
			continue
		}
		results = append(results, s)
	}
	return results, nil
}
