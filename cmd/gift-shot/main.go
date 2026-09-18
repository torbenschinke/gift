// Command gift-shot renders one of gift's example screens to a PNG file.
//
//	go run ./cmd/gift-shot -example kitchensink -tab Settings -theme dark -o settings.png
//	go run ./cmd/gift-shot -example slider -theme light -o slider.png
//	go run ./cmd/gift-shot -list
//
// # Why this exists
//
// A framework whose correctness is only ever asserted structurally ships
// screens that are wrong in ways no structural assertion can see. This project
// has the evidence: fourteen review gates passed over a demo in which the
// theme switch left every memoised tab in the old colours, a nested list drew
// its separators three hundred pixels above its rows and into the page header,
// and a slider drew no knob at all. Every one of those is a statement about
// pixels, every one of them is obvious in a picture, and not one of them is
// visible to an assertion about the shape of a tree. The goldens next to the
// components are the regression half of the answer; this is the other half,
// for the moment before a golden exists — when somebody wants to *look*.
//
// It is not a test binary. It takes flags, it writes a file a person opens,
// and it never asserts anything: a tool that failed would be a tool people
// stop reaching for, and the thing being looked at is precisely the thing no
// assertion has been written for yet.
//
// # Flags
//
//	-example  the scene: "kitchensink", or one of the component scenes; -list prints them
//	-tab      the kitchen sink tab: Home, Settings, List or Form
//	-theme    "light" or "dark"
//	-size     the logical viewport, as WxH; the scene's own size when empty
//	-density  the device density, as a raw platform factor; it is rounded the
//	          way a monitor reading is, so the PNG is size times the rounded
//	          density in physical pixels
//	-o        the output file
//
// # What it renders through
//
// The real backend renderer, into an offscreen image, from the display list of
// a settled frame — which is exactly what [gifttest.Harness.AssertGolden]
// compares. There is no second drawing path here, because a picture produced
// by a path the application does not use would be a picture that can agree
// with the golden while the window shows something else.
//
// Ebitengine's run loop is started and immediately terminated, because reading
// pixels back is only legal inside it: the pinned source says so at
// ebiten.Image.ReadPixels. A window flashes up on a desktop; on a machine with
// no display this command cannot run at all, which is the same limitation the
// golden tests have.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	eb "github.com/hajimehoshi/ebiten/v2"
	backend "github.com/worldiety/gift/backend/ebiten"
	"github.com/worldiety/gift/font/inter"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/gifttest"
	"github.com/worldiety/gift/internal/example"
	"github.com/worldiety/gift/internal/example/components"
	"github.com/worldiety/gift/internal/example/kitchensink"
	"github.com/worldiety/gift/ui"
)

var (
	exampleName = flag.String("example", "kitchensink", "the scene to render; -list prints the names")
	tabName     = flag.String("tab", "Home", "the kitchen sink tab: Home, Settings, List or Form")
	themeName   = flag.String("theme", "light", `the theme: "light" or "dark"`)
	sizeSpec    = flag.String("size", "", "the logical viewport as WxH; the scene's own size when empty")
	density     = flag.Float64("density", 1, "the device density as a raw platform factor")
	out         = flag.String("o", "shot.png", "the file to write")
	listScenes  = flag.Bool("list", false, "print the scene names and exit")
	modal       = flag.Bool("modal", false, "open the kitchen sink's alert before the shot")
	keyboard    = flag.Bool("keyboard", false, "focus the kitchen sink's text field so the on-screen keyboard is up")
	switchTheme = flag.Bool("switch", false, "press the kitchen sink's theme switch before the shot, so that the picture shows the result of a runtime theme change rather than of a theme installed at start-up")
)

func main() {
	flag.Parse()
	if *listScenes {
		fmt.Println("kitchensink")
		for _, n := range components.Names() {
			fmt.Println(n)
		}
		return
	}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gift-shot:", err)
		os.Exit(1)
	}
}

// run does the whole job inside Ebitengine's loop and stops it again.
//
// The error a shot produces travels out through a variable rather than out of
// Update, because an error returned from Update is reported by RunGame as a
// game that failed, and "the tab you asked for does not exist" is not that.
func run() error {
	if err := example.LoadFont(); err != nil {
		return err
	}
	var shotErr error
	g := &shooter{do: func() { shotErr = shoot() }}
	if err := eb.RunGame(g); err != nil {
		return err
	}
	return shotErr
}

