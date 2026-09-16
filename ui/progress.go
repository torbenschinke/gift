package ui

import (
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

var progressBarType = gift.RegisterType("ui.ProgressBar")

// Metrics and timings of a progress bar.
const (
	// progressHeight is how thick the bar is when nothing else decides. Six
	// logical pixels is thick enough to read the fill against the track from
	// across a room and thin enough to sit under a line of text without
	// becoming the loudest thing on the screen. A caller who wants another
	// one writes [ProgressBarView.Frame].
	progressHeight = float32(6)

	// progressDefaultWidth is how wide a bar makes itself when nothing bounds
	// it; see [sliderDefaultWidth] for the argument, which is the same one.
	progressDefaultWidth = float32(200)

	// ProgressPeriod is how long the indeterminate pill takes to cross the
	// bar once, and progressPillShare how much of the bar it covers.
	//
	// A period much shorter than this reads as agitation rather than as
	// progress; much longer and the user starts to wonder whether it has
	// stopped, which is the one thing an indeterminate indicator exists to
	// answer.
	//
	// It is exported because it is the only way to sample the animation at a
	// known point: a test that wanted to see the pill in three places would
	// otherwise have to hard code this number and go stale silently when it
	// changed. It is a constant and not a modifier, for the reason
	// [ControlAnimation] is one.
	ProgressPeriod    = 1200 * time.Millisecond
	progressPillShare = float32(0.35)

	// progressAnimationWindow is how far ahead an indeterminate bar keeps its
	// repaint enrolment alive. It is re-armed from the painter on every
	// frame; see [ProgressBarView.Indeterminate] and [gift.PaintContext.Animate].
	//
	// It is short on purpose. It is the length of time an indeterminate bar
	// keeps the machine awake after the application takes it out of the tree,
	// and there is no reason for that to be longer than a few frames.
	//
	// "Out of the tree" and not "off the screen". gift has no paint culling:
	// [gift.App.Paint] walks every mounted node, so a bar that has been
	// scrolled out of the viewport is still painted, still re-arms and still
	// holds the backend at its full tick rate while drawing nothing anybody
	// can see. Unmounting it is the only thing that stops it.
	progressAnimationWindow = 100 * time.Millisecond
)

// Default appearance of a progress bar; unresolved, so that it follows a later
// [SetTheme]. See button.go for the full argument.
var (
	defaultProgressTrack = ColorControl
	defaultProgressFill  = ColorAccent
)

// ProgressBarView shows how much of a task is done, or that one is running
// when there is no way to say how much. It is created by [ProgressBar]; the
// zero value is not useful.
//
//	ui.ProgressBar(done / total)
//	ui.ProgressBar(0).Indeterminate()
//
// # Why there is no separate Spinner
//
// The obvious second widget for the indeterminate case is a rotating ring, and
// it is deliberately absent. A ring is an *arc*, and gift has no way to draw
// one: the project plan, section 21, records that the single shader is a
// rounded box SDF with all four vertex attributes taken, that a second shader
// is a second draw call, and that a path primitive would need a side table and
// eight more bytes in [render.Op]. A spinner would therefore have to be either
// a new material, which is the cost section 21 exists to avoid, or a CPU
// rasterised image re-rasterised at every angle, which is worse than either.
//
// An indeterminate *bar*, by contrast, is the determinate bar's own two
// rounded rectangles with one of them in motion. It is one widget with two
// modes rather than two widgets of which one needs renderer work, and it says
// the same thing. When a path primitive exists, a ring becomes a reasonable
// thing to add next to this; it is not a reason to hold up the bar.
//
// # The indeterminate mode never stops, and that costs something
//
// An indeterminate bar re-arms its own repaint enrolment from its painter, so
// it holds the backend at its full tick rate for as long as it is *mounted*.
// Every other animation in this package is bounded and lets the machine go
// back to sleep; see [gift.EventContext.Animate] on why that is the rule. This
// one cannot be, because "no end is known" is its entire message.
//
// Mounted, not visible. gift paints every node in the tree — there is no paint
// culling in [gift.App.Paint] — so scrolling the bar out of the viewport, or
// covering it with a sheet, changes nothing at all: the painter still runs,
// still calls [gift.PaintContext.Animate] and still costs a full frame rate
// for zero visible pixels. The *only* way to stop it is to remove the view
// from the tree.
//
// The consequence is an application defect with a price attached: an
// indeterminate bar left in the tree after the work finished keeps a kiosk at
// sixty frames a second indefinitely. Remove it when the work is done, or
// switch it to a fraction as soon as one is known — do not rely on it being
// scrolled away.
//
// # It is not interactive
//
// A progress bar is not a control: it takes no input, holds no focus and is
// not a hit target, so a tap on it reaches whatever is behind it. That is why
// it has no [ControlHitTarget] floor and is six logical pixels tall — there is
// nothing to hit.
type ProgressBarView struct {
	base
	fraction      float64
	indeterminate bool
	name          string

	tint    Color
	hasTint bool
}

// ProgressBar returns a determinate progress bar at the given fraction, which
// is clamped to the range 0 to 1 for drawing.
//
// The clamp is for drawing only and there is no diagnosis, because a fraction
// slightly above one is the normal arithmetic of a download whose declared
// size was a little short, and a bar that panicked on it would turn a cosmetic
// inaccuracy into a crash.
//
// A fraction that is not a finite number is another matter and *panics*: a NaN
// or an infinity — which is what done/total produces when total turned out to
// be zero — would reach the backend as a NaN vertex position and empty the
// window three layers below the mistake. The panic is raised here, during the
// build and outside the frame path; see [checkFraction].
func ProgressBar(fraction float64) ProgressBarView {
	return ProgressBarView{fraction: checkFraction("ProgressBar", fraction)}
}

// ViewType implements gift.View.
func (p ProgressBarView) ViewType() gift.TypeID { return progressBarType }

// Build implements gift.View.
func (p ProgressBarView) Build(*gift.BuildContext) gift.Element {
	fill := defaultProgressFill
	if p.hasTint {
		fill = p.tint
	}
	f := float32(p.fraction)
	n := &progressNode{
		fr:            p.frame,
		fraction:      clamp01(f),
		indeterminate: p.indeterminate,
		track:         ResolveColor(defaultProgressTrack),
		fill:          ResolveColor(fill),
	}
	return gift.Element{
		Key:      p.key,
		Flex:     p.flex,
		Layouter: n,
		Painter:  n,
		Label:    p.name,
		// No Interactor: see the type documentation. A nil one makes the node
		// transparent to input without affecting anything around it.
	}
}

// progressNode is the retained half of a [ProgressBarView].
//
// It keeps no [gift.ControlState] at all, and that is the point of the design:
// the determinate bar is a pure function of the fraction it was built with,
// and the indeterminate one is a pure function of the clock. Neither has a
// phase that a rebuild could lose.
type progressNode struct {
	fr            frameSpec
	fraction      float32
	indeterminate bool
	track, fill   Color
}

// Layout takes the width it is offered and the thickness of a bar.
func (n *progressNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	cc := n.fr.apply(c)
	w := progressDefaultWidth
	if cc.HasBoundedWidth() {
		w = cc.Max.W
	}
	// The floor is one logical pixel and not [progressHeight], which is the
	// one place where the rule of [controlSize] is not simply "the size it
	// draws at". A bar is a capsule that fills its bounds, so it is still a
	// bar at any positive thickness — `.Frame(90, 3)` is a legitimate dense
	// row and must not be silently fattened to six. What is not a bar is a
	// bar of no height, which draws nothing at all; that is what this
	// excludes, and the overflow it reports says so.
	return controlSize(ctx, cc, geom.Sz(w, progressHeight), geom.Sz(1, 1))
}

// Paint draws the track and then whichever fill the mode calls for.
func (n *progressNode) Paint(ctx *gift.PaintContext) {
	// See [toggleNode.Paint]. There is no transparency gate in this painter,
	// but the fill is skipped when its width is zero and a wrong colour here
	// would be as silent as one behind a gate.
	assertResolved(n.track, "the track of a ProgressBar")
	assertResolved(n.fill, "the fill of a ProgressBar")

	b := ctx.Bounds()
	fillCapsule(ctx, b, n.track)
	if n.indeterminate {
		n.paintIndeterminate(ctx, b)
		return
	}
	if w := b.Width() * n.fraction; w > 0 {
		fillCapsule(ctx, geom.Rc(b.Min.X, b.Min.Y, b.Min.X+w, b.Max.Y), n.fill)
	}
}

// paintIndeterminate draws the travelling pill and keeps the frames coming.
//
// The pill enters from before the left edge and leaves past the right one, and
// is cut to the track on both sides, so it grows in and shrinks out rather
// than appearing whole. The position is a pure function of
// [gift.PaintContext.Now], so two bars on one screen are in step and a test
// that moves the clock sees exactly the frame it asked for.
func (n *progressNode) paintIndeterminate(ctx *gift.PaintContext, b geom.Rect) {
	// The re-arm. It is the first thing, not the last, so that a painter
	// which returns early below still asks for the next frame.
	ctx.Animate(progressAnimationWindow)

	pill := b.Width() * progressPillShare
	t := float32(ctx.Now()%ProgressPeriod) / float32(ProgressPeriod)
	x := b.Min.X - pill + t*(b.Width()+pill)
	r := geom.Rc(x, b.Min.Y, x+pill, b.Max.Y).Intersect(b)
	if r.IsEmpty() {
		return
	}
	fillCapsule(ctx, r, n.fill)
}

// --- modifiers -------------------------------------------------------------

// Indeterminate switches the bar to the mode that says "something is
// happening" without saying how much of it. The fraction is ignored.
//
// Read the cost paragraph of [ProgressBarView] before using it: this is the
// one animation in the package that never ends on its own, and it goes on
// costing a full frame rate while the bar is mounted even if nothing on the
// screen shows it.
func (p ProgressBarView) Indeterminate() ProgressBarView { p.indeterminate = true; return p }

// Tint sets the colour of the filled part, replacing [ColorAccent]. It may be
// a semantic colour and is resolved during build.
func (p ProgressBarView) Tint(v Color) ProgressBarView { p.tint, p.hasTint = v, true; return p }

// Label sets the accessible name of the bar: what is making progress. It
// changes nothing visual and is never drawn; see [gift.Element.Label].
func (p ProgressBarView) Label(v string) ProgressBarView { p.name = v; return p }

// Frame fixes both axes, which is how a bar is made thicker. Pass
// [geom.Unbounded] for an axis that should stay free; see [frameSpec].
func (p ProgressBarView) Frame(w, h float32) ProgressBarView { p.setFrame(w, h); return p }

// MinWidth raises the minimum width of the bar; see [frameSpec].
func (p ProgressBarView) MinWidth(v float32) ProgressBarView { p.setMinWidth(v); return p }

// MaxWidth lowers the maximum width of the bar; see [frameSpec].
func (p ProgressBarView) MaxWidth(v float32) ProgressBarView { p.setMaxWidth(v); return p }

// Key sets the reconciliation key of this view among its siblings.
func (p ProgressBarView) Key(v string) ProgressBarView { p.setKey(v); return p }

// Flex makes the bar take a share of the remaining main axis space of its
// parent stack, which is the usual way to put one in a row.
func (p ProgressBarView) Flex(v float32) ProgressBarView { p.setFlex(v); return p }
