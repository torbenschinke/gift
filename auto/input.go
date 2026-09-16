//go:build giftauto

package auto

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"image"
	"image/png"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/geom"
)

// Step is one entry of an input batch.
//
// A batch is a sequence of these, executed in order, and it is how "press
// here, wait 100 ms, move there, wait two frames, release" becomes one
// request:
//
//	{"steps":[
//	  {"op":"pointerDown","x":100,"y":200},
//	  {"op":"waitMs","ms":100},
//	  {"op":"pointerMove","x":100,"y":260},
//	  {"op":"waitFrames","frames":2},
//	  {"op":"pointerUp"}
//	]}
//
// Every coordinate is in gift's logical pixels, which is what /tree reports;
// see the package documentation.
type Step struct {
	// Op is the verb. The recognised values are listed on [runStep].
	Op string `json:"op"`

	// X and Y are the pointer position. A pointer step that omits both uses
	// the position of the previous pointer step, so a release does not have
	// to repeat the coordinate of its press.
	X *float32 `json:"x,omitempty"`
	Y *float32 `json:"y,omitempty"`

	// DX and DY are the wheel delta in notches, positive down and right.
	DX float32 `json:"dx,omitempty"`
	DY float32 `json:"dy,omitempty"`

	// Key is the name of a key, such as "tab", "escape" or "backspace". See
	// [keyByName] for the whole table.
	Key string `json:"key,omitempty"`

	// Mods are the modifiers held for this step: "shift", "ctrl", "alt",
	// "meta". They stay in force until a later step changes them.
	Mods []string `json:"mods,omitempty"`

	// Text is typed character by character through [gift.App.TypeRune],
	// which is the channel a real keyboard's characters arrive on.
	Text string `json:"text,omitempty"`

	// Ms, Frames and Ticks are the three waits. Milliseconds are wall clock,
	// frames are drawn frames and ticks are updates.
	Ms     int `json:"ms,omitempty"`
	Frames int `json:"frames,omitempty"`
	Ticks  int `json:"ticks,omitempty"`

	// Mouse makes the pointer a mouse rather than a touch.
	//
	// # Why touch is the default, and the trap this avoids
	//
	// The Ebitengine backend polls the real mouse once per tick and calls
	// [gift.App.PointerMove] with the real cursor position unconditionally —
	// see inputBridge.pollMouse. A synthesised mouse press is therefore
	// dragged to wherever the operator's physical cursor happens to rest on
	// the very next tick, gift marks the gesture as dragged, and the release
	// is declined by every control that checks [gift.Event.Dragged], which is
	// all of them. The press is delivered, the focus moves, and nothing
	// activates: a failure that looks exactly like a broken button.
	//
	// A touch pointer uses a different slot of the pointer table and the
	// backend never writes to it, so it is unaffected. It is also what the
	// kiosk this framework targets actually delivers.
	//
	// Use Mouse for the cases where mouse semantics are the subject — hover,
	// and the focus ring after a click — and use the "click" op, which
	// performs the whole gesture inside a single update and so cannot be
	// overtaken by the poll.
	Mouse bool `json:"mouse,omitempty"`

	// Forward is the direction of the "focus" op.
	Forward *bool `json:"forward,omitempty"`

	// PNG asks the "capture" op to include the pixels of every captured
	// frame, base64 encoded, and not only their hashes and their counts of
	// changed pixels.
	PNG bool `json:"png,omitempty"`
}

// Batch is the body of a /input request.
type Batch struct {
	Steps []Step `json:"steps"`
}

// Capture is one frame of a burst; see the "capture" op of [runStep].
type Capture struct {
	// Index is the position of this frame in the burst and Frame is the
	// backend's own drawn frame counter for it, the same number the
	// X-Gift-Frames header of a screenshot carries.
	Index int    `json:"index"`
	Frame uint64 `json:"frame"`

	// SHA is the SHA-256 of the raw pixels, and Changed is how many of them
	// differ from the previous frame of this burst. Changed is -1 for the
	// first frame, which has nothing to be compared with.
	//
	// These two numbers are the point of the whole mechanism: "nothing
	// moves" is a run of frames whose hashes are equal, and a transition is
	// a run whose Changed counts are large and then fall to zero. A caller
	// gets that without transferring a single pixel.
	SHA     string `json:"sha"`
	Changed int    `json:"changed"`

	// PNG is the frame itself, base64 encoded, when the step asked for it.
	PNG string `json:"png,omitempty"`
}

// BatchResult is what /input answers with: the counters before and after, so
// a caller can see how many frames its gesture cost and whether any were
// drawn at all.
type BatchResult struct {
	Steps        int    `json:"steps"`
	FramesBefore uint64 `json:"framesBefore"`
	FramesAfter  uint64 `json:"framesAfter"`
	UpdatesAfter uint64 `json:"updatesAfter"`

	// Captured are the frames of the "capture" op, if the batch had one.
	Captured []Capture `json:"captured,omitempty"`

	// Animating and Animations are [gift.Diagnostics] as of the end of the
	// batch: whether anything has asked to be repainted, and how many things
	// have. They are here as well as in /diag because the interesting moment
	// is the one immediately after an input, and a second request would be a
	// second round trip during which a 180 ms transition can end.
	Animating  bool   `json:"animating"`
	Animations uint64 `json:"animations"`
}

