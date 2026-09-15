// Package stress holds the several hundred node windowed scene that the
// project plan, section 12, "Go/No-Go vor Schritt 2", criterion 2, requires:
// a non trivial static stack scene that a window can hold at sixty frames per
// second on a Raspberry Pi 4.
//
// # Why it is here and not in an example
//
// It used to be the body of the layout example, where it was doing two
// incompatible jobs.
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
	"fmt"
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
	Ink        = ui.RGB(236, 238, 244)
	InkDim     = ui.RGBA(255, 255, 255, 150)

	// PanelShadow and RowShadow are the scene's shadows.
	//
	// They exist because a measurement of a scene without them is a
	// measurement of something else. Until this work unit the scene contained
	// zero ui.Text and zero Shadow, and a run reported glyph_draw_calls 0,
	// shaped_glyphs 0, atlas hits and misses 0 and shadow_ops 0 — so every
	// frame time and allocation figure quoted for step 2 of the project plan
	// was in fact a figure about step 1.
	//
	// The blurs are deliberately modest. A shadow's quad is its shape grown
	// by render.ShadowSigmas times half the blur on every side, so a large
	// blur on forty rows is a fill rate experiment rather than a realistic
	// interface; see backend/ebiten.RendererStats.ShadowOps.
	PanelShadow = ui.Shadow{Blur: 16, OffsetY: 4, Color: ui.RGBA(0, 0, 0, 110)}
	RowShadow   = ui.Shadow{Blur: 6, OffsetY: 2, Color: ui.RGBA(0, 0, 0, 90)}
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

	// font is the embedded typeface every label in the scene is shaped with.
	// It is a fixture property and not a knob; see [Font].
	font ui.Font

	// keys, labels and titles are the per row strings, formatted once in New
	// so that a build does not produce one string per row per frame. The
	// scene's determinism depends on it: the same parameters must produce the
	// same text, and therefore the same shaping cache and atlas behaviour, on
	// every run.
	keys     []string
	labels   []string
	titles   []string
	subtitle string

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
	s := &Scene{
		rows: rows, cells: cells,
		font:   Font(),
		keys:   make([]string, rows),
		labels: make([]string, rows),
		titles: make([]string, rows),
	}
	for i := range s.keys {
		s.keys[i] = "row" + strconv.Itoa(i)
		// A fixed width index, so that every row's label shapes to the same
		// advance and a row's height cannot depend on how many digits its
		// number has. The test that pins "every row is the same height"
		// would otherwise start failing at row 10.
		s.labels[i] = fmt.Sprintf("%03d", i)
		// Deterministic prose from a fixed table. Real text, in the sense
		// that matters here: a few dozen distinct strings, each shaped once
		// and then served from the cache, which is exactly the steady state
		// the allocation contract of the project plan, section 11, is about.
		s.titles[i] = words[i%len(words)] + " " + words[(i*7+3)%len(words)]
	}
	s.subtitle = strconv.Itoa(rows) + " rows x " + strconv.Itoa(cells) + " cells"
	return s
}

// words is the deterministic vocabulary of the row titles. It is a fixed table
// and not a generator, because a measurement has to shape the same strings on
// every run.
var words = [...]string{
	"aperture", "backdrop", "cadence", "diffuse", "envelope", "fixture",
	"gradient", "harmonic", "isotope", "junction", "keystone", "lattice",
	"meridian", "nucleus", "oscillate", "parallax", "quantise", "resonant",
	"spectrum", "threshold",
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
			Label: s.labels[i],
			Title: s.titles[i],
			Font:  s.font,
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
			Shadow(PanelShadow).
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
				// Real text, not a grey bar standing in for it. The title is
				// the one string in the scene large enough to exercise the
				// atlas at a display size.
				ui.Text("Gift stress scene").Font(s.font).FontSize(22).Foreground(Ink),
				ui.Text(s.subtitle).Font(s.font).FontSize(13).Foreground(InkDim),
			).Gap(4),
			ui.Spacer(),
			gift.Component("badge", s.badge),
		).Padding(14).Align(geom.Alignment{Y: 0.5}),
	).
		Background(Accent).
		CornerRadius(10).
		Border(Line).
		Shadow(PanelShadow).
		Frame(geom.Unbounded(), 84)
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
	// The count as text next to the pips. It changes when Bump is called and
	// is therefore the one label in the scene that misses the shaping cache
	// on purpose: a measurement wants both sides of that boundary visible.
	return ui.HStack(
		ui.Text(strconv.Itoa(n)).Font(s.font).FontSize(14).Foreground(Ink),
		ui.HStack(pips...).Gap(6),
	).
		Gap(10).
		Padding(10).
		Align(geom.Alignment{Y: 0.5}).
		AlignBaseline().
		Background(ui.RGBA(0, 0, 0, 90)).
		CornerRadius(12).
		Border(Line).
		Shadow(RowShadow)
}

func (s *Scene) footer() gift.View {
	return ui.HStack(
		ui.Text("space bumps the badge, escape quits").Font(s.font).FontSize(12).Foreground(InkDim),
		ui.Spacer(),
		ui.Text("deterministic").Font(s.font).FontSize(12).Foreground(InkDim),
		ui.Box().Frame(40, 10).Background(ui.RGBA(255, 255, 255, 60)).CornerRadius(5),
	).Gap(8).Padding(8).Align(geom.Alignment{Y: 0.5}).Frame(geom.Unbounded(), 34)
}

// rowProps is the comparable input of a memoised row. Everything the row
// reads is in here, and nothing in here is a slice, a map or a closure.
type rowProps struct {
	Index int
	Cells int
	Hot   bool
	// Label and Title are the row's two strings. They are precomputed by
	// [New] and travel in the props, which keeps buildRow a pure function of
	// a comparable value — a string is comparable, a slice would not be.
	Label string
	Title string
	// Font is a handle to shared immutable tables and is comparable, so it
	// belongs in the props like everything else the row reads.
	Font ui.Font
}

// buildRow builds one row. It is a plain function of its props and has no
// state, which is what makes the memoisation sound.
func buildRow(_ *gift.Context, p rowProps) gift.View {
	back := RowBg
	if p.Hot {
		back = RowHot
	}

	cells := make([]gift.View, 0, p.Cells+4)
	cells = append(cells, ui.Box().Frame(26, 26).
		Background(tint(p.Index, 0)).
		CornerRadius(13).
		Border(Line))
	// Two labels per row: a fixed width index and a title. Together with the
	// header and the footer this is what puts glyphs into the display list at
	// all, and at forty rows it is eighty runs of real shaped text per frame.
	cells = append(cells,
		ui.Text(p.Label).Font(p.Font).FontSize(12).Foreground(InkDim),
		ui.Text(p.Title).Font(p.Font).FontSize(13).Foreground(Ink).Frame(150, geom.Unbounded()),
	)

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
		Border(Line).
		Shadow(RowShadow)
}

// tint is a deterministic colour ramp.
func tint(row, col int) ui.Color {
	h := (row*37 + col*61) % 180
	return ui.RGB(uint8(60+h), uint8(90+(h/2)), uint8(200-h/2))
}
