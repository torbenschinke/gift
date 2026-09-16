package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/render"
)

var (
	listType    = gift.RegisterType("ui.List")
	sectionType = gift.RegisterType("ui.Section")
)

// Metrics of a list, in logical pixels.
const (
	// ListSeparatorInset is the leading inset of the hairline between two
	// rows. It is [RowPadding], so the separator starts where the row's
	// content starts and the two edges line up.
	ListSeparatorInset = RowPadding

	sectionTopPadding    = float32(22)
	sectionBottomPadding = float32(6)
	sectionFontSize      = float32(12)
)

// SectionHeaderView is the caption above a group of rows in a [ListView]. It
// is created by [Section]; the zero value is not useful.
//
// It is a distinct type and not merely a [TextView] with a padding, because
// [ListView] has to be able to *recognise* it: the separator rule is stated in
// terms of which neighbours are rows, and a header is the thing that is not
// one. See [ListView] for the rule and for what happens to a header that is
// wrapped in something else.
type SectionHeaderView struct {
	base
	title string
}

// Section returns a section header for use inside a [List].
func Section(title string) SectionHeaderView { return SectionHeaderView{title: title} }

// ViewType implements gift.View.
func (v SectionHeaderView) ViewType() gift.TypeID { return sectionType }

// Build implements gift.View.
func (v SectionHeaderView) Build(bc *gift.BuildContext) gift.Element {
	return Text(v.title).
		FontSize(sectionFontSize).
		Foreground(ColorSecondaryLabel).
		MaxLines(1).
		PaddingInsets(geom.Insets{
			Top:    sectionTopPadding,
			Right:  RowPadding,
			Bottom: sectionBottomPadding,
			Left:   RowPadding,
		}).
		Key(v.key).
		Flex(v.flex).
		Build(bc)
}

// Key sets the reconciliation key of this view among its siblings.
func (v SectionHeaderView) Key(s string) SectionHeaderView { v.setKey(s); return v }

// Flex makes the header take a share of the remaining main axis space of its
// parent stack. A header wants none.
func (v SectionHeaderView) Flex(f float32) SectionHeaderView { v.setFlex(f); return v }

