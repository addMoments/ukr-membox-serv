package types

import "testing"

// Kural frontend'deki ukr-membox/src/client/cart.ts -> getQtyRule ile ayni olmali.
// Bu test iki tarafin ayrismasini yakalamaz ama kuralin kendisini sabitler.
func TestValidateQuantity(t *testing.T) {
	cases := []struct {
		name      string
		productID string
		isAddOn   bool
		quantity  int
		wantErr   bool
	}{
		// QR kart: en az 4, 4'un katlari.
		{"qr card 4", "printedBanner", true, 4, false},
		{"qr card 8", "printedBanner", true, 8, false},
		{"qr card 12", "printedBanner", true, 12, false},
		{"qr card 1 reddedilir", "printedBanner", true, 1, true},
		{"qr card 3 reddedilir", "printedBanner", true, 3, true},
		{"qr card 6 reddedilir", "printedBanner", true, 6, true},
		{"qr card 0 reddedilir", "printedBanner", true, 0, true},
		{"qr card negatif reddedilir", "printedBanner", true, -4, true},

		// Welcome Board: en az 1, birer birer artar.
		{"welcome board 1", "welcome_board", true, 1, false},
		{"welcome board 2", "welcome_board", true, 2, false},
		{"welcome board 3", "welcome_board", true, 3, false},
		{"welcome board 4", "welcome_board", true, 4, false},
		{"welcome board 0 reddedilir", "welcome_board", true, 0, true},
		{"welcome board negatif reddedilir", "welcome_board", true, -1, true},

		// Tek adetlik add-on'lar: tam olarak 1.
		{"audio guestbook 1", "audioGuestbook", true, 1, false},
		{"audio guestbook 2 reddedilir", "audioGuestbook", true, 2, true},
		{"audio guestbook 0 reddedilir", "audioGuestbook", true, 0, true},
		{"advertorial 1", "advertorial", true, 1, false},
		{"advertorial 4 reddedilir", "advertorial", true, 4, true},

		// Paketler (add-on degil): en az 1, birer birer.
		{"premium 1", "premium", false, 1, false},
		{"premium 2", "premium", false, 2, false},
		{"premium 0 reddedilir", "premium", false, 0, true},
		{"premium negatif reddedilir", "premium", false, -1, true},

		// Tasma korumasi.
		{"ust sinir", "printedBanner", true, MaxLineQuantity, false},
		{"ust sinir asilirsa reddedilir", "printedBanner", true, MaxLineQuantity + 4, true},
		{"paket ust siniri asilirsa reddedilir", "premium", false, MaxLineQuantity + 1, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateQuantity(tc.productID, tc.isAddOn, tc.quantity)
			if tc.wantErr && err == nil {
				t.Fatalf("quantity %d for %s: hata bekleniyordu, nil dondu", tc.quantity, tc.productID)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("quantity %d for %s: hata beklenmiyordu, %v dondu", tc.quantity, tc.productID, err)
			}
			if tc.wantErr && !IsQuantityRuleError(err) {
				t.Fatalf("quantity %d for %s: QuantityRuleError bekleniyordu, %T dondu", tc.quantity, tc.productID, err)
			}
		})
	}
}

// MaxLineQuantity paket adimina bolunebilir olmali; aksi halde ust sinir hicbir gecerli
// paket adediyle tam olarak yakalanamaz.
func TestMaxLineQuantityIsReachable(t *testing.T) {
	if MaxLineQuantity%PackQtyStep != 0 {
		t.Fatalf("MaxLineQuantity (%d) PackQtyStep'e (%d) bolunmuyor", MaxLineQuantity, PackQtyStep)
	}
	if err := ValidateQuantity("printedBanner", true, MaxLineQuantity); err != nil {
		t.Fatalf("ust sinir gecerli bir paket adedi olmali: %v", err)
	}
}
