package ui

import (
	"math"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/Fan-chou/magpie-native-routing/internal/config"
	"github.com/Fan-chou/magpie-native-routing/internal/routing"
)

// View is the root native view. All reads come from immutable snapshots; all
// network and file work is invoked through Actions outside this frame.
func (m *Model) View(c *ui.Context) {
	if c == nil {
		return
	}
	s := m.snapshot()
	now := c.Now()
	width, _ := c.Size()
	routes := m.visibleRoutes(s, m.tabKind() == tabHistory)
	m.syncReplay(s, now)
	m.syncFlightTracks(s, routes, now)
	m.mu.Lock()
	m.topology.reduceMotion = c.Preferences().ReduceMotion
	m.mu.Unlock()
	p := colors(c, m.Settings().Theme)
	c.SetTheme(themeFor(c, p))

	ui.Column(c).Fill().Children(func() {
		if c.TitleBar().Height > 0 {
			m.appToolbar(c, s, p, locForSettings(m.Settings()))
		}
		m.statusBanners(c, s, p)
		ui.Box(c).Grow(1).Children(func() {
			switch m.tabKind() {
			case tabLive:
				m.routingHome(c, s, p, now, false, width)
			case tabHistory:
				m.routingHome(c, s, p, now, true, width)
			case tabGroups:
				m.groupsView(c, s, p)
			}
		})
		m.settingsModal(c, p)
	})
}

func locForSettings(settings config.AppSettings) Localization {
	return NewLocalization(settings.Language)
}

// appToolbar is app.css's .top: a 50px macOS window bar with the traffic
// lights in it, the magpie mark and name at the left, centered 24px page
// tabs, and 26px actions at the right. The whole bar drags the window; its
// controls are built as buttons, so MyGo's click handling takes precedence
// over the drag flag.
func (m *Model) appToolbar(c *ui.Context, s routing.Snapshot, p palette, loc Localization) {
	bar := c.TitleBar()
	height := max(bar.Height, 50)
	ui.Row(c).Height(height).Shrink(0).Padding(0, 12, 0, bar.Left+10).
		AlignItems(ui.Center).Background(p.Canvas).BorderWidth(0, 0, 1, 0).
		BorderColor(p.LineSoft).DragWindow().Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(18, 18).Draw(func(pt *ui.Painter, r ui.Rect) {
				if !drawMagicLogo(pt, r, p.Text) {
					drawMagpieMark(pt, r, p.Text, p.Canvas)
				}
			})
			ui.Text(c, "magpie").FontSize(13).FontWeight(600).TextColor(p.Text)
		})
		ui.Spacer(c)
		m.appTabs(c, p, loc)
		ui.Spacer(c)
		ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
			refresh := ui.ButtonBase(c.Key("toolbar-refresh")).Size(26, 26).Radius(7).
				Tooltip(loc.T("app.refresh"))
			if refresh.Hovered() {
				refresh.Background(p.PanelMuted)
			}
			refresh.Draw(func(pt *ui.Painter, r ui.Rect) {
				drawRefreshGlyph(pt, r, boolColor(refresh.Hovered(), p.Text, p.Muted))
			})
			if refresh.Clicked() {
				if m.actions.Refresh != nil {
					go m.actions.Refresh()
				}
				if m.actions.RefreshHistory != nil {
					go m.actions.RefreshHistory()
				}
			}
			settings := ui.ButtonBase(c.Key("toolbar-settings")).Size(26, 26).Radius(7).
				Tooltip(loc.T("app.settings"))
			if settings.Hovered() {
				settings.Background(p.PanelMuted)
			}
			settings.Draw(func(pt *ui.Painter, r ui.Rect) {
				drawGearGlyph(pt, r, boolColor(settings.Hovered(), p.Text, p.Muted))
			})
			if settings.Clicked() {
				m.setSettingsOpen(true)
			}
		})
	})
}

func (m *Model) appTabs(c *ui.Context, p palette, loc Localization) {
	tab := m.tabKind()
	tabButton := func(kind tabKind, key, label string) {
		selected := tab == kind
		btn := ui.ButtonBase(c.Key(key)).Height(24).Padding(0, 12).Radius(6)
		if selected {
			btn.Background(p.Text.Alpha(0.09))
		} else if btn.Hovered() {
			btn.Background(p.Text.Alpha(0.035))
		}
		btn.Children(func() {
			ui.Text(c, label).FontSize(12.5).TextColor(boolColor(selected, p.Text, p.Muted)).NoWrap()
		})
		if btn.Clicked() {
			m.setTab(kind)
			if kind == tabHistory {
				m.mu.Lock()
				m.syncListSel = true
				m.mu.Unlock()
			}
		}
	}
	ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
		tabButton(tabLive, "toolbar-live", loc.T("nav.live"))
		tabButton(tabHistory, "toolbar-history", loc.T("nav.history"))
		tabButton(tabGroups, "toolbar-groups", loc.T("nav.groups"))
	})
}

func drawRefreshGlyph(p *ui.Painter, r ui.Rect, color ui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	var path ui.Path
	const steps = 28
	for i := 0; i <= steps; i++ {
		a := -0.35*math.Pi + (1.8*math.Pi)*float64(i)/steps
		x, y := cx+5*float32(math.Cos(a)), cy+5*float32(math.Sin(a))
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	p.StrokePath(&path, 1.5, color)
	p.Line(cx+4.7, cy-3.2, cx+5.2, cy+1.4, 1.5, color)
	p.Line(cx+5.2, cy+1.4, cx+0.8, cy+0.4, 1.5, color)
}

func drawGearGlyph(p *ui.Painter, r ui.Rect, color ui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	p.Stroke(ui.Rect{X: cx - 3.3, Y: cy - 3.3, W: 6.6, H: 6.6}, color, 3.3, 1.4)
	for i := 0; i < 8; i++ {
		a := float64(i) * math.Pi / 4
		x, y := float32(math.Cos(a)), float32(math.Sin(a))
		p.Line(cx+x*4.1, cy+y*4.1, cx+x*6, cy+y*6, 1.5, color)
	}
}

func (m *Model) tabKind() tabKind {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tab
}

func (m *Model) statusBanners(c *ui.Context, s routing.Snapshot, p palette) {
	loc := m.localization()
	if !s.GatewayOnline {
		ui.Row(c).Padding(7, 18).Gap(8).AlignItems(ui.Center).Background(p.WarningSoft).Children(func() {
			ui.Box(c).Size(7, 7).Radius(4).Background(p.Warning)
			ui.Text(c, firstNonEmpty(s.GatewayError, loc.T("gateway.reconnecting"))).FontSize(11).TextColor(p.Text)
			ui.Spacer(c)
			ui.Text(c, loc.T("routing.reconnectHint")).FontSize(10).TextColor(p.Muted)
			if ui.Button(c.Key("retry"), loc.T("app.retry")).Clicked() && m.actions.Refresh != nil {
				go m.actions.Refresh()
			}
		})
	}
	if s.GatewayWarning != "" {
		ui.Row(c).Padding(6, 18).Gap(8).AlignItems(ui.Center).Background(p.PanelMuted).Children(func() {
			ui.Text(c, loc.T("gateway.warning")).Bold().FontSize(10).TextColor(p.Warning)
			ui.Text(c, s.GatewayWarning).FontSize(10).TextColor(p.Muted)
		})
	}
}

func (m *Model) connectionPill(c *ui.Context, s routing.Snapshot, p palette, loc Localization) {
	color, text := p.Warning, loc.T("gateway.connecting")
	switch {
	case s.GatewayOnline && s.GatewayError == "":
		color, text = p.Success, loc.T("gateway.online")
	case !s.GatewayOnline && !s.LastPoll.IsZero():
		color, text = p.Danger, loc.T("gateway.offline")
	}
	ui.Row(c).Gap(6).Padding(5, 9).Radius(20).Background(color.Alpha(0.12)).Children(func() {
		ui.Box(c).Size(7, 7).Radius(4).Background(color)
		ui.Text(c, text).FontSize(10).TextColor(color).NoWrap()
		ui.Text(c, m.Settings().GatewayURL).FontSize(9).TextColor(p.Faint).NoWrap()
	})
}

func (m *Model) liveView(c *ui.Context, s routing.Snapshot, p palette, now time.Time) {
	routes := m.visibleRoutes(s, false)
	m.prepareListSelection(routes)
	sel, ok := m.selectedRoute(routes)
	ui.Box(c).HeightPercent(100).Children(func() {
		ui.SplitPercent(c, &m.splitPercent,
			func() { m.topologyPane(c, s, routes, sel, ok, p, now) },
			func() { m.detailPane(c, s, routes, sel, ok, p) },
		).Fill()
	})
}

// routingHome reproduces Magpie's Routing page structure: one centered,
// single-column workspace with a routing story card, then the request list
// and account column. The selected request's detailed trail follows below;
// it is not a permanent right-hand inspector.
func (m *Model) routingHome(c *ui.Context, s routing.Snapshot, p palette, now time.Time, history bool, windowWidth float32) {
	routes := m.visibleRoutes(s, history)
	m.prepareListSelection(routes)
	sel, ok := m.selectedRoute(routes)
	ui.Box(c).Fill().Background(p.Canvas).Children(func() {
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).WidthPercent(100).MaxWidth(1200).AlignSelf(ui.Center).Padding(14, 20, 24, 20).Gap(12).Children(func() {
				m.routingPageHead(c, s, p, history)
				if history {
					m.replayBar(c, s, routes, p, now)
				}
				m.routingStageCard(c, s, routes, sel, ok, p, now, history)
				m.routingRequestSection(c, s, routes, sel, ok, p, history, windowWidth, now)
				m.routingGroupsSection(c, s, p)
			})
		})
	})
}

