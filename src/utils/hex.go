package utils

import (
	"errors"
	"image/color"
	"strconv"
	"strings"
)

func HexToColor(hex string) (color.RGBA, error) {
	hex = strings.TrimPrefix(hex, "#")

	if len(hex) != 6 {
		return color.RGBA{}, errors.New("invalid hex color")
	}

	r, err := strconv.ParseUint(hex[0:2], 16, 8)
	if err != nil {
		return color.RGBA{}, err
	}
	g, err := strconv.ParseUint(hex[2:4], 16, 8)
	if err != nil {
		return color.RGBA{}, err
	}
	b, err := strconv.ParseUint(hex[4:6], 16, 8)
	if err != nil {
		return color.RGBA{}, err
	}

	return color.RGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: 255}, nil
}
