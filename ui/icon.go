package ui

import (
	"math"
	"sync/atomic"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/internal/icon"
	"github.com/torbenschinke/gift/render"
)

var iconType = gift.RegisterType("ui.Icon")

// --- the symbol --------------------------------------------------------------

// Symbol is one icon's geometry: pre-parsed outlines, ready to be rasterised
// at whatever size and density they are drawn at.
//
// It is a value, not a picture. There is no bitmap in it and no texture behind
// it; both are produced on demand by [Icon], keyed by the size in *device*
// pixels, and cached. A Symbol is therefore free to copy, free to store in a
// package level variable and free to pass around, which is what makes the
// generated packages — see gift/icon/outline — a list of variables and an init
// that does no work worth measuring.
//
// The zero Symbol draws nothing and is the "no icon" value. [IconView] accepts
// it and leaves its space blank rather than panicking, because a symbol chosen
// by a table lookup that missed is an ordinary thing for an application to
// have.
//
// # Why this type is in ui
//
// The same reason [Font] is: the packages that carry the data — gift/icon/*,
// like gift/font/* — sit *above* ui in the dependency order of the project
// plan, section 3, and can only be reached by an application's side effect
// free import. They need a type to declare their variables as, and it has to
// be one they can import. internal/icon, which holds the rasteriser and the
// byte format, cannot be it.
type Symbol struct {
	// data is the encoded geometry; see internal/icon.
	data []byte
	// box is the edge of the icon's square viewBox in its own units.
	box float32
	// id is what the mask cache is keyed by.
	//
	// A process wide counter rather than a hash of the bytes or the address
	// of the slice. A hash would cost a walk of the data at init for every
	// icon in a package, of which there are hundreds, and comparing slice
	// addresses is not something a Go program may do. The counter is assigned
	// once, during package initialisation, and is then a plain integer in a
	// comparable struct key — which is what keeps the cache hit path to one
	// map lookup.
	id uint32
}

// nextSymbolID hands out [Symbol.id].
//
// Atomic because a generated package's variable initialisers run during init,
// and while Go initialises one package's variables sequentially, two packages
// of icons are two initialisations the specification does not order.
var nextSymbolID atomic.Uint32

// NewSymbol wraps encoded icon geometry produced by cmd/gift-icongen.
//
// box is the edge length of the icon's square viewBox in its own units, 24 for
// the Flowbite corpora. data is borrowed and never modified; the generated
// packages hand it a window into one embedded blob, so a package of three
// hundred icons holds three hundred slice headers and one byte slice.
//
// It is exported for the generator's output and not really for hand written
// calls, but it is honest to make that possible: the format is documented in
// internal/icon and an application with its own icon pipeline may target it.
// A caller that hands it nonsense gets a symbol that fails to rasterise and
// therefore draws nothing, which is the same outcome as an empty one.
func NewSymbol(data []byte, box float32) Symbol {
	if len(data) == 0 || !(box > 0) {
		return Symbol{}
	}
	return Symbol{data: data, box: box, id: nextSymbolID.Add(1)}
}

// IsZero reports whether s carries no geometry.
func (s Symbol) IsZero() bool { return s.id == 0 }

// --- the mask cache ----------------------------------------------------------

// iconKey identifies one rasterised icon: the symbol and the edge of its mask
// in device pixels.
//
// The density is in the key by being folded into the pixel size, exactly as
// the glyph atlas folds it into its size field: a 20 pixel icon on a 2x
// display is a 40 pixel entry with a 40 pixel mask, not a 20 pixel entry
// stretched. That is the whole of the project plan, section 18, on this side,
// and it is what makes an icon crisp rather than merely larger.
type iconKey struct {
	sym uint32
	px  int32
}

