package routes

import (
	"io"
	"membox-serv/src/env"
	"membox-serv/src/payments"
	sendemail "membox-serv/src/send_email"
)

// Ne: Bir satin alma icin signup (aktivasyon) linkini uretir.
// Nasil: payments.PaymentToken'i sifreleyip /signup/<token> adresini kurar.
// Neden: Ayni link hem odeme callback'inde, hem odeme durumu ucunda, hem de admin
//
//	panelindeki "yeniden gonder" isinde lazim; tek yerden uretilsin.
func activationSignupURL(packedPurchaseUID, email string) (string, error) {
	pt := payments.PaymentToken{
		ReferenceNo: packedPurchaseUID,
		Status:      "m:" + email,
		Provider:    "admt_payment",
	}

	tkn, err := pt.Encrypt(env.Env().PaymentSecret)
	if err != nil {
		return "", err
	}

	return "https://" + env.Env().ServRoot + "/signup/" + tkn, nil
}

// Ne: Satin alma sonrasi gonderilen aktivasyon mailini yollar.
// Nasil: Odeme callback'i ve admin panelindeki yeniden gonderme ayni fonksiyonu cagirir.
// Neden: Metin iki yerde ayri ayri dursa zamanla birbirinden ayrilir; musteriye giden
//
//	mailin ikinci gonderimde farkli gorunmesi istenmez.
func sendActivationMail(email, signupURL string) error {
	return sendemail.Info_mail.Send([]string{email}, "Add Moments Payment Confirmation", func(w io.WriteCloser) {
		sendemail.Write_html(w, "Thank you for your payment!", []string{
			"Your order has been confirmed. Click the button below to set up your account and access your event.",
			sendemail.Button(signupURL, "Set Up My Account"),
			"If the button doesn't work, copy and paste this link into your browser:<br>" + signupURL,
		})
	}, nil)
}
