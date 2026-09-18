package ui

import (
	"fmt"
	"time"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/geom"
	"github.com/worldiety/gift/render"
)

// ScrollBar is the look and the timing of the scroll indicator of a viewport.
// It is accepted by [ScrollView.ScrollBar] and by [GalleryView.ScrollBar]; the
// zero value means [DefaultScrollBar].
//
// # Why it is a decoration of the container and not a view
//
// A scroll bar is not a child. A child of a scroll container is translated by
// the scroll offset — that is the entire mechanism of scrolling, see
// [gift.PaintContext] — so a thumb built as a child would slide off the end of
// its own track at the first wheel notch. And a virtualising container's
// children are a positional tile pool whose indices are its bookkeeping, so
// there is no free slot to put one in. The bar is therefore drawn and grabbed
// by the container itself, out of [gift.ScrollInfo], which already carries
// every number it needs.
//
// # Visibility
//
// The bar is fully opaque while the content is moving — a drag, a fling, a
// wheel — and while the thumb is held or the pointer is over the track. When
// none of that is true it stays for [ScrollBar.Hold] and then fades over
// [ScrollBar.Fade]. Leaving the track counts as movement for that purpose, so
// a bar the pointer rested on for a minute fades out when the pointer leaves
// instead of disappearing in a single frame. It is deliberately not invisible-until-hover: a bar nobody
// can see does not answer the question a bar exists to answer, which is how
// much of the document there is and where in it the user currently is.
//
// The fade needs frames, and frames at the idle tick rate would make it three
// steps rather than a fade. [gift.ScrollIndicatorLinger] is what keeps them
// coming; a Hold plus Fade longer than it ends with a snap instead of a fade.
//
// # What it deliberately has no knob for
//
// The corner radius. The bar is a capsule, always, because the one place a
// second answer would show up is the two view types that must both accept this
// value, and a decoration with a knob that has one sensible setting is a knob
// that lets those two drift apart.
type ScrollBar struct {
	// Hidden turns the indicator off. It is the only way to have no bar: a
	// transparent Thumb or Track selects the default colour rather than
	// invisibility, because "I did not set this field" and "I want this
	// invisible" are the same zero value and the first one is far more
	// common.
	Hidden bool

	// Width is the thickness of the bar in logical pixels, across the scroll
	// axis. Zero takes the default.
	Width float32
	// MinThumb is the shortest the thumb may become, in logical pixels.
	// Without it a hundred thousand item gallery has a thumb well under one
	// pixel long. Zero takes the default.
	MinThumb float32
	// Margin is the gap between the bar and the edges of the viewport, on all
	// sides of the track.
	//
	// It is the one number here that is *not* defaulted from
	// [DefaultScrollBar], and the default is zero: the bar sits flush with
	// the trailing edge. Defaulting it would have made a flush bar
	// inexpressible, because zero would then have meant "I did not set this"
	// — and a style knob with a value nobody can ask for is the shape of
	// modifier this package refuses elsewhere.
	Margin float32

	// Track is the colour of the groove the thumb runs in, and Thumb the
	// colour of the thumb. Both are drawn at the current opacity of the bar.
	//
	// Either may be a semantic colour; it is resolved during build, like
	// every other colour in this package. A transparent value takes the
	// default from [DefaultScrollBar].
	//
	// # ColorClear does not work here, and that is deliberate
	//
	// The fallback runs *after* resolution — it has to, because a semantic
	// colour is transparent until it is resolved and testing first would
	// throw every named colour away — so [ColorClear], which resolves to
	// transparency, lands in the fallback and comes back as the default. A
	// scroll bar with an invisible track is therefore not expressible.
	//
	// This is inconsistent with [ButtonStyle], where ColorClear is the
	// documented way to say "nothing", and the inconsistency is kept rather
	// than removed. The reason is that the two fields do not have the same
	// job. A button face is content: a borderless, fill-less button is a
	// perfectly ordinary design and the library must not stand in its way. A
	// scroll bar is a control whose entire purpose is to be findable by eye
	// while the content moves, and "half of it invisible" is a look with no
	// use that a zero value would hand out by accident. A caller who really
	// wants no groove has the better tool already: [ScrollBar.Width] and the
	// indicator timings shape the bar, and a viewport that wants no bar at
	// all says so with [ScrollView.ScrollBar] rather than by painting one in
	// nothing.
	//
	// Fixing it the other way — an explicit "was this set" bit — would mean
	// the same exported-struct problem [ButtonStyle] documents, and it would
	// buy a look nobody has asked for.
	Track Color
	Thumb Color

	// Hold is how long the bar stays fully visible after the content stopped
	// moving, and Fade how long it then takes to disappear. Zero takes the
	// defaults; a negative value is rejected, because a negative duration
	// here is an arithmetic accident rather than an intent.
	//
	// Both are defaulted from [DefaultScrollBar], unlike Margin above, and
	// the asymmetry is the same rule applied twice rather than an oversight:
	// zero means "I did not set this" for every field whose default is not
	// itself zero, and it can mean "I want zero" only for a field whose
	// default is. A bar that vanishes the instant the content stops is still
	// expressible — a Hold and a Fade of one nanosecond are both over inside
	// the frame that follows — so nothing is lost, and a zero value
	// [ScrollBar] keeps meaning [DefaultScrollBar] as this type promises.
	Hold time.Duration
	Fade time.Duration
}

