package gift

import "sync"

// postbox is the only inbox of the UI executor.
//
// Worker goroutines do not touch state; they hand a closure to [App.Post] and
// the UI executor runs it at the beginning of the next [App.Update]. That is
// the "Worker-Ergebnisse werden gepostet" half of the project plan, section 5.
// The other half, checking the scope and the request generation before
// accepting the result, is [Token].
type postbox struct {
	mu      sync.Mutex
	pending []func()
	// running is the buffer being drained. Keeping two buffers and swapping
	// them means a drain holds the lock only for the swap, so a worker can
	// post while the previous batch runs, and neither side allocates once
	// both buffers have grown.
	running []func()
}

// Post queues fn to run on the UI executor at the beginning of the next
// [App.Update].
//
// This is the only method besides [App.Diagnostics] that may be called from
// another goroutine. It is how an asynchronous result reaches the user
// interface: the worker never touches state, it posts, and the posted closure
// runs where state is allowed to be touched. Callbacks are never invoked while
// a lock is held; see the project plan, section 5.
//
// Posting from inside a posted closure is allowed and defers the new closure
// to the following update, so a feedback loop cannot stall a frame.
func (a *App) Post(fn func()) {
	if fn == nil {
		panic("gift: App.Post with a nil function")
	}
	a.box.mu.Lock()
	a.box.pending = append(a.box.pending, fn)
	a.box.mu.Unlock()
}

// DrainPosts runs everything posted since the last update, outside a frame.
//
// An application needs it exactly once: after the last frame and after closing
// the asset pipeline. [asset.Config.Deliver] requires that every closure it is
// handed eventually runs, including the ones produced during Close, because a
// delivered thumbnail releases its reference *inside* the closure — an
// executor that drops one holds those pixels against the pixel budget until
// the process exits. The frame loop has stopped by then, so nothing would run
// them.
//
//	defer func() {
//	    pipe.Close()
//	    app.DrainPosts()
//	}()
//
// It must be called from the goroutine that ran the frame loop, like
// everything else that touches state. Calling it during a frame is pointless
// but harmless: [App.Update] drains the same queue.
func (a *App) DrainPosts() { a.drainPosts() }

// drainPosts runs everything posted since the last update.
func (a *App) drainPosts() {
	a.box.mu.Lock()
	if len(a.box.pending) == 0 {
		a.box.mu.Unlock()
		return
	}
	a.box.running, a.box.pending = a.box.pending, a.box.running[:0]
	a.box.mu.Unlock()

	for i, fn := range a.box.running {
		a.box.running[i] = nil // do not retain the closure past its run
		fn()
	}
	a.box.running = a.box.running[:0]
}

// Token identifies one asynchronous request of one component instance.
//
// A worker result must not be applied blindly. Between starting a request and
// its completion the component may have been unmounted, or it may have started
// a newer request whose answer supersedes this one. Applying a stale result
// then shows the wrong image in a recycled tile or resurrects the state of a
// dead scope. The project plan, sections 5 and 9, keeps content version,
// request generation and slot generation apart for exactly this reason.
//
// The pattern is:
//
//	tok := ctx.Request()
//	go func() {
//	    v := expensive()
//	    app.Post(func() {
//	        if !tok.Valid() {
//	            return // unmounted, or a newer request won
//	        }
//	        state.Set(v)
//	    })
//	}()
//
// The zero Token is never valid.
type Token struct {
	sc  *scope
	gen uint64
}

// Valid reports whether the result of this request may still be applied: the
// component instance is still mounted and no newer request has been started on
// it.
//
// It must be called on the UI executor, which is where a posted closure runs.
func (t Token) Valid() bool {
	return t.sc != nil && t.sc.alive && t.sc.gen == t.gen
}

// Request starts a new asynchronous request for this component instance and
// returns the [Token] that identifies it.
//
// Every call invalidates the tokens of all earlier requests of this instance,
// so the newest request always wins and the answers of the ones it replaced
// are dropped when they arrive.
func (c *Context) Request() Token {
	c.assertBuilding("Context.Request")
	c.scope.gen++
	return Token{sc: c.scope, gen: c.scope.gen}
}
