package qr

import (
	"github.com/yeqown/go-qrcode/writer/standard"
)

func dotShape(radiusPercent float64) standard.IShape {
	return &smallerCircle{smallerPercent: radiusPercent}
}

// smallerCircle use smaller circle to qrcode.
type smallerCircle struct {
	smallerPercent float64
}

func (sc *smallerCircle) DrawFinder(ctx *standard.DrawContext) {
	// use normal radius to draw finder for that qrcode image can be recognized.
	backup := sc.smallerPercent
	sc.smallerPercent = 1.0
	sc.Draw(ctx)
	sc.smallerPercent = backup
}

func (sc *smallerCircle) Draw(ctx *standard.DrawContext) {
	w, h := ctx.Edge()
	dx, dy := ctx.UpperLeft()
	color := ctx.Color()
	upperLeft := IPoint{X: int(dx), Y: int(dy)}

	// choose a proper radius values
	radius := w / 2
	r2 := h / 2
	if r2 <= radius {
		radius = r2
	}

	// 80 percent smaller
	radius = int(float64(radius) * sc.smallerPercent)

	cx, cy := upperLeft.X+w/2, upperLeft.Y+h/2 // get center point
	ctx.DrawCircle(float64(cx), float64(cy), float64(radius))
	ctx.SetColor(color)
	ctx.Fill()

}
