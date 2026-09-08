package qr

import (
	"bytes"
	"io"
	"membox-serv/src/env"
	s3wrap "membox-serv/src/s3-wrap"
	"membox-serv/src/utils"

	qrcode "github.com/yeqown/go-qrcode/v2"
	"github.com/yeqown/go-qrcode/writer/standard"
	"github.com/yeqown/go-qrcode/writer/standard/shapes"
)

var QrShapes = map[string]standard.ImageOption{}

func HChainBlock(ctx *standard.DrawContext) {
	w, h := ctx.Edge()
	fw, fh := float64(w), float64(h)
	x, y := ctx.UpperLeft()
	cx, cy := x+fw/2, y+fh/2
	r := fw * 0.85 / 2 // todo:
	l := r * 0.2

	ctx.SetColor(ctx.Color())

	mask := ctx.Neighbours()

	drawRect := func(x, y, w, h float64) {
		ctx.DrawRectangle(x, y, w, h)
		ctx.Fill()
	}
	_ = mask
	_ = drawRect

	ctx.DrawCircle(cx, cy, r)

	if mask&standard.NLeft|standard.NSelf == standard.NLeft|standard.NSelf {
		drawRect(x, cy-l, fw/2, 2*l)
	}
	if mask&standard.NRight|standard.NSelf == standard.NRight|standard.NSelf {
		drawRect(cx, cy-l, fw/2, 2*l)
	}

	ctx.Fill()
}

func init() {

	QrShapes = map[string]standard.ImageOption{
		"circle":      standard.WithCustomShape(shapes.Assemble(shapes.RoundedFinder(), shapes.CircleBlocks(1))),
		"liquidblock": standard.WithCustomShape(shapes.Assemble(shapes.RoundedFinder(), shapes.LiquidBlock())),
		"dots":        standard.WithCustomShape(shapes.Assemble(shapes.RoundedFinder(), HChainBlock)),
	}
}

type IPoint struct {
	X int
	Y int
}

func GenerateQR(url string, opts ...standard.ImageOption) (result io.Reader, err error) {
	qrc, err := qrcode.New(url)
	if err != nil {
		return
	}

	result, writer := io.Pipe()

	w := standard.NewWithWriter(writer, opts...)

	go func() {
		err := qrc.Save(w)
		writer.CloseWithError(err)
	}()

	return
}

// UpdateQR, verilen URL icin QR uretip S3'te s3Path'e yazar.
// Etkinlik ve album QR'lari ayni yoldan gecer; yalnizca hedef URL ve dosya yolu degisir.
func UpdateQR(qrUrl string, s3Path string, opts ...standard.ImageOption) (err error) {
	qrReader, err := GenerateQR(qrUrl, opts...)
	if err != nil {
		err = utils.Tag_err("mce6", err)
		return
	}

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, qrReader)
	if err != nil {
		err = utils.Tag_err("mce6.1", err)
		return
	}

	readSeeker := bytes.NewReader(buf.Bytes())

	err = s3wrap.Public_s3.Store(s3Path, readSeeker)
	return
}

func EventQRPath(packedEventUID string) string {
	return "/events/" + packedEventUID + "/qr.png"
}

// AlbumQRPath: /events/<ev>/albums/<al>/qr.png — frontend albumQrImageUrl ile ayni.
func AlbumQRPath(packedEventUID string, packedAlbumUID string) string {
	return "/events/" + packedEventUID + "/albums/" + packedAlbumUID + "/qr.png"
}

func UpdateEventQR(packedEventUID string, opts ...standard.ImageOption) (err error) {
	qrUrl := "https://" + env.Env().ServRoot + "/l/q" + packedEventUID
	return UpdateQR(qrUrl, EventQRPath(packedEventUID), opts...)
}

// UpdateAlbumQR, album linki (/l/a<packedAlbumUid>) icin QR uretir.
func UpdateAlbumQR(packedEventUID string, packedAlbumUID string, opts ...standard.ImageOption) (err error) {
	qrUrl := "https://" + env.Env().ServRoot + "/l/a" + packedAlbumUID
	return UpdateQR(qrUrl, AlbumQRPath(packedEventUID, packedAlbumUID), opts...)
}
