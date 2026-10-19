package ui

import (
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
			// Only the native title-bar strip moves the window. Dragging
			// the whole view stole clicks and drags from lists, sliders and
			// text selection below it.
			ui.Box(c).Height(c.TitleBar().Height).DragWindow()
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
				m.routingRequestSection(c, s, routes, sel, ok, p, history, windowWidth)
				m.routingGroupsSection(c, s, p)
			})
		})
	})
}

func (m *Model) routingPageHead(c *ui.Context, s routing.Snapshot, p palette, history bool) {
	loc := m.localization()
	note := loc.T("routing.liveNote")
	if history {
		note = loc.T("routing.historyNote")
	}
	ui.Column(c).Gap(6).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(p.Success)
			ui.Text(c, "Routing").Bold().FontSize(13)
			ui.Text(c, note).FontSize(11).TextColor(p.Muted)
			ui.Spacer(c)
			m.connectionPill(c, s, p, loc)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
			ui.Spacer(c)
			if ui.Button(c.Key("refresh"), loc.T("app.refresh")).Clicked() {
				if m.actions.Refresh != nil {
					go m.actions.Refresh()
				}
				if m.actions.RefreshHistory != nil {
					go m.actions.RefreshHistory()
				}
			}
			mask := loc.T("app.hideAccounts")
			if m.accountsMasked() {
				mask = loc.T("app.showAccounts")
			}
			if ui.Button(c.Key("mask"), mask).Clicked() {
				m.toggleAccountMask()
			}
			if ui.Button(c.Key("settings"), loc.T("app.settings")).Clicked() {
				m.setSettingsOpen(true)
			}
		})
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
	stageWidth := float32(0)
	if w, _ := c.Size(); w > 0 {
		// The stage sits inside 20px page padding and 16px card padding.
		// This is the .rt card's own width, which is what client.js's
		// ResizeObserver uses for its max560 layout, not the window's.
		stageWidth = min(w, 1200) - 40 - 32
	}
	stageHeight := m.routingStageHeightForWidth(stageWidth, s, routes, sel, ok, now)
	ui.Column(c).Radius(14).Background(p.Panel).Border(1, p.Line).Clip().Children(func() {
		// The original Routing card opens with a two-line explanation of
		// the actual routing mode, never with a dashboard title.
		ui.Column(c).Padding(14, 16, 10, 16).Gap(3).Children(func() {
			ui.Row(c).Gap(0).AlignItems(ui.Center).Wrap().Children(func() {
				ui.Text(c, title).Bold().FontSize(13).TextColor(p.Text)
				if ok && sel.Group != nil {
					ui.Text(c, " · "+loc.T("app.group")+" · "+itoa(len(sel.Order))+" "+loc.T("routing.membersOn")).FontSize(11).TextColor(p.Muted)
				}
			})
			ui.Text(c, detail).FontSize(12).TextColor(p.Muted)
		})
		ui.Box(c).Height(stageHeight).Padding(8, 16).Draw(func(pt *ui.Painter, r ui.Rect) {
			m.drawRoutingStageCanvas(pt, r, s, routes, sel, ok, p, now, history)
		})
		if ok {
			m.routingStory(c, sel, p, loc)
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

func (m *Model) routingStory(c *ui.Context, rv routing.RouteView, p palette, loc Localization) {
	fail := finalRouteFailure(rv)
	ui.Column(c).Gap(7).Padding(10, 16).BorderWidth(1, 0, 0, 0).BorderColor(p.Line).Children(func() {
		ui.Row(c).Gap(16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, firstNonEmpty(fail, routingStoryText(rv, loc))).FontSize(13).TextColor(boolColor(fail != "", p.Danger, p.Text)).Grow(1)
			// A request is one route; tries are attempts within it. The
			// previous count of len(Tries) made a single rerouted request
			// look like two separate requests, unlike renderStats().
			requests := 0
			if !rv.Time.IsZero() {
				requests = 1
			}
			rerouted := countReroutes(rv)
			metricStack(c, loc.T("app.requests"), itoa(requests), p)
			metricStack(c, loc.T("routing.rerouted"), itoa(rerouted), p)
			metricStack(c, loc.T("app.failed"), boolText(routeFailed(rv), "1", "0"), p)
		})
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, routingTracePhrase(rv, loc)).FontSize(11).TextColor(p.Muted)
			ui.Text(c, durationText(routeMillis(rv))).FontSize(11).TextColor(p.Muted)
			ui.Text(c, loc.T("app.ttft")+" "+durationText(rv.TTFT)).FontSize(11).TextColor(p.Muted)
			ui.Text(c, tokenText(rv)+" "+loc.T("app.tokens")).FontSize(11).TextColor(p.Muted)
			ui.Spacer(c)
			if ui.Button(c, loc.T("app.replay")).Clicked() && rv.HistoryDay != "" {
				m.PlayRoute(rv.HistoryDay, rv.ID)
				if m.actions.OpenDay != nil {
					go m.actions.OpenDay(rv.HistoryDay)
				}
			}
		})
		// The original "how the last request was routed" is a sentence
		// list, not a dashboard table. Every line is derived only from the
		// recorded Order/Tries of this exact route.
		for i, tr := range rv.Tries {
			line := loc.F("routing.tryStory", loc.T("routing.attempt")+" "+itoa(i+1)+" · "+agentName(rv.Agent), tryWho(tr, m.accountsMasked()), tryStatusText(tr), durationText(tr.Millis))
			if reason := routing.TryFailure(tr); reason != "" {
				line += " · " + reason
			}
			dot := p.Success
			if tryFailed(tr) {
				dot = p.Danger
			} else if !tr.Done {
				dot = p.Text
			}
			ui.Row(c).Gap(8).AlignItems(ui.Start).Children(func() {
				ui.Box(c).Size(6, 6).Radius(3).Background(dot)
				ui.Text(c, line).FontSize(11).TextColor(boolColor(tryFailed(tr), p.Danger, p.Muted)).Grow(1)
				if i == len(rv.Tries)-1 {
					ui.Text(c, loc.T("routing.answer")).FontSize(10).TextColor(p.Faint)
				}
			})
		}
	})
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