// ListView is a vertical run of rows with hairlines between them and section
// headers in between. It is created by [List]; the zero value is not useful.
//
//	ui.VScroll(
//		ui.List(
//			ui.Section("Display"),
//			ui.Row("Brightness").Value("60 %"),
//			ui.Row("Dark mode").Accessory(ui.Toggle(dark, setDark)),
//			ui.Section("About"),
//			ui.Row("Version").Value("1.4.0"),
//		),
//	).Flex(1)
//
// # It builds every row, and here is where that stops working
//
// A List is **not** virtualised. Every item handed to it is built, laid out
// and painted, whether or not it is on the screen. [Gallery] in this same
// package recycles tiles and proves the mechanism exists, so this is a
// decision and not a limitation of the framework. Two numbers decide when it
// is the wrong decision, and both were measured rather than estimated; see
// list_bench_test.go.
//
// **One: a rebuild is linear at about 2.8 microseconds per row.** Measured on
// the development machine (Apple M1 Max, Go 1.27) for a frame in which every
// row is dirty, so build, layout and paint all run over the whole list. A row
// here is the ordinary settings row: an icon, a title, a subtitle and a
// trailing value.
//
//	  20 rows    61 µs    3.1 µs/row    331 allocations
//	 100 rows   277 µs    2.8 µs/row   1611 allocations
//	 400 rows   1.04 ms   2.6 µs/row   6411 allocations
//	 700 rows   1.92 ms   2.8 µs/row  11211 allocations
//	1000 rows   2.83 ms   2.8 µs/row  16011 allocations
//
// The figures are with the labels already in gift's shaping cache, which is
// what a rebuild normally is: the strings of a settings screen do not change
// when one switch does. Shaping them for the first time is a separate cost and
// is the subject of the second limit below.
//
// The reference platform of the project plan, section 13, is a Raspberry
// Pi 4, whose Cortex-A72 cores run this kind of pointer-chasing, allocating Go
// code between eight and twelve times slower than that machine. Taking **ten**
// as the factor — an assumption to be re-measured on the target, not something
// this work unit verified — a row costs about 28 µs on the Pi. A rebuild has
// to share its 16.6 ms frame with layout, paint and the backend; allowing it
// half:
//
//	**about 280 rows on a Raspberry Pi 4, and about 2800 on the
//	development machine.**
//
// **Two: there is a hard cliff at roughly 1500 distinct labels, and it is not
// this component's.** gift's shaping cache is a single process wide budget of
// one mebibyte shared by everything on the screen, and gift culls no paint, so
// every *visible* paragraph asks the shaper for its layout on every frame. One
// paragraph past the budget and the whole scene misses on every frame, whether
// or not anything changed. A List is merely the first component able to walk
// off that edge on its own; a stack of labels, a log view or a table reaches it
// the same way. The explanation, the measurement and the remedies are in
// [TextView] and in the package documentation of internal/text, which is where
// a future author will be when they need them.
//
// Applied to this component: with two distinct labels per row the limit is
// **about 750 rows**, or whatever number of rows produces 1500 distinct
// strings. Unlike the first limit it does not scale with the machine — a
// faster CPU shapes the same text faster and still shapes all of it — and rows
// whose labels repeat, a list of statuses out of a fixed vocabulary, do not
// count against it at all, because the cache is keyed on the string.
//
// Below both numbers a List is the right tool and is far simpler than a
// recycler: no slot pool, no binding, no anchor, and no class of defect where
// state from the item that used to be in a slot leaks into the one that is
// there now — which is what review gates 12 and 13 of this project found twice
// in the code that does recycle.
//
// Above them, a List is the wrong tool, and this documentation says so rather
// than degrading quietly. A thousand-row list on a kiosk wants a virtualised
// container, and building one is a work unit of its own with [Gallery] as its
// model.
//
// # The steady state, and the hidden tab
//
// A rebuild only happens when something the list depends on changed.
// Scrolling rebuilds nothing at all — the offset lives in the retained node —
// so the steady state of a visible list below the cliff is layout and paint,
// with no allocation at all. Measured at **0.49 µs per row**, 494 µs for a
// thousand rows on the development machine.
//
// That number gets the same ×10 Pi derate as every other number here, because
// it is the number that is paid most often:
//
//	**about 4.9 ms per frame for a thousand rows on a Raspberry Pi 4 —
//	30 % of the frame budget, every frame, for a list nobody is touching —
//	and about 1.4 ms, 8 %, at the 280 rows recommended above.**
//
// It is flat, it is allocation free and it is unavoidable while the rows are
// on the screen, which is exactly why it belongs in the budget rather than in
// a footnote.
//
// A list in a hidden [TabBarView] tab or under a covered
// [NavigationStackView] screen is not painted, so it pays neither the paint
// nor the cliff. It does still pay its **build and layout**, and that is not
// the relief an earlier version of this paragraph made it sound like.
// Measured on a root rebuild of a two-tab TabBar whose *hidden* tab holds rows
// of the shape above (BenchmarkListInAHiddenTab):
//
//	  0 rows     6.6 µs      57 allocations
//	 20 rows    58.8 µs     380 allocations
//	100 rows   234.8 µs    1660 allocations
//	200 rows   449.7 µs    3260 allocations
//
// A factor of 68 for content nobody can see, which on the Pi is 4.5 ms — 27 %
// of the frame — spent on an invisible tab. The framework cannot fix this:
// gift builds a hidden subtree deliberately, so that a tab keeps its scroll
// offset and its half typed text; see [gift.Element.Hidden].
//
// What fixes it is scoping the *write*. A dependency belongs to the
// [gift.Component] scope that read the state, so a root that reads everything
// is a root that is invalidated by everything and rebuilds every tab. Give
// each screen a component of its own and let it read the slots it shows, and a
// write reaches the one tab that shows it. cmd/example-kitchensink is arranged
// that way and measures the difference: 702 µs and 6255 allocations for one
// tap before, 70 µs and 485 after.
//
// # Separators
//
// A separator is drawn between two consecutive items *only when both of them
// are rows*. That single rule gives the three properties a grouped list needs:
// there is none after the last item, none between a row and a following
// [SectionHeaderView], and none between a header and the row under it — so a
// section boundary is one visual break and never two stacked on top of each
// other.
//
// A separator is a real part of the layout: it takes its thickness out of the
// vertical run, so rows do not overlap it and the height of a list is the sum
// of its items plus one thickness per separator. It is painted by this node
// rather than being a child view, which is what lets it be snapped onto whole
// device pixels — the hairline argument of [DividerView] applies here with
// more force, because a list has many of them at many different fractional
// offsets and a blurry one next to a crisp one is visible at a glance.
//
// It is drawn *after* the children, so a row's pressed face does not cover it.
//
// # What counts as a header
//
// A value whose dynamic type is [SectionHeaderView], and nothing else. A
// header wrapped in anything — a [ZStack], a conditional helper that returns
// gift.View — is an ordinary item as far as the separator rule is concerned
// and gets hairlines around it. That is a real sharp edge and it is stated
// rather than papered over with a reflective search; the alternative,
// inspecting the built element, would move a layout decision into a place
// where the view has already been turned into a node.
//
// # Width
//
// Rows are measured with a *tight* width, so they fill the list, which is what
// makes [RowView]'s trailing group sit at the trailing edge and what makes a
// pressed row's face reach both edges. On an unbounded axis — a List inside an
// [HStack], or an inflexible child of one — there is no width to be tight
// about and the rows shrink-wrap instead.
type ListView struct {
	base
	items        []gift.View
	sep          Color
	hasSep       bool
	lead, trail  float32
	hasSepInset  bool
	sepThickness float32
}

