// Package stress holds the several hundred node windowed scene that the
// project plan, section 12, "Go/No-Go vor Schritt 2", criterion 2, requires:
// a non trivial static stack scene that a window can hold at sixty frames per
// second on a Raspberry Pi 4.
//
// # Why it is here and not in an example
//
// It used to be cmd/example-layout, where it was doing two incompatible jobs.
// A benchmark scene wants knobs, hundreds of nodes and determinism; an
// example wants to be read in one sitting. The scene lost the second contest,
// so it moved here, to a package that is honest about being a fixture:
//
//   - internal, because it is not API. Nothing outside this module should
//     build against a benchmark scene.
//   - a package and not a command, so that tests and benchmarks can construct
//     it headless and assert on the display list. Its own tests are the
//     layout assertions that WU-C2 left behind, which are properties of gift
//     and not of any example.
//   - cmd/gift-stress is a thin wrapper that puts it in a window, because
//     criterion 2 is about a window on real hardware and a test binary is not
//     one.
//
// The scene is deterministic: the same parameters produce the same tree, the
// same display list and the same number of nodes on every run, so two
// measurements are comparable. There is no randomness anywhere in it.
package stress

import (
	"strconv"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/ui"
)

// Colours of the scene. They are exported because the tests identify the row
// plates in the display list by their fill colour, which is the only way to
// recognise them without the test knowing the shape of the tree.
var (
	Background = ui.RGB(18, 20, 26)
	PanelBg    = ui.RGB(30, 34, 44)
	RowBg      = ui.RGB(38, 43, 56)
	RowHot     = ui.RGB(64, 96, 150)
	Line       = ui.Border{Width: 1, Color: ui.RGBA(255, 255, 255, 40)}
	Accent     = ui.RGB(220, 120, 60)
)

// Scene owns the parameters and the handles to the state the input path
// drives.
//
// The state pointers are captured during the build of the component that owns
// them. That is deliberate and not a back door: the state still belongs to its
// scope, and writing it from the update callback happens on the UI executor,
// which is where writes are allowed.
type Scene struct {
	rows, cells int

	// keys are the per row reconciliation keys, formatted once in New so
	// that a build does not produce one string per row per frame.
	keys []string

	frame     int
	highlight *gift.State[int]
	presses   *gift.State[int]
}

// New precomputes everything about the scene that does not change. rows and
// cells are clamped to at least one.
func New(rows, cells int) *Scene {
	if rows < 1 {
		rows = 1
	}
	if cells < 1 {
		cells = 1
	}
	s := &Scene{rows: rows, cells: cells, keys: make([]string, rows)}
	for i := range s.keys {
		s.keys[i] = "row" + strconv.Itoa(i)
	}
	return s
}

// Rows and Cells report the parameters.
func (s *Scene) Rows() int  { return s.rows }
func (s *Scene) Cells() int { return s.cells }

// Tick advances the highlighted row once a second, assuming sixty ticks.
//
// Only two rows change their props, so only two memoised rows are rebuilt.
// The build counter of the measurement output is how you check that.
func (s *Scene) Tick() {
	s.frame++
	if s.highlight == nil {
		return
	}
	s.highlight.Set((s.frame / 60) % s.rows)
}

// Bump increments the counter of the badge component.
//
// The badge has its own scope, so this rebuilds one component and nothing
// else, not the root and not the rows.
func (s *Scene) Bump() {
	if s.presses == nil {
		return
	}
	s.presses.Set(s.presses.Get() + 1)
}

// Root is the root component. It owns the highlighted row index, so a tick
// rebuilds it — which is exactly the situation [gift.Memo] exists for.
func (s *Scene) Root(ctx *gift.Context) gift.View {
	s.highlight = ctx.State("highlight", 0)
	hot := ctx.Read(s.highlight)

	body := make([]gift.View, 0, s.rows)
	for i := 0; i < s.rows; i++ {
		// The props are an explicit comparable value, so a row whose index
		// and highlight state did not change is not rebuilt at all.
		//
		// The key is per row and not the constant "row". Sibling keys have to
		// be unique — the project plan, section 5, asks for stable model keys
		// and a giftdebug build rejects duplicates — because the matcher
		// otherwise falls back to matching the duplicates by their position
		// among each other, which is the positional identity the keys were
		// there to remove. The bug was invisible until this scene got tests
		// of its own, since nothing here ever reorders the rows.
		body = append(body, gift.Memo(s.keys[i], rowProps{
			Index: i,
			Cells: s.cells,
			Hot:   i == hot,
		}, buildRow))
	}

	return ui.VStack(
		s.header(),
		ui.VStack(body...).
			Gap(4).
			Padding(12).
			Background(PanelBg).
			CornerRadius(14).
			Border(Line).
			Clip(true).
			Flex(1),
		s.footer(),
	).
		Gap(12).
		Padding(16).
		Background(Background)
}

