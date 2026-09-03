package sendemail

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
)

func buildSample() []byte {
	m := &Mail_serv{
		Outgoing_server: "mail.addmoments.com.ua",
		Username:        "noreply@addmoments.com.ua",
		Display_name:    "Add Moments",
	}
	link := "https://addmoments.com.ua/signup/TOKEN123"
	var body bytes.Buffer
	Write_html(&body, "Thank you for your payment!", []string{
		"Your order has been confirmed. Click the button below to set up your account and access your event.",
		Button(link, "Set Up My Account"),
		"If the button doesn't work, copy and paste this link into your browser:<br>" + link,
	})
	return m.build_message([]string{"buyer@example.com"}, "Add Moments Payment Confirmation", body.Bytes(), nil)
}

func TestMessageParsesAndCarriesRequiredHeaders(t *testing.T) {
	raw := buildSample()

	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("mesaj ayristirilamadi: %v", err)
	}
	for _, h := range []string{"Date", "Message-ID", "MIME-Version", "From", "To", "Subject", "Content-Type"} {
		if msg.Header.Get(h) == "" {
			t.Errorf("eksik baslik: %s", h)
		}
	}
	if _, err := msg.Header.Date(); err != nil {
		t.Errorf("Date ayristirilamadi: %v", err)
	}
	if got := msg.Header.Get("From"); got != "Add Moments <noreply@addmoments.com.ua>" {
		t.Errorf("From beklenmedik: %q", got)
	}

	mediatype, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || mediatype != "multipart/alternative" {
		t.Fatalf("Content-Type multipart/alternative degil: %q %v", mediatype, err)
	}

	seen := map[string]string{}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("parca okunamadi: %v", err)
		}
		ct, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		// multipart.Reader Content-Transfer-Encoding'i cozmez, base64 elle acilir.
		if enc := part.Header.Get("Content-Transfer-Encoding"); enc != "base64" {
			t.Fatalf("parca %s base64 degil: %q", ct, enc)
		}
		content, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, part))
		if err != nil {
			t.Fatalf("parca govdesi cozulemedi (%s): %v", ct, err)
		}
		seen[ct] = string(content)
	}

	if len(seen) != 2 {
		t.Fatalf("iki parca bekleniyordu, %d geldi: %v", len(seen), keys(seen))
	}
	if !strings.Contains(seen["text/html"], "<html") {
		t.Error("html parcasi HTML degil")
	}

	plain := seen["text/plain"]
	if strings.Contains(plain, "<") {
		t.Errorf("duz metin parcasinda etiket kalmis: %q", plain)
	}
	if !strings.Contains(plain, "https://addmoments.com.ua/signup/TOKEN123") {
		t.Errorf("duz metinde link yok:\n%s", plain)
	}
	if !strings.Contains(plain, "Set Up My Account") {
		t.Errorf("duz metinde buton etiketi yok:\n%s", plain)
	}
	t.Logf("duz metin parcasi:\n%s", plain)
}

func TestNoLineExceedsSmtpLimit(t *testing.T) {
	for i, line := range strings.Split(string(buildSample()), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("satir %d SMTP sinirini asiyor: %d karakter", i+1, len(line))
		}
	}
}

// Footer bilerek bos birakildi: eskiden her mailin altinda baska bir sirkete ait
// Turkce telif satiri ve kakooo.co'ya giden bir gizlilik linki vardi.
func TestFooterIsEmpty(t *testing.T) {
	var body bytes.Buffer
	Write_html(&body, "Baslik", []string{"Govde"})
	for _, needle := range []string{"kakooo", "Nanbis", "Gizlilik", "haklari saklidir"} {
		if strings.Contains(body.String(), needle) {
			t.Errorf("footer artigi html sablonunda: %q", needle)
		}
	}
}

func keys(m map[string]string) []string {
	out := []string{}
	for k := range m {
		out = append(out, k)
	}
	return out
}