func tryWho(tr routing.Try, masked bool) string {
	who := firstNonEmpty(tr.ID, providerOf(tr.Model), tr.Model)
	if masked && who != "" {
		who = maskWho(who)
	}
	return who
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
				ui.Text(c, g.Routing).FontSize(10.5).TextColor(p.Muted).NoWrap()
				ui.Spacer(c)
				ui.Text(c, itoa(len(g.Members))+" "+loc.T("groups.membersCount")).FontSize(10.5).TextColor(p.Muted).NoWrap()
				ui.Text(c, itoa(g.Observed)+" "+loc.T("groups.observedRequests")).FontSize(10.5).TextColor(p.Faint).NoWrap()
			})
		}
	})
}

func routingStoryText(rv routing.RouteView, loc Localization) string {
	if rv.Group != nil && rv.Group.ID != "" {
		return loc.F("routing.groupStory", agentName(rv.Agent), firstNonEmpty(rv.Group.Name, rv.Group.ID))
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

func (m *Model) routingRequestSection(c *ui.Context, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, ok bool, p palette, history bool, windowWidth float32) {
	loc := m.localization()
	twoColumn := windowWidth >= 1120
	contentWidth := min(windowWidth, 1200) - 40
	compactFilters := contentWidth < 900
	listWidth := float32(100)
	accountWidth := float32(100)
	if twoColumn {
		listWidth, accountWidth = 68, 31
	}
	requests, rerouted, failed := routingStats(routes)
	addFilters := func() {
		m.dayChips(c, s, p, history)
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
	}
	ui.Column(c).Gap(7).Children(func() {
		summary := func(showSource bool) {
			ui.Text(c, loc.T("routing.requests")).Bold().FontSize(12)
			ui.Text(c, itoa(len(routes))).FontSize(9).TextColor(p.Muted)
			if showSource && ok {
				ui.Text(c, traceSourceLabel(sel, loc)).FontSize(9).TextColor(p.Muted).NoWrap()
			}
		}
		stats := loc.T("app.requests") + " " + itoa(requests) + " · " +
			loc.T("routing.rerouted") + " " + itoa(rerouted) + " · " +
			loc.T("app.failed") + " " + itoa(failed)
		if compactFilters {
			ui.Column(c).Gap(6).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					summary(false)
					ui.Spacer(c)
					ui.Text(c, stats).FontSize(9).TextColor(p.Muted).NoWrap()
				})
				if ok {
					ui.Text(c, traceSourceLabel(sel, loc)).FontSize(9).TextColor(p.Muted).NoWrap()
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Wrap().Children(func() {
					m.dayChips(c, s, p, history)
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
			})
		} else {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				summary(true)
				ui.Spacer(c)
				ui.Text(c, stats).FontSize(9).TextColor(p.Muted).NoWrap()
				addFilters()
			})
		}
		ui.Row(c).Gap(10).AlignItems(ui.Start).Children(func() {
			m.routingRequestList(c, routes, p, listWidth)
			if twoColumn {
				m.routingAccountColumn(c, sel, ok, p, accountWidth)
			}
		})
		if !twoColumn {
			m.routingAccountColumn(c, sel, ok, p, 100)
		}
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

func (m *Model) routingRequestList(c *ui.Context, routes []routing.RouteView, p palette, widthPercent float32) {
	loc := m.localization()
	if len(routes) == 0 {
		ui.Box(c).WidthPercent(widthPercent).Height(140).Radius(10).Background(p.Panel).Border(1, p.Line).Center().Children(func() {
			ui.Text(c, loc.T("status.empty")).FontSize(10).TextColor(p.Muted)
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
		m.routingRequestRow(c, routes[i], i == sel, p)
	}).WidthPercent(widthPercent).Height(360)
	if list.Changed() {
		m.commitListSelection(routes)
	}
}

func (m *Model) routingRequestRow(c *ui.Context, rv routing.RouteView, selected bool, p palette) {
	loc := m.localization()
	status := statusKey(rv)
	color := p.Success
	if status == "active" {
		color = p.Accent
	} else if status == "failed" {
		color = p.Danger
	}
	row := ui.Row(c).Padding(6, 8).Gap(12).AlignItems(ui.Center).Radius(8)
	if selected {
		row.Background(p.PanelMuted).Border(1, p.Line)
	}
	row.Children(func() {
		ui.Text(c, clockText(rv.Time)).Width(52).FontSize(12).TextColor(p.Muted).NoWrap()
		ui.Box(c).Size(8, 8).Radius(4).Background(agentHue(rv.Agent))
		ui.Row(c).Grow(3).MinWidth(180).Gap(6).AlignItems(ui.Center).Children(func() {
			drawRoutingIconWidget(c, m, []string{agentIcon(rv.Agent), rv.Agent}, 14, p.Text)
			ui.Text(c, firstNonEmpty(routeGroupName(rv), rv.Model)).FontSize(12).TextColor(p.Text).NoWrap().Grow(1)
			if rv.Group != nil {
				ui.Box(c).Padding(0, 5).Radius(4).Background(p.PanelMuted).Children(func() {
					ui.Text(c, loc.T("app.group")).FontSize(10).TextColor(p.Muted).NoWrap()
				})
			}
		})
		ui.Row(c).Grow(4).MinWidth(140).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(6, 6).Radius(3).Background(color)
			ui.Text(c, providerAccountText(rv, m.accountsMasked())).FontSize(11).TextColor(p.Text).NoWrap().Grow(1)
			ui.Text(c, statusMetric(rv, loc)).FontSize(10.5).TextColor(color).NoWrap()
		})
		ui.Spacer(c)
		ui.Text(c, durationText(routeMillis(rv))+" · "+loc.T("app.ttft")+" "+durationText(rv.TTFT)+" · "+tokenText(rv)+" "+loc.T("app.tokens")).FontSize(10.5).TextColor(p.Muted).NoWrap()
	})
}

func drawRoutingIconWidget(c *ui.Context, m *Model, keys []string, size float32, ink ui.Color) {
	ui.Box(c).Size(size, size).Draw(func(p *ui.Painter, r ui.Rect) {
		if !drawRoutingIcon(p, r, keys, ink) {
			drawGenericIcon(p, r, ink.Alpha(0.7))
		}
	})
}

func (m *Model) routingAccountColumn(c *ui.Context, rv routing.RouteView, ok bool, p palette, widthPercent float32) {
	loc := m.localization()
	ui.Column(c).WidthPercent(widthPercent).Gap(6).Children(func() {
		ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
			ui.Text(c, loc.T("app.accounts")).Bold().FontSize(12)
			ui.Text(c, loc.T("routing.readOnly")).FontSize(10).TextColor(p.Faint)
		})
		if !ok {
			ui.Text(c, loc.T("routing.selectPrompt")).FontSize(9).TextColor(p.Muted)
			return
		}
		rows := providerRows(rv, m.accountsMasked())
		if len(rows) == 0 {
			ui.Text(c, loc.T("status.noProvider")).FontSize(9).TextColor(p.Muted)
		}
		for _, row := range rows {
			ui.Column(c).Padding(8, 10).Radius(10).Background(p.Panel).Border(1, p.Line).Gap(4).Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Box(c).Size(8, 8).Radius(4).Background(boolColor(row.Failed, p.Danger, boolColor(row.Current, p.Text, p.Faint)))
					drawRoutingIconWidget(c, m, []string{row.Icon, row.Provider}, 16, p.Muted)
					ui.Text(c, firstNonEmpty(row.Name, row.Provider)).FontSize(12).Bold().NoWrap()
					ui.Spacer(c)
					ui.Text(c, filterDash(row.Status)).FontSize(10.5).TextColor(boolColor(row.Failed, p.Danger, p.Muted))
				})
				ui.Text(c, firstNonEmpty(row.Who, row.Model, row.Plan)).FontSize(10.5).TextColor(p.Muted).NoWrap()
			})
		}
	})
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
			ui.Text(c, trimFixed(w.Used*100, 0)+"%").FontSize(8).TextColor(p.Muted)
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
