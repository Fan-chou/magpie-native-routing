package ui

import (
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/Fan-chou/magpie-native-routing/internal/routing"
)

type topologyFrameLayout struct {
	width, height float32
	agents        []nodeBox
	gateway       nodeBox
	providers     []nodeBox
}

type nodeBox struct {
	x, y, w, h float32
}

func (b nodeBox) rect() ui.Rect { return ui.Rect{X: b.x, Y: b.y, W: b.w, H: b.h} }
func (b nodeBox) rightCenter() (float32, float32) {
	return b.x + b.w, b.y + b.h/2
}
func (b nodeBox) leftCenter() (float32, float32) {
	return b.x, b.y + b.h/2
}

func point(x, y float32) [2]float32 { return [2]float32{x, y} }

func (m *Model) trackTopology(s routing.Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.topology.agentFlight == nil {
		m.topology.agentFlight = map[int64]*flightState{}
	}
	now := s.LastPoll
	if now.IsZero() {
		now = time.Now()
	}
	active := map[int64]bool{}
	for _, l := range s.Live {
		if l.RouteID > 0 {
			active[l.RouteID] = true
		}
	}
	for id, f := range m.topology.agentFlight {
		if !active[id] {
			f.done = true
			continue
		}
		if now.Sub(f.legAt) > 1400*time.Millisecond {
			f.leg = (f.leg + 1) % 2
			f.legAt = now
		}
	}
	for id := range active {
		if _, ok := m.topology.agentFlight[id]; !ok {
			m.topology.agentFlight[id] = &flightState{started: now, legAt: now}
		}
	}
	m.topology.lastVersion = s.Version
}

