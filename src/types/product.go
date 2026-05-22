package types

import "time"

type Product struct {
	UID              string    `json:"uid"`
	Price            float64   `json:"price"`
	ID               string    `json:"id"`
	CreatedAt        time.Time `json:"created_at"`
	Options          Js_object `json:"options"`
	Priority         int       `json:"priority"`
	FullfillmentType string    `json:"fullfillment_type"`
}