// DefaultScrollBar is the style a viewport uses when none was set.
//
// The two colours are the label colour at eight and fifty one percent, and
// they are written as a [Fade] of [ColorLabel] rather than as literals for a
// reason that only shows up in the dark theme: a bar that is always black is
// invisible on a dark background, which is the one place a scroll bar has to
// be found by eye. Under the light theme the fade produces exactly the
// RGBA(0, 0, 0, 20) and RGBA(0, 0, 0, 130) this variable used to hold, so the
// committed goldens do not move.
//
// Thin enough not to cover content and thick enough to be a target: eight
// logical pixels is above the smallest comfortable pointer target for a one
// dimensional drag, which is what the thumb is.
//
// Hold plus Fade has to finish inside [gift.ScrollIndicatorLinger], or the
// last part of the fade is drawn at the idle tick rate and the bar snaps away
// instead. 750 against 900 ms leaves nine frames of margin at sixty hertz, and
// TestDefaultScrollBarFitsInsideTheIndicatorLinger fails if a change here eats
// it.
var DefaultScrollBar = ScrollBar{
	Width:    8,
	MinThumb: 28,
	Margin:   0,
	Track:    Fade(ColorLabel, 20.0/255),
	Thumb:    Fade(ColorLabel, 130.0/255),
	Hold:     500 * time.Millisecond,
	Fade:     250 * time.Millisecond,
}

// withDefaults fills the zero fields from [DefaultScrollBar] and rejects the
// values that have no reading. It runs during build, never in the frame path.
func (b ScrollBar) withDefaults() ScrollBar {
	switch {
	case !isFinite(b.Width) || b.Width < 0:
		panic(fmt.Sprintf("gift/ui: ScrollBar.Width(%v) must be a finite, non negative number", b.Width))
	case !isFinite(b.MinThumb) || b.MinThumb < 0:
		panic(fmt.Sprintf("gift/ui: ScrollBar.MinThumb(%v) must be a finite, non negative number", b.MinThumb))
	case !isFinite(b.Margin) || b.Margin < 0:
		panic(fmt.Sprintf("gift/ui: ScrollBar.Margin(%v) must be a finite, non negative number", b.Margin))
	case b.Hold < 0 || b.Fade < 0:
		panic(fmt.Sprintf("gift/ui: ScrollBar.Hold(%v)/Fade(%v) must not be negative", b.Hold, b.Fade))
	}
	if b.Width == 0 {
		b.Width = DefaultScrollBar.Width
	}
	if b.MinThumb == 0 {
		b.MinThumb = DefaultScrollBar.MinThumb
	}
	// Resolution before the fallback, and in that order: a semantic colour
	// has an alpha of zero until it is resolved, so asking IsTransparent
	// first would throw away every named colour a caller passed in.
	b.Track = ResolveColor(b.Track)
	b.Thumb = ResolveColor(b.Thumb)
	if b.Track.IsTransparent() {
		b.Track = ResolveColor(DefaultScrollBar.Track)
	}
	if b.Thumb.IsTransparent() {
		b.Thumb = ResolveColor(DefaultScrollBar.Thumb)
	}
	if b.Hold == 0 {
		b.Hold = DefaultScrollBar.Hold
	}
	if b.Fade == 0 {
		b.Fade = DefaultScrollBar.Fade
	}
	return b
}