func (m *Model) drawTopology(p *ui.Painter, r ui.Rect, s routing.Snapshot, routes []routing.RouteView, sel routing.RouteView, hasSel bool, pal palette, now time.Time) {
	if r.W < 80 || r.H < 100 {
		return
	}
	drawTopologyGrid(p, r, pal)
	agents := m.visibleAgents(routes, s.Live)
	if len(agents) == 0 && hasSel {
		agents = m.visibleAgents([]routing.RouteView{sel}, nil)
	}
	replay, replayActive := m.replayTopologyState()
	topologyRoutes := append([]routing.RouteView(nil), routes...)
	if replayActive {
		for _, rv := range replay.routes {
			topologyRoutes = appendIfMissingRoute(topologyRoutes, rv)
		}
	}
	providers := providerRowsForTopology(sel, hasSel, topologyRoutes, s.Live, m.accountsMasked())
	if len(providers) == 0 {
		providers = providerRowsFromRoutes(topologyRoutes)
	}
	if len(providers) > 5 {
		providers = providers[:5]
	}

	pad := float32(20)
	colW := min((r.W-pad*2-72)/3, 150)
	colW = max(colW, 112)
	cardH := float32(52)
	agentGap := float32(12)
	agentTop := r.Y + max((r.H-float32(len(agents))*cardH-float32(max(len(agents)-1, 0))*agentGap)/2, 54)
	if len(agents) == 0 {
		agentTop = r.Y + r.H/2 - 24
	}
	leftX := r.X + pad
	centerX := r.X + r.W/2 - colW/2
	rightX := r.X + r.W - pad - colW

	layout := topologyFrameLayout{width: r.W, height: r.H, gateway: nodeBox{x: centerX, y: r.Y + r.H/2 - cardH/2, w: colW, h: cardH + 16}}
	for i := range agents {
		layout.agents = append(layout.agents, nodeBox{x: leftX, y: agentTop + float32(i)*(cardH+agentGap), w: colW, h: cardH})
	}
	providerTop := r.Y + max((r.H-float32(len(providers))*cardH-float32(max(len(providers)-1, 0))*agentGap)/2, 54)
	if len(providers) == 0 {
		providerTop = r.Y + r.H/2 - 24
	}
	for i := range providers {
		layout.providers = append(layout.providers, nodeBox{x: rightX, y: providerTop + float32(i)*(cardH+agentGap), w: colW, h: cardH})
	}
	m.mu.Lock()
	m.topology.layout = layout
	m.mu.Unlock()

	// Draw idle links first so activity paints above them.
	if len(layout.agents) == 0 {
		p.Fill(ui.Rect{X: leftX, Y: layout.gateway.y + layout.gateway.h/2 - 1, W: layout.gateway.x - leftX, H: 2}, pal.RouteIdle.Alpha(0.5), 1)
	} else {
		for _, a := range layout.agents {
			ax, ay := a.rightCenter()
			gx, gy := layout.gateway.leftCenter()
			drawCubic(p, point(ax, ay), point(gx, gy), pal.RouteIdle.Alpha(0.45), 1.2)
		}
	}
	if len(layout.providers) == 0 {
		p.Fill(ui.Rect{X: layout.gateway.x + layout.gateway.w, Y: layout.gateway.y + layout.gateway.h/2 - 1, W: rightX - (layout.gateway.x + layout.gateway.w), H: 2}, pal.RouteIdle.Alpha(0.5), 1)
	} else {
		for _, prov := range layout.providers {
			gx, gy := layout.gateway.rightCenter()
			px, py := prov.leftCenter()
			drawCubic(p, point(gx, gy), point(px, py), pal.RouteIdle.Alpha(0.45), 1.2)
		}
	}

	// Draw only paths backed by an observation in this snapshot or by the
	// completed trace currently being replayed. A provider endpoint is used
	// only when its exact id is present in the card layout; otherwise the
	// truthful path stops at Gateway.
	activities := make([]topologyActivity, 0, len(s.Live))
	for _, l := range s.Live {
		activities = append(activities, topologyActivity{
			agent: l.Agent, provider: l.Provider, color: pal.Accent,
			seed: int(l.ID % 997), animate: true,
		})
	}
	for _, rv := range replay.routes {
		activity := topologyActivity{
			agent: rv.Agent, provider: rv.Provider, color: agentHue(rv.Agent),
			seed: int(rv.ID % 997), animate: replay.playing,
			failed: rv.Status >= 400 || rv.Error != "",
		}
		if activity.provider == "" {
			activity.provider = rv.DisplayProvider()
		}
		activities = append(activities, activity)
	}
	for _, activity := range activities {
		ai := indexAgentNode(agents, activity.agent)
		if ai < 0 {
			continue
		}
		anchor := layout.agents[ai]
		ax, ay := anchor.rightCenter()
		gx, gy := layout.gateway.leftCenter()
		drawCubic(p, point(ax, ay), point(gx, gy), activity.color.Alpha(0.9), 2.2)
		drawFlier(p, point(ax, ay), point(gx, gy), activity.color, now, activity.seed, activity.failed)
		pi := indexProviderNode(providers, activity.provider)
		if pi >= 0 {
			gx, gy := layout.gateway.rightCenter()
			px, py := layout.providers[pi].leftCenter()
			drawCubic(p, point(gx, gy), point(px, py), activity.color.Alpha(0.9), 2.2)
			drawFlier(p, point(gx, gy), point(px, py), activity.color, now.Add(650*time.Millisecond), activity.seed+1, activity.failed)
		}
		if activity.animate {
			p.AnimationFrame()
		}
	}

	for i, a := range agents {
		localPalette := pal
		localPalette.Accent = a.Hue
		drawAgentCard(p, layout.agents[i].rect(), a, localPalette)
	}
	drawGatewayCard(p, layout.gateway.rect(), s, pal)
	for i, prov := range providers {
		drawProviderCard(p, layout.providers[i].rect(), prov, pal)
	}
	if len(agents) == 0 {
		p.Text(leftX+2, r.Y+r.H/2+12, m.localization().T("routing.noAgents"), 9, pal.Faint)
	}
	_ = sel
}

type topologyActivity struct {
	agent    string
	provider string
	color    ui.Color
	seed     int
	animate  bool
	failed   bool
}

type replayTopology struct {
	routes  []routing.RouteView
	playing bool
}

// replayTopologyState returns only routes that the real replay timeline says
// are in flight at its current position. It never invents a provider or a
// route from timing heuristics.
func (m *Model) replayTopologyState() (replayTopology, bool) {
	m.mu.RLock()
	tab := m.tab
	replay := m.replay
	snapshot := m.lastSnapshot
	m.mu.RUnlock()
	if tab != tabHistory || !replay.loaded || replay.timeline.Duration <= 0 {
		return replayTopology{}, false
	}
	_, inFlight := routing.ReplayVisibleRoutes(replay.timeline, replay.position)
	if len(inFlight) == 0 {
		return replayTopology{playing: replay.playing}, true
	}
	byID := make(map[int64]routing.RouteView, len(snapshot.Routes))
	for _, rv := range snapshot.Routes {
		byID[rv.ID] = rv
	}
	out := replayTopology{playing: replay.playing}
	for _, route := range inFlight {
		if rv, ok := byID[route.ID]; ok {
			out.routes = append(out.routes, rv)
			continue
		}
		out.routes = append(out.routes, routing.RouteView{
			Route:        route,
			Completeness: routing.Completeness{History: true},
		})
	}
	return out, true
}

