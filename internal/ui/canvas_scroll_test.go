package ui

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCanvasInsideScrollingClipKeepsSourceOrigin(t *testing.T) {
	canvas := ui.NewCanvas(40, 20)
	canvas.Bitmap().Pixelated()
	drawn := false
	var scroll ui.ScrollState
	tt := ui.NewTester(func(c *ui.Context) {
		ui.Scroll(c).Fill().TrackScroll(&scroll).Children(func() {
			ui.Column(c).Height(600).Shrink(0).Children(func() {
				ui.Box(c).Height(100).Shrink(0)
				ui.Box(c).Size(40, 20).Draw(func(p *ui.Painter, r ui.Rect) {
					if !drawn {
						drawn = true
						canvas.Draw(p, func(cp *ui.Painter) {
							cp.Fill(ui.Rect{W: 40, H: 20}, ui.RGB(255, 0, 0), 0)
						})
					}
					p.Image(canvas.Bitmap(), r, ui.FillBox)
				})
				ui.Box(c).Height(480).Shrink(0)
			})
		})
	}, 100, 100)
	scroll.Y = 70
	tt.Frame()
	img := tt.Image()
	if got := img.RGBAAt(20, 45); got != (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("canvas at 20,45 = %v, want red", got)
	}
	if got := img.RGBAAt(20, 20); got == (color.RGBA{255, 0, 0, 255}) {
		t.Fatalf("canvas appeared at the wrong scroll offset: %v", got)
	}
}