// --- geometry ----------------------------------------------------------------

// scrollBarGeom is the resolved pair of rectangles of one bar, in whatever
// space the bounds it was computed from lived in.
//
// It is a value and is returned by value. The whole point is that neither the
// painter nor the interactor keeps any of it between frames: the track and the
// thumb are a pure function of the container's bounds and its [gift.ScrollInfo],
// so there is no cached geometry that can be stale after a resize.
type scrollBarGeom struct {
	track geom.Rect
	thumb geom.Rect
	// travel is how far the thumb's leading edge can move along the axis,
	// that is the track extent minus the thumb extent. It is the
	// denominator that turns a thumb position back into a document offset.
	travel float32
	// horizontal mirrors the axis, so that callers do not have to carry the
	// ScrollInfo around beside this value.
	horizontal bool
}

// along returns the component of p along the axis of the bar.
func (g scrollBarGeom) along(p geom.Point) float32 {
	if g.horizontal {
		return p.X
	}
	return p.Y
}

// leading returns the leading edge of r along the axis of the bar.
func (g scrollBarGeom) leading(r geom.Rect) float32 {
	if g.horizontal {
		return r.Min.X
	}
	return r.Min.Y
}

// radius is the corner radius of the capsule: half the thickness.
func (g scrollBarGeom) radius() float32 {
	if g.horizontal {
		return g.track.Height() / 2
	}
	return g.track.Width() / 2
}

// geometry resolves the bar over bounds, or reports false when there is
// nothing to show.
//
// There is nothing to show in three cases, and all three are silence rather
// than a diagnosis: the bar is off, the content fits in the viewport so there
// is no position to indicate, and the viewport is so short that the thumb
// would fill the whole track and could not move. The last one is the honest
// answer for a forty pixel high scroller — an indicator that cannot indicate
// anything is worse than none, because it looks like a bar that is stuck.
func (b ScrollBar) geometry(bounds geom.Rect, info gift.ScrollInfo) (scrollBarGeom, bool) {
	var g scrollBarGeom
	if b.Hidden || !(info.MaxOffset > 0) || !(info.ContentExtent > 0) {
		return g, false
	}
	g.horizontal = info.Axis == gift.ScrollHorizontal
	m := b.Margin
	if g.horizontal {
		g.track = geom.Rc(bounds.Min.X+m, bounds.Max.Y-m-b.Width, bounds.Max.X-m, bounds.Max.Y-m)
	} else {
		g.track = geom.Rc(bounds.Max.X-m-b.Width, bounds.Min.Y+m, bounds.Max.X-m, bounds.Max.Y-m)
	}
	if g.track.IsEmpty() {
		return g, false
	}

	trackLen := g.track.Height()
	if g.horizontal {
		trackLen = g.track.Width()
	}
	// The thumb is as long a share of the track as the viewport is of the
	// document, which is the one property that makes a scroll bar readable as
	// "how much of this is on screen". MinThumb overrides it for documents
	// where that share is a fraction of a pixel.
	thumbLen := trackLen * float32(float64(info.ViewportExtent)/info.ContentExtent)
	if thumbLen < b.MinThumb {
		thumbLen = b.MinThumb
	}
	g.travel = trackLen - thumbLen
	if !(g.travel > 0) {
		return g, false
	}

	frac := float32(info.Offset / info.MaxOffset)
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	pos := g.travel * frac
	if g.horizontal {
		g.thumb = geom.Rc(g.track.Min.X+pos, g.track.Min.Y, g.track.Min.X+pos+thumbLen, g.track.Max.Y)
	} else {
		g.thumb = geom.Rc(g.track.Min.X, g.track.Min.Y+pos, g.track.Max.X, g.track.Min.Y+pos+thumbLen)
	}
	return g, true
}