// iconService is the process wide mask cache and texture table of [IconView].
//
// # Why this is not the glyph atlas
//
// The project plan, section 21, says the existing glyph atlas is reused rather
// than rebuilt, and section 14 excludes an own atlas engine. Neither is
// violated here, and the reason is worth stating plainly rather than
// asserting: backend/ebiten.GlyphAtlas is not a general coverage cache. Its
// key is glyphKey{render.FontID, size, render.GlyphID}; its miss path calls
// text.Lookup on the font id, refuses a lookup that finds no *text.Font, and
// then calls text.Rasterizer.Glyph on an outline from that font. An icon has
// no font, no glyph id and no outline in a font table. Putting one through
// that path means a fake FontID, a fake GlyphID, a second registry to map the
// pair back to a symbol, and a branch in the miss path of the hottest cache in
// the renderer — for a workload whose shelf packing assumption, "every glyph
// of one font at one size has almost the same height", icons do not share.
//
// So icons go through [render.Images], which is the service that already
// exists for exactly this shape of problem: CPU pixels in, a durable handle
// out, admission controlled per drawn frame, evicted by age and by byte
// budget, and with a generation that makes an evicted handle detectable rather
// than wrong. This file is thirty lines of cache on top of it and not a second
// atlas engine. The cost, stated: one texture per icon per size instead of a
// shared page, so an interface with two hundred distinct icons on screen
// issues two hundred image operations that cannot batch. Text cannot afford
// that and icons can — there are tens of them on a screen, not thousands.
//
// # The qualification the generation needs
//
// "Detectable rather than wrong" holds for *one* [render.Images]. This cache
// is a package variable and the service is whichever one the current
// [gift.PaintContext] carries, so a handle minted against one App's texture
// cache and resolved against another's is neither stale nor right: the
// generations belong to different counters and a match between them means
// nothing. Two Apps in one process would therefore draw each other's pictures.
//
// It is left that way, deliberately and with the same reasoning the project
// plan, section 13, applies to the process wide shaper: gift supports one
// window and one App, nothing else in the package is arranged for a second
// one — [imageService] in image.go has exactly this shape, [SetDefaultFont],
// [SetTheme] and [SetClipboard] are process wide by design — and the fix is
// not a lock or a key of two parts. It is a service per App, and that is a
// change to the layout and paint contracts, which hand a layouter a
// *LayoutContext and a painter a *PaintContext and no application handle. The
// trigger to do it is the same one section 13 names: the first time a second
// App is really wanted. Until then this comment is the honest statement of
// what the generation does and does not promise, and a silent qualification
// would have been the defect.
//
// It belongs to the UI executor and is not safe for concurrent use, like
// [imageService] next to it and with the same caveat about t.Parallel that the
// project plan, section 13, already records.
type iconService struct {
	rast *icon.Rasterizer
	mask icon.Mask
	tex  map[iconKey]texEntry
	// staging is the reused RGBA upload buffer. Coverage is one byte and
	// render.Pixels is premultiplied RGBA, so the mask is expanded into all
	// four channels — premultiplied white — which multiplied by the
	// premultiplied tint of the operation is that colour at that coverage.
	// backend/ebiten.GlyphAtlas.upload does the identical thing for glyphs.
	staging []byte
	// blank records symbol sizes that rasterised to nothing, so that an icon
	// which produces no ink is not re-rasterised on every frame. It is the
	// same cached negative the glyph atlas keeps for a space.
	blank map[iconKey]bool

	rasterised, uploads uint64
}

var icons = &iconService{
	rast:  icon.NewRasterizer(),
	tex:   make(map[iconKey]texEntry, 64),
	blank: make(map[iconKey]bool, 16),
}

// resolve returns the operation level id of the mask of s at px device pixels,
// rasterising and uploading it on a miss.
//
// It takes the [render.Images] rather than the [gift.PaintContext] it came out
// of, so that the cache can be exercised without a graphics context — see
// icon_test.go, which is how "the same icon in two colours comes from one
// mask" is measured rather than asserted.
//
// This is the frame path. The warm case is one map lookup here and two integer
// comparisons inside the backend, and allocates nothing;
// BenchmarkIconFramePathIsAllocationFree is the measurement.
func (s *iconService) resolve(im render.Images, sym Symbol, px int) (render.ImageID, bool) {
	if sym.IsZero() || px <= 0 {
		return 0, false
	}
	k := iconKey{sym: sym.id, px: int32(px)}
	if im == nil {
		// Headless. Every icon falls back to drawing nothing, which is what a
		// layout test and a gifttest harness without a graphics context want;
		// the structural assertions still see the view, its bounds and its
		// label.
		return 0, false
	}
	if e, ok := s.tex[k]; ok {
		if id, ok := im.Resolve(e.h); ok {
			return id, true
		}
		// Evicted between two frames by the backend's age or byte budget. The
		// generation in the handle is what made that this branch rather than
		// a wrong picture.
		delete(s.tex, k)
	}
	if s.blank[k] {
		return 0, false
	}
	if !s.rast.Rasterize(sym.data, sym.box, px, &s.mask) {
		s.blank[k] = true
		return 0, false
	}
	s.rasterised++
	n := px * px * 4
	if cap(s.staging) < n {
		s.staging = make([]byte, n)
	}
	buf := s.staging[:n]
	for i, c := range s.mask.Pix[:px*px] {
		j := i * 4
		buf[j], buf[j+1], buf[j+2], buf[j+3] = c, c, c, c
	}
	h, ok := im.Acquire(render.Pixels{Pix: buf, W: px, H: px, Stride: px * 4})
	if !ok {
		// Refused by the per drawn frame upload budget. Not an error and not
		// cached as a blank: the caller draws nothing this frame and this runs
		// again on the next one.
		return 0, false
	}
	s.tex[k] = texEntry{h: h, w: px, h2: px}
	s.uploads++
	return h.ID, true
}