func (m *Model) routingPageHead(c *ui.Context, s routing.Snapshot, p palette, history bool) {
	loc := m.localization()
	note := loc.T("routing.liveNoteUpstream")
	if history {
		note = loc.T("routing.historyNote")
	}
	// app.css and routing.css put this line at 14px top, 2px side, 26px
	// high, with a 6px rtlive dot after the title, an 8px gap before the
	// note, and the two page actions at the right.
	ui.Row(c).Height(26).Gap(0).AlignItems(ui.Center).Margin(14, 0, 6, 0).PaddingX(2).Children(func() {
		ui.Text(c, "Routing").Bold().FontSize(13).TextColor(p.Text)
		liveDot(c, p.Success, 6, 7)
		ui.Text(c, note).FontSize(11).TextColor(p.Faint).PaddingStart(8)
		ui.Spacer(c)
		// New group and Hide accounts are the two actions in the original
		// .row-head. Group editing is blocked by the public API boundary;
		// the entry remains visible and says so instead of writing files.
		newGroup := ui.ButtonBase(c.Key("newGroup")).Padding(2, 6).Radius(6).Tooltip(loc.T("routing.newGroupUnsupported")).Children(func() {
			ui.Text(c, "+ "+loc.T("routing.newGroup")).FontSize(11).TextColor(p.Muted)
		})
		if newGroup.Clicked() {
			m.setGroupNotice(loc.T("routing.newGroupUnsupported"))
			m.setTab(tabGroups)
		}
		mask := loc.T("app.hideAccounts")
		if m.accountsMasked() {
			mask = loc.T("app.showAccounts")
		}
		if ui.ButtonBase(c.Key("mask")).Padding(2, 6).Radius(6).Children(func() {
			ui.Text(c, mask).FontSize(11).TextColor(p.Muted)
		}).Clicked() {
			m.toggleAccountMask()
		}
	})
}

// liveDot draws upstream's .rt-livedot: a 6px green disc whose rtlive
// ripple is 0 at the start, reaches 6px by 70%, and stays invisible to
// 100%. reduced motion leaves the still disc.
func liveDot(c *ui.Context, color ui.Color, size, marginStart float32) {
	reduce := c.Preferences().ReduceMotion
	ui.Box(c).Size(size, size).MarginStart(marginStart).Draw(func(p *ui.Painter, r ui.Rect) {
		cx, cy := r.X+r.W/2, r.Y+r.H/2
		p.Fill(ui.Rect{X: cx - size/2, Y: cy - size/2, W: size, H: size}, color, size/2)
		if reduce {
			return
		}
		phase := float32(p.Now().UnixMilli()%1800) / 1800
		k := clamp01(phase / 0.7)
		ring := size + 6*k
		alpha := 0.4 * (1 - k)
		if alpha > 0.005 {
			p.Stroke(ui.Rect{X: cx - ring/2, Y: cy - ring/2, W: ring, H: ring}, color.Alpha(alpha), ring/2, 1)
		}
	})
}

func (m *Model) routingStageCard(c *ui.Context, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, ok bool, p palette, now time.Time, history bool) {
	loc := m.localization()
	title := loc.T("routing.liveStage")
	detail := loc.T("routing.decisionNote")
	if history {
		title = loc.T("routing.historyStage")
		detail = loc.T("history.replayWarning")
	}
	if ok {
		title = firstNonEmpty(routeGroupName(sel), sel.Model)
	}
	modeID := "unknown"
	if ok {
		providerRouting, _ := routeProviderRouting(sel, s.Config)
		modeID = routingModeID(sel, providerRouting)
	}
	if text := loc.T("routing.mode." + modeID); text != "routing.mode."+modeID {
		detail = text
	}
	stageWidth := float32(0)
	if w, _ := c.Size(); w > 0 {
		// The stage sits inside 20px page padding and 16px card padding.
		// This is the .rt card's own width, which is what client.js's
		// ResizeObserver uses for its max560 layout, not the window's.
		stageWidth = min(w, 1200) - 40 - 32
	}
	stageHeight := m.routingStageHeightForWidth(stageWidth, s, routes, sel, ok, now)
	ui.Column(c).Shrink(0).Radius(14).Background(p.Panel).Border(1, p.Line).Clip().Children(func() {
		// The original Routing card opens with a two-line explanation of
		// the actual routing mode, never with a dashboard title.
		ui.Column(c).Padding(14, 16, 10, 16).Gap(3).Children(func() {
			ui.Row(c).Gap(0).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, title).Bold().FontSize(13).TextColor(p.Text)
				if ok && sel.Group != nil {
					ui.Text(c, " · "+loc.T("app.group")+" · "+itoa(len(sel.Order))+" "+loc.T("routing.membersOn")).FontSize(11).TextColor(p.Muted)
				}
			})
			ui.Text(c, detail).FontSize(12.5).TextColor(p.Muted)
		})
		// routing.css: .rt-stage has 22px vertically and 2px horizontally.
		// Those gutters live in layoutRoutingStage, so the draw box is the
		// complete stage and node geometry is identical for every layer.
		ui.Box(c).Height(stageHeight).Draw(func(pt *ui.Painter, r ui.Rect) {
			m.drawRoutingStageCanvas(pt, r, s, routes, sel, ok, p, now, history)
		})
		if ok {
			m.routingStory(c, sel, routes, p, loc)
			if sel.Prompt != nil {
				m.routingContextCard(c, sel, p, loc)
			}
		}
	})
}

// routingStageHeight lets concurrent agents and accounts use the space they
// need rather than being silently clipped. It mirrors stageAgents and
// stageAccountRows, including the replay return window.
func (m *Model) routingStageHeight(s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, hasSel bool, now time.Time) float32 {
	return m.routingStageHeightForWidth(0, s, routes, sel, hasSel, now)
}

func (m *Model) routingStageHeightForWidth(width float32, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, hasSel bool, now time.Time) float32 {
	_ = now
	replay, replayActive := m.replayTopologyState()
	agents := m.stageAgents(s, sel, hasSel, routes, replay, replayActive)
	rows := m.stageAccountRows(sel, hasSel, s.Live, replay.routes, replayActive)
	if len(rows) == 0 {
		rows = []providerNode{{}}
	}
	content := routingStageContentHeight(len(agents), len(rows))
	if width > 0 && width <= routingStageNarrowWidth {
		// Two columns above (agents and hub) and the accounts below,
		// matching the max560 grid's 26px row gap.
		metrics := routingStageMetrics(len(agents), len(rows))
		top := max(stageStackHeight(len(agents), metrics.agentH, metrics.agentGap), routingStageHubH)
		content = top + 26 + stageStackHeight(len(rows), metrics.rowH, metrics.rowGap)
	}
	return max(routingStageMinHeight, content+routingStagePadding*2)
}

func boolColor(v bool, yes, no ui.Color) ui.Color {
	if v {
		return yes
	}
	return no
}

func routeGroupName(rv routing.RouteView) string {
	if rv.Group == nil {
		return ""
	}
	return firstNonEmpty(rv.Group.Name, rv.Group.ID)
}

func (m *Model) routingStory(c *ui.Context, rv routing.RouteView, routes []routing.RouteView, p palette, loc Localization) {
	fail := finalRouteFailure(rv)
	requests, rerouted, failed := routingStats(routes)
	ui.Column(c).Gap(0).Padding(10, 16).BorderWidth(1, 0, 0, 0).BorderColor(p.Line).Children(func() {
		// .rt-foot: the caption and counters on one baseline, then the
		// request's sentence and compact facts. These counters are scoped to
		// the same filtered route set as the list, never to the one selected
		// request and never to tries mistaken for requests.
		ui.Row(c).Gap(10).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, firstNonEmpty(fail, routingStoryText(rv, loc))).FontSize(13).TextColor(boolColor(fail != "", p.Danger, p.Text)).Grow(1)
			metricStack(c, loc.T("app.requests"), itoa(requests), p)
			metricStack(c, loc.T("routing.rerouted"), itoa(rerouted), p)
			metricStack(c, loc.T("routing.errors"), itoa(failed), p)
		})
		// .rt-log is a separate block under .rt-foot: a heading naming this
		// request and its actions, then the recorded steps.
		ui.Column(c).Gap(5).Margin(10, 0, 0, 0).Padding(10, 0, 0, 0).
			BorderWidth(1, 0, 0, 0).BorderColor(p.Line).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				heading := loc.T("routing.storyLive")
				if m.isPinned(rv.ID) {
					heading = loc.F("routing.storyAt", clockText(rv.Time))
				}
				ui.Text(c, heading).Bold().FontSize(12).TextColor(p.Text).Grow(1)
				if m.isPinned(rv.ID) {
					if ui.ButtonBase(c.Key("storyBackLive")).Padding(2, 5).Radius(6).Children(func() {
						ui.Text(c, loc.T("routing.storyBackLive")).FontSize(11).TextColor(p.Muted)
					}).Clicked() {
						m.togglePin(rv.ID)
					}
				}
				if rv.HistoryDay != "" {
					if ui.ButtonBase(c.Key("storyReplay")).Padding(2, 5).Radius(6).Children(func() {
						ui.Text(c, loc.T("app.replay")).FontSize(11).TextColor(p.Muted)
					}).Clicked() {
						m.PlayRoute(rv.HistoryDay, rv.ID)
						if m.actions.OpenDay != nil {
							go m.actions.OpenDay(rv.HistoryDay)
						}
					}
				}
			})
			for _, line := range routingStorySteps(rv, m.accountsMasked(), loc) {
				ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
					dotColor := p.Muted
					if line.kind == "why" {
						dotColor = p.Text
					} else if line.kind == "ok" {
						dotColor = p.Success
					} else if line.kind == "bad" {
						dotColor = p.Danger
					} else if line.kind == "wait" {
						dotColor = p.Text
					}
					if line.kind == "aside" {
						ui.Box(c).Size(5, 5).Radius(3).Margin(4, 1, 0, 1).Background(p.Faint)
					} else {
						ui.Box(c).Size(6, 6).Radius(3).Margin(3, 2, 0, 0).Background(dotColor)
					}
					ui.Text(c, line.text).FontSize(12).TextColor(boolColor(line.kind == "bad", p.Danger, boolColor(line.kind == "aside", p.Muted, p.Text2))).Grow(1)
				})
			}
		})
	})
}

type routingStoryStep struct {
	text string
	kind string // why, aside, ok, bad, wait
}

