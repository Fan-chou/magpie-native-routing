package ui

import (
	"math"
	"sort"
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

func ctxHealthLabel(key string, loc Localization) string {
	switch key {
	case "healthy":
		return loc.T("routing.contextHealthy")
	case "filling":
		return loc.T("routing.contextFilling")
	case "nearFull":
		return loc.T("routing.contextNearFull")
	default:
		return ""
	}
}

const (
	ctxCellCount        = 400
	ctxFullWaffleCols   = 50
	ctxNarrowWaffleCols = 25
	ctxWaffleGap        = float32(2)
	ctxWaffleCardWidth  = float32(520)
)

// ctxWaffleColumns follows context.js's width classes: a card at 520 CSS
// pixels or less uses 25 columns instead of 50. The caller passes the
// card's content width (already inside its 16px inline padding).
func ctxWaffleColumns(cardWidth float32) int {
	if cardWidth > 0 && cardWidth <= ctxWaffleCardWidth {
		return ctxNarrowWaffleCols
	}
	return ctxFullWaffleCols
}

// ctxCells mirrors context.js's cellsOf(): 400 cells of one 1/400 window
// each, filled by prompt part. Each cell also names the item whose share
// its centre falls in, exactly as upstream's largest-remainder allocation
// does. The grid is a data view of the recorded tokens, not a decorative
// bar; a window smaller than the prompt is scaled by the prompt.
func ctxCells(p routing.Prompt) []ctxCell {
	cells := make([]ctxCell, 0, ctxCellCount)
	scale := max(p.Window, p.Tokens, 1)
	used := min(ctxCellCount, int(float64(p.Tokens)/float64(scale)*ctxCellCount+0.5))
	type partCell struct {
		part  *routing.PromptPart
		cells int
		want  float64
		items []routing.PromptItem
	}
	var parts []partCell
	sum := 0
	for i := range p.Parts {
		part := &p.Parts[i]
		if part.Kind == "" || part.Tokens <= 0 {
			continue
		}
		parts = append(parts, partCell{part: part})
		sum += part.Tokens
	}
	if sum <= 0 || used <= 0 {
		return make([]ctxCell, ctxCellCount)
	}
	// Largest remainder, so the cells add up to used and every non-empty
	// part gets at least one cell. Upstream sorts the remainders
	// descending; ties retain the stable context.js part order.
	sort.SliceStable(parts, func(i, j int) bool {
		ri := ctxPartOrder(parts[i].part.Kind)
		rj := ctxPartOrder(parts[j].part.Kind)
		if ri == rj {
			return parts[i].part.Kind < parts[j].part.Kind
		}
		return ri < rj
	})
	left := used
	for i := range parts {
		parts[i].want = float64(parts[i].part.Tokens) / float64(sum) * float64(used)
		parts[i].cells = max(1, int(math.Floor(parts[i].want)))
		left -= parts[i].cells
	}
	remainderOrder := make([]int, len(parts))
	for i := range parts {
		remainderOrder[i] = i
	}
	sort.SliceStable(remainderOrder, func(i, j int) bool {
		ri := parts[remainderOrder[i]].want - math.Floor(parts[remainderOrder[i]].want)
		rj := parts[remainderOrder[j]].want - math.Floor(parts[remainderOrder[j]].want)
		return ri > rj
	})
	for j := 0; left > 0 && len(remainderOrder) > 0; j = (j + 1) % len(remainderOrder) {
		parts[remainderOrder[j]].cells++
		left--
	}
	for j := len(parts) - 1; left < 0 && j >= 0; j-- {
		for left < 0 && parts[j].cells > 1 {
			parts[j].cells--
			left++
		}
	}
	for i := range parts {
		parts[i].items = append([]routing.PromptItem(nil), parts[i].part.Items...)
		sort.SliceStable(parts[i].items, func(a, b int) bool {
			return parts[i].items[a].Tokens > parts[i].items[b].Tokens
		})
		itemSum := 0
		for _, item := range parts[i].items {
			itemSum += item.Tokens
		}
		if itemSum <= 0 {
			itemSum = 1
		}
		at, acc := 0, 0
		for c := 0; c < parts[i].cells; c++ {
			mid := (float64(c) + 0.5) / float64(parts[i].cells) * float64(itemSum)
			for at < len(parts[i].items)-1 &&
				float64(acc+parts[i].items[at].Tokens) < mid {
				acc += parts[i].items[at].Tokens
				at++
			}
			item := -1
			if len(parts[i].items) > 0 && at < len(parts[i].items) {
				item = at
			}
			cells = append(cells, ctxCell{
				kind: parts[i].part.Kind,
				item: item,
			})
		}
	}
	for len(cells) < ctxCellCount {
		cells = append(cells, ctxCell{})
	}
	return cells[:ctxCellCount]
}

// ctxCell is one 1/400 slice of the context window. item indexes the part's
// items after the same descending-token sort used by context.js, or -1 for
// a part without item detail.
type ctxCell struct {
	kind string
	item int
}

func ctxPartOrder(kind string) int {
	for i, candidate := range ctxParts {
		if candidate == kind {
			return i
		}
	}
	return len(ctxParts)
}

func ctxPartItems(pr routing.Prompt, kind string) []routing.PromptItem {
	for _, part := range pr.Parts {
		if part.Kind == kind {
			return part.Items
		}
	}
	return nil
}

func ctxItemName(item routing.PromptItem, loc Localization) string {
	switch item.Tag {
	case "turn":
		return loc.F("routing.contextTurnItem", item.Name)
	case "turns":
		return loc.F("routing.contextEarlierTurns", item.N)
	case "more":
		return loc.F("routing.contextMoreItems", item.N)
	}
	names := map[string]string{
		"prompt":      "routing.contextItemPrompt",
		"harness":     "routing.contextItemHarness",
		"billing":     "routing.contextItemBilling",
		"environment": "routing.contextItemEnvironment",
		"context":     "routing.contextItemContext",
		"skills":      "routing.contextItemSkills",
		"reminders":   "routing.contextItemReminders",
		"compacted":   "routing.contextItemCompacted",
		"image":       "routing.contextItemImage",
		"document":    "routing.contextItemDocument",
	}
	if key, ok := names[item.Name]; ok {
		return loc.T(key)
	}
	return item.Name
}

func ctxItemTag(item routing.PromptItem) string {
	switch item.Tag {
	case "turn", "turns", "more", "compacted", "":
		return ""
	default:
		return item.Tag
	}
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
	state, live, counted := ctxState(rv, loc)
	cacheRead, cacheTotal, cacheOK := ctxCache(*pr, rv.Usage)
	open := m.contextCardOpen()
	healthKey, _ := ctxHealth(full)

	ui.Column(c).Margin(12, 0, 0, 0).Padding(14, 16, 14, 16).
		Radius(14).Background(p.Panel).Border(1, p.Line).Gap(14).Children(func() {
		// The same head draws folded and open, so its title does not jump
		// when the details appear.
		ui.Row(c).Height(20).Gap(8).AlignItems(ui.Center).Children(func() {
			chevron := "▸"
			if open {
				chevron = "▾"
			}
			head := ui.ButtonBase(c.Key("context-fold")).Label(loc.T("routing.contextWindow")).
				Padding(0, 0, 0, 0).Radius(6).Children(func() {
				ui.Text(c, chevron+" "+loc.T("routing.contextWindow")).Bold().FontSize(13).TextColor(p.Text).NoWrap()
			})
			if head.Hovered() {
				head.Background(p.PanelMuted)
			}
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
			if live || counted {
				stateColor = p.Success
			}
			ui.Box(c).Padding(0, 8).Height(20).Radius(10).Background(boolColor(live || counted, p.SuccessSoft, p.PanelMuted)).Children(func() {
				ui.Row(c).Gap(5).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(6, 6).Radius(3).Background(stateColor)
					ui.Text(c, state).FontSize(11).TextColor(stateColor).NoWrap()
				})
			})
		})

		if open {
			ui.Grid(c).ColumnTracks(ui.Fr(1.4), ui.Fr(1)).GapX(28).GapY(12).Children(func() {
				ui.Column(c).Gap(4).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, loc.T("routing.contextUsed")).FontSize(10.5).Bold().TextColor(p.Muted).NoWrap()
						if pr.Window > 0 {
							badge := p.Muted
							bg := p.PanelMuted
							switch healthKey {
							case "healthy":
								badge, bg = p.Success, p.SuccessSoft
							case "filling":
								badge, bg = p.Warning, p.WarningSoft
							case "nearFull":
								badge, bg = p.Danger, p.DangerSoft
							}
							ui.Box(c).Padding(0, 7).Height(17).Radius(9).Background(bg).Children(func() {
								ui.Text(c, ctxHealthLabel(healthKey, loc)).FontSize(10).TextColor(badge).NoWrap()
							})
						}
					})
					ui.Row(c).Gap(4).AlignItems(ui.Baseline).Children(func() {
						ui.Text(c, ctxFmtK(pr.Tokens)).FontWeight(600).FontSize(28).TextColor(p.Text).NoWrap()
						window := loc.T("routing.contextTokens")
						if pr.Window > 0 {
							window = "/ " + ctxFmtK(pr.Window) + " " + window
						}
						ui.Text(c, window).FontSize(13).TextColor(p.Muted).NoWrap()
					})
					detail := loc.T("routing.contextNoWindow")
					if pr.Window > 0 {
						detail = loc.F("routing.contextFull", ctxFullText(full)) + " · " +
							loc.F("routing.contextFree", ctxFmtK(max(0, pr.Window-pr.Tokens)))
					}
					ui.Text(c, detail).FontSize(10.5).TextColor(p.Muted).NoWrap()
				})
				ui.Column(c).Gap(4).Children(func() {
					ui.Text(c, loc.T("routing.contextPromptCache")).FontSize(10.5).Bold().TextColor(p.Muted).NoWrap()
					value := "—"
					if cacheOK {
						value = trimFixed(float64(cacheRead)/float64(cacheTotal)*100, 0) + "%"
					}
					ui.Row(c).Gap(4).AlignItems(ui.Baseline).Children(func() {
						ui.Text(c, value).FontWeight(600).FontSize(28).TextColor(boolColor(cacheOK, p.Text, p.Faint)).NoWrap()
						ui.Text(c, loc.T("routing.contextHit")).FontSize(13).TextColor(p.Muted).NoWrap()
					})
					if cacheOK {
						ui.Box(c).Height(6).Radius(3).Background(p.Text.Alpha(0.07)).Draw(func(pt *ui.Painter, r ui.Rect) {
							w := r.W * clamp01(float32(cacheRead)/float32(max(cacheTotal, 1)))
							pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: w, H: r.H}, p.Success, 3)
						})
					}
					detail := loc.T("routing.contextCacheUnknown")
					if cacheOK {
						detail = loc.F("routing.contextCached", ctxFmtK(cacheRead), ctxFmtK(cacheTotal-cacheRead))
					}
					ui.Text(c, detail).FontSize(10.5).TextColor(p.Muted).NoWrap()
				})
			})

			// .ctx-waffle: one cell for each 1/400 of the window. The
			// recorded parts choose the colour; free cells keep the card
			// ink at 7%, as --k-free does upstream.
			cardWidth, _ := c.Size()
			// The card lives in the 1200px page cap, inside 20px page
			// padding and 16px card padding. ctxCard measures its own
			// content box, so the waffle breakpoint is 520px content.
			waffleCols := ctxWaffleColumns(min(cardWidth, 1200) - 40 - 32)
			waffleRows := (ctxCellCount + waffleCols - 1) / waffleCols
			ui.Box(c).AspectRatio(float32(waffleCols) / float32(waffleRows)).Draw(func(pt *ui.Painter, r ui.Rect) {
				cells := ctxCells(*pr)
				gap := ctxWaffleGap
				cell := min(
					(r.W-gap*float32(waffleCols-1))/float32(waffleCols),
					(r.H-gap*float32(waffleRows-1))/float32(waffleRows),
				)
				if cell <= 0 {
					return
				}
				for i, part := range cells {
					x := r.X + float32(i%waffleCols)*(cell+gap)
					y := r.Y + float32(i/waffleCols)*(cell+gap)
					color := p.Text.Alpha(0.07)
					if part.kind != "" {
						color = ctxPartColor(part.kind, p)
					}
					pt.Fill(ui.Rect{X: x, Y: y, W: cell, H: cell}, color, 2)
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
				if pr.Window > pr.Tokens {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Size(8, 8).Radius(2.5).Background(p.Text.Alpha(0.07)).Border(1, p.Text.Alpha(0.10))
						ui.Text(c, loc.T("routing.contextFreeSpace")).FontSize(11.5).TextColor(p.Text2)
						ui.Text(c, ctxFmtK(pr.Window-pr.Tokens)).FontSize(11.5).TextColor(p.Muted)
					})
				}
			})

			items := m.contextItems(*pr)
			ui.Column(c).Gap(6).Padding(12, 0, 0, 0).BorderWidth(1, 0, 0, 0).BorderColor(p.Line).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, loc.T("routing.contextContents")).FontSize(10.5).Bold().TextColor(p.Muted).NoWrap()
					ui.Spacer(c)
					if len(items) > 0 {
						ui.Text(c, itoa(len(items))).FontSize(10.5).TextColor(p.Faint).NoWrap()
					}
				})
				if len(items) == 0 {
					ui.Text(c, loc.T("routing.contextNothing")).FontSize(12).TextColor(p.Muted)
					return
				}
				showAll := m.contextShowAll()
				limit := len(items)
				if !showAll && limit > 8 {
					limit = 8
				}
				for _, item := range items[:limit] {
					m.contextItemRow(c, item, p, loc)
				}
				if len(items) > 8 {
					label := loc.F("routing.contextShowAll", len(items))
					if showAll {
						label = loc.T("routing.contextShowFewer")
					}
					if ui.ButtonBase(c.Key("ctx-more")).Padding(2, 5).Radius(6).Children(func() {
						ui.Text(c, label).FontSize(11.5).TextColor(p.Muted)
					}).Clicked() {
						m.toggleContextShowAll()
						c.Invalidate()
					}
				}
			})

			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				ui.Text(c, ctxCrumbs(rv)).FontSize(10.5).TextColor(p.Muted).NoWrap().Grow(1)
				var notes []string
				if pr.Held {
					notes = append(notes, loc.T("routing.contextHeld"))
				}
				if pr.Turns > 0 {
					notes = append(notes, loc.F("routing.contextTurns", pr.Turns))
				}
				if len(notes) > 0 {
					ui.Text(c, strings.Join(notes, " · ")).FontSize(10.5).TextColor(p.Muted).NoWrap()
				}
			})
		}
	})
}