// List returns a list of items, which are normally [RowView]s and
// [SectionHeaderView]s but may be any view at all.
//
// The items slice belongs to gift from this call onwards, exactly like
// [VStack]'s.
func List(items ...gift.View) ListView {
	return ListView{items: items, sepThickness: DividerThickness}
}

// ViewType implements gift.View.
func (v ListView) ViewType() gift.TypeID { return listType }

// Build implements gift.View.
func (v ListView) Build(*gift.BuildContext) gift.Element {
	sep := ColorSeparator
	if v.hasSep {
		sep = v.sep
	}
	lead, trail := ListSeparatorInset, float32(0)
	if v.hasSepInset {
		lead, trail = v.lead, v.trail
	}
	n := &listNode{
		fr:        v.frame,
		st:        v.style.resolved(),
		pad:       v.pad,
		sep:       ResolveColor(sep),
		thickness: v.sepThickness,
		lead:      lead,
		trail:     trail,
		clip:      v.style.clip,
		rule:      separatorRule(v.items),
	}
	return gift.Element{
		Key:      v.key,
		Flex:     v.flex,
		Layouter: n,
		Painter:  n,
		Children: v.items,
		Clip:     v.style.clip,
	}
}

// separatorRule returns, for each item, whether a hairline is drawn under it.
//
// The whole rule of [ListView] in three lines: between two rows, and nowhere
// else. It allocates one bit slice per build, which is a build cost and not a
// frame cost; a list with no separators at all — one item, or nothing but
// headers — allocates nothing, because the loop never has a true to record.
//
// # It is computed from the views, and cannot see [gift.Element.Hidden]
//
// The rule is decided here, before anything is built, so it knows what the
// caller passed in and not what those views turned into. [gift.Element.Hidden]
// is set during Build and deliberately does not skip layout, so a hidden item
// keeps its height and the run of separators keeps counting it as an item.
// Three rows with the middle one hidden come out as two hairlines, at y=44 and
// y=89, bracketing a 45 pixel blank band — two rules where the whole rule of
// this component is "between two rows and nowhere else".
//
// This is stated rather than fixed, and the reason is the same one
// [gift.LayoutContext.OffScreen] gives for itself: deciding a layout from
// whether something is hidden would make the list a different shape depending
// on a flag that is allowed to change without a relayout, and the blank band
// would still be there afterwards, because Hidden keeps the space.
//
// **Do not hide an item of a List. Leave it out of the slice.** The items are
// an ordinary []gift.View built by the caller, so a conditional append is the
// whole of it:
//
//	items := make([]gift.View, 0, 3)
//	items = append(items, ui.Row("Wi-Fi"))
//	if expert {
//		items = append(items, ui.Row("Proxy"))
//	}
//	items = append(items, ui.Row("About"))
//
// That removes the height as well as the two hairlines, which is what "hidden"
// meant in the first place. [gift.Element.Hidden] is for a subtree that has to
// stay mounted to keep its state — a tab, a covered navigation screen — and a
// row of a list is not that.
func separatorRule(items []gift.View) []bool {
	if len(items) < 2 {
		return nil
	}
	var out []bool
	for i := 0; i+1 < len(items); i++ {
		if isSectionHeader(items[i]) || isSectionHeader(items[i+1]) {
			continue
		}
		if out == nil {
			out = make([]bool, len(items)-1)
		}
		out[i] = true
	}
	return out
}