// pointerID is the identifier every synthesised touch uses. gift only compares
// it for equality and a second finger is out of scope; see the project plan,
// section 7.
const pointerID gift.PointerID = 1

// keyByName is the name table of the keys gift has a name for.
//
// The names are lower case and are what a caller writes in a JSON step. A key
// gift does not name cannot be synthesised, which is deliberate: the enum is
// widened for a named need and not for completeness, and a driver that could
// ask for a key the framework has no constant for would be asking for
// [gift.KeyOther] under a misleading name.
var keyByName = map[string]gift.Key{
	"tab": gift.KeyTab, "space": gift.KeySpace, "enter": gift.KeyEnter,
	"escape": gift.KeyEscape, "esc": gift.KeyEscape,
	"left": gift.KeyLeft, "right": gift.KeyRight, "up": gift.KeyUp, "down": gift.KeyDown,
	"home": gift.KeyHome, "end": gift.KeyEnd,
	"pageup": gift.KeyPageUp, "pagedown": gift.KeyPageDown,
	"backspace": gift.KeyBackspace, "delete": gift.KeyDelete,
	"a": gift.KeyA, "c": gift.KeyC, "v": gift.KeyV, "x": gift.KeyX, "z": gift.KeyZ,
}

// modByName is the name table of the modifiers.
var modByName = map[string]gift.Mods{
	"shift": gift.ModShift, "ctrl": gift.ModControl, "control": gift.ModControl,
	"alt": gift.ModAlt, "meta": gift.ModMeta, "cmd": gift.ModMeta,
}

// runBatch executes the steps in order and answers with the frame counters.
//
// Each step that touches gift is its own [driver.do], so each lands in its own
// update and the next one starts only after the previous one has been
// dispatched and its handlers have run. That is what makes a press followed by
// a release behave like a press followed by a release rather than like two
// events in one tick, and it is why the batch is a batch at all: the ordering
// is guaranteed by the transport, not hoped for by the caller.
func (d *driver) runBatch(b Batch) (BatchResult, error) {
	res := BatchResult{Steps: len(b.Steps), FramesBefore: d.Frames()}
	var pending *burst
	var wantPNG bool
	for i, s := range b.Steps {
		if s.Op == "capture" {
			n := max(1, s.Frames)
			// Armed from the UI goroutine, so that the first collected frame
			// is the first one drawn after the step before it; see [burst].
			if err := d.do(func(*gift.App) { pending = d.armBurst(n) }); err != nil {
				return res, fmt.Errorf("step %d (capture): %w", i, err)
			}
			wantPNG = s.PNG
			continue
		}
		if err := d.runStep(s); err != nil {
			return res, fmt.Errorf("step %d (%s): %w", i, s.Op, err)
		}
	}
	if pending != nil {
		shots, err := d.waitBurst(pending)
		res.Captured = encodeCaptures(shots, wantPNG)
		if err != nil {
			return res, err
		}
	}
	res.FramesAfter = d.Frames()
	if app, _, err := d.target(); err == nil {
		diag := app.Diagnostics()
		res.UpdatesAfter = diag.Updates
		res.Animating, res.Animations = diag.Animating, diag.Animations
	}
	return res, nil
}

// encodeCaptures turns the captured framebuffers into what a driving script
// can read: a hash per frame, the number of pixels that differ from the frame
// before it, and optionally the picture.
func encodeCaptures(shots []shot, withPNG bool) []Capture {
	out := make([]Capture, 0, len(shots))
	for i, s := range shots {
		sum := sha256.Sum256(s.img.Pix)
		c := Capture{Index: i, Frame: s.count, SHA: hex.EncodeToString(sum[:]), Changed: -1}
		if i > 0 {
			c.Changed = changedPixels(shots[i-1].img, s.img)
		}
		if withPNG {
			var buf bytes.Buffer
			if err := png.Encode(&buf, s.img); err == nil {
				c.PNG = base64.StdEncoding.EncodeToString(buf.Bytes())
			}
		}
		out = append(out, c)
	}
	return out
}

// changedPixels counts the pixels of two frames of the same size that differ.
//
// It is the measurement the defect report was written in — "frame 4000 differs
// from the pre-tap frame by 1,723,004 pixels, and frames 4006, 4012, 4018 and
// 4024 are bit-identical to it" — computed where the pixels already are
// instead of in a script that had to fetch two PNGs to do it.
func changedPixels(a, b *image.RGBA) int {
	if a.Bounds() != b.Bounds() || len(a.Pix) != len(b.Pix) {
		return -1
	}
	n := 0
	for i := 0; i+3 < len(a.Pix); i += 4 {
		if a.Pix[i] != b.Pix[i] || a.Pix[i+1] != b.Pix[i+1] ||
			a.Pix[i+2] != b.Pix[i+2] || a.Pix[i+3] != b.Pix[i+3] {
			n++
		}
	}
	return n
}

