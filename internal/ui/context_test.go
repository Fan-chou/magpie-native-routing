package ui

import (
	"testing"

	"github.com/Fan-chou/magpie-native-routing/internal/routing"
)

func TestContextCellsFollowLargestRemainderAndItemMidpoints(t *testing.T) {
	prompt := routing.Prompt{
		Window: 10_000,
		Tokens: 5_000,
		Parts: []routing.PromptPart{
			{Kind: "chat", Tokens: 2_000, Items: []routing.PromptItem{
				{Name: "small", Tokens: 500},
				{Name: "large", Tokens: 1_500},
			}},
			{Kind: "system", Tokens: 3_000, Items: []routing.PromptItem{
				{Name: "prompt", Tokens: 3_000},
			}},
		},
	}
	cells := ctxCells(prompt)
	if len(cells) != ctxCellCount {
		t.Fatalf("cells = %d, want %d", len(cells), ctxCellCount)
	}
	counts := map[string]int{}
	for _, cell := range cells {
		if cell.kind != "" {
			counts[cell.kind]++
		}
	}
	// Magpie's cellsOf keeps context.js part order rather than sorting by
	// size. system's 60% takes the first 120 cells and chat's 40% the
	// remaining 80.
	if counts["chat"] != 80 || counts["system"] != 120 {
		t.Fatalf("part cells = %#v, want chat 80 and system 120", counts)
	}
	if cells[0].kind != "system" || cells[119].kind != "system" || cells[120].kind != "chat" {
		t.Fatalf("cells did not retain context.js part order: first=%+v boundary=%+v", cells[0], cells[400-counts["chat"]])
	}
	var chatItems []int
	for _, cell := range cells {
		if cell.kind == "chat" {
			chatItems = append(chatItems, cell.item)
		}
	}
	// Items are sorted by token count before the midpoint lookup. The
	// 1500-token item occupies the first 75% of chat's 80 cells, then
	// the 500-token item takes the rest.
	if chatItems[0] != 0 || chatItems[59] != 0 || chatItems[60] != 1 {
		t.Fatalf("chat item mapping = %v, want 60 cells of item 0 then item 1", chatItems)
	}
}

func TestContextCellsScaleAnOverrunPromptToTheWholeWindow(t *testing.T) {
	prompt := routing.Prompt{
		Window: 1000,
		Tokens: 2000,
		Parts:  []routing.PromptPart{{Kind: "tools", Tokens: 2000}},
	}
	cells := ctxCells(prompt)
	free := 0
	for _, cell := range cells {
		if cell.kind == "" {
			free++
		}
	}
	if free != 0 {
		t.Fatalf("overrun prompt had %d free cells, want all %d occupied", free, ctxCellCount)
	}
}

func TestContextCellsAreMostlyFreeForAnEmptyPrompt(t *testing.T) {
	// No recorded parts means no truthful colour to put in any cell,
	// even though the token total says the window is a quarter full.
	// Upstream's cellsOf behaves the same way.
	cells := ctxCells(routing.Prompt{Window: 100_000, Tokens: 25_000})
	free := 0
	for _, cell := range cells {
		if cell.kind == "" {
			free++
		}
	}
	if free != ctxCellCount {
		t.Fatalf("free cells = %d, want %d without part data", free, ctxCellCount)
	}

	cells = ctxCells(routing.Prompt{
		Window: 100_000, Tokens: 25_000,
		Parts: []routing.PromptPart{{Kind: "chat", Tokens: 25_000}},
	})
	free = 0
	for _, cell := range cells {
		if cell.kind == "" {
			free++
		}
	}
	if free != 300 {
		t.Fatalf("free cells = %d, want 300 with part data", free)
	}
}

func TestContextWaffleBreakpointUsesCardContentWidth(t *testing.T) {
	cases := []struct {
		width float32
		want  int
	}{
		{width: 0, want: 50},
		{width: 520, want: 25},
		{width: 520.5, want: 50},
		{width: 800, want: 50},
	}
	for _, tc := range cases {
		if got := ctxWaffleColumns(tc.width); got != tc.want {
			t.Errorf("ctxWaffleColumns(%v) = %d, want %d", tc.width, got, tc.want)
		}
	}
}

func TestContextCardFoldStartsClosedAndOpensOnClick(t *testing.T) {
	model, _ := testModel(t, completedSyntheticSnapshot(t), Actions{})
	model.SetSnapshot(routing.Snapshot{Routes: []routing.RouteView{{
		Route: routing.Route{
			ID: 11, Agent: "codex", Model: "synthetic/alpha",
			Prompt: &routing.Prompt{
				Window: 200_000, Tokens: 50_000,
				Parts: []routing.PromptPart{{Kind: "chat", Tokens: 50_000}},
			},
		},
		Completeness: routing.Completeness{History: true},
	}}})
	tester := render(t, model, 1100, 1400)
	if model.contextCardOpen() {
		t.Fatal("context card unexpectedly started open")
	}
	if err := tester.Click("Context window"); err != nil {
		t.Fatalf("fold header not clickable: %v", err)
	}
	if !model.contextCardOpen() {
		t.Fatal("context card did not open after clicking its header")
	}
	tester.Frame()
	if !tester.HasText("Contents") {
		t.Fatalf("open context card lacks contents: %v", tester.Texts())
	}
}