func appendIfMissingRoute(routes []routing.RouteView, rv routing.RouteView) []routing.RouteView {
	for _, existing := range routes {
		if existing.ID == rv.ID {
			return routes
		}
	}
	return append(routes, rv)
}

func indexAgentNode(agents []agentNode, id string) int {
	for i := range agents {
		if strings.EqualFold(agents[i].ID, id) {
			return i
		}
	}
	return -1
}

func indexProviderNode(providers []providerNode, provider string) int {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return -1
	}
	for i := range providers {
		row := providers[i]
		if strings.EqualFold(row.Provider, provider) {
			return i
		}
		if row.Provider == "" && strings.EqualFold(providerOf(row.Model), provider) {
			return i
		}
	}
	return -1
}

func providerRowsForTopology(sel routing.RouteView, hasSel bool, routes []routing.RouteView, live []routing.LiveRequest, hidden bool) []providerNode {
	var out []providerNode
	seen := map[string]bool{}
	add := func(rows []providerNode) {
		for _, row := range rows {
			key := strings.ToLower(row.Provider + "\x00" + row.Name + "\x00" + row.Model)
			if key == "\x00\x00" || seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, row)
		}
	}
	if hasSel {
		add(providerRows(sel, hidden))
	}
	for _, rv := range routes {
		add(providerRows(rv, hidden))
	}
	for _, l := range live {
		if l.Provider == "" {
			continue
		}
		add([]providerNode{{
			Provider: l.Provider, Name: l.Provider, Model: l.Model,
			Status: string(l.Phase), Current: true,
		}})
	}
	return out
}

func drawTopologyGrid(p *ui.Painter, r ui.Rect, pal palette) {
	step := float32(28)
	for x := r.X; x < r.X+r.W; x += step {
		p.Line(x, r.Y, x, r.Y+r.H, 0.5, pal.Grid.Alpha(0.5))
	}
	for y := r.Y; y < r.Y+r.H; y += step {
		p.Line(r.X, y, r.X+r.W, y, 0.5, pal.Grid.Alpha(0.5))
	}
}

func drawAgentCard(p *ui.Painter, r ui.Rect, a agentNode, pal palette) {
	if a.Active {
		p.Shadow(r, 10, 0, 4, 14, 0, pal.Shadow)
	}
	p.Fill(r, pal.PanelRaised, 12)
	p.Stroke(r, a.Hue.Alpha(0.35+boolFloat(a.Active, 0.3)), 12, 1)
	circle := ui.Rect{X: r.X + 10, Y: r.Y + 12, W: 28, H: 28}
	p.Fill(circle, a.Hue.Alpha(0.14), 14)
	p.Stroke(circle, a.Hue.Alpha(0.55), 14, 1)
	p.Text(r.X+16, r.Y+20, agentInitials(a.ID), 9, a.Hue)
	p.Text(r.X+46, r.Y+10, truncate(a.Name, 18), 10.5, pal.Text)
	sub := a.Model
	if a.Active {
		sub = "● " + sub
	}
	if sub == "" {
		sub = "—"
	}
	p.Text(r.X+46, r.Y+27, truncate(sub, 19), 8.5, pal.Muted)
	if a.Requests > 0 {
		p.Text(r.X+r.W-26, r.Y+10, itoa(a.Requests), 9, pal.Faint)
	}
	if a.Active {
		p.Fill(ui.Rect{X: r.X + r.W - 12, Y: r.Y + 8, W: 5, H: 5}, pal.Accent, 3)
	}
}

