package ui

import (
	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/asset"
	"github.com/torbenschinke/gift/geom"
	"github.com/torbenschinke/gift/metrics"
	"github.com/torbenschinke/gift/render"
)

var imageType = gift.RegisterType("ui.Image")

// --- the shared image service ------------------------------------------------

// SetImagePipeline installs the application wide image pipeline that
// [Image] and every [Gallery] draw through.
//
// The project plan, section 10, requires that "ImageGallery ... verwendet
// denselben Bildservice wie ui.Image(source)", and this is the seam that makes
// it one service and not two: one [asset.Pipeline], so one fetch, one decode
// and one disk cache entry per picture, and one table of GPU textures, so one
// upload — even when the same source is on screen twice in two different
// widgets.
//
// It is process wide and mutable, exactly like [SetDefaultFont], and carries
// the same caveat the project plan, section 13, already records about process
// wide state: nothing here is safe against a test that calls t.Parallel, and
// the repository contains no such test. The alternative — threading a service
// through every view constructor — would put a second argument on
// ui.Image(source), which is the spelling the plan fixes in section 10.
//
// Passing nil detaches the pipeline. Views then draw their placeholders, which
// is what a layout test without a pipeline sees and is a normal, quiet state
// rather than an error.
//
// The application owns the pipeline's lifetime and closes it:
//
//	pipe := asset.NewPipeline(asset.Config{Deliver: app.Post, Disk: ...})
//	defer pipe.Close()
//	ui.SetImagePipeline(pipe)
func SetImagePipeline(p *asset.Pipeline) { images.setPipeline(p) }

// ImagePipeline returns the pipeline installed by [SetImagePipeline], or nil.
func ImagePipeline() *asset.Pipeline { return images.pipe }

// images is the one service. See [SetImagePipeline].
var images = newImageService()

// imageKey identifies one *picture at one size at one revision*, which is the
// granularity a GPU texture has.
//
// All three parts are load bearing. Without the rung a resize would draw a
// 128 pixel thumbnail into a 512 pixel tile for ever; without the revision an
// edited picture would keep its old texture until the least recently used
// sweep happened to reach it, which is the "keine falschen Bilder ... und
// Quellenrevisionen" half of the project plan, section 13.
type imageKey struct {
	id   asset.ID
	rung int
	rev  string
}

func (k imageKey) ok() bool { return k.id != "" && k.rung > 0 }

// texEntry is one resident texture: the durable handle and the pixel size the
// aspect ratio is computed from.
//
// The size is kept here rather than asked of the backend because [render.Images]
// deliberately exposes no query for it: a handle is a promise about residency,
// not a description of the picture, and the consumer already knew the
// dimensions when it handed the pixels over.
type texEntry struct {
	h     render.ImageHandle
	w, h2 int
}

// reqKey is the identity of an in flight or settled request of [ImageView]:
// the picture and the size that was asked for, before the pipeline's ladder
// and hysteresis had their say.
type reqKey struct {
	id   asset.ID
	size int
}

// imageRequest is what [ImageView] remembers about one picture between builds.
//
// It lives in the service and not in the node because a view node is rebuilt
// and thrown away on every build — see the project plan, section 4 — and a
// request that restarted on every rebuild would refetch the picture every time
// a sibling's label changed. The [Gallery] has the opposite problem, a node
// that outlives many pictures, and solves it the opposite way with a per slot
// generation; see [TileBinding.Generation].
type imageRequest struct {
	ticket   asset.Ticket
	inFlight bool
	key      imageKey
	ok       bool
	err      error
	// w and h are the oriented pixel dimensions the pipeline reported, used
	// for the aspect ratio before a texture exists.
	w, h uint32
	// notify are the invalidators of the nodes waiting for this picture. They
	// are called once and dropped: a node that is still interested registers
	// again on its next layout.
	notify []func()
}

// imageService is the shared half of the GPU image path: the pipeline, the
// table of resident textures and the request state of [ImageView].
//
// It belongs to the UI executor, like everything else a frame touches, and is
// not safe for concurrent use. The pipeline it wraps is the opposite and says
// so; results arrive here through [asset.Config.Deliver], which an application
// wires to gift.App.Post.
type imageService struct {
	pipe *asset.Pipeline
	tex  map[imageKey]texEntry
	reqs map[reqKey]*imageRequest

	uploads, evictedHandles uint64
}

