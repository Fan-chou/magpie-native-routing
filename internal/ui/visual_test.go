package ui

import (
	"image"
	"testing"

	"github.com/Fan-chou/magpie-native-routing/internal/config"
)

// TestRoutingStageRendersBrandingAndDenseRows is a pixel-level guard for
// the visual layer. It deliberately checks structure and brand color rather
// than storing platform-specific golden images: font rasterization differs
// across macOS, Windows and Linux.
func TestRoutingStageRendersBrandingAndDenseRows(t *testing.T) {
	for _, tc := range []struct {
		name          string
		width, height int
		theme         config.ThemeMode
	}{
		{name: "light", width: 1240, height: 900, theme: config.ThemeLight},
		{name: "dark", width: 1240, height: 900, theme: config.ThemeDark},
		{name: "narrow", width: 600, height: 1000, theme: config.ThemeLight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model, _ := testModel(t, completedSyntheticSnapshot(t), Actions{})
			settings := model.Settings()
			settings.Theme = tc.theme
			model.SetSettings(settings)
			if tc.width == 600 {
				model.mu.Lock()
				model.historyDay = "2026-10-18"
				model.replay = replayController{speed: 1, day: "2026-10-18"}
				model.mu.Unlock()
			}
			tester := render(t, model, tc.width, tc.height)
			tester.Frame()

			img := tester.Image()
			assertImageHasContrast(t, img, 0, 0, img.Bounds().Dx(), img.Bounds().Dy(), tc.theme)
			for _, text := range []string{"Routing", "Codex", "Synthetic Provider", "Backup Provider"} {
				if !tester.HasText(text) {
					t.Fatalf("routing stage lacks %q: %v", text, tester.Texts())
				}
			}
		})
	}
}

func assertImageHasContrast(t *testing.T, img *image.RGBA, x0, y0, x1, y1 int, theme config.ThemeMode) {
	t.Helper()
	x1, y1 = min(x1, img.Bounds().Dx()), min(y1, img.Bounds().Dy())
	colors := map[[3]uint8]struct{}{}
	for y := max(0, y0); y < y1; y += 3 {
		for x := max(0, x0); x < x1; x += 3 {
			i := img.PixOffset(x, y)
			colors[[3]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2]}] = struct{}{}
		}
	}
	if len(colors) < 8 {
		t.Fatalf("%s stage rendered only %d sampled colors; branding/wires are missing", theme, len(colors))
	}
}
