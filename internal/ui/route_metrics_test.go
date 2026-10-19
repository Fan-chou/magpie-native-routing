package ui

import (
	"testing"
	"time"

	"github.com/Fan-chou/magpie-native-routing/internal/routing"
)

func TestVisibleRouteMetricsCanHideClassesWithoutEnablingCost(t *testing.T) {
	model, _ := testModel(t, routing.Snapshot{}, Actions{})
	rv := routing.RouteView{Route: routing.Route{
		ID: 1, Done: true, Status: 200, Millis: 1200, TTFT: 250, Tokens: 80,
		Usage: []routing.RouteUsage{{Input: 30, Output: 20, CacheRead: 30}},
	}}
	loc := model.localization()
	if got := model.visibleRouteMetrics(rv, "ok", loc); len(got) < 3 {
		t.Fatalf("visible metrics before filtering = %d, want at least 3", len(got))
	}
	model.toggleMetric("duration")
	for _, metric := range model.visibleRouteMetrics(rv, "ok", loc) {
		if metric.key == "duration" {
			t.Fatal("duration stayed visible after toggling it off")
		}
		if metric.key == "cost" {
			t.Fatal("cost became visible without a public price source")
		}
	}
	model.toggleMetric("cost")
	for _, metric := range model.visibleRouteMetrics(rv, "ok", loc) {
		if metric.key == "cost" {
			t.Fatal("cost became visible after toggling an unavailable option")
		}
	}
}

func TestMetricMenuListsOriginalClassesAndDisablesCost(t *testing.T) {
	model, _ := testModel(t, completedSyntheticSnapshot(t), Actions{})
	tester := render(t, model, 1240, 820)
	if err := tester.Click("Metrics"); err != nil {
		t.Fatalf("metrics menu button not found: %v", err)
	}
	menu := tester.Menu()
	want := []string{"Duration", "First token", "Tokens", "Cache", "Speed", "Cost"}
	if len(menu) != len(want) {
		t.Fatalf("metric menu = %v, want %v", menu, want)
	}
	for i := range want {
		if menu[i] != want[i] {
			t.Fatalf("metric menu[%d] = %q, want %q", i, menu[i], want[i])
		}
	}
	if err := tester.ChooseMenuItem("Cost"); err == nil {
		t.Fatal("Cost was selectable without a price source")
	}
}

func TestCachedAccountTallyInvalidatesWhenTraceChanges(t *testing.T) {
	start := time.Date(2026, 10, 19, 2, 0, 0, 0, time.UTC)
	route := routing.RouteView{Route: routing.Route{
		ID: 41, Time: start, Agent: "codex", Model: "qoder/model",
		Provider: "qoder", Done: true, Status: 200, Millis: 1200,
		Order: []routing.Weighed{{
			ID: "qoder", Provider: "qoder", Name: "Qoder",
			Who: "first@example.test", Model: "model", Kind: "account",
		}},
		Tries: []routing.Try{{
			ID: "qoder", Model: "model", Start: start,
			Done: true, Status: 200, Millis: 1200,
		}},
	}}
	model, _ := testModel(t, routing.Snapshot{}, Actions{})
	first := model.cachedAccountTally([]routing.RouteView{route})
	if len(first) != 1 || first[0].Who != "first@example.test" || first[0].OK != 1 {
		t.Fatalf("first tally = %+v", first)
	}
	route.Order[0].Who = "second@example.test"
	route.Tries[0].Status = 429
	route.Tries[0].Fail = "rate limited"
	second := model.cachedAccountTally([]routing.RouteView{route})
	if len(second) != 1 || second[0].Who != "second@example.test" ||
		second[0].OK != 0 || second[0].Failures != 1 {
		t.Fatalf("cached tally did not follow the trace update: %+v", second)
	}
}
