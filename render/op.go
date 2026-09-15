package render

import "github.com/torbenschinke/gift/geom"

// OpKind discriminates the drawing operations of an [Op].
//
// New kinds are appended at the end. A backend that does not know a kind must
// skip the operation instead of failing, so that a newer gift version can be
// used with an older backend during development.
type OpKind uint8

const (
	// OpNone is the zero value. It draws nothing and is skipped by backends.
	OpNone OpKind = iota
	// OpFillRect fills Bounds with Color.
	OpFillRect
	// OpFillRoundRect fills Bounds with Color, with corners rounded by
	// CornerRadius.
	OpFillRoundRect
	// OpStrokeRoundRect strokes the outline of Bounds with Color, with
	// corners rounded by CornerRadius and a line width of StrokeWidth.
	// The stroke lies inside Bounds; see the project plan, section 8.
	OpStrokeRoundRect
	// OpGlyphs draws GlyphCount glyphs from the glyph side table of the
	// owning list, starting at index Glyphs, in Color.
	//
	// Bounds is the paint rectangle of the text block. It is not the shape
	// that is filled — the glyph masks are — but it is what a backend culls
	// against and what a reader of a display list dump can make sense of.
	// CornerRadius and StrokeWidth are ignored.
	OpGlyphs
	// OpShadow fills Bounds with Color through a Gaussian falloff of
	// standard deviation Blur/2, with corners rounded by CornerRadius.
	//
	// Bounds is the *shape*, not the painted area: the offset and the spread
	// of a [Shadow] are already folded into it by the producer, and the blur
	// is carried separately so that the backend can grow the geometry by
	// exactly as much as it needs. See [Op.PaintBounds].
	//
	// A Blur of zero or less is a hard edged rounded fill, which is what a
	// shadow with a spread and an offset but no blur is. StrokeWidth is
	// ignored: a shadow is never stroked.
	OpShadow
	// OpImage draws the whole of the image resource named by Image into
	// Bounds, modulated by Color.
	//
	// The mapping is the obvious affine one: the full texture is stretched
	// onto Bounds. There is deliberately no source rectangle in an operation,
	// and that is a decision with a reason rather than an omission. Four more
	// float32 would have grown every operation of every list by sixteen
	// bytes, and the two things a source rectangle is wanted for are both
	// already expressible:
	//
	//   - Letterboxing is a *smaller Bounds*. The producer knows the aspect
	//     ratio of the picture it asked for, so it computes the fitted
	//     rectangle and emits that.
	//   - Cropping to fill is a *clip*. The producer pushes the tile
	//     rectangle, emits an oversized Bounds and pops. The backend already
	//     clips exactly, at vertex level, interpolating the texture
	//     coordinates along with the corners — which is the same machinery a
	//     source rectangle would have needed anyway, minus the sixteen bytes.
	//
	// Color is a *tint*, premultiplied like every other colour here. Opaque
	// white leaves the picture alone; a lower alpha fades it over whatever is
	// behind it, which is how a cross fade or a disabled state is drawn
	// without a second material. A fully transparent colour skips the
	// operation. CornerRadius, StrokeWidth and Blur are ignored: rounding an
	// image means clipping it, and that is the caller's clip.
	OpImage
	// OpMaterial declares a material region: a background whose appearance
	// depends on what was drawn before it. Bounds is the region, CornerRadius
	// its shape and Material the index of its parameters in the material side
	// table of the owning list; see [List.AddMaterial].
	//
	// # It is a barrier, and that is its whole meaning
	//
	// A material reads its backdrop, and the backdrop is *everything emitted
	// earlier in this list and nothing else* — not the material, not its
	// children, not a sibling that comes after it. The display list has no
	// separate z field and needs none: emission order is z order, so
	// "earlier" is a position in the slice and the dependency is expressed by
	// the operation's own index. That is the "Z-Reihenfolge und
	// Hintergrundabhaengigkeiten" of the project plan, section 8, and it is
	// also why a backend cannot batch across one: every operation before it
	// has to have reached the target before its backdrop can be sampled.
	//
	// The consequence a producer has to honour is the drawing order of a
	// styled node — shadow, background or material, content, border. A
	// painter that emitted its children first and its material second would
	// be asking for a backdrop that contains its own content, and nothing in
	// the list could tell the difference.
	//
	// Color, StrokeWidth, Blur, Glyphs and GlyphCount are ignored: every
	// number a material needs is in its side table entry, because there are
	// six of them and a [Glass] may grow a seventh.
	OpMaterial
)

