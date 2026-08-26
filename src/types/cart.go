package types

import (
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"

	db_layer "membox-serv/src/db_layer"
)

type Cart struct {
	UID       string    `json:"uid"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
}

type CartItem struct {
	CartUID    string    `json:"cart_uid"`
	ProductUID string    `json:"product_uid"`
	Quantity   int       `json:"quantity"`
	CreatedAt  time.Time `json:"created_at"`
	Note       string    `json:"note"`
	Status     string    `json:"status"`
}

func (c *Cart) InsertQuantityMap(quantityMap map[string]int) (cartUID string, cartItems []CartItem, err error) {
	return c.InsertQuantityMapWithConfigs(quantityMap, nil)
}

// InsertQuantityMapWithConfigs creates a cart and inserts items, optionally setting buyer_config per product ID.
func (c *Cart) InsertQuantityMapWithConfigs(quantityMap map[string]int, buyerConfigs map[string]string) (cartUID string, cartItems []CartItem, err error) {
	productIDs := []string{}

	for productID := range quantityMap {
		productIDs = append(productIDs, productID)
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("id", "uid", "is_add_on").From("products").Where(sb.In("id", db_layer.Interface_ar(productIDs)...))
	res, err := db_layer.Query_all(sb)
	if err != nil {
		return
	}

	idUidMap := make(map[string]string)
	isAddOnMap := make(map[string]bool)
	for _, row := range res {
		productID := string(row[0])
		idUidMap[productID] = string(row[1])
		// is_add_on nullable; NULL bos byte dizisi olarak gelir ve false'a duser -- dogru varsayilan.
		isAddOnMap[productID] = string(row[2]) == "true"
	}

	// Adet kurallarini sepet olusturmadan once dogrula: bir satir bile kurala uymuyorsa
	// istegin tamami reddedilsin ve arkada yetim bir carts kaydi kalmasin.
	for productID, quantity := range quantityMap {
		if _, ok := idUidMap[productID]; !ok {
			err = fmt.Errorf("product not found: %s", productID)
			return
		}
		if err = ValidateQuantity(productID, isAddOnMap[productID], quantity); err != nil {
			return
		}
	}

	ib := sqlbuilder.NewInsertBuilder()
	ib.InsertInto("carts").Cols("note").Values("Created by app server\n").SQL("RETURNING uid")
	row, err := db_layer.Query_one(ib)
	if err != nil {
		return
	}
	cartUID = string(row[0])

	for productID, quantity := range quantityMap {
		productUID, ok := idUidMap[productID]
		if !ok {
			err = fmt.Errorf("product not found: %s", productID)
			return
		}

		itemIB := sqlbuilder.NewInsertBuilder()
		if buyerConfigs != nil {
			if cfg, hasCfg := buyerConfigs[productID]; hasCfg && cfg != "" {
				itemIB.InsertInto("cart_items").Cols("cart_uid", "product_uid", "quantity", "buyer_config")
				itemIB.Values(cartUID, productUID, quantity, cfg)
			} else {
				itemIB.InsertInto("cart_items").Cols("cart_uid", "product_uid", "quantity")
				itemIB.Values(cartUID, productUID, quantity)
			}
		} else {
			itemIB.InsertInto("cart_items").Cols("cart_uid", "product_uid", "quantity")
			itemIB.Values(cartUID, productUID, quantity)
		}
		itemIB.SQL(db_layer.Conflict_update(
			[]string{"quantity"},
			"cart_uid, product_uid",
		))
		err = db_layer.Exec(itemIB)
		if err != nil {
			return
		}

		cartItems = append(cartItems, CartItem{
			CartUID:    cartUID,
			ProductUID: productUID,
			Quantity:   quantity,
		})
	}
	return cartUID, cartItems, nil
}