func isSectionHeader(v gift.View) bool {
	_, ok := v.(SectionHeaderView)
	return ok
}

// --- modifiers -------------------------------------------------------------

// SeparatorColor sets the colour of the hairlines, replacing [ColorSeparator].
// [ColorClear] removes them while keeping the space they occupy, which is the
// honest way to hide them: a list whose rows suddenly moved by a pixel each
// when the separators were switched off would be a layout that depends on a
// colour.
func (v ListView) SeparatorColor(c Color) ListView { v.sep, v.hasSep = c, true; return v }

// SeparatorThickness sets the height of the hairlines in logical pixels, and
// with it the space each one takes in the layout. It must be finite and
// positive; see [DividerView.Thickness] for why a thickness below one device
// pixel still draws one.
func (v ListView) SeparatorThickness(f float32) ListView {
	v.sepThickness = DividerView{}.Thickness(f).thickness
	return v
}

// SeparatorInsets shortens the hairlines at their leading and trailing ends,
// replacing the default of [ListSeparatorInset] and zero.
//
// The default lines the separator up with the *text* of a plain row. A list
// whose rows carry leading icons wants more — the icon size and the row's gap
// on top of it — and a list that should look like a table wants zero:
//
//	ui.List(rows...).SeparatorInsets(ui.RowPadding+22+12, 0)
func (v ListView) SeparatorInsets(leading, trailing float32) ListView {
	v.lead = checkPadding("List.SeparatorInsets leading", leading)
	v.trail = checkPadding("List.SeparatorInsets trailing", trailing)
	v.hasSepInset = true
	return v
}

// Padding sets the same inset on all four edges around the run of items. It
// insets the separators with everything else.
func (v ListView) Padding(f float32) ListView { v.setPadding(f); return v }

// PaddingInsets sets the inset per edge, replacing any previous padding.
func (v ListView) PaddingInsets(i geom.Insets) ListView { v.setPaddingInsets(i); return v }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should stay
// free; see [frameSpec] for the precedence.
func (v ListView) Frame(w, h float32) ListView { v.setFrame(w, h); return v }

// MinWidth raises the minimum width of the list, and the maximum with it if
// that is lower; see [frameSpec].
func (v ListView) MinWidth(f float32) ListView { v.setMinWidth(f); return v }

// MinHeight raises the minimum height of the list, and the maximum with it if
// that is lower; see [frameSpec].
func (v ListView) MinHeight(f float32) ListView { v.setMinHeight(f); return v }

// MaxWidth lowers the maximum width of the list, and the minimum with it if
// that is higher; see [frameSpec].
func (v ListView) MaxWidth(f float32) ListView { v.setMaxWidth(f); return v }

// MaxHeight lowers the maximum height of the list, and the minimum with it if
// that is higher; see [frameSpec].
func (v ListView) MaxHeight(f float32) ListView { v.setMaxHeight(f); return v }

// Background fills the bounds behind the rows.
func (v ListView) Background(b Background) ListView { v.setBackgroundSpec(b); return v }

// Border strokes the inside of the bounds after the rows were drawn.
func (v ListView) Border(b Border) ListView { v.setBorder(b); return v }

// Shadow draws a blurred copy of the background shape behind the list.
func (v ListView) Shadow(s Shadow) ListView { v.setShadow(s); return v }

// CornerRadius rounds the background and the border.
func (v ListView) CornerRadius(f float32) ListView { v.setCornerRadius(f); return v }