// routingStorySteps follows client.js's renderSteps: the initial request,
// any recorded affinity or rule reason, then each real try. Each line comes
// from the trace; text the public API does not provide (request bodies,
// private rule state) is simply omitted rather than guessed.
func routingStorySteps(rv routing.RouteView, masked bool, loc Localization) []routingStoryStep {
	out := make([]routingStoryStep, 0, 4+len(rv.Tries)*2)
	asked := firstNonEmpty(rv.Model, loc.T("status.noModel"))
	main := routing.Weighed{}
	for _, w := range rv.Order {
		if !w.Fallback {
			main = w
			break
		}
	}
	if rv.Group != nil {
		members := strings.TrimSpace(strings.Join(rv.Group.Members, ", "))
		line := loc.F("routing.groupStory", agentName(rv.Agent), firstNonEmpty(rv.Group.Name, rv.Group.ID))
		if members != "" {
			line = loc.F("routing.groupStoryMembers", agentName(rv.Agent),
				firstNonEmpty(rv.Group.Name, rv.Group.ID), members)
		}
		out = append(out, routingStoryStep{
			text: line,
		})
	} else {
		line := loc.F("routing.routeStory", agentName(rv.Agent), asked)
		if main.Name != "" {
			line += " → " + main.Name
		}
		out = append(out, routingStoryStep{text: line})
	}
	if why := routingAffinityWhy(rv, masked, loc); why != "" {
		out = append(out, routingStoryStep{text: why, kind: "why"})
	}
	if why := routingRuleWhy(rv, loc); why != "" {
		out = append(out, routingStoryStep{text: why, kind: "why"})
	}
	for i, tr := range rv.Tries {
		kind := "ok"
		if !tr.Done {
			kind = "wait"
		} else if tryFailed(tr) {
			kind = "bad"
		}
		who := routeTryWho(rv, tr)
		if masked {
			who = maskWho(who)
		}
		line := loc.F("routing.tryStory",
			loc.T("routing.attempt")+" "+itoa(i+1)+" · "+agentName(rv.Agent),
			who, tryStatusText(tr), durationText(tr.Millis))
		if reason := routing.TryFailure(tr); reason != "" {
			line += " · " + reason
		}
		out = append(out, routingStoryStep{text: line, kind: kind})
		if tr.TTFT > 0 {
			out = append(out, routingStoryStep{
				text: loc.F("routing.firstTokenStep", durationText(tr.TTFT)),
				kind: "aside",
			})
		}
	}
	if rv.Done && len(rv.Tries) == 0 {
		out = append(out, routingStoryStep{
			text: loc.F("routing.nothingTried", firstNonEmpty(rv.Error, itoa(rv.Status))),
			kind: "bad",
		})
	}
	return out
}

func routingAffinityWhy(rv routing.RouteView, masked bool, loc Localization) string {
	a := rv.Affinity
	if a == nil || a.Why == "" {
		return ""
	}
	who := a.Last
	if masked && who != "" {
		who = maskWho(who)
	}
	switch a.Why {
	case "turn":
		return loc.F("routing.affinityTurn", who, agentName(rv.Agent), a.Turn)
	case "cache":
		return loc.F("routing.affinityCache", who, a.Turn)
	case "session":
		return loc.F("routing.affinitySession", who)
	default:
		return ""
	}
}

func routingRuleWhy(rv routing.RouteView, loc Localization) string {
	if rv.Rule == nil {
		return ""
	}
	if len(rv.Rule.When) > 0 {
		return loc.F("routing.ruleMatched", rv.Rule.N, join(", ", rv.Rule.When))
	}
	if rv.Rule.N > 0 {
		return loc.F("routing.ruleMatchedNoWhen", rv.Rule.N)
	}
	return ""
}

// finalRouteFailure is the failure the agent actually received. Earlier
// attempts may fail and still be rerouted successfully; using any failure
// in the try list made a successful fallback look like a failed request.
func finalRouteFailure(rv routing.RouteView) string {
	if len(rv.Tries) > 0 {
		last := rv.Tries[len(rv.Tries)-1]
		if tryFailed(last) {
			return routing.TryFailure(last)
		}
		return ""
	}
	if rv.Done || rv.Completeness.History {
		if rv.Error != "" {
			return rv.Error
		}
		if rv.Status >= 400 {
			return routing.TryFailure(routing.Try{Status: rv.Status})
		}
	}
	return ""
}

func tryStatusText(tr routing.Try) string {
	if tr.Status > 0 {
		return "HTTP " + itoa(tr.Status)
	}
	if !tr.Done {
		return "…"
	}
	return ""
}

func metricStack(c *ui.Context, label, value string, p palette) {
	ui.Column(c).Gap(1).AlignItems(ui.Start).Children(func() {
		ui.Text(c, value).Bold().FontSize(19).TextColor(p.Text).NoWrap()
		ui.Text(c, label).FontSize(11).TextColor(p.Muted).NoWrap()
	})
}

// routingGroupsSection is the compact groups block that sits under the
// requests on Magpie's Routing page. It shows only read facts; editing is
// intentionally unavailable because providers.json is never written.
func (m *Model) routingGroupsSection(c *ui.Context, s routing.Snapshot, p palette) {
	loc := m.localization()
	groups := routing.GroupViews(s.Config, s.Routes)
	ui.Column(c).Gap(7).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, loc.T("groups.title")).Bold().FontSize(12)
			ui.Text(c, itoa(len(groups))).FontSize(10).TextColor(p.Muted)
			ui.Spacer(c)
			ui.Text(c, loc.T("groups.readOnly")).FontSize(10).TextColor(p.Faint)
		})
		if len(groups) == 0 {
			ui.Box(c).Padding(10, 12).Radius(10).Background(p.Panel).Border(1, p.Line).Children(func() {
				ui.Text(c, loc.T("groups.noGroups")).FontSize(11).TextColor(p.Muted)
			})
			return
		}
		for _, g := range groups {
			g := g
			row := ui.Row(c).Padding(8, 12).Radius(8).Gap(10).AlignItems(ui.Center)
			row.BorderWidth(1, 0, 0, 0).BorderColor(p.LineSoft)
			row.Children(func() {
				ui.Box(c).Size(7, 7).Radius(4).Background(boolColor(g.Auto, p.Faint, p.Text))
				ui.Text(c, g.Name).FontSize(12).Bold().NoWrap()
				mode := "Smart"
				if key := "routing.mode.name." + routingModeID(routing.RouteView{Route: routing.Route{Group: groupRef(g)}}, ""); loc.T(key) != key {
					mode = loc.T(key)
				}
				ui.Text(c, mode).FontSize(10.5).TextColor(p.Muted).NoWrap()
				ui.Spacer(c)
				ui.Text(c, itoa(len(g.Members))+" "+loc.T("groups.membersCount")).FontSize(10.5).TextColor(p.Muted).NoWrap()
				ui.Text(c, itoa(g.Observed)+" "+loc.T("groups.observedRequests")).FontSize(10.5).TextColor(p.Faint).NoWrap()
			})
		}
	})
}

func groupRef(g routing.GroupView) *routing.GroupRef {
	return &routing.GroupRef{
		ID: g.ID, Name: g.Name, Routing: g.Routing, Affinity: g.Affinity,
		Auto: g.Auto, Sink: g.Sink, Members: g.Members, Fast: g.Fast,
	}
}

func routingStoryText(rv routing.RouteView, loc Localization) string {
	if rv.Group != nil && rv.Group.ID != "" {
		name := firstNonEmpty(rv.Group.Name, rv.Group.ID)
		if members := strings.TrimSpace(strings.Join(rv.Group.Members, ", ")); members != "" {
			return loc.F("routing.groupStoryMembers", agentName(rv.Agent), name, members)
		}
		return loc.F("routing.groupStory", agentName(rv.Agent), name)
	}
	return loc.F("routing.routeStory", agentName(rv.Agent), firstNonEmpty(rv.Model, loc.T("status.noModel")))
}

func routingTracePhrase(rv routing.RouteView, loc Localization) string {
	if !rv.CandidateOrderKnown {
		return loc.T("routing.liveOnly")
	}
	return loc.F("routing.candidatesObserved", len(rv.Order), len(rv.Tries))
}

func (m *Model) routingTrailSection(c *ui.Context, rv routing.RouteView, p palette) {
	loc := m.localization()
	ui.Column(c).Radius(12).Background(p.Panel).Border(1, p.Line).Children(func() {
		ui.Row(c).Padding(9, 12).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, loc.T("routing.trace")).Bold().FontSize(11)
			ui.Text(c, traceSourceLabel(rv, loc)).FontSize(9).TextColor(p.Muted)
			ui.Spacer(c)
			ui.Text(c, replayClock(time.Duration(routeMillis(rv))*time.Millisecond)).FontSize(9).TextColor(p.Faint)
		})
		ui.Column(c).Padding(10, 12).Gap(7).Children(func() {
			m.traceContents(c, rv, p, loc)
		})
	})
}

func (m *Model) traceContents(c *ui.Context, rv routing.RouteView, p palette, loc Localization) {
	if !rv.CandidateOrderKnown {
		notice(c, p, loc.T("routing.orderUnknown"))
	}
	for i, w := range rv.Order {
		m.candidateRow(c, i, w, rv, p, loc)
	}
	if len(rv.Tries) > 0 {
		ui.Divider(c)
	}
	for i, tr := range rv.Tries {
		m.tryRow(c, i, tr, p, loc)
	}
}

func (m *Model) routingRequestSection(c *ui.Context, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, ok bool, p palette, history bool, windowWidth float32, now time.Time) {
	loc := m.localization()
	layout := requestSectionLayout(windowWidth)
	pageWidth := layout.pageWidth
	twoColumn := layout.twoColumn
	compactFilters := pageWidth < 900
	ui.Column(c).Shrink(0).Gap(7).Children(func() {
		m.requestSectionHead(c, routes, sel, ok, p, history)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			m.dayChips(c, s, p, history)
			if !compactFilters {
				ui.Spacer(c)
			}
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			query := m.queryValue()
			if ui.SearchField(c, &query).Label(loc.T("routing.requestList")).Width(180).Shrink(0).Changed() {
				m.mu.Lock()
				m.query = query
				m.syncListSel = true
				m.mu.Unlock()
			}
			statuses := []string{loc.T("app.all"), loc.T("app.active"), loc.T("app.completed"), loc.T("app.failed")}
			idx := m.statusValue()
			if ui.Segmented(c, &idx, statuses...).Label(loc.T("app.status")).Changed() {
				m.setStatus([]string{"all", "active", "completed", "failed"}[idx], idx)
			}
		})
		if twoColumn {
			ui.Grid(c).ColumnTracks(ui.Fr(1), ui.Fixed(layout.accountWidth)).GapX(16).AlignItems(ui.Start).Children(func() {
				m.routingRequestList(c, routes, p, 0, layout.metaUnder)
				m.routingAccountColumn(c, routes, p, layout.accountWidth, now, sel, ok)
			})
		} else {
			m.routingRequestList(c, routes, p, 100, layout.metaUnder)
			m.routingAccountColumn(c, routes, p, 100, now, sel, ok)
		}
	})
}

