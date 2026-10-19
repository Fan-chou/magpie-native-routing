package ui

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/Fan-chou/magpie-native-routing/internal/config"
)

type palette struct {
	Canvas      ui.Color
	Panel       ui.Color
	PanelRaised ui.Color
	PanelMuted  ui.Color
	Line        ui.Color
	LineSoft    ui.Color
	Text        ui.Color
	Text2       ui.Color
	Muted       ui.Color
	Faint       ui.Color
	Accent      ui.Color
	AccentSoft  ui.Color
	Success     ui.Color
	SuccessSoft ui.Color
	Warning     ui.Color
	WarningSoft ui.Color
	Danger      ui.Color
	DangerSoft  ui.Color
	Violet      ui.Color
	Cyan        ui.Color
	C1          ui.Color
	C2          ui.Color
	C3          ui.Color
	C4          ui.Color
	C5          ui.Color
	C6          ui.Color
	Shadow      ui.Color
	RouteIdle   ui.Color
	RouteLit    ui.Color
	RouteDanger ui.Color
	RouteWarm   ui.Color
	BirdBody    ui.Color
	BirdWing    ui.Color
	BirdBelly   ui.Color
	Grid        ui.Color
}

func colors(c *ui.Context, mode config.ThemeMode) palette {
	dark := false
	switch mode {
	case config.ThemeDark:
		dark = true
	case config.ThemeLight:
		dark = false
	default:
		if c != nil {
			// The tester and a window both expose the resolved system
			// appearance through the frame's theme.
			dark = c.Theme().Dark
		}
	}
	if dark {
		return palette{
			// Magpie Desktop's dark palette (internal/gui/assets/app.css).
			Canvas:      ui.Hex("#1a1a1e"),
			Panel:       ui.Hex("#232327"),
			PanelRaised: ui.Hex("#232327"),
			PanelMuted:  ui.Hex("#1f1f23"),
			Line:        ui.Hex("#333339"),
			LineSoft:    ui.Hex("#2c2c32"),
			Text:        ui.Hex("#ededf1"),
			Text2:       ui.Hex("#b9b9c1"),
			Muted:       ui.Hex("#8b8b94"),
			Faint:       ui.Hex("#56565f"),
			Accent:      ui.Hex("#a5b4fc"),
			AccentSoft:  ui.Hex("#2b2c47"),
			Success:     ui.Hex("#6fd99b"),
			SuccessSoft: ui.Hex("#1e3a2b"),
			Warning:     ui.Hex("#f2b544"),
			WarningSoft: ui.Hex("#3f3220"),
			Danger:      ui.Hex("#f28b82"),
			DangerSoft:  ui.Hex("#45272a"),
			Violet:      ui.Hex("#c4b5fd"),
			Cyan:        ui.Hex("#7dd3fc"),
			C1:          ui.Hex("#a5b4fc"),
			C2:          ui.Hex("#7dd3fc"),
			C3:          ui.Hex("#6ee7b7"),
			C4:          ui.Hex("#fcd34d"),
			C5:          ui.Hex("#f9a8d4"),
			C6:          ui.Hex("#c4b5fd"),
			Shadow:      ui.RGBA(0, 0, 0, 0.32),
			RouteIdle:   ui.Hex("#333339"),
			RouteLit:    ui.Hex("#a5b4fc"),
			RouteDanger: ui.Hex("#f28b82"),
			RouteWarm:   ui.Hex("#f2b544"),
			BirdBody:    ui.Hex("#ededf1"),
			BirdWing:    ui.Hex("#b9b9c1"),
			BirdBelly:   ui.Hex("#232327"),
			Grid:        ui.Hex("#2c2c32"),
		}
	}
	return palette{
		// Magpie Desktop's light palette (internal/gui/assets/app.css).
		Canvas:      ui.Hex("#f2f2f5"),
		Panel:       ui.Hex("#ffffff"),
		PanelRaised: ui.Hex("#ffffff"),
		PanelMuted:  ui.Hex("#f9f9fb"),
		Line:        ui.Hex("#e3e3e8"),
		LineSoft:    ui.Hex("#ececf0"),
		Text:        ui.Hex("#1c1c21"),
		Text2:       ui.Hex("#4b4b53"),
		Muted:       ui.Hex("#85858d"),
		Faint:       ui.Hex("#b9b9c2"),
		Accent:      ui.Hex("#4f46e5"),
		AccentSoft:  ui.Hex("#eeedfd"),
		Success:     ui.Hex("#189a58"),
		SuccessSoft: ui.Hex("#e4f6ec"),
		Warning:     ui.Hex("#b45309"),
		WarningSoft: ui.Hex("#fbf0df"),
		Danger:      ui.Hex("#d64545"),
		DangerSoft:  ui.Hex("#fbe9e9"),
		Violet:      ui.Hex("#8b5cf6"),
		Cyan:        ui.Hex("#0ea5e9"),
		C1:          ui.Hex("#4f46e5"),
		C2:          ui.Hex("#0ea5e9"),
		C3:          ui.Hex("#10b981"),
		C4:          ui.Hex("#f59e0b"),
		C5:          ui.Hex("#ec4899"),
		C6:          ui.Hex("#8b5cf6"),
		Shadow:      ui.RGBA(20, 22, 30, 0.10),
		RouteIdle:   ui.Hex("#e3e3e8"),
		RouteLit:    ui.Hex("#4f46e5"),
		RouteDanger: ui.Hex("#d64545"),
		RouteWarm:   ui.Hex("#b45309"),
		BirdBody:    ui.Hex("#1c1c21"),
		BirdWing:    ui.Hex("#4b4b53"),
		BirdBelly:   ui.Hex("#ffffff"),
		Grid:        ui.Hex("#ececf0"),
	}
}

func themeFor(c *ui.Context, p palette) *ui.Theme {
	t := ui.LightTheme()
	if p.Canvas.R < 80 {
		t = ui.DarkTheme()
	}
	t.Background = p.Canvas
	t.Surface = p.PanelRaised
	t.SurfaceHover = p.PanelMuted
	t.SurfacePressed = p.LineSoft
	t.Border = p.Line
	t.Text = p.Text
	t.TextMuted = p.Muted
	t.Accent = p.Accent
	t.AccentHover = p.Accent
	t.AccentPressed = p.Accent
	t.AccentText = p.PanelRaised
	t.Danger = p.Danger
	t.Warning = p.Warning
	t.Success = p.Success
	t.Selection = p.AccentSoft
	t.Focus = p.Accent.Alpha(0.55)
	t.Scrollbar = p.Muted.Alpha(0.35)
	t.Radius = 7
	t.Spacing = 4
	t.FontSize = 13
	return t
}

type springValue struct {
	from  float32
	to    float32
	start time.Time
	dur   time.Duration
	valid bool
}

func (s *springValue) value(now time.Time) float32 {
	if !s.valid {
		return s.to
	}
	if s.dur <= 0 {
		return s.to
	}
	t := float32(now.Sub(s.start)) / float32(s.dur)
	if t < 0 {
		return s.from
	}
	if t >= 1 {
		return s.to
	}
	// Ease-out cubic, close to Magpie's transform transitions.
	u := 1 - t
	return s.from + (s.to-s.from)*(1-u*u*u)
}

func (s *springValue) set(to float32, now time.Time, dur time.Duration) {
	if !s.valid || s.to != to {
		s.from = s.value(now)
		s.to = to
		s.start = now
		s.dur = dur
		s.valid = true
	}
}

func (s *springValue) moving(now time.Time) bool {
	return s.valid && s.dur > 0 && now.Sub(s.start) < s.dur && s.from != s.to
}