// IconStats are the counters of the icon mask cache.
//
// Rasterised should stop climbing once a screen is steady; a value that keeps
// rising at frame rate means something is asking for a new size every frame,
// which is the one way this cache can be defeated.
type IconStats struct {
	// Entries is the number of resident masks.
	Entries int
	// Rasterised is the number of masks produced on the CPU since the process
	// started, and Uploads the number handed to the backend. They differ only
	// by uploads the frame budget refused.
	Rasterised, Uploads uint64
	// Blanks is the number of symbol-and-size pairs known to produce no ink.
	Blanks int
}

// IconCacheStats returns a snapshot of the icon mask cache.
func IconCacheStats() IconStats {
	return IconStats{
		Entries:    len(icons.tex),
		Rasterised: icons.rasterised,
		Uploads:    icons.uploads,
		Blanks:     len(icons.blank),
	}
}

// --- the view ----------------------------------------------------------------

// DefaultIconSize is the edge of an icon that was not given a [IconView.Size],
// in logical pixels.
//
// Twenty rather than the twenty four of the corpus's viewBox. An icon next to
// a line of text should be about the height of a capital letter and not the
// height of the line, and 20 against the 17 pixel default body size of this
// package is that relation. Ask for 24 to get the artwork at its design size.
const DefaultIconSize = 20

// IconView is one [Symbol] drawn as a tinted coverage mask. Create one with
// [Icon].
//
// # How it gets on screen
//
// The geometry is rasterised on the CPU into an eight bit coverage mask at the
// exact number of device pixels it will occupy, uploaded as a premultiplied
// white image and drawn with [render.OpImage], whose colour is a multiply. A
// white mask times a premultiplied foreground is that foreground at that
// coverage, correctly antialiased. That is the whole mechanism, it adds no
// operation kind and no shader, and it is what the project plan, section 21,
// fixes in place of a path primitive.
//
// # What it costs
//
// One rasterisation and one upload per (symbol, device size) pair, once. A
// warmed frame is a map lookup and one operation, and allocates nothing.
//
// # Colour
//
// The default foreground is [ColorLabel], so an icon follows the theme like
// text does and a light/dark switch moves it without the call site saying
// anything. Give it [IconView.Foreground] for anything else.
type IconView struct {
	base
	sym  Symbol
	fg   Color
	size float32
	name string
}

// Icon returns a view of s at [DefaultIconSize].
//
//	ui.Icon(outline.User)
//	ui.Icon(solid.Check).Size(16).Foreground(ui.ColorAccent)
//
// A zero [Symbol] is accepted and draws nothing; see [Symbol].
func Icon(s Symbol) IconView {
	return IconView{sym: s, fg: ColorLabel, size: DefaultIconSize}
}

// ViewType implements gift.View.
func (v IconView) ViewType() gift.TypeID { return iconType }

// Build implements gift.View.
//
// The foreground is resolved here, which is where this package resolves every
// colour: it is the one moment gift asks for an appearance, and a semantic
// colour that reached the painter unresolved would be transparent and
// therefore invisible rather than wrong. See [ResolveColor].
func (v IconView) Build(*gift.BuildContext) gift.Element {
	n := &iconNode{
		fr: v.frame, st: v.style.resolved(), pad: v.pad,
		sym: v.sym, fg: ResolveColor(v.fg), size: v.size,
	}
	return gift.Element{
		Key:      v.key,
		Flex:     v.flex,
		Label:    v.name,
		Layouter: n,
		Painter:  n,
		Clip:     v.style.clip,
	}
}

// Size sets the edge of the icon in logical pixels. The default is
// [DefaultIconSize].
//
// Logical, like every other measurement an application writes down. The device
// density multiplies it on the way to the rasteriser and nowhere else; see
// [iconKey].
func (v IconView) Size(px float32) IconView {
	if !(px > 0) || !isFinite(px) {
		panic("gift/ui: Icon.Size needs a positive, finite number of logical pixels")
	}
	v.size = px
	return v
}

// Foreground is the colour the mask is tinted with. The default is
// [ColorLabel].
func (v IconView) Foreground(c Color) IconView { v.fg = c; return v }

// Label sets the accessible name; see [gift.Element.Label]. An icon that
// carries meaning on its own should have one, because unlike a [TextView] it
// contributes no text for a selector or a future accessibility bridge to find.
func (v IconView) Label(s string) IconView { v.name = s; return v }

// --- the shared modifier set -------------------------------------------------

// Padding sets the same padding on all four edges, outside the icon.
func (v IconView) Padding(f float32) IconView { v.setPadding(f); return v }

// PaddingInsets sets the padding per edge.
func (v IconView) PaddingInsets(i geom.Insets) IconView { v.setPaddingInsets(i); return v }