// Clip confines the rows to the bounds, for paint and for hit testing alike,
// and it confines the separators with them.
//
// The separators need saying because they are not children. gift applies
// [gift.Element.Clip] on the way into a node's subtree and deliberately leaves
// the node's own drawing outside it, so that a shadow is not cut off by the
// shape it belongs to. A list's hairlines went out through that same door:
// measured on a 120 px tall list of ten rows with Clip(true), nine separators
// were emitted, seven of them entirely below the clip rectangle and the
// furthest 285 logical pixels below the bottom edge, drawn over whatever was
// underneath. A parent scroll viewport caught them, which is why this was
// invisible until somebody put a list in a fixed frame — which is precisely
// what this modifier is for.
//
// So a separator is treated as content and not as decoration, and
// [listNode.Paint] puts it inside the same rectangle the rows are in. A
// background, a border and a shadow keep the old behaviour, for the reason
// gift's paintKids gives.
func (v ListView) Clip(b bool) ListView { v.setClip(b); return v }

// Key sets the reconciliation key of this view among its siblings.
func (v ListView) Key(s string) ListView { v.setKey(s); return v }

// Flex makes the list take a share of the remaining main axis space of its
// parent stack. A list inside a [ScrollView] wants none: it is measured with
// an unbounded main axis there and is as tall as its rows.
func (v ListView) Flex(f float32) ListView { v.setFlex(f); return v }

// --- the node ----------------------------------------------------------------

// listNode is the retained half of a [ListView]: the layouter that stacks the
// items and reserves the hairlines, and the painter that draws them.
//
// It is one object for both roles and it carries its own scratch, like every
// other container node in this package, so that a frame which only relayouts
// allocates nothing.
type listNode struct {
	fr          frameSpec
	st          styleSpec
	pad         geom.Insets
	sep         Color
	thickness   float32
	lead, trail float32
	// clip is [ListView.Clip]. gift applies it to the children on its own;
	// this copy is here because the separators are the node's own drawing and
	// therefore outside that clip unless this painter puts them inside it.
	// See [listNode.Paint].
	clip bool

	// rule[i] is true when a hairline is drawn under item i. It is built once
	// per build by [separatorRule] and is nil for a list that has none.
	rule []bool

	origins []geom.Point
	// sepY holds the local top edge of each hairline actually drawn, and
	// nsep how many of them there are. They are written by Layout and read by
	// Paint, which is the same direction every other node in this package
	// works in.
	sepY    []float32
	nsep    int
	density float32
}

// Layout implements gift.Layouter.
//
// A vertical run with no flex: an item's height is its own, the separators
// take their thickness out of the run, and the list is the sum. There is
// deliberately no flexible item — a row that stretched to fill a list would
// change height when a row was added somewhere else, which is not what a list
// is.
func (n *listNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	n.density = ctx.Density()
	k := ctx.ChildCount()
	n.ensure(k)

	cc := n.fr.apply(c)
	inner := cc.Deflate(n.pad)
	// Tight on the cross axis where there is one, unbounded on the main axis;
	// see [ListView] and rule 1 of the overflow model of the project plan,
	// section 7.
	kids := geom.Constraints{
		Min: geom.Sz(0, 0),
		Max: geom.Sz(inner.Max.W, geom.Unbounded()),
	}
	if inner.HasBoundedWidth() {
		kids.Min.W = inner.Max.W
	}

	y := n.pad.Top
	var maxW float32
	n.nsep = 0
	for i := range k {
		s := ctx.Measure(i, kids)
		n.origins[i] = geom.Pt(n.pad.Left, y)
		if s.W > maxW {
			maxW = s.W
		}
		y += s.H
		if i < len(n.rule) && n.rule[i] {
			n.sepY[n.nsep] = y
			n.nsep++
			y += n.thickness
		}
	}
	for i := range k {
		ctx.Place(i, n.origins[i])
	}

	size := geom.Sz(maxW+n.pad.Horizontal(), y+n.pad.Bottom)
	out := cc.Constrain(size)
	// Reported, never hidden; the overflow model of the project plan,
	// section 7, rule 3.
	ctx.ReportOverflow(geom.Sz(clampLow(size.W-out.W), clampLow(size.H-out.H)))
	return out
}

// ensure sizes the scratch buffers, reusing the backing arrays whenever they
// are large enough. sepY is sized to k rather than to k-1 so that one
// allocation covers both and an empty list needs neither.
func (n *listNode) ensure(k int) {
	if cap(n.origins) < k {
		n.origins = make([]geom.Point, k)
		n.sepY = make([]float32, k)
		return
	}
	n.origins = n.origins[:k]
	n.sepY = n.sepY[:k]
}

