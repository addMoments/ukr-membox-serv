package types

import (
	"encoding/base64"
	"encoding/json"
)

type MessageScreen struct {
	Title   string                `json:"title,omitempty"`
	Message string                `json:"message,omitempty"`
	Subtext string                `json:"subtext,omitempty"`
	Buttons []MessageScreenButton `json:"buttons,omitempty"`
	Image   string                `json:"image,omitempty"`
	Warning string                `json:"warning,omitempty"`
	// Link: notice ekraninda QR kod ve kopyalanabilir metin olarak gosterilir.
	// QR frontend'de uretilir; buraya data-URI konmaz, base64 payload sismesin.
	Link string `json:"link,omitempty"`
}

type MessageScreenButton struct {
	Text string `json:"text"`
	Href string `json:"href"`
}

func (m MessageScreen) Encode() (string, error) {
	// Marshal the struct to JSON
	jsonData, err := json.Marshal(m)
	if err != nil {
		return "", err
	}

	// Encode to base64
	encoded := base64.StdEncoding.EncodeToString(jsonData)
	return encoded, nil
}