// Op is a single drawing operation.
//
// It is plain old data on purpose: no pointers, no slices, no interfaces and
// no maps. Operations live in one flat, reused slice inside a [List], so a
// frame that emits the same operations as the previous one performs no
// allocation at all.
//
// Clip and Xform are indices into the side tables of the owning list rather
// than embedded values, because most operations share the clip and the
// transform of their neighbours and copying a rectangle and a matrix into
// every operation would triple the size of the list. Glyphs and GlyphCount
// follow the same pattern for the same reason, one step further: a text
// operation is a *range* in a third side table, because the number of glyphs
// is not bounded and a variable length payload cannot live in a fixed size
// struct.
//
// Size: 52 bytes before [OpGlyphs] existed, 60 after, 64 since [OpShadow],
// 68 since [OpImage].
// The eight bytes of OpGlyphs are the index and the count and buy every
// operation kind the same flat layout; the alternative, reusing CornerRadius
// and StrokeWidth as an untyped union, would have cost nothing and been a
// float32 quietly holding an array index.
//
// The four bytes of OpShadow are Blur, and they are a genuine four byte growth
// of every operation in every list. Reusing StrokeWidth would have been
// defensible — unlike the glyph index it is a length in the same units — but
// the two fields would then have had to be documented as "stroke width, except
// when it is a blur", and a shadow with a stroke is the sort of thing a later
// work unit asks for.
//
// The four bytes of [OpImage] are Image, and they cost the property that used
// to be worth stating here: sixty four was exactly one cache line, sixty eight
// is not, so an operation now straddles one every sixteenth element. The
// alternative was to overload Glyphs — an unused uint32 in an image operation,
// exactly as Blur is unused in a glyph operation — and it was rejected for the
// reason above, twice over: an image id in a field called Glyphs is a union
// with no tag but the kind, and the first reader to write l.Glyphs(op.Glyphs,
// op.GlyphCount) on an image operation gets a plausible looking slice of
// somebody else's text. The measured cost of the growth is in
// BenchmarkFramePathWithImages: the list is a few per cent larger and the
// frame path is still 0 B/op, because the slice is reused and never grows
// again after the first frames.
//
// The four bytes of [OpMaterial] are Material, and they take the struct from
// 68 to 72 bytes. Measured with unsafe.Sizeof in TestOpSize, which pins the
// number so that the next field is a decision and not an accident.
//
// This one was worth arguing about, because a material operation is rare —
// one or two per frame against hundreds of fills — and four bytes on every
// operation to serve it is the worst ratio in the struct. Three alternatives
// were considered and rejected:
//
//   - Reusing Glyphs, as an image id could have been. Rejected for the reason
//     already recorded above: a field called Glyphs holding something else is
//     a union with no tag, and the first reader to call l.Glyphs on it gets a
//     plausible looking slice of somebody else's text.
//   - Putting the six glass parameters in Color, CornerRadius, StrokeWidth
//     and Blur, which between them hold seven floats. It fits today and only
//     today: Color would have had to mean "tint", StrokeWidth "refraction"
//     and Blur "blur radius, but a radius here and a diameter on a shadow".
//     Section 8 already calls the material experimental, which is the
//     opposite of a reason to freeze its parameter count into the operation
//     struct.
//   - A separate parallel slice indexed by operation number. That is the
//     same four bytes per operation with an extra indirection and an
//     invariant nobody can see.
//
// The cost is measured rather than assumed: BenchmarkFramePath is unchanged at
// 0 B/op, and a frame of a thousand operations grew from 68 to 72 kilobytes of
// reused backing array.
type Op struct {
	// Kind selects how the remaining fields are interpreted.
	Kind OpKind
	// Bounds is the axis aligned target rectangle in the coordinate system
	// selected by Xform.
	Bounds geom.Rect
	// Color is the fill or stroke colour in premultiplied alpha. For
	// [OpGlyphs] it is the text colour, which is where the colour of text
	// comes from: the glyph atlas holds coverage only.
	Color Color
	// CornerRadius is the corner radius for the round rect kinds. It is
	// clamped by the backend to half of the smaller edge.
	CornerRadius float32
	// StrokeWidth is the line width for the stroke kinds.
	StrokeWidth float32
	// Blur is the blur diameter of an [OpShadow] in the coordinate system
	// selected by Xform. The standard deviation of the Gaussian is half of
	// it; see [Shadow]. It is ignored by every other kind.
	Blur float32
	// Clip is the index of the clip rectangle in the owning list.
	// Index 0 means unclipped; see [List.Clip].
	Clip uint32
	// Xform is the index of the transform in the owning list.
	// Index 0 means identity; see [List.Xform].
	Xform uint32
	// Glyphs is the index of the first glyph of an [OpGlyphs] operation in
	// the glyph side table of the owning list; see [List.Glyphs].
	Glyphs uint32
	// GlyphCount is the number of glyphs of an [OpGlyphs] operation. Zero
	// means the operation draws nothing.
	GlyphCount uint32
	// Image is the resource an [OpImage] draws; see [ImageID]. It is valid
	// only for the frame it was obtained in, which is why a consumer keeps an
	// [ImageHandle] and puts the resolved id here once per frame.
	Image ImageID
	// Material is the index of the material parameters of an [OpMaterial] in
	// the material side table of the owning list. Index 0 means no material;
	// see [List.Material].
	Material uint32
}

// PaintBounds returns the rectangle this operation can touch.
//
// For every kind but [OpShadow] it is Bounds. For a shadow it is Bounds grown
// by [Shadow.Extent], which is the "Shadow erweitert die Paint-Bounds" of the
// project plan, section 8, stated in terms of a single operation. The backend
// sizes the quad it emits from exactly this rectangle.
//
// Bounds of an [OpShadow] is already the shadow *shape* — the node's
// rectangle inflated by the spread and moved by the offset; see
// [Shadow.Shape] — so the extent is all that is left to add. The arithmetic
// is [Shadow]'s and is not repeated here, which is the point: this and the
// quad the Ebitengine backend emits must not be able to drift apart, and for
// a while they were two independent inline copies of the same formula.
//
// It is not the *visible* area: a clip may cut it, and a hit test ignores it
// entirely. In particular it does not consult the colour: a transparent
// shadow is skipped by the backend rather than resized here.
func (o Op) PaintBounds() geom.Rect {
	if o.Kind != OpShadow {
		return o.Bounds
	}
	e := Shadow{Blur: o.Blur}.Extent()
	if !(e > 0) {
		return o.Bounds
	}
	return geom.Rc(o.Bounds.Min.X-e, o.Bounds.Min.Y-e, o.Bounds.Max.X+e, o.Bounds.Max.Y+e)
}