// shooter runs one closure inside the loop and terminates it.
type shooter struct{ do func() }

func (g *shooter) Update() error {
	g.do()
	return eb.Termination
}

func (*shooter) Draw(*eb.Image)             {}
func (*shooter) Layout(int, int) (int, int) { return 64, 64 }

// shoot builds the scene, settles it and writes the file.
func shoot() error {
	theme, err := themeByName(*themeName)
	if err != nil {
		return err
	}
	opts, err := sceneOptions(theme)
	if err != nil {
		return err
	}
	if *sizeSpec != "" {
		if opts.Size, err = parseSize(*sizeSpec); err != nil {
			return err
		}
	}
	opts.Density = *density
	opts.Font = ui.MustFont(ui.FontQuery{Family: inter.Family})
	// No background is set here, and there is no field for one any more.
	//
	// There was, and it filled the image with ui.ColorBackground before the
	// scene was rendered. Every golden and every shot this tool produced was
	// therefore a picture of the application *plus a rectangle the
	// application never painted* — in a tool built to end exactly that class
	// of reconstruction. The scenes paint their own window background now;
	// see ui.Window.

	tb := &shotTB{}
	var h *gifttest.Harness
	if err := tb.catch(func() { h = gifttest.New(tb, opts) }); err != nil {
		return err
	}
	defer tb.runCleanup()

	r, err := backend.NewRenderer()
	if err != nil {
		return err
	}
	h.App().SetImages(r.Images())

	if err := tb.catch(func() { arrange(h) }); err != nil {
		return err
	}

	img, err := renderPNG(h, r)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(*out); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	if err := os.WriteFile(*out, img, 0o644); err != nil {
		return err
	}
	fmt.Printf("gift-shot: wrote %s\n", *out)
	return nil
}

// sceneOptions turns the -example flag into harness options.
func sceneOptions(theme ui.Theme) (gifttest.Options, error) {
	if *exampleName == "kitchensink" {
		// The pictures of the Home tab. A scratch directory rather than the
		// user's cache, so that a shot never depends on what an earlier run of
		// the demo left behind.
		dir := filepath.Join(os.TempDir(), "gift-shot", "kitchensink")
		pics, err := kitchensink.Samples(dir)
		if err != nil {
			return gifttest.Options{}, err
		}
		kitchensink.Pictures = pics
		return gifttest.Options{
			Root:  kitchensink.Screen,
			Size:  geom.Sz(900, 760),
			Theme: theme,
		}, nil
	}
	sc, ok := components.Get(*exampleName)
	if !ok {
		return gifttest.Options{}, fmt.Errorf("no scene called %q; try -list", *exampleName)
	}
	return gifttest.Options{View: sc.View, Size: sc.Size, Theme: theme}, nil
}

// arrange puts the scene into the state the flags asked for: the tab, the
// alert, the keyboard. It does so by clicking what a person would click, so
// that a shot cannot show a state the application cannot reach.
func arrange(h *gifttest.Harness) {
	if *exampleName != "kitchensink" {
		return
	}
	kitchensink.App = h.App()
	if *switchTheme {
		// Before the tab is chosen, so that the shot shows what a tab which
		// was *already built* looks like after the switch. That is the whole
		// of the difference between a theme installed at start-up and a theme
		// changed at run time, and it is the difference no structural
		// assertion in this repository could see.
		h.First(gifttest.ByText(switchLabel(h))).Click()
	}
	h.Find(gifttest.ByKey(*tabName).And(gifttest.ByType("ui.Button"))).Click()
	if *modal {
		h.First(gifttest.ByText("Reset everything")).Click()
	}
	if *keyboard {
		h.Find(gifttest.ByKey("name")).Click()
	}
}

// switchLabel is what the theme switch says right now: it offers the theme it
// would move to, so it reads "Dark" while the light theme is in force.
func switchLabel(h *gifttest.Harness) string {
	if ui.CurrentTheme().IsDark() {
		return "Light"
	}
	return "Dark"
}