// opacity is how visible the bar is right now, in [0, 1].
//
// active covers everything the container cannot know about: the thumb being
// held, and the pointer resting on the track. Both are state of the indicator
// rather than of the scroll offset, which is why they are a parameter and not
// read out of the ScrollInfo.
func (b ScrollBar) opacity(info gift.ScrollInfo, active bool) float32 {
	if active || info.Dragging || info.Flinging {
		return 1
	}
	idle := info.IdleFor
	if idle <= b.Hold {
		return 1
	}
	// Fade is positive after withDefaults — see the field documentation for
	// why zero is the default rather than an instant hide — so this division
	// is safe and there is no branch here for a zero fade.
	if idle >= b.Hold+b.Fade {
		return 0
	}
	return 1 - float32(idle-b.Hold)/float32(b.Fade)
}

// scale multiplies a premultiplied colour by k. Premultiplied is what makes
// this one multiplication per channel rather than a conversion; see
// [render.Color].
func scaleColor(c Color, k float32) Color {
	return Color{R: c.R * k, G: c.G * k, B: c.B * k, A: c.A * k}
}

// --- the retained half --------------------------------------------------------

// scrollBarState is the retained half of one container's indicator.
//
// It holds the resolved style and nothing else. The two pieces of state a bar
// really has — whether its thumb is held, and where inside the thumb it was
// taken hold of — live in gift's node payload as a [gift.ScrollIndicatorState]
// instead, next to the scroll offset they drive.
//
// # Why they are not here
//
// Because this object does not live long enough. A build constructs a fresh ui
// node and gift installs it, so a component that rebuilds itself — from a
// timer, from a [gift.App.Post], from any of the framework's own asynchronous
// patterns — while the user is holding the thumb would drop the grab. The
// consequence is worse than losing the gesture: the pointer capture and the
// drag flag are the dispatcher's and survive, so the next move would fall
// through [node.HandleEvent] into [gift.ScrollInteractor] and be read as a
// *content* drag, which moves the content the opposite way from the thumb the
// user is still holding. gift keeps the offset across a rebuild on purpose;
// the grab that writes that offset is state of the same gesture and now gets
// the same lifetime. See [gift.ScrollIndicatorState].
type scrollBarState struct {
	// style is resolved once per build, so the frame path reads numbers and
	// never defaults them. It is genuinely a property of the view and is
	// rebuilt with it, which is why it stays on this side.
	style ScrollBar
}

// paint draws the track and the thumb of the container being painted.
//
// It is called after the children and after the border, so the bar is on top
// of the content, which is the only place an overlay indicator can be. It
// emits at most two operations and allocates nothing.
func (st *scrollBarState) paint(ctx *gift.PaintContext) {
	if st.style.Hidden {
		return
	}
	info, ok := ctx.ScrollInfo()
	if !ok {
		return
	}
	ind, _ := ctx.ScrollIndicator()
	a := st.style.opacity(info, ind.Active())
	if a <= 0 {
		return
	}
	g, ok := st.style.geometry(ctx.Bounds(), info)
	if !ok {
		return
	}
	r := g.radius()
	ctx.Add(render.Op{
		Kind:         render.OpFillRoundRect,
		Bounds:       g.track,
		Color:        scaleColor(st.style.Track, a),
		CornerRadius: r,
	})
	ctx.Add(render.Op{
		Kind:         render.OpFillRoundRect,
		Bounds:       g.thumb,
		Color:        scaleColor(st.style.Thumb, a),
		CornerRadius: r,
	})
}