func drawGatewayCard(p *ui.Painter, r ui.Rect, s routing.Snapshot, pal palette) {
	color := pal.Warning
	if s.GatewayOnline {
		color = pal.Accent
	}
	p.Shadow(r, 14, 0, 8, 24, 0, pal.Shadow)
	p.FillGradient(r, ui.LinearGradient{From: pal.PanelRaised, To: pal.PanelMuted, Angle: 120}, 14)
	p.Stroke(r, color.Alpha(0.75), 14, 1.4)
	cx := r.X + r.W/2
	p.Fill(ui.Rect{X: cx - 19, Y: r.Y + 10, W: 38, H: 38}, color.Alpha(0.14), 19)
	drawMagpieMark(p, ui.Rect{X: cx - 14, Y: r.Y + 14, W: 28, H: 28}, color, pal.PanelRaised)
	p.Text(r.X+12, r.Y+52, "MAGPIE GATEWAY", 8, pal.Muted)
	p.Text(r.X+12, r.Y+65, gatewayStatusText(s), 9.5, color)
	p.Text(r.X+r.W-12, r.Y+65, m2(s), 8, pal.Faint)
}

func gatewayStatusText(s routing.Snapshot) string {
	if s.GatewayOnline {
		return "online · " + itoa(len(s.Live)) + " live"
	}
	return "offline"
}

func m2(s routing.Snapshot) string {
	if len(s.Live) == 0 {
		return "idle"
	}
	return itoa(len(s.Live)) + " req"
}

func drawProviderCard(p *ui.Painter, r ui.Rect, n providerNode, pal palette) {
	color := pal.PanelRaised
	border := pal.LineSoft
	if n.Failed {
		border = pal.Danger.Alpha(0.7)
	} else if n.Current {
		border = pal.Accent.Alpha(0.8)
	}
	p.Fill(r, color, 12)
	p.Stroke(r, border, 12, 1.1)
	p.Text(r.X+10, r.Y+9, truncate(firstNonEmpty(n.Name, n.Provider, "provider"), 18), 10, pal.Text)
	model := firstNonEmpty(n.Model, n.Who, n.Plan)
	p.Text(r.X+10, r.Y+25, truncate(model, 20), 8.5, pal.Muted)
	if n.Status != "" {
		statusColor := pal.Success
		if n.Failed {
			statusColor = pal.Danger
		} else if n.Current {
			statusColor = pal.Accent
		}
		p.Text(r.X+r.W-40, r.Y+9, truncate(n.Status, 8), 8, statusColor)
	}
	if n.Note != "" {
		p.Text(r.X+10, r.Y+40, truncate(n.Note, 24), 7.5, pal.Faint)
	}
	if n.Current {
		p.Fill(ui.Rect{X: r.X + r.W - 9, Y: r.Y + r.H - 9, W: 4, H: 4}, pal.Accent, 2)
	}
}

func providerRowsFromRoutes(routes []routing.RouteView) []providerNode {
	var out []providerNode
	seen := map[string]bool{}
	for _, r := range routes {
		for _, n := range providerRows(r, false) {
			key := n.Provider + "\x00" + n.Name + "\x00" + n.Model
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, n)
			if len(out) >= 5 {
				return out
			}
		}
	}
	return out
}

func drawCubic(p *ui.Painter, a, b [2]float32, color ui.Color, width float32) {
	if a[0] == b[0] && a[1] == b[1] {
		return
	}
	dx := max((b[0]-a[0])*0.48, 22)
	var path ui.Path
	path.MoveTo(a[0], a[1])
	path.CubeTo(a[0]+dx, a[1], b[0]-dx, b[1], b[0], b[1])
	p.StrokePath(&path, width, color)
}

func drawPacket(p *ui.Painter, a, b [2]float32, progress float32, color ui.Color, now time.Time, seed int) {
	t := progress
	if t <= 0 || t >= 1 {
		t = float32((now.UnixMilli()+int64(seed*137))%1800) / 1800
	}
	x, y := cubicPoint(a, b, t)
	drawBirdAt(p, x, y, 5.2, color, now, seed)
	p.Fill(ui.Rect{X: x - 8, Y: y - 8, W: 16, H: 16}, color.Alpha(0.10), 8)
}

func drawFlier(p *ui.Painter, a, b [2]float32, color ui.Color, now time.Time, seed int, failed bool) {
	drawPacket(p, a, b, 0, color, now, seed)
	if failed {
		t := float32((now.UnixMilli()+int64(seed*137))%1800) / 1800
		x, y := cubicPoint(a, b, t)
		p.Stroke(ui.Rect{X: x - 9, Y: y - 9, W: 18, H: 18}, ui.Hex("#b94a44"), 9, 1.2)
	}
}