// Paint implements gift.Painter, in the drawing order of the project plan,
// section 8, with the hairlines between the content and the border.
//
// After the children and not before them, so that a row whose face has gone to
// [ColorControlPressed] does not paint over the separator under it.
//
// # The transparency gate, which a divider does not have
//
// [dividerNode.Paint] deliberately has none, and its argument is quantitative:
// emitting a fully transparent fill costs *one* operation the backend
// discards, and a gate would buy that one operation back at the price of
// putting the painter into the class where an unresolved colour becomes an
// invisible widget instead of a diagnosis.
//
// This function used to borrow that argument, and it does not transfer. A list
// emits one fill per separator, and [ListView.SeparatorColor] with [ColorClear]
// is the documented way to switch separators off. Measured with ColorClear: ten
// rows produce nine transparent fills per frame, two hundred produce 199, a
// thousand produce 999 — three orders of magnitude away from the premise. So
// there is a gate here, and the price the divider refused to pay is not paid
// either: the assertion is the first statement of this function and runs
// whatever the colour is, so an unresolved colour is still a diagnosis and
// never an invisible list. The gate is registered in the inventory of
// TestTheGateInventoryIsComplete and has a case in
// TestEveryVisibilityGateIsGuarded.
//
// # The clip
//
// The separators are emitted under the node's own [gift.Element.Clip] when it
// declared one, which is not what gift does with a node's own drawing; see
// [ListView.Clip] for the 285 pixels of hairline that made the difference
// worth the four extra lines.
func (n *listNode) Paint(ctx *gift.PaintContext) {
	assertResolved(n.sep, "the separator colour of a List")
	b := ctx.Bounds()
	paintBackground(ctx, n.st, b)
	ctx.PaintChildren()
	if !n.sep.IsTransparent() {
		if n.clip {
			ctx.PushClip(ctx.DeviceBounds())
			n.paintSeparators(ctx, b, ctx.DeviceBounds().Min.Y)
			ctx.PopClip()
		} else {
			n.paintSeparators(ctx, b, ctx.DeviceBounds().Min.Y)
		}
	}
	paintBorder(ctx, n.st, b)
}

// paintSeparators emits one snapped hairline per reserved gap.
//
// The device top edge of the list is taken once, outside the loop: it is the
// same for every hairline and asking for it per separator would multiply a
// matrix per row for no gain.
//
// # The origin, which was wrong for a whole work unit
//
// [listNode.Layout] records a hairline's position in the node's *local* space
// — the same space [gift.LayoutContext.Place] takes, counted from the node's
// own top left — while [gift.PaintContext.Bounds] is the node's rectangle in
// the space the operations are emitted in. The two are the same number only
// when the list happens to sit at the top of its parent, which is exactly what
// a list placed directly in a [ScrollView] does. Nested one level deeper — in
// a [CardView] in a scroller, which is the ordinary shape of a grouped list —
// they differ by however far down the list starts, and the separators were
// emitted that far *above* the rows they belong to: measured on
// cmd/example-kitchensink at 900x760, rows at y=342, 395 and 448 with their
// hairlines at y=52 and y=105, drawn through the page header and the theme
// switch. The origin has to be added, and this is the line that adds it.
//
// The local top edge passed to [snapHairline] stays b.Min.Y for the same
// reason it always was: that function measures the offset of the band from the
// node's own edge in order to convert it into device space, and both of its
// arguments must therefore be in one space.
func (n *listNode) paintSeparators(ctx *gift.PaintContext, b geom.Rect, deviceTop float32) {
	if n.nsep == 0 {
		return
	}
	x0 := b.Min.X + n.pad.Left + n.lead
	x1 := b.Max.X - n.pad.Right - n.trail
	if !(x1 > x0) {
		return
	}
	for i := range n.nsep {
		top, th := snapHairline(b.Min.Y+n.sepY[i], n.thickness, b.Min.Y, deviceTop, n.density)
		ctx.Add(render.Op{
			Kind:   render.OpFillRect,
			Bounds: geom.Rc(x0, top, x1, top+th),
			Color:  n.sep,
		})
	}
}
