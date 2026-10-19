package ui

import (
	"embed"
	"path"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Upstream Magpie ships these icons under the MIT license. They are embedded
// and parsed once: the routing stage may redraw sixty times a second, but it
// must never read or parse an SVG on that path. A "-color" icon renders in its
// authored colors; the others are masks drawn in the interface ink, as in
// app.js's icon().
//
//go:embed assets/icons/*.svg assets/icons/*.png
var routingIconFS embed.FS

// routingIcon is one upstream icon, decoded once at startup. Colored SVGs
// keep their authored paint; mask SVGs are tinted per use; PNGs already
// carry the vendor's colors.
type routingIcon struct {
	svg     *ui.SVG
	bitmap  *ui.Bitmap
	colored bool
}

var (
	routingIconsOnce sync.Once
	routingIcons     map[string]routingIcon
)

func loadRoutingIcons() {
	routingIconsOnce.Do(func() {
		routingIcons = make(map[string]routingIcon)
		entries, err := routingIconFS.ReadDir("assets/icons")
		if err != nil {
			return
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), path.Ext(entry.Name()))
			data, err := routingIconFS.ReadFile("assets/icons/" + entry.Name())
			if err != nil {
				continue
			}
			icon := routingIcon{
				colored: strings.HasSuffix(name, "-color") || path.Ext(entry.Name()) == ".png",
			}
			if path.Ext(entry.Name()) == ".png" {
				bitmap, err := ui.DecodeBitmap(data)
				if err != nil {
					continue
				}
				icon.bitmap = bitmap
			} else {
				svg, err := ui.ParseSVG(data)
				if err != nil {
					continue
				}
				icon.svg = svg
			}
			routingIcons[strings.ToLower(name)] = icon
		}
	})
}

// routingIconFor resolves the icon keys Magpie records in Trace and
// providers.json. The second result is the generic fallback: a provider with
// no known picture must not borrow another vendor's mark.
func routingIconFor(keys ...string) (routingIcon, bool) {
	loadRoutingIcons()
	for _, key := range keys {
		key = strings.ToLower(strings.TrimSpace(key))
		if key == "" {
			continue
		}
		if icon, ok := routingIcons[key]; ok {
			return icon, true
		}
		for _, alias := range iconAliases(key) {
			if icon, ok := routingIcons[alias]; ok {
				return icon, true
			}
		}
		if icon, ok := routingIcons[strings.TrimPrefix(key, "app-")]; ok {
			return icon, true
		}
	}
	return routingIcon{}, false
}

func iconAliases(key string) []string {
	switch key {
	case "claude-code", "claudecode", "claude":
		return []string{"claudecode-color", "claude-color"}
	case "codex-cli":
		return []string{"codex-color"}
	case "gemini-cli", "geminicli":
		return []string{"gemini-color"}
	case "github-copilot", "copilot":
		return []string{"copilot-color", "githubcopilot"}
	case "deepseek-chat":
		return []string{"deepseek-color"}
	case "deepseek":
		return []string{"deepseek-color"}
	case "crush":
		return []string{"crush"}
	case "qwen":
		return []string{"qwen-color"}
	case "minimax":
		return []string{"minimax-color"}
	case "moonshot-ai":
		return []string{"moonshot"}
	case "x-ai", "xai-grok":
		return []string{"xai"}
	case "z-ai", "zhipu":
		return []string{"zai"}
	}
	return nil
}

func drawRoutingIcon(p *ui.Painter, r ui.Rect, keys []string, ink ui.Color) bool {
	icon, ok := routingIconFor(keys...)
	if !ok {
		return false
	}
	switch {
	case icon.bitmap != nil:
		p.Image(icon.bitmap, r, ui.Contain)
	case icon.colored:
		p.Image(icon.svg, r, ui.Contain)
	case icon.svg != nil:
		p.Icon(icon.svg, r, ink)
	default:
		return false
	}
	return true
}

func drawMagicLogo(p *ui.Painter, r ui.Rect, ink ui.Color) bool {
	icon, ok := routingIconFor("magpie")
	if !ok {
		return false
	}
	p.Icon(icon.svg, r, ink)
	return true
}