type requestSectionGeometry struct {
	pageWidth    float32
	twoColumn    bool
	accountWidth float32
	// metaFit in client.js moves a request's numbers below its destination
	// when they no longer fit beside it. Beside the accounts at the 1150px
	// breakpoint the list is about 800px, the exact case upstream names.
	metaUnder bool
}

func requestSectionLayout(windowWidth float32) requestSectionGeometry {
	pageWidth := min(windowWidth, 1200)
	twoColumn := pageWidth >= 1150
	accountWidth := max(260, min(pageWidth*0.28, 360))
	return requestSectionGeometry{
		pageWidth: pageWidth, twoColumn: twoColumn,
		accountWidth: accountWidth, metaUnder: twoColumn,
	}
}

func (m *Model) requestSectionHead(c *ui.Context, routes []routing.RouteView, sel routing.RouteView, ok bool, p palette, history bool) {
	loc := m.localization()
	note := loc.F("routing.listScope", len(routes))
	if history {
		note = loc.F("routing.listScopeDay", len(routes), m.currentHistoryDay())
	}
	ui.Row(c).Height(26).AlignItems(ui.Center).Children(func() {
		ui.Text(c, loc.T("routing.requests")).Bold().FontSize(13).TextColor(p.Text)
		if ok {
			// Keep the provenance explicit. Live trace data, completed
			// history and their merge are different evidence, even when
			// the row layout is the original's.
			ui.Text(c, traceSourceLabel(sel, loc)).FontSize(10.5).TextColor(p.Muted).NoWrap().PaddingStart(8)
		}
		ui.Spacer(c)
		ui.Text(c, note).FontSize(10.5).TextColor(p.Muted).NoWrap()
	})
}
func (m *Model) dayChips(c *ui.Context, s routing.Snapshot, p palette, history bool) {
	loc := m.localization()
	current := m.currentHistoryDay()
	live := ui.Button(c.Key("day-live"), loc.T("history.live"))
	live.Selected(!history)
	if live.Clicked() {
		m.setTab(tabLive)
		m.setStatus("all", 0)
	}
	days := m.historyDays(s)
	for i, d := range days {
		if i >= 4 {
			break
		}
		day := d.Day
		label := day
		if len(day) == 10 {
			label = day[5:]
		}
		if day == time.Now().Format("2006-01-02") {
			label = loc.T("status.today")
		}
		if d.Requests > 0 {
			label += " " + itoa(d.Requests)
		}
		btn := ui.Button(c.Key("day-"+day), label)
		btn.Selected(history && day == current)
		if btn.Clicked() {
			m.setTab(tabHistory)
			m.setStatus("completed", 1)
			m.SetHistoryDay(day)
			m.mu.Lock()
			m.syncListSel = true
			m.mu.Unlock()
			if m.actions.OpenDay != nil {
				go m.actions.OpenDay(day)
			}
		}
	}
}

func (m *Model) routingRequestList(c *ui.Context, routes []routing.RouteView, p palette, widthPercent float32, metaUnder bool) {
	loc := m.localization()
	if len(routes) == 0 {
		ui.Column(c).Grow(1).Gap(4).Padding(14, 12, 18, 12).Children(func() {
			ui.Text(c, loc.T("routing.noRequests")).Bold().FontSize(13).TextColor(p.Text)
			ui.Text(c, loc.T("routing.noRequestsHint")).FontSize(12).TextColor(p.Muted)
		})
		return
	}
	m.mu.Lock()
	m.listState.Key = func(i int) any { return routes[i].ID }
	m.listState.Label = func(i int) string { return routeLabel(routes[i]) }
	m.listState.Selected = &m.requestSel
	sel := m.requestSel
	m.mu.Unlock()
	list := ui.List(c, &m.listState, len(routes), func(i int) {
		m.routingRequestRow(c, routes[i], i == sel, p, metaUnder)
	}).Grow(1).MaxHeight(420)
	if widthPercent > 0 {
		list.WidthPercent(widthPercent)
	}
	if list.Changed() {
		m.commitListSelection(routes)
	}
}

func (m *Model) routingRequestRow(c *ui.Context, rv routing.RouteView, selected bool, p palette, metaUnder bool) {
	loc := m.localization()
	said, how, tr := routeOutcome(rv)
	tracks := []ui.Track{ui.Fixed(60), ui.FitContent(), ui.Fr(1), ui.FitContent()}
	rowGap := float32(0)
	if metaUnder {
		tracks = []ui.Track{ui.Fixed(60), ui.FitContent(), ui.Fr(1)}
		rowGap = 2
	}
	row := ui.Grid(c).ColumnTracks(tracks...).GapX(12).GapY(rowGap).
		AlignItems(ui.Center).Padding(6, 8).Radius(8).Cursor(ui.CursorPointer)
	if selected {
		row.Background(p.PanelMuted).Border(1, p.Line)
	} else if row.Hovered() {
		row.Background(p.PanelMuted.Alpha(0.7))
	}
	row.Children(func() {
		ui.Text(c, clockText(rv.Time)).FontSize(12).TextColor(p.Muted).NoWrap()
		ui.Row(c).MinWidth(130).MaxWidth(300).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(8, 8).Radius(4).Background(agentHue(rv.Agent))
			drawRoutingIconWidget(c, m, []string{agentIcon(rv.Agent), rv.Agent}, 14, p.Text)
			ui.Text(c, firstNonEmpty(rv.Model, loc.T("status.noModel"))).FontSize(12).TextColor(p.Text).NoWrap().Grow(1)
			if rv.Kind != "" {
				ui.Box(c).Padding(0, 5).Radius(5).Background(p.PanelMuted).Border(1, p.Line).Children(func() {
					ui.Text(c, rv.Kind).FontSize(10).TextColor(p.Muted).NoWrap()
				})
			}
		})
		ui.Row(c).MinWidth(100).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Grow(1).Height(20).Clip().Draw(func(pt *ui.Painter, r ui.Rect) {
				drawRequestDestination(pt, r, said, how, p)
			})
			for _, tag := range routeDestinationTags(rv, tr, loc) {
				ui.Box(c).Padding(0, 5).Radius(5).Background(boolColor(tag.warn, p.WarningSoft, p.PanelMuted)).Children(func() {
					ui.Text(c, tag.text).FontSize(10.5).TextColor(boolColor(tag.warn, p.Warning, p.Muted)).NoWrap()
				})
			}
		})
		metrics := routeMetrics(rv, how, loc)
		meta := ui.Row(c).Gap(5).AlignItems(ui.Center)
		if metaUnder {
			meta = meta.RowStart(2).ColumnStart(2).ColumnSpan(2).Justify(ui.Start)
		} else {
			meta = meta.Justify(ui.End)
		}
		meta.Children(func() {
			first := true
			separator := func() {
				if !first {
					ui.Text(c, "·").FontSize(11).TextColor(p.Faint)
				}
				first = false
			}
			if len(rv.Tries) > 1 {
				separator()
				ui.Text(c, loc.F("routing.triesCount", len(rv.Tries))).FontSize(10.5).TextColor(p.Muted).NoWrap()
			}
			for _, metric := range metrics {
				separator()
				switch metric.key {
				case "ttft", "cache":
					ui.Text(c, metric.label).FontSize(10.5).TextColor(p.Muted).NoWrap()
				}
				ui.Text(c, metric.value).FontSize(11).TextColor(p.Muted).NoWrap()
				if metric.key == "tokens" {
					ui.Text(c, loc.T("app.tokens")).FontSize(10.5).TextColor(p.Muted).NoWrap()
				}
			}
		})
	})
}

func splitRouteEndpoint(said string) (account, model string) {
	if i := strings.LastIndex(said, " · "); i >= 0 {
		return strings.TrimSpace(said[:i]), strings.TrimSpace(said[i+3:])
	}
	return strings.TrimSpace(said), ""
}

// drawRequestDestination uses the widget's measured width and Painter's
// font metrics. The previous two Text widgets could overflow their grid
// cell and paint through the metrics; a clipped painter keeps the original
// account/model split without that overlap.
func drawRequestDestination(p *ui.Painter, r ui.Rect, said, how string, pal palette) {
	account, model := splitRouteEndpoint(said)
	status := pal.Success
	if how == "moved" {
		status = pal.Accent
	} else if how == "bad" {
		status = pal.Danger
	}
	dot := float32(6)
	p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H/2 - dot/2, W: dot, H: dot}, status, dot/2)
	x := r.X + dot + 6
	width := r.W - dot - 6
	if width <= 0 {
		return
	}
	const size = 12
	sep := ""
	if model != "" {
		sep = " · "
	}
	sepW, _ := p.MeasureText(0, ui.Span{Text: sep, Size: size, Color: pal.Text})
	accountMax := width
	if model != "" {
		accountMax = max(width*0.58-sepW, 24)
	}
	account = fitText(p, account, size, 400, accountMax)
	accountW, _ := p.MeasureText(0, ui.Span{Text: account, Size: size, Color: boolColor(how == "bad", pal.Danger, pal.Text)})
	p.RichText(x, r.Y, width, ui.Span{
		Text: account, Size: size, Color: boolColor(how == "bad", pal.Danger, pal.Text),
	})
	if model == "" {
		return
	}
	modelX := x + accountW
	modelMax := width - accountW - sepW
	if modelMax <= 4 {
		return
	}
	p.RichText(modelX, r.Y, modelMax+2, ui.Span{Text: sep, Size: size, Color: pal.Muted})
	model = fitText(p, model, size, 400, modelMax)
	p.RichText(modelX+sepW, r.Y, modelMax, ui.Span{Text: model, Size: size, Color: pal.Text})
}

type destinationTag struct {
	text string
	warn bool
}

