package routes

import (
	"encoding/json"
	"fmt"
	"io"
	db "membox-serv/src/db_layer"
	"membox-serv/src/env"
	"membox-serv/src/mycrypto"
	"membox-serv/src/payments"
	sendemail "membox-serv/src/send_email"
	"membox-serv/src/types"
	"membox-serv/src/utils"
	"net/http"
	"strings"

	"github.com/huandu/go-sqlbuilder"
)

type payment_cb_routes_typ struct{}

var PaymentCBRoutes payment_cb_routes_typ

func (rfrnc payment_cb_routes_typ) Payment_cb(w http.ResponseWriter, r *http.Request, paymentRes payments.PaymentRes, pt payments.PaymentToken) {
	var redr_url string
	var err error
	var stat_code int

	defer (func() {
		if err != nil {
			if stat_code == 0 {
				stat_code = 500
			}
			http.Error(w, err.Error(), stat_code)
			return
		}

		if stat_code == 0 {
			stat_code = http.StatusPermanentRedirect
		}

		http.Redirect(w, r, redr_url, stat_code)
	})()

	fmt.Println(pt.ReferenceNo)

	packedPurchaseUID, err := mycrypto.Decrypt(pt.ReferenceNo, []byte(env.Env().PaymentSecret))
	if err != nil {
		err = fmt.Errorf("pcb1 | invalid reference no" + err.Error())
		return
	}

	fmt.Println("pt.Status", pt.Status)

	if pt.Status != "success" {
		fmt.Println("payment failed")
		msgScreen := types.MessageScreen{
			Title:   "Payment Failed!",
			Message: "There was an error processing your payment.",
			Subtext: "If your card has been charged, please contact support with your transaction ID. Hold onto your transaction ID.",
			Buttons: []types.MessageScreenButton{
				{Text: "Contact Support", Href: "/contact"},
				{Text: "Try Again", Href: "/checkout"},
			},
			Warning: "Transaction ID: " + packedPurchaseUID,
			Image:   "https://addmoments.com.ua/ui/assets/err.svg",
		}

		encodedMsgScreen, err := msgScreen.Encode()
		if err != nil {
			err = utils.Tag_err("pcb8", err)
			return
		}
		redr_url = "/notice/" + encodedMsgScreen
		fmt.Println("redr_url", redr_url)
		return
	}

	purchaseUID, err := utils.UUID.UnpackUUID(packedPurchaseUID)
	if err != nil {
		err = fmt.Errorf("pcb2 | invalid reference no" + err.Error())
		return
	}

	sb := sqlbuilder.NewSelectBuilder()
	sb.Select("buyer_uid", "purchase_info", "cart_uid", "provider_id", "promo_code_uid").From("purchases").Where(
		sb.Equal("uid", purchaseUID),
	)
	res, err := db.Query_one(sb)
	if err != nil {
		err = utils.Tag_err("pcb3", err)
		return
	}
	purchaseInfo := types.Js_object{}
	err = json.Unmarshal(res[1], &purchaseInfo)
	if err != nil {
		err = utils.Tag_err("pcb5", err)
		return
	}

	providerID := string(res[3])
	promoCodeUID := string(res[4])
	if providerID != "" {
		// Already processed by a previous callback — skip DB writes but still
		// redirect the browser to the notice page (handles browser GET arriving after server POST)
		decEmail := purchaseInfo["email"].(string)
		pt.ReferenceNo = packedPurchaseUID
		pt.Status = "m:" + decEmail
		pt.Provider = "admt_payment"
		encPaymentTkn, encErr := pt.Encrypt(env.Env().PaymentSecret)
		if encErr != nil {
			stat_code = http.StatusOK
			return
		}
		server_url := "https://" + env.Env().ServRoot
		signup_url := server_url + "/signup/" + encPaymentTkn
		msgScreen := types.MessageScreen{
			Title:   "Payment Successful!",
			Message: "A signup link has been sent to user " + decEmail,
			Subtext: "Click the link in your inbox or the button below to sign up.",
			Buttons: []types.MessageScreenButton{
				{Text: "Sign up now", Href: signup_url},
			},
			Warning: "Transaction ID: " + packedPurchaseUID,
			Image:   "https://addmoments.com.ua/ui/assets/checkmark.svg",
		}
		encodedMsgScreen, encErr := msgScreen.Encode()
		if encErr != nil {
			stat_code = http.StatusOK
			return
		}
		redr_url = "/notice/" + encodedMsgScreen
		return
	}

	ub := sqlbuilder.NewUpdateBuilder()
	if paymentRes.ProviderId != "browser_redirect" {
		fmt.Println("pcb | writing provider_id:", paymentRes.ProviderId, "for purchase:", purchaseUID)
		ub.Update("purchases").Set(
			ub.Assign("provider_id", paymentRes.ProviderId),
			ub.Assign("provider", paymentRes.Provider),
		).Where(
			ub.Equal("uid", purchaseUID),
			ub.IsNull("provider_id"),
		)
		ub.SQL("RETURNING uid")
		updateRows, updateErr := db.Query_all(ub)
		if updateErr != nil {
			err = utils.Tag_err("pcb4", updateErr)
			return
		}
		if len(updateRows) == 0 {
			// provider_id doluysa bu duplicate callback'tir. Mevcut akisin idempotent
			// davranisini korumak icin DB yazimlarini ve promo usage artisini atlariz.
			fmt.Println("pcb | provider_id was already written, skipping duplicate callback")
			stat_code = http.StatusOK
			return
		}
		fmt.Println("pcb | provider_id written successfully")
	} else {
		fmt.Println("pcb | browser_redirect, skipping DB write")
	}

	// Failed payment from server POST — record it and stop processing
	if strings.HasPrefix(paymentRes.ProviderId, "failed:") {
		msgScreen := types.MessageScreen{
			Title:   "Payment Failed!",
			Message: "There was an error processing your payment.",
			Subtext: "If your card has been charged, please contact support with your transaction ID.",
			Buttons: []types.MessageScreenButton{
				{Text: "Contact Support", Href: "/contact"},
				{Text: "Try Again", Href: "/checkout"},
			},
			Warning: "Transaction ID: " + packedPurchaseUID,
			Image:   "https://addmoments.com.ua/ui/assets/err.svg",
		}
		encodedMsgScreen, encErr := msgScreen.Encode()
		if encErr != nil {
			err = utils.Tag_err("pcb4f", encErr)
			return
		}
		redr_url = "/notice/" + encodedMsgScreen
		return
	}

	if promoCodeUID != "" && paymentRes.ProviderId != "browser_redirect" {
		// Promo kullanimini yalniz basarili odeme ilk kez islenirken artiriyoruz.
		// Duplicate callback'ler yukaridaki provider_id guard'i nedeniyle buraya ulasmaz.
		promoUB := sqlbuilder.NewUpdateBuilder()
		promoUB.Update("promo_codes").Set(
			promoUB.Assign("usage_count", sqlbuilder.Raw("usage_count + 1")),
		).Where(promoUB.Equal("uid", promoCodeUID))
		if promoErr := db.Exec(promoUB); promoErr != nil {
			err = utils.Tag_err("pcb4p", promoErr)
			return
		}
	}

	cartUID := string(res[2])

	// Get cart item uid, product_uid, fulfillment type, buyer_config and product options
	cartProductsSB := sqlbuilder.NewSelectBuilder()
	cartProductsSB.Select("ci.uid", "ci.product_uid", "p.fullfillment_type", "ci.buyer_config", "p.options").
		From("cart_items ci").
		Join("products p", "ci.product_uid = p.uid").
		Where(cartProductsSB.Equal("ci.cart_uid", cartUID))

	rows, err := db.Query_all(cartProductsSB)
	if err != nil {
		err = utils.Tag_err("pcb5a", err)
		return
	}

	type physicalConfiguredItem struct {
		cartItemUID    string
		buyerConfig    []byte
		productOptions []byte
	}

	digitalProdUids := []interface{}{}
	physicalConfiguredProdUids := []interface{}{}
	physicalConfiguredItems := []physicalConfiguredItem{}
	physicalPendingProdUids := []interface{}{}
	for _, row := range rows {
		cartItemUID := string(row[0])
		prodUID := string(row[1])
		fulfillmentType := string(row[2])
		buyerConfig := string(row[3])
		if fulfillmentType == "digital" {
			digitalProdUids = append(digitalProdUids, prodUID)
		} else if fulfillmentType == "physical" {
			// If buyer already filled config at checkout, go straight to admin-action
			if buyerConfig != "" && buyerConfig != "{}" {
				physicalConfiguredProdUids = append(physicalConfiguredProdUids, prodUID)
				physicalConfiguredItems = append(physicalConfiguredItems, physicalConfiguredItem{
					cartItemUID:    cartItemUID,
					buyerConfig:    row[3],
					productOptions: row[4],
				})
			} else {
				physicalPendingProdUids = append(physicalPendingProdUids, prodUID)
			}
		}
	}

	// Update digital products to 'purchased' status
	if len(digitalProdUids) > 0 {
		ub = sqlbuilder.NewUpdateBuilder()
		ub.Update("cart_items").Set(
			ub.Assign("status", "purchased"),
		).Where(
			ub.Equal("cart_uid", cartUID),
			ub.In("product_uid", digitalProdUids...),
		)
		err = db.Exec(ub)
		if err != nil {
			err = utils.Tag_err("pcb5b", err)
			return
		}
	}

	// Physical items that already have buyer config → admin-action (ready to fulfill)
	if len(physicalConfiguredProdUids) > 0 {
		ub = sqlbuilder.NewUpdateBuilder()
		ub.Update("cart_items").Set(
			ub.Assign("status", "admin-action"),
		).Where(
			ub.Equal("cart_uid", cartUID),
			ub.In("product_uid", physicalConfiguredProdUids...),
		)
		err = db.Exec(ub)
		if err != nil {
			err = utils.Tag_err("pcb5c", err)
			return
		}
	}

	// Auto-create NP waybills for physical items that are ready (non-fatal)
	if len(physicalConfiguredItems) > 0 {
		shippingAddr := shippingAddressFromPurchaseInfo(res[1])
		for _, item := range physicalConfiguredItems {
			var bc map[string]interface{}
			json.Unmarshal(item.buyerConfig, &bc)
			var po map[string]interface{}
			json.Unmarshal(item.productOptions, &po)
			go tryCreateNPWaybill(item.cartItemUID, shippingAddr, bc, po)
		}
	}

	// Physical items without config → client-action (buyer must still configure)
	if len(physicalPendingProdUids) > 0 {
		ub = sqlbuilder.NewUpdateBuilder()
		ub.Update("cart_items").Set(
			ub.Assign("status", "client-action"),
		).Where(
			ub.Equal("cart_uid", cartUID),
			ub.In("product_uid", physicalPendingProdUids...),
		)
		err = db.Exec(ub)
		if err != nil {
			err = utils.Tag_err("pcb5d", err)
			return
		}
	}

	decEmail := purchaseInfo["email"].(string)
	pt.ReferenceNo = packedPurchaseUID
	pt.Status = "m:" + decEmail
	pt.Provider = "admt_payment"

	encPaymentTkn, err := pt.Encrypt(env.Env().PaymentSecret)
	if err != nil {
		err = fmt.Errorf("pcb6 | invalid reference no")
		return
	}

	server_url := "https://" + env.Env().ServRoot
	signup_url := server_url + "/signup/" + encPaymentTkn

	msgScreen := types.MessageScreen{
		Title:   "Payment Successful!",
		Message: "A signup link has been sent to user " + decEmail,
		Subtext: "Click the link in your inbox or the button below to sign up.",
		Buttons: []types.MessageScreenButton{
			{Text: "Sign up now", Href: signup_url},
		},
		Warning: "Transaction ID: " + packedPurchaseUID,
		Image:   "https://addmoments.com.ua/ui/assets/checkmark.svg",
	}

	encodedMsgScreen, err := msgScreen.Encode()
	if err != nil {
		err = utils.Tag_err("pcb9", err)
		return
	}

	redr_url = "/notice/" + encodedMsgScreen

	fmt.Println("sending email to ", decEmail)
	emailErr := sendemail.Info_mail.Send([]string{decEmail}, "Add Moments Payment Confirmation", func(w io.WriteCloser) {
		sendemail.Write_html(w, "Thank you for your payment!", []string{
			"Your order has been confirmed. Click the button below to set up your account and access your event.",
			sendemail.Button(signup_url, "Set Up My Account"),
			"If the button doesn't work, copy and paste this link into your browser:<br>" + signup_url,
		})
	}, nil)
	if emailErr != nil {
		fmt.Println("email send error (non-fatal): ", emailErr)
	}

}