// runStep executes one step.
//
// The verbs are:
//
//	pointerMove, pointerDown, pointerUp, pointerCancel, tap, click
//	wheel
//	keyDown, keyUp, key
//	text
//	focus
//	waitMs, waitFrames, waitTicks
//	capture
func (d *driver) runStep(s Step) error {
	switch s.Op {
	case "waitMs":
		time.Sleep(time.Duration(s.Ms) * time.Millisecond)
		return nil
	case "waitFrames":
		return d.waitFrames(uint64(max(0, s.Frames)))
	case "waitTicks":
		return d.waitTicks(uint64(max(0, s.Ticks)))
	}

	kind := gift.PointerTouch
	id := pointerID
	if s.Mouse {
		kind, id = gift.PointerMouse, gift.MousePointer
	}

	switch s.Op {
	case "pointerMove":
		return d.do(func(app *gift.App) { app.PointerMove(id, kind, d.at(s)) })
	case "pointerDown":
		return d.do(func(app *gift.App) { app.PointerDown(id, kind, d.at(s)) })
	case "pointerUp":
		return d.do(func(app *gift.App) { app.PointerUp(id, kind, d.at(s)) })
	case "pointerCancel":
		return d.do(func(app *gift.App) { app.PointerCancel(id) })
	case "tap":
		// A move first, so that a mouse tap leaves the hover state a real
		// mouse would leave; a touch never sets hover and is unaffected.
		if err := d.do(func(app *gift.App) { app.PointerMove(id, kind, d.at(s)) }); err != nil {
			return err
		}
		if err := d.do(func(app *gift.App) { app.PointerDown(id, kind, d.at(s)) }); err != nil {
			return err
		}
		return d.do(func(app *gift.App) { app.PointerUp(id, kind, d.at(s)) })
	case "click":
		// The whole gesture in one update, with the mouse pointer whatever
		// the step said. It exists because the backend's per-tick cursor poll
		// would otherwise turn a two-tick mouse press into a drag; see
		// [Step.Mouse]. Nothing else in this interface compresses a gesture
		// like this, and nothing else has to.
		return d.do(func(app *gift.App) {
			p := d.at(s)
			app.PointerMove(gift.MousePointer, gift.PointerMouse, p)
			app.PointerDown(gift.MousePointer, gift.PointerMouse, p)
			app.PointerUp(gift.MousePointer, gift.PointerMouse, p)
		})
	case "wheel":
		return d.do(func(app *gift.App) {
			app.PointerWheel(d.at(s), geom.Pt(s.DX, s.DY))
		})
	case "keyDown", "keyUp", "key":
		k, ok := keyByName[s.Key]
		if !ok {
			return fmt.Errorf("unknown key %q", s.Key)
		}
		mods, err := parseMods(s.Mods)
		if err != nil {
			return err
		}
		return d.key(s.Op, k, mods)
	case "text":
		for _, r := range s.Text {
			if err := d.do(func(app *gift.App) { app.TypeRune(r) }); err != nil {
				return err
			}
		}
		return nil
	case "focus":
		forward := true
		if s.Forward != nil {
			forward = *s.Forward
		}
		return d.do(func(app *gift.App) { app.MoveFocus(forward) })
	}
	return fmt.Errorf("unknown op %q", s.Op)
}

// key dispatches one of the three keyboard verbs.
func (d *driver) key(op string, k gift.Key, mods gift.Mods) error {
	down := func(app *gift.App) { app.SetModifiers(mods); app.KeyDown(k, mods) }
	up := func(app *gift.App) { app.KeyUp(k, mods) }
	switch op {
	case "keyDown":
		return d.do(down)
	case "keyUp":
		return d.do(up)
	}
	if err := d.do(down); err != nil {
		return err
	}
	return d.do(up)
}

// at is the position of a pointer step: the one it names, or the last one if
// it names none. It runs inside a posted closure, so the remembered position
// is only ever touched on the UI goroutine.
func (d *driver) at(s Step) geom.Point {
	if s.X != nil {
		d.pointer.X = *s.X
	}
	if s.Y != nil {
		d.pointer.Y = *s.Y
	}
	return d.pointer
}

// parseMods turns the modifier names of a step into a [gift.Mods] set.
func parseMods(names []string) (gift.Mods, error) {
	var m gift.Mods
	for _, n := range names {
		v, ok := modByName[n]
		if !ok {
			return 0, fmt.Errorf("unknown modifier %q", n)
		}
		m |= v
	}
	return m, nil
}