// handleEvent gives the indicator first refusal on a pointer event and reports
// whether it took it.
//
// A container calls this before delegating to [gift.ScrollInteractor], and the
// order is the whole of the interaction between the two. A grabbed thumb
// consumes every move, which stops the event bubbling further — see
// [gift.Interactor] — and therefore stops the viewport from recognising the
// same movement as a content drag and calling StealPointer on it. Without that
// the two would fight over one pointer and the content would move twice per
// pixel, in opposite directions.
//
// Positions are compared in device space, because that is the space
// [gift.Event.Pos] is in; see [gift.EventContext.DeviceBounds].
func (st *scrollBarState) handleEvent(ctx *gift.EventContext, e gift.Event) bool {
	if st.style.Hidden {
		return false
	}
	ind, ok := ctx.ScrollIndicator()
	if !ok {
		return false
	}
	switch e.Kind {
	case gift.EventPointerDown:
		return st.press(ctx, ind, e)

	case gift.EventPointerMove:
		if ind.Grabbed {
			st.drag(ctx, ind, e)
			return true
		}
		return st.updateHover(ctx, ind, e.Pos)

	case gift.EventPointerUp, gift.EventPointerCancel:
		if !ind.Grabbed {
			return false
		}
		ind.Grabbed = false
		ctx.SetScrollIndicator(ind)
		// Consumed: this release ends the gesture this handler owned, and a
		// viewport that also acted on it would read a fling velocity out of
		// samples it never collected.
		return true

	case gift.EventPointerLeave:
		// The pointer left the container, so it is off the track whatever the
		// last move said. The write also restarts the idle clock, which is
		// what turns this into a fade instead of a snap; see
		// [gift.EventContext.SetScrollIndicator].
		ind.Hover = false
		ctx.SetScrollIndicator(ind)
		return false
	}
	return false
}

// scrollBarTouchSlop is how far outside the drawn thumb a press still counts
// as a press on the thumb, in logical pixels.
//
// A thumb is [DefaultScrollBar].Width wide, which is eight, and eight pixels is
// a mouse target and not a fingertip: both Apple and Google document forty four
// and forty eight for a finger. The slop makes the grab region twenty four
// across, of which sixteen are over the viewport, without making the *drawn*
// bar any fatter — an indicator that is thick enough to be grabbed by a finger
// would cover the content it is indicating.
//
// It is the smallest number that does the job rather than the largest that
// would still look reasonable, because every pixel of it is a pixel where an
// ordinary content drag near the trailing edge becomes a thumb drag instead;
// see [TestAContentDragNearTheTrailingEdgeStillScrollsTheContent].
const scrollBarTouchSlop = float32(8)