// drawBirdAt draws a small native magpie in flight: a body, a tail, and two
// wings whose span changes with the frame clock. It is intentionally built
// from UI paths rather than an SVG or web asset.
func drawBirdAt(p *ui.Painter, x, y, size float32, color ui.Color, now time.Time, seed int) {
	beat := float32((now.UnixMilli()/55 + int64(seed)*7) % 8)
	if beat > 4 {
		beat = 8 - beat
	}
	wingLift := -size * (0.35 + beat*0.09)
	body := ui.Rect{X: x - size*0.75, Y: y - size*0.28, W: size * 1.5, H: size * 0.56}
	p.Fill(body, color, size*0.28)
	var tail ui.Path
	tail.MoveTo(x-size*0.55, y)
	tail.LineTo(x-size*1.18, y-size*0.32)
	tail.LineTo(x-size*0.92, y+size*0.2)
	p.FillPath(&tail, color)
	var leftWing ui.Path
	leftWing.MoveTo(x-size*0.12, y-size*0.12)
	leftWing.CubeTo(x-size*0.48, wingLift-size*0.18, x-size*0.82, wingLift, x-size*0.95, wingLift+size*0.2)
	leftWing.CubeTo(x-size*0.58, wingLift+size*0.46, x-size*0.26, y+size*0.18, x-size*0.12, y-size*0.12)
	p.FillPath(&leftWing, color.Alpha(0.82))
	var rightWing ui.Path
	rightWing.MoveTo(x+size*0.12, y-size*0.12)
	rightWing.CubeTo(x+size*0.5, wingLift-size*0.12, x+size*0.82, wingLift, x+size*0.95, wingLift+size*0.2)
	rightWing.CubeTo(x+size*0.58, wingLift+size*0.46, x+size*0.26, y+size*0.18, x+size*0.12, y-size*0.12)
	p.FillPath(&rightWing, color.Alpha(0.68))
}

func cubicPoint(a, b [2]float32, t float32) (float32, float32) {
	dx := max((b[0]-a[0])*0.48, 22)
	c1x, c1y := a[0]+dx, a[1]
	c2x, c2y := b[0]-dx, b[1]
	u := 1 - t
	x := u*u*u*a[0] + 3*u*u*t*c1x + 3*u*t*t*c2x + t*t*t*b[0]
	y := u*u*u*a[1] + 3*u*u*t*c1y + 3*u*t*t*c2y + t*t*t*b[1]
	return x, y
}

func drawMagpieMark(p *ui.Painter, r ui.Rect, body, belly ui.Color) {
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	s := min(r.W, r.H)
	var wing ui.Path
	wing.MoveTo(cx-s*0.32, cy-s*0.02)
	wing.CubeTo(cx-s*0.58, cy-s*0.24, cx-s*0.44, cy-s*0.42, cx-s*0.18, cy-s*0.22)
	wing.CubeTo(cx-s*0.08, cy-s*0.13, cx-s*0.12, cy+s*0.08, cx-s*0.32, cy-s*0.02)
	p.FillPath(&wing, body)
	p.Fill(ui.Rect{X: cx - s*0.16, Y: cy - s*0.18, W: s * 0.38, H: s * 0.26}, body, s*0.13)
	p.Fill(ui.Rect{X: cx + s*0.09, Y: cy - s*0.22, W: s * 0.18, H: s * 0.17}, belly, s*0.07)
	var tail ui.Path
	tail.MoveTo(cx+s*0.2, cy+s*0.02)
	tail.LineTo(cx+s*0.36, cy+s*0.2)
	tail.LineTo(cx+s*0.12, cy+s*0.08)
	p.FillPath(&tail, body)
	p.Fill(ui.Rect{X: cx + s*0.2, Y: cy - s*0.17, W: s * 0.025, H: s * 0.025}, belly, 1)
}

func agentInitials(id string) string {
	if id == "" {
		return "A"
	}
	name := agentName(id)
	if name == "" {
		return "A"
	}
	rs := []rune(name)
	if len(rs) >= 2 {
		return string(rs[:2])
	}
	return string(rs)
}

func truncate(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	if n <= 1 {
		return string(rs[:n])
	}
	return string(rs[:n-1]) + "…"
}

func boolFloat(v bool, f float32) float32 {
	if v {
		return f
	}
	return 0
}
