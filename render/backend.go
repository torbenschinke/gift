package render

import "github.com/torbenschinke/gift/geom"

// Backend is the contract a concrete renderer fulfils. The Ebitengine
// implementation lives in backend/ebiten.
//
// Exactly one backend instance belongs to one window and is driven from the
// UI executor goroutine. The methods are called in the order BeginFrame,
// Submit, EndFrame, once per drawn frame. Note that a frame is drawn less
// often than the application is updated; see the project plan, section 6.
type Backend interface {
	// BeginFrame is called once per drawn frame, before any Submit. size is
	// the current drawable size in logical pixels.
	BeginFrame(size geom.Size)

	// Submit draws the operations of l.
	//
	// The list is borrowed for the duration of the call only. The backend
	// must not retain l or any slice obtained from it, because the producer
	// reuses the backing arrays for the next frame. Anything the backend
	// needs later, for example for a deferred material pass, must be copied
	// into backend owned memory.
	Submit(l *List)

	// EndFrame is called once per drawn frame after the last Submit. It is
	// the point at which the backend may present and recycle per frame
	// resources.
	EndFrame()
}