func routeDestinationTags(rv routing.RouteView, tr routing.Try, loc Localization) []destinationTag {
	var out []destinationTag
	if tr.Effort != "" {
		label := tr.Effort
		if rv.Effort != "" && rv.Effort != tr.Effort {
			label = rv.Effort + " → " + tr.Effort
		}
		out = append(out, destinationTag{text: label})
	}
	if tr.Fast {
		out = append(out, destinationTag{text: "fast"})
	}
	if tr.Swapped && tr.Done && !tryFailed(tr) && tr.Served != "" {
		out = append(out, destinationTag{text: "served " + tr.Served, warn: true})
	}
	if tr.Routed && tr.Done && !tryFailed(tr) && tr.Served != "" {
		out = append(out, destinationTag{text: "served " + tr.Served})
	}
	if tr.Upstream != "" && tr.Done && !tryFailed(tr) {
		out = append(out, destinationTag{text: "Upstream: " + tr.Upstream})
	}
	return out
}

func drawRoutingIconWidget(c *ui.Context, m *Model, keys []string, size float32, ink ui.Color) {
	ui.Box(c).Size(size, size).Draw(func(p *ui.Painter, r ui.Rect) {
		if !drawRoutingIcon(p, r, keys, ink) {
			drawGenericIcon(p, r, ink.Alpha(0.7))
		}
	})
}

func (m *Model) routingAccountColumn(c *ui.Context, routes []routing.RouteView, p palette, width float32, now time.Time, sel routing.RouteView, hasSel bool) {
	loc := m.localization()
	ui.Column(c).Width(width).Gap(6).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Text(c, loc.T("routing.accountsAndKeys")).Bold().FontSize(13)
			ui.Spacer(c)
			ui.Text(c, loc.T("routing.overThoseRequests")).FontSize(11).TextColor(p.Muted)
		})
		rows := accountTally(routes)
		if len(rows) == 0 {
			ui.Text(c, loc.T("status.noProvider")).FontSize(11).TextColor(p.Muted)
			return
		}
		hidden := m.accountsMasked()
		provider := ""
		for _, row := range rows {
			if row.Provider != provider {
				provider = row.Provider
				ui.Row(c).Gap(7).AlignItems(ui.Center).Padding(8, 0, 4, 0).Children(func() {
					drawRoutingIconWidget(c, m, []string{row.Icon, row.Provider}, 14, p.Muted)
					ui.Text(c, firstNonEmpty(row.Name, row.Provider)).FontSize(12).Bold().NoWrap()
					ui.Spacer(c)
					if row.Routing != "" {
						ui.Text(c, row.Routing).FontSize(10.5).TextColor(p.Muted).NoWrap()
					}
				})
			}
			ui.Column(c).Gap(3).Padding(6, 12, 6, 33).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					who := accountTallyTitle(row)
					if hidden {
						who = maskWho(who)
					}
					ui.Text(c, who).FontSize(12).Bold().TextColor(p.Text).NoWrap().Grow(1)
					if plan := accountTallyPlan(row, loc); plan != "" {
						ui.Text(c, plan).FontSize(11).TextColor(p.Muted).NoWrap()
					}
				})
				restText := accountTallyStatus(row, now, loc)
				if restText != "" {
					ui.Text(c, restText).FontSize(11).TextColor(boolColor(row.Rest != nil, p.Danger, p.Text2))
				}
				ui.Row(c).Gap(12).AlignItems(ui.Center).Wrap().Children(func() {
					ui.Text(c, loc.F("routing.accountTally.tried", row.Tried)).FontSize(11).TextColor(p.Muted).NoWrap()
					ui.Text(c, loc.F("routing.accountTally.answered", row.OK)).FontSize(11).TextColor(p.Success).NoWrap()
					if row.Failures > 0 {
						ui.Text(c, loc.F("routing.accountTally.failed", row.Failures)).FontSize(11).TextColor(p.Danger).NoWrap()
					}
					if !row.LastAt.IsZero() {
						ui.Text(c, loc.F("routing.accountTally.lastAnswered", clockText(row.LastAt))).FontSize(11).TextColor(p.Muted).NoWrap()
					}
				})
			})
		}
	})
}

func accountTallyTitle(row routeAccountTally) string {
	if strings.EqualFold(row.Kind, "provider") {
		return firstNonEmpty(row.Model, row.Name, row.Provider)
	}
	if row.Who != "" {
		return row.Who
	}
	return firstNonEmpty(row.Name, row.Provider, row.Model)
}

func accountTallyPlan(row routeAccountTally, loc Localization) string {
	if strings.EqualFold(row.Kind, "provider") {
		return ""
	}
	if row.Plan != "" {
		return row.Plan
	}
	if strings.EqualFold(row.Kind, "key") {
		return loc.T("routing.accountStatus.apiKey")
	}
	return ""
}

func accountTallyStatus(row routeAccountTally, now time.Time, loc Localization) string {
	if row.Rest != nil && row.Rest.Until.After(now) {
		why := firstNonEmpty(failWord(row.Rest.Why), loc.T("routing.accountStatus.resting"))
		return loc.F("routing.accountStatus.restingOnly", why+" · "+restDurationText(row.Rest.Until.Sub(now)))
	}
	if row.Known && !row.ObservedAt.IsZero() {
		return loc.F("routing.accountStatus.usedAt", int(row.Used+0.5), clockText(row.ObservedAt))
	}
	if strings.EqualFold(row.Kind, "account") {
		return loc.T("routing.accountStatus.unknownQuota")
	}
	return ""
}

func providerAccountText(rv routing.RouteView, masked bool) string {
	for _, tr := range rv.Tries {
		provider := providerOf(tr.Model)
		if provider == "" {
			continue
		}
		who := tr.ID
		if who == "" {
			who = tr.Served
		}
		if masked && who != "" {
			who = maskWho(who)
		}
		return provider + " · " + firstNonEmpty(who, tr.Model)
	}
	return firstNonEmpty(rv.DisplayProvider(), "—")
}

func statusMetric(rv routing.RouteView, loc Localization) string {
	status := selected(loc, statusKey(rv))
	if rv.Status >= 400 {
		return itoa(rv.Status) + " · " + status
	}
	return status
}

func (m *Model) topologyPane(c *ui.Context, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, ok bool, p palette, now time.Time) {
	loc := m.localization()
	ui.Column(c).Fill().Padding(14).Gap(10).Children(func() {
		m.metricStrip(c, s, p, loc)
		card := ui.Column(c).Grow(1).MinHeight(300).Radius(12).Background(p.Panel).Border(1, p.LineSoft).Clip()
		card.Children(func() {
			ui.Row(c).Padding(12, 14).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, loc.T("routing.topology")).Bold().FontSize(12)
				ui.Text(c, loc.T("routing.liveNote")).FontSize(9).TextColor(p.Muted).NoWrap()
				ui.Spacer(c)
				ui.Text(c, timeText(s.LastPoll, loc)).FontSize(9).TextColor(p.Faint).NoWrap()
			})
			ui.Box(c).Grow(1).Draw(func(pt *ui.Painter, r ui.Rect) {
				m.drawTopology(pt, r, s, routes, sel, ok, p, now)
			})
		})
		m.requestList(c, s, routes, p)
	})
}

func (m *Model) metricStrip(c *ui.Context, s routing.Snapshot, p palette, loc Localization) {
	active := int64(len(s.Live))
	failed, total := 0, 0
	rerouted := 0
	for _, r := range s.Routes {
		total++
		if len(r.Tries) > 1 || r.Retried > 0 {
			rerouted++
		}
		if r.Status >= 400 || r.Error != "" {
			failed++
		}
	}
	metric := func(label, value string, accent ui.Color) {
		ui.Column(c).Grow(1).Padding(9, 12).Radius(9).Background(p.Panel).Border(1, p.LineSoft).Gap(1).Children(func() {
			ui.Text(c, value).FontSize(18).Bold().TextColor(accent).NoWrap()
			ui.Text(c, label).FontSize(9).TextColor(p.Muted).NoWrap()
		})
	}
	ui.Row(c).Gap(8).Children(func() {
		metric(loc.T("routing.inFlight"), itoa(int(active)), p.Accent)
		metric(loc.T("routing.requests"), itoa(total), p.Text)
		metric(loc.T("routing.rerouted"), itoa(rerouted), p.Warning)
		metric(loc.T("routing.errors"), itoa(failed), p.Danger)
		metric(loc.T("routing.candidates"), m.candidateSummary(s), p.Cyan)
	})
}

func (m *Model) candidateSummary(s routing.Snapshot) string {
	known := 0
	for _, r := range s.Routes {
		if r.CandidateOrderKnown {
			known++
		}
	}
	if known == 0 {
		return "—"
	}
	return itoa(known)
}

func (m *Model) requestList(c *ui.Context, s routing.Snapshot, routes []routing.RouteView, p palette) {
	loc := m.localization()
	ui.Column(c).Gap(8).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, loc.T("routing.requestList")).Bold().FontSize(11)
			ui.Text(c, itoa(len(routes))).FontSize(9).TextColor(p.Muted)
			ui.Spacer(c)
			query := m.queryValue()
			if ui.SearchField(c, &query).Label(loc.T("routing.requestList")).Changed() {
				m.mu.Lock()
				m.query = query
				m.syncListSel = true
				m.mu.Unlock()
			}
			statuses := []string{loc.T("app.all"), loc.T("app.active"), loc.T("app.completed"), loc.T("app.failed")}
			idx := m.statusValue()
			if ui.Segmented(c, &idx, statuses...).Label(loc.T("app.status")).Changed() {
				m.setStatus([]string{"all", "active", "completed", "failed"}[idx], idx)
				m.mu.Lock()
				m.syncListSel = true
				m.mu.Unlock()
			}
		})

		if len(routes) == 0 {
			ui.Box(c).Height(84).Radius(9).Background(p.Panel).Border(1, p.LineSoft).Center().Children(func() {
				ui.Text(c, loc.T("status.empty")).FontSize(11).TextColor(p.Muted)
			})
			return
		}

		m.mu.Lock()
		m.listState.Key = func(i int) any {
			if i >= 0 && i < len(routes) {
				return routes[i].ID
			}
			return i
		}
		m.listState.Label = func(i int) string { return routeLabel(routes[i]) }
		m.listState.Selected = &m.requestSel
		sel := m.requestSel
		m.mu.Unlock()

		list := ui.List(c, &m.listState, len(routes), func(i int) {
			m.routeRow(c, routes[i], i == sel, p)
		}).Grow(1)
		if list.Changed() {
			m.commitListSelection(routes)
		}
	})
}

