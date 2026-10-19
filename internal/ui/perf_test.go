package ui

import (
	"fmt"
	"testing"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/config"
	"github.com/Fan-chou/magpie-native-routing/internal/routing"
	"github.com/egoist/mygo/ui"
)

func BenchmarkAccountTally(b *testing.B) {
	routes := benchmarkRoutes(1200)
	b.ReportAllocs()
	for range b.N {
		_ = accountTally(routes)
	}
}

func benchmarkRoutes(n int) []routing.RouteView {
	start := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	out := make([]routing.RouteView, 0, n)
	for i := 0; i < n; i++ {
		at := start.Add(time.Duration(i) * time.Second)
		provider := fmt.Sprintf("provider-%d", i%7)
		out = append(out, routing.RouteView{Route: routing.Route{
			ID: int64(900000 + i), Time: at, Agent: "codex", Model: provider + "/model",
			Provider: provider, Done: true, Status: 200, Millis: 1200,
			Order: []routing.Weighed{{
				ID: fmt.Sprintf("account-%d", i%23), Provider: provider,
				Name: provider, Who: fmt.Sprintf("user%d@example.test", i%23),
				Model: "model", Kind: "account", Plan: "Pro", Known: true, Used: float64(i % 100),
			}},
			Tries: []routing.Try{{
				ID: fmt.Sprintf("account-%d", i%23), Model: "model",
				Start: at, Done: true, Status: 200, Millis: 1200,
			}},
		}})
	}
	return out
}

func BenchmarkCachedAccountTallyCold(b *testing.B) {
	store := routing.NewStore()
	store.ReplaceHistory(benchmarkRouteValues(1200))
	settings := config.DefaultSettings()
	settings.Language = "en"
	settings.Theme = config.ThemeLight
	model := NewModel(store, settings, config.Paths{}, "/synthetic/magpie", Actions{})
	model.SetSnapshot(store.Snapshot())
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		model.viewCache = uiViewCache{}
		_ = model.cachedAccountTally(model.visibleRoutes(model.snapshot(), false))
	}
}

func BenchmarkCachedAccountTallyWarm(b *testing.B) {
	store := routing.NewStore()
	store.ReplaceHistory(benchmarkRouteValues(1200))
	settings := config.DefaultSettings()
	settings.Language = "en"
	settings.Theme = config.ThemeLight
	model := NewModel(store, settings, config.Paths{}, "/synthetic/magpie", Actions{})
	model.SetSnapshot(store.Snapshot())
	routes := model.visibleRoutes(model.snapshot(), false)
	_ = model.cachedAccountTally(routes)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = model.cachedAccountTally(routes)
	}
}

func BenchmarkDrawFlowingPath(b *testing.B) {
	path := newFlightPath(cubicFlightPoints(20, 80, 880, 360))
	b.ReportAllocs()
	for i := range b.N {
		tester := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				drawFlowingPath(p, path, ui.RGB(80, 90, 220), 1.45, time.UnixMilli(int64(i)))
			})
		}, 920, 460)
		_ = tester.Image()
	}
}

func BenchmarkFlightDashPath(b *testing.B) {
	path := newFlightPath(cubicFlightPoints(20, 80, 880, 360))
	b.ReportAllocs()
	for i := range b.N {
		_, _ = flightDashPath(path, float32(i%8), 3, 8)
	}
}

func BenchmarkAppendFlightArcLegacy(b *testing.B) {
	path := newFlightPath(cubicFlightPoints(20, 80, 880, 360))
	length := path.length()
	b.ReportAllocs()
	for i := range b.N {
		offset := float32(i % 8)
		var flow ui.Path
		for d := -offset; d < length; d += 8 {
			appendFlightArc(&flow, path, max(d, 0), min(d+3, length))
		}
	}
}

func BenchmarkDrawMagpieFrame(b *testing.B) {
	b.ReportAllocs()
	for i := range b.N {
		tester := ui.NewTester(func(c *ui.Context) {
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				drawMagpie(p, magpiePose{
					x: r.X + r.W/2, y: r.Y + r.H/2, scale: 1, aim: 8, flip: 1,
					flapPhase: float32(i%42) / 42, appear: 1,
					body: ui.RGB(30, 30, 30), belly: ui.RGB(255, 255, 255),
					far: ui.RGB(30, 30, 30),
				}, ui.RGB(90, 100, 240), magpieCarryRequest,
					packetOutcomeUnknown, false)
			})
		}, 920, 460)
		_ = tester.Image()
	}
}

func benchmarkRouteValues(n int) []routing.Route {
	out := make([]routing.Route, 0, n)
	for _, rv := range benchmarkRoutes(n) {
		out = append(out, rv.Route)
	}
	return out
}