func newImageService() *imageService {
	return &imageService{
		tex:  make(map[imageKey]texEntry, 64),
		reqs: make(map[reqKey]*imageRequest, 64),
	}
}

// setPipeline swaps the pipeline and forgets everything derived from the old
// one.
//
// The textures are dropped as well, and not merely the request state: they are
// keyed by picture and revision, and a different pipeline may have a different
// catalogue behind the same identity. Keeping them would be the one thing this
// whole file exists to prevent.
func (s *imageService) setPipeline(p *asset.Pipeline) {
	if s.pipe == p {
		return
	}
	for _, r := range s.reqs {
		r.ticket.Cancel()
	}
	clear(s.reqs)
	clear(s.tex)
	s.pipe = p
	s.uploads, s.evictedHandles = 0, 0
}

// resolve returns the operation level id of a resident texture, uploading it
// first if the pixels are in the CPU cache and the frame's upload budget
// allows.
//
// This is the frame path and it is called once per drawn image per frame. The
// warm case — a texture that is already resident — is one map lookup and two
// integer comparisons inside the backend, and allocates nothing.
//
// # Where the reference counting happens
//
// [asset.Pipeline.Lookup] returns a *retained* thumbnail and the release is
// this function's, which is precisely the "Retain before upload, Release
// after" the project plan, section 9, asks of an uploader: the pixels stay
// charged against [asset.Config.PixelBudget] for the duration of the upload
// and come back the moment it is done. A leak shows up as a pixel budget that
// never falls back to the size of the CPU cache, which is what
// TestImageUploadReleasesItsThumbnail asserts.
func (s *imageService) resolve(ctx *gift.PaintContext, k imageKey) (render.ImageID, int, int, bool) {
	if !k.ok() {
		return 0, 0, 0, false
	}
	im := ctx.Images()
	if im == nil {
		// Headless. Every image view falls back to its placeholder, which is
		// what a layout test and a gifttest harness without a graphics
		// context want to see.
		return 0, 0, 0, false
	}
	if e, ok := s.tex[k]; ok {
		if id, ok := im.Resolve(e.h); ok {
			return id, e.w, e.h2, true
		}
		// Evicted between two frames. The generation in the handle is what
		// turned that from a wrong picture into this branch.
		delete(s.tex, k)
		s.evictedHandles++
	}
	if s.pipe == nil {
		return 0, 0, 0, false
	}
	t, ok := s.pipe.Lookup(k.id, k.rung)
	if !ok {
		return 0, 0, 0, false
	}
	h, ok := im.Acquire(render.Pixels{
		Pix: t.Pix(), W: t.Width(), H: t.Height(), Stride: t.Stride(),
	})
	w, hh := t.Width(), t.Height()
	t.Release()
	if !ok {
		// Refused by the per drawn frame upload budget. Not an error: the
		// caller draws its placeholder and this runs again next frame.
		return 0, 0, 0, false
	}
	s.tex[k] = texEntry{h: h, w: w, h2: hh}
	s.uploads++
	return h.ID, w, hh, true
}

// forget releases the texture of one key explicitly.
//
// It is the [render.Images.Deallocate] half of the contract and exists so that
// a picture whose revision changed gives its memory back now rather than
// waiting for the age sweep. It needs the service to be inside a frame, which
// is why only a painter calls it.
func (s *imageService) forget(im render.Images, k imageKey) {
	if e, ok := s.tex[k]; ok {
		delete(s.tex, k)
		if im != nil {
			im.Deallocate(e.h)
		}
	}
}

// state returns the request record for one picture at one size, creating it on
// demand.
func (s *imageService) state(id asset.ID, size int) *imageRequest {
	k := reqKey{id: id, size: size}
	r, ok := s.reqs[k]
	if !ok {
		r = &imageRequest{}
		s.reqs[k] = r
	}
	return r
}