func (m *Model) routeRow(c *ui.Context, rv routing.RouteView, selected bool, p palette) {
	loc := m.localization()
	status := statusKey(rv)
	statusColor := p.Success
	if status == "active" {
		statusColor = p.Accent
	} else if status == "failed" {
		statusColor = p.Danger
	}
	row := ui.Row(c).Padding(8, 10).Gap(9).AlignItems(ui.Center).Radius(7)
	if selected {
		row.Background(p.AccentSoft)
	}
	row.Children(func() {
		ui.Box(c).Width(3).Height(30).Radius(2).Background(statusColor)
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, agentName(rv.Agent)).FontSize(10).Bold().NoWrap()
				ui.Text(c, shortID(rv.ID)).FontSize(9).TextColor(p.Faint).NoWrap()
				ui.Text(c, loc.T("app."+status)).FontSize(9).TextColor(statusColor).NoWrap()
				if rv.Completeness.History {
					ui.Text(c, loc.T("app.completedHistory")).FontSize(8).TextColor(p.Muted).NoWrap()
				}
			})
			ui.Text(c, firstNonEmpty(rv.Model, loc.T("status.noModel"))).FontSize(9).TextColor(p.Muted).NoWrap()
		})
		ui.Column(c).Gap(2).AlignItems(ui.End).Children(func() {
			ui.Text(c, durationText(routeMillis(rv))).FontSize(10).TextColor(p.Text).NoWrap()
			ui.Text(c, clockText(rv.Time)).FontSize(8).TextColor(p.Faint).NoWrap()
		})
	})
}

func countRerouted(routes []routing.RouteView) int {
	n := 0
	for _, r := range routes {
		if len(r.Tries) > 1 || r.Retried > 0 {
			n++
		}
	}
	return n
}

func countFailed(routes []routing.RouteView) int {
	n := 0
	for _, r := range routes {
		if routeFailed(r) {
			n++
		}
	}
	return n
}

func routeFailed(r routing.RouteView) bool {
	if r.Active || !(r.Done || r.Completeness.History) {
		return false
	}
	if r.Status >= 400 || r.Error != "" {
		return true
	}
	if len(r.Tries) > 0 {
		return tryFailed(r.Tries[len(r.Tries)-1])
	}
	return false
}

func countReroutes(r routing.RouteView) int {
	n := 0
	for i, tr := range r.Tries {
		if i+1 >= len(r.Tries) {
			break
		}
		if tr.Rest != nil || tr.Again != 0 {
			n++
		}
	}
	return n
}

func routingStats(routes []routing.RouteView) (requests, rerouted, failed int) {
	for _, r := range routes {
		if r.Time.IsZero() {
			continue
		}
		requests++
		rerouted += countReroutes(r)
		if routeFailed(r) {
			failed++
		}
	}
	return requests, rerouted, failed
}

func (m *Model) detailPane(c *ui.Context, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, ok bool, p palette) {
	loc := m.localization()
	ui.Column(c).Fill().Padding(14, 14, 14, 6).Children(func() {
		if !ok {
			emptyState(c, p, loc.T("routing.selectPrompt"))
			return
		}
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).PaddingEnd(8).Gap(10).Children(func() {
				m.routeHeader(c, sel, p, loc)
				m.traceCard(c, sel, p, loc)
				m.routeFacts(c, sel, p, loc)
			})
		})
	})
}

func (m *Model) routeHeader(c *ui.Context, rv routing.RouteView, p palette, loc Localization) {
	status := statusKey(rv)
	statusColor := p.Success
	if status == "active" {
		statusColor = p.Accent
	} else if status == "failed" {
		statusColor = p.Danger
	}
	ui.Column(c).Gap(8).Padding(13).Radius(11).Background(p.Panel).Border(1, p.LineSoft).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(9, 9).Radius(5).Background(statusColor)
			ui.Text(c, agentName(rv.Agent)+" → "+firstNonEmpty(rv.Model, loc.T("status.noModel"))).Bold().FontSize(13).Grow(1).NoWrap()
			if rv.Active {
				ui.Box(c).Padding(3, 6).Radius(5).Background(p.AccentSoft).Children(func() {
					ui.Text(c, loc.T("app.live")).FontSize(8).TextColor(p.Accent)
				})
			}
			if rv.Completeness.History {
				ui.Box(c).Padding(3, 6).Radius(5).Background(p.PanelMuted).Children(func() {
					ui.Text(c, loc.T("app.completedHistory")).FontSize(8).TextColor(p.Muted)
				})
			}
		})
		ui.Row(c).Gap(12).Children(func() {
			factInline(c, loc.T("app.status"), selected(loc, status), statusColor, p)
			factInline(c, shortID(rv.ID), sessionText(rv.Session), p.Muted, p)
			factInline(c, loc.T("app.provider"), filterDash(rv.DisplayProvider()), p.Text, p)
			factInline(c, loc.T("app.duration"), durationText(routeMillis(rv)), p.Text, p)
			factInline(c, loc.T("app.ttft"), durationText(rv.TTFT), p.Text, p)
			factInline(c, loc.T("app.tokens"), tokenText(rv), p.Text, p)
		})
		pinned := m.isPinned(rv.ID)
		label := loc.T("app.pin")
		if pinned {
			label = loc.T("app.unpin")
		}
		if ui.Button(c.Key("pin"), label).Clicked() {
			m.togglePin(rv.ID)
		}
	})
}

func factInline(c *ui.Context, label, value string, valueColor ui.Color, p palette) {
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, label).FontSize(9).TextColor(p.Faint).NoWrap()
		ui.Text(c, value).FontSize(9).TextColor(valueColor).NoWrap()
	})
}

func (m *Model) traceCard(c *ui.Context, rv routing.RouteView, p palette, loc Localization) {
	ui.Column(c).Gap(8).Padding(13).Radius(11).Background(p.Panel).Border(1, p.LineSoft).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, loc.T("routing.trace")).Bold().FontSize(12)
			ui.Spacer(c)
			ui.Text(c, traceSourceLabel(rv, loc)).FontSize(9).TextColor(p.Muted)
		})
		if !rv.CandidateOrderKnown {
			notice(c, p, loc.T("routing.orderUnknown"))
		}
		if len(rv.Order) > 0 {
			ui.Column(c).Gap(5).Children(func() {
				for i, w := range rv.Order {
					m.candidateRow(c, i, w, rv, p, loc)
				}
			})
		}
		ui.Divider(c)
		if len(rv.Tries) == 0 {
			ui.Text(c, loc.T("routing.noTrace")).FontSize(10).TextColor(p.Muted)
			return
		}
		for i, tr := range rv.Tries {
			m.tryRow(c, i, tr, p, loc)
		}
		if len(rv.Left) > 0 {
			ui.Text(c, loc.T("routing.candidateOrder")+": "+itoa(len(rv.Left))).FontSize(9).TextColor(p.Faint)
		}
	})
}

func (m *Model) candidateRow(c *ui.Context, i int, w routing.Weighed, rv routing.RouteView, p palette, loc Localization) {
	color := p.Text
	note := ""
	if w.Barred {
		color, note = p.Danger, "barred"
	} else if w.Held {
		color, note = p.Warning, "held"
	} else if w.NoCredits {
		color, note = p.Danger, "no credits"
	}
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Size(19, 19).Radius(10).Background(p.PanelMuted).Center().Children(func() {
			ui.Text(c, itoa(i+1)).FontSize(8).TextColor(p.Muted)
		})
		ui.Column(c).Grow(1).Gap(1).Children(func() {
			ui.Text(c, firstNonEmpty(w.Name, w.Provider, w.ID)).FontSize(10).TextColor(color).NoWrap()
			secondary := firstNonEmpty(w.Model, w.Plan, loc.T("app.none"))
			if w.Who != "" {
				who := w.Who
				if m.accountsMasked() {
					who = maskWho(who)
				}
				secondary += " · " + who
			}
			ui.Text(c, secondary).FontSize(8).TextColor(p.Faint).NoWrap()
		})
		if note != "" {
			ui.Text(c, note).FontSize(8).TextColor(color)
		} else if w.Used > 0 {
			ui.Text(c, trimFixed(w.Used, 0)+"%").FontSize(8).TextColor(p.Muted)
		}
	})
}

func (m *Model) tryRow(c *ui.Context, i int, tr routing.Try, p palette, loc Localization) {
	iconColor := p.Success
	fail := routing.TryFailure(tr)
	if fail != "" || tr.Status >= 400 {
		iconColor = p.Danger
	} else if !tr.Done {
		iconColor = p.Accent
	}
	ui.Row(c).Gap(9).AlignItems(ui.Start).Children(func() {
		ui.Column(c).AlignItems(ui.Center).Gap(2).Children(func() {
			ui.Box(c).Size(17, 17).Radius(9).Background(iconColor.Alpha(0.16)).Draw(func(pt *ui.Painter, r ui.Rect) {
				if fail != "" || tr.Status >= 400 {
					pt.Line(r.X+5, r.Y+5, r.X+r.W-5, r.Y+r.H-5, 1.5, iconColor)
					pt.Line(r.X+r.W-5, r.Y+5, r.X+5, r.Y+r.H-5, 1.5, iconColor)
				} else {
					pt.Line(r.X+4, r.Y+9, r.X+7, r.Y+12, 1.5, iconColor)
					pt.Line(r.X+7, r.Y+12, r.X+13, r.Y+5, 1.5, iconColor)
				}
			})
			if i < 5 {
				ui.Box(c).Width(1).Height(16).Background(p.LineSoft)
			}
		})
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, loc.T("routing.attempt")+" "+itoa(i+1)).FontSize(9).TextColor(p.Muted).NoWrap()
				ui.Text(c, firstNonEmpty(tr.Model, tr.ID)).FontSize(10).Bold().NoWrap()
				if tr.Status > 0 {
					ui.Text(c, "HTTP "+itoa(tr.Status)).FontSize(8).TextColor(iconColor)
				}
				ui.Spacer(c)
				ui.Text(c, durationText(tr.Millis)).FontSize(9).TextColor(p.Muted)
			})
			if fail != "" {
				ui.Text(c, loc.T("routing.reason")+": "+fail).FontSize(9).TextColor(p.Danger)
			}
			meta := []string{}
			if tr.Fast {
				meta = append(meta, "fast")
			}
			if tr.Swapped {
				meta = append(meta, "swapped")
			}
			if tr.Tier != "" {
				meta = append(meta, tr.Tier)
			}
			if tr.Upstream != "" {
				meta = append(meta, tr.Upstream)
			}
			if tr.TTFT > 0 {
				meta = append(meta, loc.T("app.ttft")+": "+durationText(tr.TTFT))
			}
			if len(meta) > 0 {
				ui.Text(c, join(", ", meta)).FontSize(8).TextColor(p.Faint)
			}
		})
	})
}

