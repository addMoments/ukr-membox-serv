package novaposhta

import "encoding/json"

// Settlement is a city/town result from searchSettlements.
type Settlement struct {
	Ref                    string `json:"Ref"`
	DeliveryCity           string `json:"DeliveryCity"`
	MainDescription        string `json:"MainDescription"`
	Present                string `json:"Present"`
	Area                   string `json:"Area"`
	Region                 string `json:"Region"`
	SettlementTypeCode     string `json:"SettlementTypeCode"`
	AddressDeliveryAllowed bool   `json:"AddressDeliveryAllowed"`
	Warehouses             int    `json:"Warehouses"`
}

// SettlementSearchResult wraps the nested response from searchSettlements.
type SettlementSearchResult struct {
	TotalCount  interface{}  `json:"TotalCount"`
	Addresses   []Settlement `json:"Addresses"`
}

// Warehouse is a Nova Poshta branch or parcel locker.
type Warehouse struct {
	Ref                string `json:"Ref"`
	Description        string `json:"Description"`
	ShortAddress       string `json:"ShortAddress"`
	CityRef            string `json:"CityRef"`
	CityDescription    string `json:"CityDescription"`
	TypeOfWarehouse    string `json:"TypeOfWarehouse"`
	Number             string `json:"Number"`
	Phone              string `json:"Phone"`
	Schedule           map[string]string `json:"Schedule"`
}

// SearchSettlements searches for Ukrainian cities/towns by name.
func SearchSettlements(apiKey, query string, limit, page int) ([]Settlement, error) {
	data, err := call(apiKey, "AddressGeneral", "searchSettlements", map[string]any{
		"CityName": query,
		"Limit":    limit,
		"Page":     page,
	})
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return []Settlement{}, nil
	}

	// searchSettlements returns a single object with nested Addresses array
	var result SettlementSearchResult
	if err := json.Unmarshal(data[0], &result); err != nil {
		return nil, err
	}
	return result.Addresses, nil
}

// GetWarehouses returns branches/lockers for a given city (by CityRef UUID).
func GetWarehouses(apiKey, cityRef string) ([]Warehouse, error) {
	data, err := call(apiKey, "AddressGeneral", "getWarehouses", map[string]any{
		"CityRef": cityRef,
		"Limit":   "500",
		"Page":    "1",
	})
	if err != nil {
		return nil, err
	}

	var warehouses []Warehouse
	for _, item := range data {
		var w Warehouse
		if err := json.Unmarshal(item, &w); err != nil {
			continue
		}
		warehouses = append(warehouses, w)
	}
	return warehouses, nil
}