// press takes the thumb, or pages the document towards a click on the track.
//
// # A faded bar can still be grabbed, and why that had to change
//
// It could not, and the sentence that said so — "only a visible bar is a
// target, a faded one is woken by hovering it" — was written for a pointer
// this framework's target hardware does not have. There is no hover on a
// touchscreen: the kiosk of the project plan, section 1, delivers a press and
// nothing before it. The bar holds for [ScrollBar.Hold] and fades over
// [ScrollBar.Fade], so on a finger-driven panel the thumb was reachable only
// within 750 ms of the content last moving, and after that a press on the
// thumb scrolled the content the *other* way as an ordinary drag. The thumb of
// a hundred thousand item gallery — the one place a scroll bar earns its
// keep — was in practice not grabbable at all.
//
// So the rule is now the one a touch device needs: a press within
// [scrollBarTouchSlop] of the thumb takes the thumb whatever the bar's current
// opacity, and taking it wakes the bar, because a grabbed indicator is an
// active one. Nothing else about a faded bar is a target — paging on a track
// nobody can see is refused below — so the region a content drag loses near
// the trailing edge is the length of the thumb and sixteen pixels of width,
// and no more. The mouse keeps the behaviour it had: hovering still wakes the
// bar, and a woken bar still pages.
//
// # Paging, and why one viewport
//
// A click on the track moves the content one viewport extent towards the
// click, once, and does not repeat while the button is held. One viewport is
// the distance that keeps a sliver of the previous screen on the new one for
// nothing — the reader's eye lands where it left off — and it is what Page Up
// and Page Down mean everywhere else in this application, so the mouse and the
// keyboard agree. Jumping straight to the clicked position is the other common
// choice and is rejected here because it makes a mis-click a loss of place in
// a hundred thousand item gallery; a user who wants that drags the thumb,
// which is one gesture away and is reversible while it is happening.
//
// Auto-repeat on hold is left out deliberately: it needs a timer in the input
// path for a gesture whose whole job the thumb already does better.
func (st *scrollBarState) press(ctx *gift.EventContext, ind gift.ScrollIndicatorState, e gift.Event) bool {
	info, ok := ctx.ScrollInfo()
	if !ok {
		return false
	}
	g, ok := st.style.geometry(ctx.DeviceBounds(), info)
	if !ok {
		return false
	}
	awake := st.style.opacity(info, ind.Active()) > 0
	if g.thumb.Inset(geom.InsetsAll(-scrollBarTouchSlop)).Contains(e.Pos) {
		// The press has to be taken away from whatever is underneath — a tile
		// in the gallery — or that node would see the release as a click and
		// select a picture the user was only scrolling past. The answer is
		// honoured: if the steal fails there is no gesture to own.
		if !ctx.StealPointer() {
			return false
		}
		ind.Grabbed = true
		ind.Grab = g.along(e.Pos) - g.leading(g.thumb)
		// Grabbed makes the indicator active, so the write below both takes
		// the grab and wakes a bar that had faded out; see
		// [gift.ScrollIndicatorState.Active] and
		// [gift.EventContext.SetScrollIndicator].
		ctx.SetScrollIndicator(ind)
		return true
	}
	if !awake || !g.track.Contains(e.Pos) {
		// A faded bar pages nothing. The thumb above is a small, findable
		// place a finger aims at; the rest of the track is most of the
		// trailing edge of the viewport, and a press there that jumped the
		// document by a screen would turn every content drag started near the
		// edge into a page. Falling through is the content drag.
		return false
	}

	page := float64(info.ViewportExtent)
	if g.along(e.Pos) < g.leading(g.thumb) {
		page = -page
	}
	ctx.ScrollBy(page)
	// Consumed either way. A click that landed on the track is a click on the
	// indicator even when the content was already at the end of its travel,
	// and letting it fall through would select whatever was behind it.
	return true
}

// drag maps the thumb's new leading edge back onto a document offset.
//
// The arithmetic is the inverse of the one in [ScrollBar.geometry] and is done
// in float64 against MaxOffset, so a document tens of millions of pixels tall
// still moves by the pixel the pointer moved; the project plan, section 10,
// requires that conversion order and [gift.ScrollInfo] supplies both numbers in
// float64 for it.
func (st *scrollBarState) drag(ctx *gift.EventContext, ind gift.ScrollIndicatorState, e gift.Event) {
	info, ok := ctx.ScrollInfo()
	if !ok {
		return
	}
	g, ok := st.style.geometry(ctx.DeviceBounds(), info)
	if !ok {
		return
	}
	frac := (g.along(e.Pos) - ind.Grab - g.leading(g.track)) / g.travel
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	ctx.ScrollTo(float64(frac) * info.MaxOffset)
}

// updateHover follows the pointer over the track and reports false always: a
// hover is an observation, not a claim on the event. Consuming it would stop
// it bubbling to an outer container that wants it for its own indicator.
//
// A change is written through [gift.EventContext.SetScrollIndicator], which
// also restarts the idle clock. That is deliberate on both edges: entering the
// track wakes a faded bar, and leaving it gives the bar its full hold and fade
// rather than the abrupt disappearance of an idle counter that kept running
// under a pointer that was resting on it.
func (st *scrollBarState) updateHover(ctx *gift.EventContext, ind gift.ScrollIndicatorState, p geom.Point) bool {
	on := false
	if info, ok := ctx.ScrollInfo(); ok {
		if g, ok := st.style.geometry(ctx.DeviceBounds(), info); ok {
			on = g.track.Contains(p)
		}
	}
	ind.Hover = on
	ctx.SetScrollIndicator(ind)
	return false
}