func (m *Model) routeFacts(c *ui.Context, rv routing.RouteView, p palette, loc Localization) {
	ui.Column(c).Gap(8).Padding(13).Radius(11).Background(p.Panel).Border(1, p.LineSoft).Children(func() {
		ui.Text(c, loc.T("settings.diagnostics")).Bold().FontSize(11)
		rows := [][2]string{
			{loc.T("app.started"), fullTime(rv.Time)},
			{loc.T("app.session"), filterDash(rv.Session)},
			{loc.T("app.group"), groupName(rv, loc)},
			{loc.T("routing.phase"), phaseText(rv, loc)},
			{loc.T("routing.staticConfig"), sourceText(rv, loc)},
		}
		for _, row := range rows {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, row[0]).Width(96).FontSize(9).TextColor(p.Faint).NoWrap()
				ui.Text(c, row[1]).Grow(1).FontSize(9).TextColor(p.Text)
			})
		}
		if rv.Error != "" {
			ui.Row(c).Gap(8).Children(func() {
				ui.Text(c, loc.T("status.error")).Width(96).FontSize(9).TextColor(p.Faint).NoWrap()
				ui.Text(c, rv.Error).Grow(1).FontSize(9).TextColor(p.Danger)
			})
		}
	})
}

func (m *Model) historyView(c *ui.Context, s routing.Snapshot, p palette, now time.Time) {
	routes := m.visibleRoutes(s, true)
	m.prepareListSelection(routes)
	sel, ok := m.selectedRoute(routes)
	loc := m.localization()
	ui.Box(c).HeightPercent(100).Children(func() {
		ui.SplitPercent(c, &m.splitPercent,
			func() { m.historyPane(c, s, routes, sel, ok, p, now) },
			func() { m.detailPane(c, s, routes, sel, ok, p) },
		).Fill()
	})
	_ = loc
}

func (m *Model) historyPane(c *ui.Context, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, ok bool, p palette, now time.Time) {
	loc := m.localization()
	ui.Column(c).Fill().Padding(14).Gap(10).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Text(c, loc.T("history.day")).Bold().FontSize(11)
			for _, d := range m.historyDays(s) {
				day := d.Day
				current := m.currentHistoryDay()
				label := day
				if len(label) == 10 {
					label = label[5:]
				}
				if d.Requests > 0 {
					label += " · " + itoa(d.Requests)
				}
				btn := ui.Button(c.Key("day-"+day), label)
				btn.Selected(day == current)
				if btn.Clicked() {
					m.SetHistoryDay(day)
					m.mu.Lock()
					m.syncListSel = true
					m.mu.Unlock()
					if m.actions.OpenDay != nil {
						go m.actions.OpenDay(day)
					}
				}
			}
			ui.Spacer(c)
			if ui.Button(c.Key("reload-day"), loc.T("history.openDay")).Clicked() {
				day := m.currentHistoryDay()
				if m.actions.ReloadDay != nil {
					go m.actions.ReloadDay(day)
				} else if m.actions.OpenDay != nil {
					go m.actions.OpenDay(day)
				}
			}
		})
		m.replayBar(c, s, routes, p, now)
		if s.HistoryError != "" {
			notice(c, p, loc.T("groups.configError")+": "+s.HistoryError)
		}
		m.requestList(c, s, routes, p)
		_ = sel
		_ = ok
	})
}

func (m *Model) replayBar(c *ui.Context, s routing.Snapshot, routes []routing.RouteView, p palette, now time.Time) {
	loc := m.localization()
	tl, playing, speed, position, replayState := m.replayState(now)
	if tl.Duration <= 0 && len(tl.Routes) == 0 {
		ui.Box(c).Padding(10, 12).Radius(9).Background(p.Panel).Border(1, p.LineSoft).Children(func() {
			ui.Text(c, loc.T("routing.emptyHistory")).FontSize(10).TextColor(p.Muted)
		})
		return
	}
	playLabel := loc.T("history.play")
	if playing {
		playLabel = loc.T("history.pause")
	}
	ui.Column(c).Gap(7).Padding(10, 12).Radius(10).Background(p.Panel).Border(1, p.LineSoft).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Padding(3, 6).Radius(5).Background(p.AccentSoft).Children(func() {
				ui.Text(c, loc.T("app.replay")).FontSize(8).TextColor(p.Accent)
			})
			ui.Text(c, loc.T("history.replayWarning")).FontSize(9).TextColor(p.Muted).Grow(1)
			ui.Text(c, "×"+speedText(speed)).FontSize(10).TextColor(p.Text)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.PrimaryButton(c.Key("play"), playLabel).Clicked() {
				m.toggleReplay(now)
				c.Invalidate()
			}
			if ui.Button(c.Key("restart"), loc.T("history.replay")).Clicked() {
				m.restartReplay(now)
				c.Invalidate()
			}
			secs := position.Seconds()
			max := tl.Duration.Seconds()
			if max <= 0 {
				max = 1
			}
			if ui.Slider(c.Key("seek"), &secs, 0, max).Changed() {
				m.seekReplay(secs)
			}
			ui.Text(c, replayClock(position)+" / "+replayClock(tl.Duration)).FontSize(9).TextColor(p.Muted).NoWrap()
			if ui.Button(c.Key("speed"), "×"+speedText(speed)).Clicked() {
				m.cycleSpeed()
			}
		})
		if playing {
			c.AnimationFrame()
		}
		_ = replayState
		_ = routes
	})
}

func (m *Model) groupsView(c *ui.Context, s routing.Snapshot, p palette) {
	loc := m.localization()
	groups := routing.GroupViews(s.Config, s.Routes)
	ui.Box(c).Fill().Background(p.Canvas).Children(func() {
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).WidthPercent(100).MaxWidth(1200).AlignSelf(ui.Center).Padding(14, 20, 24, 20).Gap(10).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, "Routing").Bold().FontSize(13)
					ui.Text(c, loc.T("groups.subtitle")).FontSize(11).TextColor(p.Muted)
					ui.Spacer(c)
					ui.Box(c).Padding(4, 8).Radius(6).Background(p.PanelMuted).Children(func() {
						ui.Text(c, loc.T("groups.readOnly")).FontSize(10).TextColor(p.Muted)
					})
				})
				if s.ConfigError != "" {
					notice(c, p, loc.T("groups.configError")+": "+s.ConfigError)
				}
				if m.GroupNotice() != "" {
					notice(c, p, m.GroupNotice())
				}
				ui.SplitPercent(c, &m.splitPercent,
					func() { m.groupList(c, groups, p) },
					func() { m.groupDetail(c, groups, p) },
				).Height(560)
			})
		})
	})
}

func (m *Model) groupList(c *ui.Context, groups []routing.GroupView, p palette) {
	loc := m.localization()
	ui.Column(c).Fill().PaddingEnd(10).Children(func() {
		if len(groups) == 0 {
			emptyState(c, p, loc.T("groups.noGroups"))
			return
		}
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).Gap(7).Children(func() {
				for _, g := range groups {
					g := g
					row := ui.Column(c).Padding(9, 11).Radius(8).Background(p.Panel).Border(1, p.LineSoft).Gap(3).Tooltip(g.ID)
					row.Children(func() {
						ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
							ui.Text(c, g.Name).Bold().FontSize(11).Grow(1).NoWrap()
							kind := loc.T("groups.manual")
							if g.Auto {
								kind = loc.T("groups.auto")
							}
							if g.ObservedOnly {
								kind = loc.T("groups.observedOnly")
							}
							ui.Text(c, kind).FontSize(8).TextColor(p.Muted).NoWrap()
						})
						ui.Text(c, itoa(len(g.Members))+" "+loc.T("groups.membersCount")+" · "+itoa(g.Observed)+" "+loc.T("groups.observedRequests")).FontSize(8).TextColor(p.Faint)
					})
					row.OnClick(func() {
						m.mu.Lock()
						m.groupID = g.ID
						m.mu.Unlock()
					})
				}
			})
		})
	})
}

func (m *Model) groupDetail(c *ui.Context, groups []routing.GroupView, p palette) {
	loc := m.localization()
	m.mu.RLock()
	id := m.groupID
	m.mu.RUnlock()
	var g routing.GroupView
	var ok bool
	for _, x := range groups {
		if x.ID == id {
			g, ok = x, true
			break
		}
	}
	if !ok && len(groups) > 0 {
		g, ok = groups[0], true
	}
	if !ok {
		emptyState(c, p, loc.T("groups.noGroups"))
		return
	}
	ui.Column(c).Fill().PaddingStart(10).Children(func() {
		ui.Scroll(c).Fill().Children(func() {
			ui.Column(c).PaddingEnd(8).Gap(10).Children(func() {
				ui.Column(c).Padding(13).Radius(10).Background(p.Panel).Border(1, p.LineSoft).Gap(5).Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						ui.Text(c, g.Name).Bold().FontSize(15).Grow(1)
						ui.Text(c, g.ID).FontSize(9).TextColor(p.Faint)
					})
					ui.Text(c, memberText(g, loc)).FontSize(10).TextColor(p.Muted)
					ui.Text(c, loc.T("groups.unsupportedHint")).FontSize(9).TextColor(p.Warning)
					ui.Row(c).Gap(6).Children(func() {
						ui.Button(c.Key("group-edit"), loc.T("groups.edit")).Disabled(true)
						ui.Button(c.Key("group-delete"), loc.T("groups.delete")).Disabled(true)
					})
				})
				ui.Column(c).Padding(13).Radius(10).Background(p.Panel).Border(1, p.LineSoft).Gap(7).Children(func() {
					text := loc.T("groups.memberOrder")
					ui.Text(c, text).Bold().FontSize(11)
					if len(g.Members) == 0 {
						ui.Text(c, loc.T("groups.noMembers")).FontSize(9).TextColor(p.Muted)
					}
					for i, member := range g.Members {
						ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
							ui.Box(c).Size(18, 18).Radius(9).Background(p.AccentSoft).Center().Children(func() {
								ui.Text(c, itoa(i+1)).FontSize(8).TextColor(p.Accent)
							})
							ui.Text(c, member).FontSize(10)
						})
					}
					ui.Divider(c)
					factLine(c, loc.T("groups.routing"), filterDash(g.Routing), p)
					factLine(c, loc.T("groups.affinity"), filterDash(g.Affinity), p)
					factLine(c, loc.T("groups.match"), filterDash(join(", ", g.Match)), p)
					factLine(c, loc.T("groups.matched"), filterDash(join(", ", g.Matched)), p)
					factLine(c, loc.T("groups.pick"), filterDash(g.Pick), p)
					factLine(c, loc.T("groups.off"), filterDash(join(", ", g.Off)), p)
					factLine(c, loc.T("groups.fast"), filterDash(join(", ", g.Fast)), p)
					factLine(c, loc.T("groups.levels"), filterDash(join(", ", g.Levels)), p)
					factLine(c, loc.T("groups.observedRequests"), itoa(g.Observed), p)
					factLine(c, loc.T("groups.configError"), boolText(g.Configured, loc.T("app.live"), loc.T("groups.observedOnly")), p)
				})
			})
		})
	})
}