// header is a ZStack: a bar with a title block on the left, a spacer and a
// stateful badge on the right, on an accent coloured plate.
//
// The plate is the ZStack's own Background and not a child Box, which is a
// matter of taste and not of necessity: since WU-D a Box is greedy on every
// bounded axis, and a ZStack bounds both, so ZStack(Box().Background(c),
// content) paints the plate behind the content just as well. See [ui.BoxView].
func (s *Scene) header() gift.View {
	return ui.ZStack(
		ui.HStack(
			ui.VStack(
				ui.Box().Frame(160, 14).Background(ui.RGB(250, 250, 250)).CornerRadius(7),
				ui.Box().Frame(110, 8).Background(ui.RGBA(255, 255, 255, 160)).CornerRadius(4),
			).Gap(6),
			ui.Spacer(),
			gift.Component("badge", s.badge),
		).Padding(14).Align(geom.Alignment{Y: 0.5}),
	).
		Background(Accent).
		CornerRadius(10).
		Border(Line).
		Frame(geom.Unbounded(), 72)
}

// badge is a component with a state scope of its own. [Scene.Bump] writes that
// state, which rebuilds this component instance and nothing above it.
func (s *Scene) badge(ctx *gift.Context) gift.View {
	s.presses = ctx.State("presses", 0)
	n := ctx.Read(s.presses)

	pips := make([]gift.View, 0, 8)
	for i := 0; i < 8; i++ {
		c := ui.RGBA(0, 0, 0, 70)
		if i < n%9 {
			c = ui.RGB(255, 255, 255)
		}
		pips = append(pips, ui.Box().Frame(10, 10).Background(c).CornerRadius(5))
	}
	return ui.HStack(pips...).
		Gap(6).
		Padding(10).
		Background(ui.RGBA(0, 0, 0, 90)).
		CornerRadius(12).
		Border(Line)
}

func (s *Scene) footer() gift.View {
	return ui.HStack(
		ui.Box().Frame(90, 10).Background(ui.RGBA(255, 255, 255, 120)).CornerRadius(5),
		ui.Spacer(),
		ui.Box().Frame(40, 10).Background(ui.RGBA(255, 255, 255, 60)).CornerRadius(5),
		ui.Box().Frame(40, 10).Background(ui.RGBA(255, 255, 255, 60)).CornerRadius(5),
	).Gap(8).Padding(8).Align(geom.Alignment{Y: 0.5}).Frame(geom.Unbounded(), 34)
}

// rowProps is the comparable input of a memoised row. Everything the row
// reads is in here, and nothing in here is a slice, a map or a closure.
type rowProps struct {
	Index int
	Cells int
	Hot   bool
}

// buildRow builds one row. It is a plain function of its props and has no
// state, which is what makes the memoisation sound.
func buildRow(_ *gift.Context, p rowProps) gift.View {
	back := RowBg
	if p.Hot {
		back = RowHot
	}

	cells := make([]gift.View, 0, p.Cells+2)
	cells = append(cells, ui.Box().Frame(26, 26).
		Background(tint(p.Index, 0)).
		CornerRadius(13).
		Border(Line))

	for c := 0; c < p.Cells; c++ {
		// A cell is a ZStack, so the scene really does exercise all three
		// stack kinds and an overlapping draw order. Both children carry a
		// frame here because the overlap is the point; an unframed Box would
		// fill the ZStack and hide the one behind it.
		cells = append(cells, ui.ZStack(
			ui.Box().
				Frame(float32(8+(p.Index*7+c*13)%26), 8).
				Background(tint(p.Index, c+1)).
				CornerRadius(4),
			ui.Box().
				Frame(6, 6).
				Background(ui.RGBA(255, 255, 255, 200)).
				CornerRadius(3),
		).
			Align(geom.Alignment{X: 0.5, Y: 0.5}).
			Background(ui.RGBA(0, 0, 0, 60)).
			CornerRadius(6).
			Frame(38, 22).
			Clip(true))
	}
	cells = append(cells, ui.Spacer())

	return ui.HStack(cells...).
		Gap(6).
		Padding(6).
		Align(geom.Alignment{Y: 0.5}).
		Background(back).
		CornerRadius(8).
		Border(Line)
}

// tint is a deterministic colour ramp.
func tint(row, col int) ui.Color {
	h := (row*37 + col*61) % 180
	return ui.RGB(uint8(60+h), uint8(90+(h/2)), uint8(200-h/2))
}