// renderPNG paints one frame through the real renderer and encodes it.
func renderPNG(h *gifttest.Harness, r *backend.Renderer) ([]byte, error) {
	w, hgt := deviceSize(h)
	dst := eb.NewImage(w, hgt)
	defer dst.Deallocate()

	// Twice: the texture upload budget of the project plan, section 21, is per
	// drawn frame, so the frame in which a painter first asks for an icon or a
	// thumbnail is the frame that requests the upload and the texture is only
	// resident from the next one. A single frame would produce a picture of
	// placeholders — which is exactly what gifttest.Harness.Warm exists for on
	// the golden path. Eight frames, because this demo needs more than one
	// budget's worth of icons; see the -icons trace of cmd/example-kitchensink.
	for range 8 {
		// Nothing is filled in first. A fresh Ebitengine image is transparent
		// black, which is what Ebitengine hands a real window at the top of
		// every Draw, so this file's output is the application and nothing
		// else. A shot with holes in it is a scene that forgot ui.Window, and
		// seeing that is the point.
		r.SetTarget(dst)
		r.BeginFrame(geom.Sz(float32(w), float32(hgt)))
		r.Submit(h.App().Paint())
		r.EndFrame()
	}

	px := make([]byte, 4*w*hgt)
	dst.ReadPixels(px)
	return encodePNG(px, w, hgt)
}

func deviceSize(h *gifttest.Harness) (int, int) {
	d := float64(h.Density())
	s := h.Size()
	return int(float64(s.W)*d + 0.5), int(float64(s.H)*d + 0.5)
}

func themeByName(s string) (ui.Theme, error) {
	switch strings.ToLower(s) {
	case "light":
		return ui.LightTheme(), nil
	case "dark":
		return ui.DarkTheme(), nil
	}
	return ui.Theme{}, fmt.Errorf("no theme called %q; it is light or dark", s)
}

// parseSize reads a WxH viewport.
func parseSize(s string) (geom.Size, error) {
	var w, h float32
	if _, err := fmt.Sscanf(s, "%gx%g", &w, &h); err != nil {
		return geom.Size{}, fmt.Errorf("-size %q: want WxH, for example 900x760", s)
	}
	if !(w > 0) || !(h > 0) {
		return geom.Size{}, fmt.Errorf("-size %q: both edges must be positive", s)
	}
	return geom.Sz(w, h), nil
}

// shotTB is the [gifttest.TB] a command has instead of a *testing.T.
//
// The harness is the only thing in this module that knows how to mount an
// application, settle it and click what a person would click, and it talks to
// a TB. Fatalf must not return — the harness relies on that the way every test
// does — so it panics with a sentinel and [shotTB.catch] turns it back into an
// error at the boundary, which is the same mechanism gifttest's own failure
// tests use.
//
// A gift.App belongs to the goroutine that created it, so everything here
// happens on the one goroutine Ebitengine calls back on.
type shotTB struct {
	cleanup []func()
}

// fatal is what Fatalf panics with.
type fatal struct{ msg string }

func (t *shotTB) Helper() {}

func (t *shotTB) Errorf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gift-shot: "+format+"\n", args...)
}

func (t *shotTB) Fatalf(format string, args ...any) {
	panic(fatal{fmt.Sprintf(format, args...)})
}

func (t *shotTB) Skipf(format string, args ...any) {
	panic(fatal{fmt.Sprintf(format, args...)})
}

func (t *shotTB) Log(args ...any) { fmt.Fprintln(os.Stderr, args...) }

func (t *shotTB) Name() string { return "gift-shot" }

func (t *shotTB) Cleanup(f func()) { t.cleanup = append(t.cleanup, f) }

func (t *shotTB) runCleanup() {
	for i := len(t.cleanup) - 1; i >= 0; i-- {
		t.cleanup[i]()
	}
	t.cleanup = nil
}

// catch runs f and converts a Fatalf into an error.
func (t *shotTB) catch(f func()) (err error) {
	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if fe, ok := r.(fatal); ok {
			err = fmt.Errorf("%s", fe.msg)
			return
		}
		panic(r)
	}()
	f()
	return nil
}

// encodePNG turns the RGBA bytes Ebitengine read back into a PNG.
//
// The bytes are non-premultiplied RGBA in Ebitengine's ReadPixels contract and
// image.RGBA is what image/png encodes, so this is a copy and nothing else.
func encodePNG(px []byte, w, h int) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, px)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