// request schedules a fetch for src at size, unless one is already in flight
// or has already settled.
//
// A settled failure is *not* retried here. [asset.BackoffPolicy] already
// refuses a hammered source, and retrying from the frame loop would turn one
// failing picture into one refused request per frame for the life of the
// process. The way back is [asset.Pipeline.Forget] plus [ForgetImage].
func (s *imageService) request(src asset.Source, size int, notify func()) *imageRequest {
	id := src.Metadata().ID
	r := s.state(id, size)
	if notify != nil {
		r.notify = append(r.notify, notify)
	}
	if s.pipe == nil || r.inFlight || r.ok || r.err != nil {
		return r
	}
	r.inFlight = true
	r.ticket = s.pipe.Request(asset.Request{
		Source:   src,
		Size:     size,
		Priority: asset.Visible,
		OnResult: func(res asset.Result) {
			// On the UI executor: asset.Config.Deliver is gift.App.Post.
			r.inFlight = false
			r.ticket = asset.Ticket{}
			if res.Err != nil {
				r.err = res.Err
			} else {
				r.key = imageKey{id: res.ID, rung: res.Size, rev: res.Metadata.Revision}
				r.w, r.h = res.Metadata.Width, res.Metadata.Height
				r.ok = true
			}
			for _, fn := range r.notify {
				fn()
			}
			r.notify = r.notify[:0]
		},
	})
	return r
}

// ForgetImage drops everything the user interface remembers about one picture:
// the settled request state of [Image] and the resident textures of every
// revision of it.
//
// It is the counterpart of [asset.Pipeline.Invalidate] and [asset.Pipeline.Forget]
// on this side of the boundary, and an application that knows a picture
// changed or that a failure is worth another attempt calls all three. The
// textures are *not* deallocated here: releasing a texture requires the frame
// the backend is drawing, and this is called from an event handler. They
// become unreferenced instead and the next age sweep collects them, which is
// the one place this package accepts a delayed release.
func ForgetImage(id asset.ID) {
	for k, r := range images.reqs {
		if k.id == id {
			r.ticket.Cancel()
			delete(images.reqs, k)
		}
	}
	for k := range images.tex {
		if k.id == id {
			delete(images.tex, k)
		}
	}
}

// --- drawing -----------------------------------------------------------------

// ImageFit is how a picture is mapped onto the rectangle it is drawn in.
type ImageFit uint8

const (
	// FitContain scales the picture until it fits entirely, letterboxing the
	// remainder. Nothing of the picture is lost. This is the default.
	FitContain ImageFit = iota
	// FitCover scales the picture until it covers the rectangle and clips the
	// overhang. Nothing of the rectangle is empty.
	//
	// The clip is a real clip and not a source rectangle, which is why it
	// costs nothing: the backend trims the geometry against the clip before
	// it emits a vertex, so the cropped part never reaches a fragment. See
	// [render.OpImage].
	FitCover
	// FitStretch fills the rectangle exactly, distorting the picture.
	FitStretch
)

// fitRect returns the rectangle the picture is drawn into, given the box b and
// the picture's pixel dimensions.
func fitRect(b geom.Rect, iw, ih int, fit ImageFit) geom.Rect {
	if fit == FitStretch || iw <= 0 || ih <= 0 {
		return b
	}
	bw, bh := b.Width(), b.Height()
	if bw <= 0 || bh <= 0 {
		return b
	}
	sx, sy := bw/float32(iw), bh/float32(ih)
	s := sx
	if fit == FitCover {
		if sy > s {
			s = sy
		}
	} else if sy < s {
		s = sy
	}
	w, h := float32(iw)*s, float32(ih)*s
	cx, cy := (b.Min.X+b.Max.X)*0.5, (b.Min.Y+b.Max.Y)*0.5
	return geom.Rc(cx-w*0.5, cy-h*0.5, cx+w*0.5, cy+h*0.5)
}

