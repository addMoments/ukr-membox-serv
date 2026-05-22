package utils

import (
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"strings"
)

type uuidTyp struct{}

var UUID uuidTyp

func (u uuidTyp) PackUUID(uuidStr string) (b64Url string, err error) {
	defer (func() {
		rec := recover()
		if rec != nil {
			err = fmt.Errorf("error packing uuid: %v", rec)
			return
		}
	})()

	// Remove dashes
	noDashes := strings.ReplaceAll(uuidStr, "-", "")

	// Decode hex to bytes
	bytes, _ := hex.DecodeString(noDashes)

	// Base64 encode
	rawB64 := base64.StdEncoding.EncodeToString(bytes)

	// Remove trailing "=="
	noSuffix := rawB64[:len(rawB64)-2]

	// Convert to URL-safe base64
	b64Url = strings.ReplaceAll(noSuffix, "/", "_")
	b64Url = strings.ReplaceAll(b64Url, "+", "-")

	return
}

func (u uuidTyp) UnpackUUID(b64uuid string) (uuidStr string, err error) {
	defer (func() {
		rec := recover()
		if rec != nil {
			err = fmt.Errorf("error packing uuid: %v", rec)
			return
		}
	})()

	// Convert URL-safe base64 back to standard
	rawB64 := strings.ReplaceAll(b64uuid, "_", "/")
	rawB64 = strings.ReplaceAll(rawB64, "-", "+")
	rawB64 += "=="

	// Decode base64
	bytes, _ := base64.StdEncoding.DecodeString(rawB64)

	// Convert bytes to hex string
	hexNoDashes := hex.EncodeToString(bytes)

	// Insert dashes to form UUID
	s := hexNoDashes
	uuidStr = s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
	return
}
