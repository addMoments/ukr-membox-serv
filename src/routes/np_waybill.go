package routes

import (
	"encoding/json"
	"fmt"
	"membox-serv/src/env"
	"membox-serv/src/novaposhta"

	db "membox-serv/src/db_layer"
	sqlbuilder "github.com/huandu/go-sqlbuilder"
)

// tryCreateNPWaybill attempts to create a Nova Poshta waybill for a cart item
// that has a shipping address with NP refs. It updates tracking_number and
// carrier on the cart_items row if successful. Errors are logged but non-fatal.
func tryCreateNPWaybill(cartItemUID string, shippingAddress map[string]interface{}, buyerConfig map[string]interface{}, productOptions map[string]interface{}) {
	npEnv := env.Env().NovaPoshta

	// Require all sender config to be populated
	if npEnv.APIKey == "" || npEnv.SenderRef == "" || npEnv.SenderCityRef == "" {
		fmt.Println("np_waybill: sender config not set, skipping")
		return
	}

	// Extract recipient fields from shipping_address
	cityRef, _ := shippingAddress["city_ref"].(string)
	warehouseRef, _ := shippingAddress["warehouse_ref"].(string)
	phone, _ := shippingAddress["phone"].(string)
	fullName, _ := shippingAddress["full_name"].(string)

	if cityRef == "" || warehouseRef == "" || phone == "" {
		fmt.Println("np_waybill: missing recipient city_ref/warehouse_ref/phone, skipping")
		return
	}

	// Try to get a cargo description from buyer_config
	description := "Товар"
	if buyerConfig != nil {
		if d, ok := buyerConfig["description"].(string); ok && d != "" {
			description = d
		}
	}

	sender := novaposhta.SenderConfig{
		APIKey:           npEnv.APIKey,
		SenderRef:        npEnv.SenderRef,
		SenderContactRef: npEnv.SenderContactRef,
		SenderAddressRef: npEnv.SenderAddressRef,
		SenderCityRef:    npEnv.SenderCityRef,
		SenderPhone:      npEnv.SenderPhone,
	}

	result, err := novaposhta.CreateWaybill(novaposhta.CreateWaybillInput{
		Sender:                sender,
		RecipientCityRef:      cityRef,
		RecipientWarehouseRef: warehouseRef,
		RecipientPhone:        phone,
		RecipientName:         fullName,
		Description:           description,
		ProductOptions:        productOptions,
	})
	if err != nil {
		fmt.Println("np_waybill: create failed:", err)
		return
	}
	if result == nil || result.IntDocNumber == "" {
		fmt.Println("np_waybill: empty result")
		return
	}

	fmt.Println("np_waybill: created waybill", result.IntDocNumber, "(ref:", result.Ref, ") for cart item", cartItemUID)

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("cart_items").Set(
		ub.Assign("tracking_number", result.IntDocNumber),
		ub.Assign("carrier", "nova_poshta"),
		ub.Assign("np_waybill_ref", result.Ref),
	).Where(ub.Equal("uid", cartItemUID))

	if err := db.Exec(ub); err != nil {
		fmt.Println("np_waybill: db update failed:", err)
	}
}

// retryWaybillForItem is a debug helper — runs waybill creation synchronously
// and returns the exact error so it can be surfaced to the admin over HTTP.
func retryWaybillForItem(cartItemUID string) error {
	saBldr := sqlbuilder.BuildNamed(`
		SELECT pu.purchase_info, ci.buyer_config, pr.options
		FROM cart_items ci
		JOIN carts c ON ci.cart_uid = c.uid
		JOIN purchases pu ON pu.cart_uid = c.uid
		JOIN products pr ON ci.product_uid = pr.uid
		WHERE ci.uid = ${cart_item_uid}
		LIMIT 1
	`, map[string]interface{}{"cart_item_uid": cartItemUID})

	row, err := db.Query_one(saBldr)
	if err != nil {
		return fmt.Errorf("db lookup: %w", err)
	}

	shippingAddr := shippingAddressFromPurchaseInfo(row[0])
	if shippingAddr == nil {
		return fmt.Errorf("no shipping_address in purchase_info")
	}

	var bc map[string]interface{}
	json.Unmarshal(row[1], &bc)

	var po map[string]interface{}
	json.Unmarshal(row[2], &po)

	npEnv := env.Env().NovaPoshta
	if npEnv.APIKey == "" || npEnv.SenderRef == "" || npEnv.SenderCityRef == "" {
		return fmt.Errorf("sender config not set in env (api_key/sender_ref/sender_city_ref empty)")
	}

	cityRef, _ := shippingAddr["city_ref"].(string)
	warehouseRef, _ := shippingAddr["warehouse_ref"].(string)
	phone, _ := shippingAddr["phone"].(string)
	fullName, _ := shippingAddr["full_name"].(string)

	if cityRef == "" || warehouseRef == "" || phone == "" {
		return fmt.Errorf("shipping_address missing fields: city_ref=%q warehouse_ref=%q phone=%q", cityRef, warehouseRef, phone)
	}

	sender := novaposhta.SenderConfig{
		APIKey:           npEnv.APIKey,
		SenderRef:        npEnv.SenderRef,
		SenderContactRef: npEnv.SenderContactRef,
		SenderAddressRef: npEnv.SenderAddressRef,
		SenderCityRef:    npEnv.SenderCityRef,
		SenderPhone:      npEnv.SenderPhone,
	}

	result, err := novaposhta.CreateWaybill(novaposhta.CreateWaybillInput{
		Sender:                sender,
		RecipientCityRef:      cityRef,
		RecipientWarehouseRef: warehouseRef,
		RecipientPhone:        phone,
		RecipientName:         fullName,
		Description:           "Товар",
		ProductOptions:        po,
	})
	if err != nil {
		return fmt.Errorf("novaposhta API: %w", err)
	}
	if result == nil || result.IntDocNumber == "" {
		return fmt.Errorf("novaposhta returned empty result")
	}

	ub := sqlbuilder.NewUpdateBuilder()
	ub.Update("cart_items").Set(
		ub.Assign("tracking_number", result.IntDocNumber),
		ub.Assign("carrier", "nova_poshta"),
		ub.Assign("np_waybill_ref", result.Ref),
	).Where(ub.Equal("uid", cartItemUID))

	if err := db.Exec(ub); err != nil {
		return fmt.Errorf("db save: %w", err)
	}
	return nil
}

// shippingAddressFromPurchaseInfo extracts the shipping_address object from
// purchase_info JSON bytes, returning nil if absent or unparseable.
func shippingAddressFromPurchaseInfo(purchaseInfoBytes []byte) map[string]interface{} {
	var info map[string]interface{}
	if err := json.Unmarshal(purchaseInfoBytes, &info); err != nil {
		return nil
	}
	sa, ok := info["shipping_address"].(map[string]interface{})
	if !ok {
		return nil
	}
	return sa
}