// Frame fixes both axes. The icon is centred in the result and keeps its
// square aspect; see [IconView.Size] for changing the icon itself.
func (v IconView) Frame(w, h float32) IconView { v.setFrame(w, h); return v }

// MinWidth raises the minimum width.
func (v IconView) MinWidth(f float32) IconView { v.setMinWidth(f); return v }

// MinHeight raises the minimum height.
func (v IconView) MinHeight(f float32) IconView { v.setMinHeight(f); return v }

// MaxWidth lowers the maximum width.
func (v IconView) MaxWidth(f float32) IconView { v.setMaxWidth(f); return v }

// MaxHeight lowers the maximum height.
func (v IconView) MaxHeight(f float32) IconView { v.setMaxHeight(f); return v }

// Background fills the bounds behind the icon.
func (v IconView) Background(c Background) IconView { v.setBackgroundSpec(c); return v }

// Border strokes the inside of the bounds after the icon was drawn.
func (v IconView) Border(b Border) IconView { v.setBorder(b); return v }

// Shadow draws a blurred copy of the bounds behind the view. It is the shadow
// of the *box* and not of the icon's silhouette: the display list carries no
// shape aware shadow, which the project plan, section 8, fixes.
func (v IconView) Shadow(s Shadow) IconView { v.setShadow(s); return v }

// CornerRadius rounds the background and the border, not the icon.
func (v IconView) CornerRadius(f float32) IconView { v.setCornerRadius(f); return v }

// Clip confines the icon to the bounds.
func (v IconView) Clip(b bool) IconView { v.style.clip = b; return v }

// Flex makes the icon take a share of the remaining main axis space. The
// artwork does not grow with it; see [IconView.Size].
func (v IconView) Flex(f float32) IconView { v.setFlex(f); return v }

// Key sets the reconciliation key of this view among its siblings.
func (v IconView) Key(s string) IconView { v.setKey(s); return v }

// --- the node ----------------------------------------------------------------

// iconNode is the retained half of an [IconView].
type iconNode struct {
	fr  frameSpec
	st  styleSpec
	pad geom.Insets

	sym  Symbol
	fg   Color
	size float32

	// px is the mask size in device pixels, computed in Layout because
	// gift.PaintContext deliberately exposes no density: the density is a
	// property of the application and layout is where this package already
	// reads it, exactly as imageNode picks its thumbnail rung there.
	px int
}

// Layout measures the icon and picks the device pixel size of its mask.
func (n *iconNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	cc := n.fr.apply(c)
	out := cc.Constrain(geom.Sz(n.size+n.pad.Horizontal(), n.size+n.pad.Vertical()))

	// Device pixels, from the logical edge the icon will actually occupy
	// after the constraints had their say. Rounding rather than truncating:
	// half a pixel of mask is half a pixel of blur at the edge, and the
	// destination rectangle is the logical one either way.
	edge := min(out.W-n.pad.Horizontal(), out.H-n.pad.Vertical())
	if edge > n.size {
		edge = n.size
	}
	n.px = int(math.Round(float64(edge * ctx.Density())))
	if n.px < 1 {
		n.px = 1
	}
	if n.px > icon.MaxIconExtent {
		n.px = icon.MaxIconExtent
	}
	ctx.ReportOverflow(geom.Size{})
	return out
}

// Paint draws the background, the tinted mask and the border, in the order the
// project plan, section 8, fixes.
//
// The assertion is the first statement and stands in front of the transparency
// gate below, for the reason [assertResolved] gives: an unresolved semantic
// colour is transparent, so the gate would drop the operation and the icon
// would be an invisible hole rather than a wrongly coloured one.
func (n *iconNode) Paint(ctx *gift.PaintContext) {
	assertResolved(n.fg, "the foreground of an Icon")
	b := ctx.Bounds()
	paintBackground(ctx, n.st, b)
	if !n.fg.IsTransparent() && !n.sym.IsZero() {
		inner := b.Inset(n.pad)
		// Square and centred: the mask is square, so drawing it into a
		// non square inner box would stretch the artwork. A Frame that is not
		// square therefore letterboxes, which is what [FitContain] does for a
		// picture and the only answer that does not distort.
		e := min(inner.Width(), inner.Height())
		cx, cy := (inner.Min.X+inner.Max.X)*0.5, (inner.Min.Y+inner.Max.Y)*0.5
		dst := geom.Rc(cx-e*0.5, cy-e*0.5, cx+e*0.5, cy+e*0.5)
		if id, ok := icons.resolve(ctx.Images(), n.sym, n.px); ok {
			ctx.Add(render.Op{Kind: render.OpImage, Bounds: dst, Color: n.fg, Image: id})
		}
	}
	paintBorder(ctx, n.st, b)
}