type contextItem struct {
	kind  string
	item  routing.PromptItem
	total int
}

func (m *Model) contextItems(pr routing.Prompt) []contextItem {
	var out []contextItem
	for _, kind := range ctxParts {
		items := append([]routing.PromptItem(nil), ctxPartItems(pr, kind)...)
		sort.SliceStable(items, func(i, j int) bool { return items[i].Tokens > items[j].Tokens })
		partTotal := prPartTokens(pr, kind)
		for _, item := range items {
			out = append(out, contextItem{kind: kind, item: item, total: partTotal})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].item.Tokens > out[j].item.Tokens })
	return out
}

func (m *Model) contextItemRow(c *ui.Context, item contextItem, p palette, loc Localization) {
	label := ctxItemName(item.item, loc)
	ui.Row(c).Height(26).Gap(8).AlignItems(ui.Center).Padding(0, 6).Radius(6).Children(func() {
		ui.Box(c).Size(8, 8).Radius(2.5).Background(ctxPartColor(item.kind, p))
		ui.Text(c, label).FontSize(12).TextColor(p.Text).NoWrap().Grow(1)
		if item.item.N > 1 && item.item.Tag != "turns" && item.item.Tag != "more" {
			ui.Text(c, "×"+itoa(item.item.N)).FontSize(11).TextColor(p.Muted).NoWrap()
		}
		if tag := ctxItemTag(item.item); tag != "" {
			ui.Box(c).Padding(0, 5).Radius(4).Background(p.PanelMuted).Children(func() {
				ui.Text(c, tag).FontSize(10).TextColor(p.Muted).NoWrap()
			})
		}
		ui.Box(c).Height(4).Width(72).Radius(2).Background(p.Text.Alpha(0.07)).Draw(func(pt *ui.Painter, r ui.Rect) {
			w := r.W * clamp01(float32(item.item.Tokens)/float32(max(item.total, 1)))
			pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: w, H: r.H}, ctxPartColor(item.kind, p), 2)
		})
		ui.Text(c, ctxFmtK(item.item.Tokens)).FontSize(11.5).TextColor(p.Text2).NoWrap()
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