// paintImage emits one image operation for the picture id inside b.
//
// For [FitCover] it pushes b as a clip, because a crop in gift is a clip: the
// display list carries no source rectangle, on purpose, and the backend
// already interpolates texture coordinates into a clipped polygon. See
// [render.OpImage].
func paintImage(ctx *gift.PaintContext, b geom.Rect, id render.ImageID, iw, ih int, fit ImageFit, tint Color) {
	dst := fitRect(b, iw, ih, fit)
	clipped := fit == FitCover && dst != b
	if clipped {
		// Device space, which is the space the clip stack lives in; see the
		// project plan, section 7.
		ctx.PushClip(ctx.DeviceBounds())
	}
	ctx.Add(render.Op{Kind: render.OpImage, Bounds: dst, Color: tint, Image: id})
	if clipped {
		ctx.PopClip()
	}
}

// OpaqueWhite is the tint that leaves a picture exactly as it was decoded.
// A lower alpha fades it over its background; see [render.OpImage].
var OpaqueWhite = RGB(255, 255, 255)

// --- the view ----------------------------------------------------------------

// ImageView is one picture from an [asset.Source]. Create one with [Image].
//
// # What it does and does not do
//
// It declares *which* picture, how large a thumbnail to ask for and how to fit
// it. It does no I/O, holds no pixels and knows nothing about a texture: the
// bytes come from the application wide pipeline of [SetImagePipeline], the
// upload happens under the backend's per drawn frame budget, and until both
// have happened the view draws [ImageView.Placeholder]. That is the
// "Platzhalter statt Warten" of the project plan, section 10, applied to a
// single picture rather than to a gallery.
//
// # Size
//
// An image measures itself from the oriented dimensions of its source, scaled
// to fit the constraints, so a picture in a stack takes the space its aspect
// ratio asks for. A source that does not know its dimensions yet measures as a
// square, and the picture reflows once the pipeline has probed it — which is
// the same provisional-then-corrected behaviour the gallery has, for the same
// reason. Give it a [ImageView.Frame] to take the question away entirely.
type ImageView struct {
	base
	src         asset.Source
	fit         ImageFit
	placeholder Color
	tint        Color
	hasTint     bool
	size        int
	name        string
}

// Image returns a view of src.
//
// src must not be nil. Construct one with [asset.File] or [asset.HTTP], or
// implement [asset.Source] over whatever the application stores pictures in;
// neither constructor performs I/O.
func Image(src asset.Source) ImageView {
	if src == nil {
		panic("gift/ui: Image with a nil asset.Source")
	}
	return ImageView{src: src, placeholder: RGBA(255, 255, 255, 20)}
}

// ViewType implements gift.View.
func (v ImageView) ViewType() gift.TypeID { return imageType }

