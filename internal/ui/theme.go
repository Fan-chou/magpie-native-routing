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
			Canvas:      ui.Hex("#0d1110"),
			Panel:       ui.Hex("#141b19"),
			PanelRaised: ui.Hex("#1b2421"),
			PanelMuted:  ui.Hex("#222d29"),
			Line:        ui.Hex("#304039"),
			LineSoft:    ui.Hex("#25322d"),
			Text:        ui.Hex("#edf4f0"),
			Muted:       ui.Hex("#93a79e"),
			Faint:       ui.Hex("#64766e"),
			Accent:      ui.Hex("#5dd6a0"),
			AccentSoft:  ui.Hex("#173c2d"),
			Success:     ui.Hex("#66d99c"),
			SuccessSoft: ui.Hex("#173b2a"),
			Warning:     ui.Hex("#e7b05f"),
			WarningSoft: ui.Hex("#3d2c17"),
			Danger:      ui.Hex("#ef7b76"),
			DangerSoft:  ui.Hex("#422120"),
			Violet:      ui.Hex("#b8a2f4"),
			Cyan:        ui.Hex("#71cfe3"),
			Shadow:      ui.RGBA(0, 0, 0, 0.32),
			RouteIdle:   ui.Hex("#354a41"),
			RouteLit:    ui.Hex("#77dfa8"),
			RouteDanger: ui.Hex("#e56f69"),
			RouteWarm:   ui.Hex("#e7b05f"),
			BirdBody:    ui.Hex("#e7f4ee"),
			BirdWing:    ui.Hex("#b7d9c8"),
			BirdBelly:   ui.Hex("#1d2b25"),
			Grid:        ui.Hex("#22312b"),
		}
	}
	return palette{
		Canvas:      ui.Hex("#f4f1ea"),
		Panel:       ui.Hex("#fbfaf6"),
		PanelRaised: ui.Hex("#ffffff"),
		PanelMuted:  ui.Hex("#ece8de"),
		Line:        ui.Hex("#d8d1c3"),
		LineSoft:    ui.Hex("#e8e3d9"),
		Text:        ui.Hex("#17201c"),
		Muted:       ui.Hex("#647169"),
		Faint:       ui.Hex("#89938c"),
		Accent:      ui.Hex("#18875d"),
		AccentSoft:  ui.Hex("#dcefe5"),
		Success:     ui.Hex("#198753"),
		SuccessSoft: ui.Hex("#e2f2e9"),
		Warning:     ui.Hex("#a86a18"),
		WarningSoft: ui.Hex("#f5ead7"),
		Danger:      ui.Hex("#b94a44"),
		DangerSoft:  ui.Hex("#f5e2e0"),
		Violet:      ui.Hex("#6854a3"),
		Cyan:        ui.Hex("#24788a"),
		Shadow:      ui.RGBA(31, 37, 33, 0.09),
		RouteIdle:   ui.Hex("#c7cfc8"),
		RouteLit:    ui.Hex("#18875d"),
		RouteDanger: ui.Hex("#b94a44"),
		RouteWarm:   ui.Hex("#a86a18"),
		BirdBody:    ui.Hex("#16251f"),
		BirdWing:    ui.Hex("#38594c"),
		BirdBelly:   ui.Hex("#f9f8f2"),
		Grid:        ui.Hex("#e3ded2"),
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
