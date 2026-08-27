package types

import (
	"errors"
	"fmt"
)

// Ne: Bir urunun satis adedi kurallarini ve bu kurallara uymayan sepet satirlarini
//     reddeden dogrulamayi tutar.
// Nasil: Kural urun kaydindan degil koddan geliyor; frontend'deki birebir karsiligi
//        ukr-membox/src/client/cart.ts -> getQtyRule / SINGLE_QUANTITY_ADDON_IDS /
//        PACK_QTY_ADDON_IDS. Iki taraf birlikte degistirilmeli, aksi halde tarayicida
//        gecen sepet sunucuda reddedilir.
// Neden: Bu dosya eklenene kadar adet yalnizca tarayicida dogrulaniyordu. /purchase'a
//        dogrudan istek atan biri 0, 3 ya da negatif adet gonderebiliyor; istek
//        fiyatlanip odemeye ve Nova Poshta irsaliyesine kadar gidiyordu.

// Adet stepper'i olmayan add-on'lar: tam olarak 1 adet satilir.
var singleQuantityAddonIDs = map[string]bool{
	"audioGuestbook": true,
	"audiobook":      true,
	"advertorial":    true,
	"sponsored":      true,
}

// 4'luk bloklar halinde basilan add-on'lar. Yalnizca QR kart; Welcome Board birer birer satilir.
var packQtyAddonIDs = map[string]bool{
	"printedBanner": true,
}

// QR kart tek sayfaya 4 adet basildigi icin 4'luk bloklar halinde satiliyor.
const PackQtyStep = 4

// Tek bir sepet satirindaki azami adet. Is kurali degil tasma korumasi: price * quantity
// hesabi ve tek irsaliyeye sigan koli sinirsiz adetle anlamsizlasiyor.
const MaxLineQuantity = 1000

// QuantityRuleError, adet kuralina uymayan istek demektir. Cagiran taraf bunu gercek
// sunucu hatalarindan ayirip 500 yerine 422 dondurebilsin diye ayri bir tip.
type QuantityRuleError struct {
	ProductID string
	Quantity  int
	Reason    string
}

func (e *QuantityRuleError) Error() string {
	return fmt.Sprintf("invalid quantity %d for product %q: %s", e.Quantity, e.ProductID, e.Reason)
}

// IsQuantityRuleError, hata zincirinde adet kurali hatasi var mi diye bakar.
func IsQuantityRuleError(err error) bool {
	var qErr *QuantityRuleError
	return errors.As(err, &qErr)
}

// QtyRule, urunun en az kac adet satildigini ve kacar kacar arttigini doner.
// 4'luk basilan add-on'lar 4/4; digerleri (paketler, Welcome Board, tek adetlik add-on'lar) 1/1.
func QtyRule(productID string, isAddOn bool) (min int, step int) {
	if isAddOn && packQtyAddonIDs[productID] {
		return PackQtyStep, PackQtyStep
	}
	return 1, 1
}

// ValidateQuantity, tek bir sepet satirinin adedini kurala gore dogrular.
func ValidateQuantity(productID string, isAddOn bool, quantity int) error {
	if quantity > MaxLineQuantity {
		return &QuantityRuleError{
			ProductID: productID,
			Quantity:  quantity,
			Reason:    fmt.Sprintf("maximum is %d", MaxLineQuantity),
		}
	}

	// Tek adetlik add-on'larda stepper yok; frontend de 1'den fazlasini 1'e cekiyor
	// (V2Checkout -> SINGLE_QUANTITY_ADDON_IDS).
	if isAddOn && singleQuantityAddonIDs[productID] {
		if quantity != 1 {
			return &QuantityRuleError{
				ProductID: productID,
				Quantity:  quantity,
				Reason:    "this product is sold as a single unit",
			}
		}
		return nil
	}

	min, step := QtyRule(productID, isAddOn)
	if quantity < min {
		return &QuantityRuleError{
			ProductID: productID,
			Quantity:  quantity,
			Reason:    fmt.Sprintf("minimum is %d", min),
		}
	}
	if (quantity-min)%step != 0 {
		return &QuantityRuleError{
			ProductID: productID,
			Quantity:  quantity,
			Reason:    fmt.Sprintf("must be %d or a multiple of %d above it", min, step),
		}
	}
	return nil
}