// Build implements gift.View.
func (v ImageView) Build(*gift.BuildContext) gift.Element {
	tint := v.tint
	if !v.hasTint {
		tint = OpaqueWhite
	}
	n := &imageNode{
		fr: v.frame, st: v.style, pad: v.pad,
		src: v.src, id: v.src.Metadata().ID,
		fit: v.fit, placeholder: v.placeholder, tint: tint,
		want: v.size,
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

// Fit selects how the picture is mapped onto the view; see [ImageFit].
func (v ImageView) Fit(f ImageFit) ImageView { v.fit = f; return v }

// Size asks the pipeline for a thumbnail of this many pixels on the longest
// edge. Zero, the default, derives it from the measured size of the view,
// which is what a layout driven picture wants.
//
// The pipeline snaps the number to a rung of [asset.Config.Sizes] with
// hysteresis, so a view that is resized by a few pixels does not re-decode
// anything; see [asset.Config.Hysteresis].
func (v ImageView) Size(px int) ImageView { v.size = px; return v }

// Placeholder fills the view until the picture is resident. The default is a
// faint white wash; a transparent value draws nothing at all.
func (v ImageView) Placeholder(c Color) ImageView { v.placeholder = c; return v }

// Tint multiplies the picture. The default is opaque white, which leaves it
// alone; a lower alpha fades it over its background. It is premultiplied like
// every other colour here.
func (v ImageView) Tint(c Color) ImageView { v.tint, v.hasTint = c, true; return v }

// Label sets the accessible name, which for a picture is its description; see
// [gift.Element.Label].
func (v ImageView) Label(s string) ImageView { v.name = s; return v }

// --- the shared modifier set -------------------------------------------------

// Padding sets the same padding on all four edges. The picture is drawn inside
// it.
func (v ImageView) Padding(f float32) ImageView { v.setPadding(f); return v }

// PaddingInsets sets the padding per edge.
func (v ImageView) PaddingInsets(i geom.Insets) ImageView { v.setPaddingInsets(i); return v }

// Frame fixes both axes. Pass [geom.Unbounded] for an axis that should follow
// the aspect ratio of the picture.
func (v ImageView) Frame(w, h float32) ImageView { v.setFrame(w, h); return v }

// MinWidth raises the minimum width.
func (v ImageView) MinWidth(f float32) ImageView { v.setMinWidth(f); return v }

// MinHeight raises the minimum height.
func (v ImageView) MinHeight(f float32) ImageView { v.setMinHeight(f); return v }

// MaxWidth lowers the maximum width.
func (v ImageView) MaxWidth(f float32) ImageView { v.setMaxWidth(f); return v }

// MaxHeight lowers the maximum height.
func (v ImageView) MaxHeight(f float32) ImageView { v.setMaxHeight(f); return v }

// Background fills the bounds behind the picture and behind the placeholder.
func (v ImageView) Background(c Color) ImageView { v.setBackground(c); return v }

// Border strokes the inside of the bounds after the picture was drawn.
func (v ImageView) Border(b Border) ImageView { v.setBorder(b); return v }

// Shadow draws a blurred copy of the bounds behind the view.
func (v ImageView) Shadow(s Shadow) ImageView { v.setShadow(s); return v }

// CornerRadius rounds the background and the border.
//
// It does *not* round the picture. Clipping a texture to a rounded rectangle
// needs a shape aware clip, which the display list does not have and which is
// material work — step 5 of the project plan, section 12, not this one. A
// rounded background behind a square picture is the honest intermediate state
// and it is visible rather than claimed.
func (v ImageView) CornerRadius(f float32) ImageView { v.setCornerRadius(f); return v }

// Clip confines the picture to the bounds. [FitCover] clips regardless,
// because a crop is a clip.
func (v ImageView) Clip(b bool) ImageView { v.style.clip = b; return v }

// Flex makes the image take a share of the remaining main axis space.
func (v ImageView) Flex(f float32) ImageView { v.setFlex(f); return v }

// Key sets the reconciliation key of this view among its siblings.
func (v ImageView) Key(s string) ImageView { v.setKey(s); return v }

// --- the node ----------------------------------------------------------------

// imageNode is the retained half of an [ImageView].
//
// It carries no request and no pixels. Both live in the process wide
// [imageService], because this object is rebuilt and discarded on every build
// and a request anchored here would restart every time a sibling changed. What
// it does carry is the invalidator, which is per node and is how an answer
// that arrives between two frames reaches the screen.
type imageNode struct {
	fr  frameSpec
	st  styleSpec
	pad geom.Insets

	src         asset.Source
	id          asset.ID
	fit         ImageFit
	placeholder Color
	tint        Color
	want        int

	// req is the shared request record of the picture at the size this node
	// last measured for. It is refreshed in Layout.
	req *imageRequest
}

// Layout measures the picture and schedules the request.
//
// The request is issued here and not in Paint for the reason the project plan,
// section 6, gives: scheduling is CPU work and belongs to the update half of
// the frame, while the *upload* is budgeted per drawn frame and therefore
// belongs to the paint half. The two halves of "get this picture on screen"
// genuinely live in the two callbacks.
func (n *imageNode) Layout(ctx *gift.LayoutContext, c geom.Constraints) geom.Size {
	cc := n.fr.apply(c)
	inner := cc.Deflate(n.pad)

	w, h := n.natural()
	size := inner.Constrain(aspectSize(inner, w, h))
	out := cc.Constrain(geom.Sz(size.W+n.pad.Horizontal(), size.H+n.pad.Vertical()))

	want := n.want
	if want <= 0 {
		want = int(max(out.W, out.H))
		if want <= 0 {
			want = 1
		}
	}
	n.req = images.request(n.src, want, ctx.Invalidator())
	ctx.ReportOverflow(geom.Size{})
	return out
}

// natural returns the oriented pixel dimensions known for the picture: what
// the pipeline probed if it has, what the source declared otherwise, and
// nothing at all before either.
func (n *imageNode) natural() (uint32, uint32) {
	if n.req != nil && n.req.w > 0 && n.req.h > 0 {
		return n.req.w, n.req.h
	}
	m := n.src.Metadata()
	return m.Width, m.Height
}

// aspectSize is the largest size with the given aspect ratio that the
// constraints allow. An unknown ratio falls back to the box itself, which for
// a bounded axis is the greedy answer a [Box] gives.
func aspectSize(c geom.Constraints, w, h uint32) geom.Size {
	bw, bh := c.Max.W, c.Max.H
	if !c.HasBoundedWidth() {
		bw = c.Min.W
	}
	if !c.HasBoundedHeight() {
		bh = c.Min.H
	}
	if w == 0 || h == 0 || bw <= 0 || bh <= 0 {
		return geom.Sz(bw, bh)
	}
	s := min(bw/float32(w), bh/float32(h))
	return geom.Sz(float32(w)*s, float32(h)*s)
}

// Paint draws the background, the picture or its placeholder, and the border,
// in the order the project plan, section 8, fixes.
func (n *imageNode) Paint(ctx *gift.PaintContext) {
	b := ctx.Bounds()
	paintBackground(ctx, n.st, b)
	inner := b.Inset(n.pad)
	drawn := false
	if n.req != nil && n.req.ok {
		if id, iw, ih, ok := images.resolve(ctx, n.req.key); ok {
			paintImage(ctx, inner, id, iw, ih, n.fit, n.tint)
			drawn = true
		}
	}
	if !drawn && !n.placeholder.IsTransparent() {
		op := render.Op{Kind: render.OpFillRect, Bounds: inner, Color: n.placeholder}
		if n.st.radius > 0 {
			op.Kind, op.CornerRadius = render.OpFillRoundRect, n.st.radius
		}
		ctx.Add(op)
	}
	paintBorder(ctx, n.st, b)
}

// ImagePipelineStats converts the counters of the installed image pipeline
// into the shape the metrics package declares.
//
// It lives here because this is the one package that holds both: metrics
// deliberately imports nothing from the framework it measures, and asset must
// not import metrics either, so the conversion belongs to whoever owns the
// pipeline. That is ui, since [SetImagePipeline] installs it.
//
// Present is false when no pipeline is installed, which is how a report tells
// "nothing loaded any pictures" apart from "a pipeline loaded none".
//
//	cfg.AssetStats = ui.ImagePipelineStats
func ImagePipelineStats() metrics.AssetStats {
	p := ImagePipeline()
	if p == nil {
		return metrics.AssetStats{}
	}
	s := p.Stats()
	return metrics.AssetStats{
		Present:      true,
		Requests:     s.Requests,
		Deduplicated: s.Deduplicated,
		Promotions:   s.Promotions,
		Dropped:      s.Dropped,
		Cancelled:    s.Cancelled,
		ReadyDropped: s.ReadyDropped,
		Completed:    s.Completed,
		Failed:       s.Failed,

		BackoffRefused: s.BackoffRefused,
		Decodes:        s.Decodes,
		MemoryHits:     s.MemoryHits,
		DiskHits:       s.DiskHits,
		DecodedPixels:  s.DecodedPixels,
		ScaledPixels:   s.ScaledPixels,

		InputBytes: s.Input.InUse, InputPeak: s.Input.Peak, InputLimit: s.Input.Limit,
		DecodeBytes: s.Decode.InUse, DecodePeak: s.Decode.Peak, DecodeLimit: s.Decode.Limit,
		PixelBytes: s.Pixels.InUse, PixelPeak: s.Pixels.Peak, PixelLimit: s.Pixels.Limit,

		CacheEntries: s.CacheEntries,
		DiskBytes:    s.Disk.Bytes,
		DiskBudget:   s.Disk.Budget,
	}
}