func (m *Model) settingsModal(c *ui.Context, p palette) {
	open, errText := m.settingsState()
	if !open {
		return
	}
	loc := m.localization()
	ui.Modal(c, &open, func() {
		ui.Column(c).Width(560).Background(p.PanelRaised).Radius(12).Border(1, p.Line).Clip().Children(func() {
			ui.Row(c).Padding(14, 16).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, loc.T("settings.title")).Bold().FontSize(15).Grow(1)
				if ui.Button(c.Key("settings-close"), loc.T("app.close")).Clicked() {
					m.setSettingsOpen(false)
				}
			})
			ui.Divider(c)
			ui.Column(c).Padding(16, 16).Gap(10).Children(func() {
				sectionTitle(c, loc.T("settings.gateway"), loc.T("settings.gatewayHint"), p)
				field(c, loc.T("gateway.url"), func() { ui.TextInput(c, &m.draftURL) })
				ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
					field(c, loc.T("settings.apiKey"), func() { ui.TextInput(c, &m.draftKey).Password() })
					if ui.Button(c.Key("clear-key"), "×").Tooltip(loc.T("settings.noApiKey")).Clicked() {
						m.draftKey = ""
					}
				})
				sectionTitle(c, loc.T("settings.dataDir"), loc.T("settings.detected")+" · "+m.Paths().Detection, p)
				ui.Row(c).Gap(6).Children(func() {
					ui.TextInput(c, &m.draftDataDir).Grow(1)
					if ui.Button(c.Key("choose-dir"), loc.T("settings.chooseDir")).Clicked() && m.actions.ChooseDataDir != nil {
						go m.actions.ChooseDataDir()
					}
				})
				ui.Row(c).Gap(8).Children(func() {
					field(c, loc.T("settings.appConfig"), func() {
						ui.Text(c, m.Paths().AppConfig).FontSize(9).TextColor(p.Muted).Selectable()
					})
					field(c, loc.T("settings.appCache"), func() {
						ui.Text(c, m.Paths().AppCache).FontSize(9).TextColor(p.Muted).Selectable()
					})
				})
				sectionTitle(c, loc.T("app.theme"), "", p)
				themeLabels := []string{loc.T("app.theme.system"), loc.T("app.theme.light"), loc.T("app.theme.dark")}
				themeIdx := 0
				switch m.draftTheme {
				case config.ThemeLight:
					themeIdx = 1
				case config.ThemeDark:
					themeIdx = 2
				}
				if ui.Segmented(c, &themeIdx, themeLabels...).Changed() {
					m.draftTheme = []config.ThemeMode{config.ThemeSystem, config.ThemeLight, config.ThemeDark}[themeIdx]
					if m.actions.ApplyTheme != nil {
						m.actions.ApplyTheme(m.draftTheme)
					}
				}
				langLabels := []string{"中文", "English"}
				langIdx := 0
				if m.draftLang == "en" {
					langIdx = 1
				}
				if ui.Segmented(c, &langIdx, langLabels...).Changed() {
					m.draftLang = []string{"zh", "en"}[langIdx]
				}
				if errText != "" {
					notice(c, p, errText)
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, loc.T("settings.restartHint")).FontSize(9).TextColor(p.Muted).Grow(1)
					if ui.Button(c.Key("settings-cancel"), loc.T("app.cancel")).Clicked() {
						m.setSettingsOpen(false)
					}
					if ui.PrimaryButton(c.Key("settings-save"), loc.T("app.save")).Clicked() {
						cfg := m.Settings()
						cfg.GatewayURL = m.draftURL
						cfg.APIKey = m.draftKey
						cfg.MagpieDir = m.draftDataDir
						cfg.Theme = m.draftTheme
						cfg.Language = m.draftLang
						if err := cfg.Validate(); err != nil {
							m.setSettingsError(err)
						} else if m.actions.SaveSettings != nil {
							if err := m.actions.SaveSettings(cfg); err != nil {
								m.setSettingsError(err)
							} else {
								m.setSettingsError(nil)
								m.setSettingsOpen(false)
							}
						}
					}
				})
			})
		})
	})
	m.setSettingsOpen(open)
}

func sectionTitle(c *ui.Context, title, detail string, p palette) {
	ui.Column(c).Gap(1).Children(func() {
		ui.Text(c, title).Bold().FontSize(11)
		if detail != "" {
			ui.Text(c, detail).FontSize(9).TextColor(p.Muted)
		}
	})
}

func field(c *ui.Context, label string, build func()) {
	ui.Column(c).Grow(1).Gap(3).Children(func() {
		ui.Text(c, label).FontSize(9)
		build()
	})
}

func notice(c *ui.Context, p palette, text string) {
	ui.Row(c).Padding(7, 9).Radius(6).Background(p.WarningSoft).Children(func() {
		ui.Text(c, text).FontSize(9).TextColor(p.Warning)
	})
}

func emptyState(c *ui.Context, p palette, text string) {
	ui.Box(c).Fill().Center().Padding(24).Children(func() {
		ui.Column(c).Gap(5).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(34, 34).Radius(17).Background(p.PanelMuted).Draw(func(pt *ui.Painter, r ui.Rect) {
				drawMagpieMark(pt, r, p.Muted, p.Panel)
			})
			ui.Text(c, text).FontSize(10).TextColor(p.Muted).TextAlign(ui.Center)
		})
	})
}

func factLine(c *ui.Context, label, value string, p palette) {
	ui.Row(c).Gap(8).Children(func() {
		ui.Text(c, label).Width(100).FontSize(9).TextColor(p.Faint).NoWrap()
		ui.Text(c, value).Grow(1).FontSize(9).TextColor(p.Text)
	})
}

func filterDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func routeLabel(r routing.RouteView) string {
	return shortID(r.ID) + " " + r.Agent + " " + r.Model
}

func routeMillis(r routing.RouteView) int64 {
	if r.Millis > 0 {
		return r.Millis
	}
	if len(r.Live) > 0 {
		var total int64
		for _, l := range r.Live {
			total = max(total, elapsedLive(l))
		}
		return total
	}
	return 0
}

func tokenText(r routing.RouteView) string {
	if r.Tokens > 0 {
		return itoa(r.Tokens)
	}
	var total int
	for _, u := range r.Usage {
		total += u.Input + u.Output + u.CacheRead + u.CacheWrite
	}
	if total > 0 {
		return itoa(total)
	}
	return "—"
}

func traceSourceLabel(r routing.RouteView, loc Localization) string {
	switch {
	case r.Completeness.Live && r.Completeness.History:
		return loc.T("routing.mixedSource")
	case r.Completeness.History:
		return loc.T("routing.historyOnly")
	case r.Completeness.SessionTrace:
		return loc.T("routing.sessionTrace")
	default:
		return loc.T("routing.liveOnly")
	}
}

func sourceText(r routing.RouteView, loc Localization) string {
	labels := make([]string, 0, 3)
	if r.Completeness.Live {
		labels = append(labels, loc.T("app.live"))
	}
	if r.Completeness.SessionTrace {
		labels = append(labels, loc.T("routing.sessionTrace"))
	}
	if r.Completeness.History {
		labels = append(labels, loc.T("app.completedHistory"))
	}
	if len(labels) == 0 {
		return loc.T("app.none")
	}
	return join(" · ", labels)
}

func phaseText(r routing.RouteView, loc Localization) string {
	if len(r.Live) == 0 {
		return "—"
	}
	parts := make([]string, 0, len(r.Live))
	for _, l := range r.Live {
		if l.Phase == "" {
			parts = append(parts, loc.T("phase.observed"))
		} else {
			parts = append(parts, loc.T("phase."+string(l.Phase)))
		}
	}
	return join(", ", parts)
}

func groupName(r routing.RouteView, loc Localization) string {
	if r.Group == nil || r.Group.ID == "" {
		return "—"
	}
	if r.Group.Name != "" {
		return r.Group.Name
	}
	return r.Group.ID
}

func memberText(g routing.GroupView, loc Localization) string {
	return itoa(len(g.Members)) + " " + loc.T("groups.membersCount")
}

func boolText(v bool, yes, no string) string {
	if v {
		return yes
	}
	return no
}

func fullTime(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	return t.Local().Format("2006-01-02 15:04:05.000")
}

func replayClock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	ms := d.Milliseconds()
	min := ms / 60_000
	sec := (ms / 1000) % 60
	frac := ms % 1000
	return itoa(int(min)) + ":" + pad2(int(sec)) + "." + pad3(int(frac))
}

func speedText(v float64) string {
	if v == float64(int(v)) {
		return itoa(int(v))
	}
	return trimFixed(v, 1)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func pad3(n int) string {
	if n < 10 {
		return "00" + itoa(n)
	}
	if n < 100 {
		return "0" + itoa(n)
	}
	return itoa(n)
}

func timeText(t time.Time, loc Localization) string {
	if t.IsZero() {
		return loc.T("status.loading")
	}
	return clockText(t)
}

func join(sep string, values []string) string {
	if len(values) == 0 {
		return ""
	}
	out := values[0]
	for _, v := range values[1:] {
		out += sep + v
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
