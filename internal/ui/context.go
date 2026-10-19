package ui

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/Fan-chou/magpie-native-routing/internal/routing"
)

// ctxParts is context.js's stable order. The order is also the order of
// the stacked bar, and must not follow the prompt's JSON order.
var ctxParts = []string{"system", "tools", "memory", "files", "results", "chat"}

// ctxFmtK matches context.js's fmtK. It is deliberately not the same
// abbreviation as the token count in a request row: the context card uses
// two decimals below 10M and no decimal at 100K and above.
func ctxFmtK(n int) string {
	if n < 0 {
		n = 0
	}
	if n >= 1_000_000 {
		if n >= 10_000_000 {
			return ctxNumber(float64(n)/1_000_000, 1) + "M"
		}
		return ctxNumber(float64(n)/1_000_000, 2) + "M"
	}
	if n >= 1_000 {
		if n >= 100_000 {
			return ctxNumber(float64(n)/1_000, 0) + "K"
		}
		return ctxNumber(float64(n)/1_000, 1) + "K"
	}
	return itoa(n)
}

func ctxNumber(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	if strings.ContainsRune(s, '.') {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

// ctxFull is the part of the context window used. A prompt over its
// recorded window is still shown as full, as context.js does.
func ctxFull(p routing.Prompt) float64 {
	if p.Window <= 0 {
		return 0
	}
	return float64(p.Tokens) / float64(p.Window)
}

func ctxFullText(full float64) string {
	return trimFixed(full*100, 0) + "%"
}

func ctxHealth(full float64) (key, word string) {
	switch {
	case full < 0.75:
		return "healthy", "routing.contextHealthy"
	case full < 0.9:
		return "filling", "routing.contextFilling"
	default:
		return "nearFull", "routing.contextNearFull"
	}
}

func ctxPartTokens(p routing.Prompt) map[string]int {
	out := make(map[string]int, len(ctxParts))
	for _, part := range p.Parts {
		out[part.Kind] += part.Tokens
	}
	return out
}

func ctxPartColor(kind string, p palette) ui.Color {
	switch kind {
	case "system":
		return p.C6
	case "tools":
		return p.C2
	case "memory":
		return p.C4
	case "files":
		return p.C3
	case "results":
		return p.C5
	case "chat":
		return p.C1
	default:
		return p.Faint
	}
}

func ctxPartName(kind string, loc Localization) string {
	switch kind {
	case "system":
		return loc.T("routing.contextSystem")
	case "tools":
		return loc.T("routing.contextTools")
	case "memory":
		return loc.T("routing.contextMemory")
	case "files":
		return loc.T("routing.contextFiles")
	case "results":
		return loc.T("routing.contextResults")
	case "chat":
		return loc.T("routing.contextChat")
	default:
		return kind
	}
}

func ctxState(rv routing.RouteView, loc Localization) (word string, live bool, counted bool) {
	if !rv.Done && !rv.Completeness.History {
		return loc.T("routing.contextLive"), true, false
	}
	if rv.Prompt != nil && rv.Prompt.Counted {
		return loc.T("routing.contextCounted"), false, true
	}
	return loc.T("routing.contextEstimated"), false, false
}

func ctxCache(p routing.Prompt, usage []routing.RouteUsage) (read, total int, ok bool) {
	_ = p
	read = 0
	total = 0
	for _, u := range usage {
		read += u.CacheRead
		total += u.Input + u.CacheRead + u.CacheWrite
	}
	if total <= 0 {
		return 0, 0, false
	}
	return read, total, true
}

func ctxCrumbs(rv routing.RouteView) string {
	parts := []string{agentName(rv.Agent)}
	session := firstNonEmpty(rv.Session, rv.Conv)
	if session != "" {
		if len([]rune(session)) > 14 {
			session = string([]rune(session)[:12]) + "…"
		}
		parts = append(parts, session)
	}
	if rv.ID != 0 {
		parts = append(parts, "#"+itoa(int(rv.ID)))
	}
	return strings.Join(parts, " › ")
}

// routingContextCard is upstream's folded ctx-card in the routing log. The
// default is folded, so the request list keeps the room; clicking the head
// keeps the current implementation's compact card and exposes the full
// figures below without rebuilding the rest of the page.
func (m *Model) routingContextCard(c *ui.Context, rv routing.RouteView, p palette, loc Localization) {
	pr := rv.Prompt
	if pr == nil {
		return
	}
	full := ctxFull(*pr)
	state, live, _ := ctxState(rv, loc)
	cacheRead, cacheTotal, cacheOK := ctxCache(*pr, rv.Usage)
	open := m.contextCardOpen()

	ui.Column(c).Margin(12, 0, 0, 0).Padding(14, 16, 14, 16).
		Radius(14).Background(p.Panel).Border(1, p.Line).Gap(14).Children(func() {
		ui.Row(c).Height(20).Gap(8).AlignItems(ui.Center).Children(func() {
			chevron := "▸"
			if open {
				chevron = "▾"
			}
			head := ui.ButtonBase(c.Key("context-fold")).Padding(0, 0, 0, 0).Radius(6).Children(func() {
				ui.Text(c, chevron+" "+loc.T("routing.contextWindow")).Bold().FontSize(13).TextColor(p.Text).NoWrap()
			})
			if head.Clicked() {
				m.toggleContextCard()
				c.Invalidate()
			}
			used := " " + ctxFmtK(pr.Tokens)
			if pr.Window > 0 {
				used += " / " + ctxFmtK(pr.Window) + " · " + ctxFullText(full)
			}
			ui.Text(c, used).FontSize(11.5).TextColor(p.Muted).NoWrap()
			ui.Box(c).Grow(1).Height(6).MinWidth(24).MaxWidth(360).Radius(3).Background(p.Text.Alpha(0.07)).Draw(func(pt *ui.Painter, r ui.Rect) {
				usedTokens := float32(0)
				scale := float32(max(pr.Window, pr.Tokens, 1))
				if scale <= 0 {
					scale = 1
				}
				for _, kind := range ctxParts {
					n := float32(prPartTokens(*pr, kind))
					if n <= 0 {
						continue
					}
					w := r.W * n / scale
					pt.Fill(ui.Rect{X: r.X + usedTokens, Y: r.Y, W: w, H: r.H}, ctxPartColor(kind, p), 0)
					usedTokens += w
				}
			})
			if rv.Model != "" {
				ui.Text(c, rv.Model).FontSize(11).TextColor(p.Muted).NoWrap()
			}
			stateColor := p.Muted
			if live {
				stateColor = p.Success
			}
			ui.Box(c).Padding(0, 8).Height(20).Radius(10).Background(boolColor(live, p.SuccessSoft, p.PanelMuted)).Children(func() {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(stateColor)
					ui.Text(c, state).FontSize(11).TextColor(stateColor).NoWrap()
				})
			})
		})

		if open {
			ui.Row(c).Gap(28).AlignItems(ui.Start).Wrap().Children(func() {
				contextStat(c, loc.T("routing.contextUsed"), ctxFmtK(pr.Tokens), func() string {
					if pr.Window > 0 {
						return loc.F("routing.contextFull", ctxFullText(full)) + " · " +
							loc.F("routing.contextFree", ctxFmtK(max(0, pr.Window-pr.Tokens)))
					}
					return loc.T("routing.contextNoWindow")
				}(), p)
				if cacheOK {
					contextStat(c, loc.T("routing.contextPromptCache"), trimFixed(float64(cacheRead)/float64(cacheTotal)*100, 0)+"%", loc.F("routing.contextCached", ctxFmtK(cacheRead), ctxFmtK(cacheTotal-cacheRead)), p)
				}
			})

			ui.Row(c).Gap(14).AlignItems(ui.Center).Wrap().Children(func() {
				tokens := ctxPartTokens(*pr)
				for _, kind := range ctxParts {
					if tokens[kind] <= 0 {
						continue
					}
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(8, 8).Radius(2.5).Background(ctxPartColor(kind, p))
						ui.Text(c, ctxPartName(kind, loc)).FontSize(11.5).TextColor(p.Text2)
						ui.Text(c, ctxFmtK(tokens[kind])).FontSize(11.5).TextColor(p.Muted)
					})
				}
				ui.Spacer(c)
				ui.Text(c, ctxCrumbs(rv)).FontSize(10.5).TextColor(p.Muted).NoWrap()
				if pr.Turns > 0 {
					ui.Text(c, loc.F("routing.contextTurns", pr.Turns)).FontSize(11).TextColor(p.Muted).NoWrap()
				}
			})
		}
	})
}

func prPartTokens(p routing.Prompt, kind string) int {
	for _, part := range p.Parts {
		if part.Kind == kind {
			return part.Tokens
		}
	}
	return 0
}

func contextStat(c *ui.Context, label, value, detail string, p palette) {
	ui.Column(c).Gap(3).MinWidth(160).Children(func() {
		ui.Text(c, label).FontSize(10.5).TextColor(p.Muted)
		ui.Text(c, value).Bold().FontSize(20).TextColor(p.Text).NoWrap()
		ui.Text(c, detail).FontSize(11).TextColor(p.Muted).NoWrap()
	})
}
