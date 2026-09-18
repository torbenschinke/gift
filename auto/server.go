//go:build giftauto

package auto

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"strconv"
	"strings"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/ui"
)

// server is the HTTP half: it parses, calls the driver, and serialises. It
// contains no synchronisation of its own — every route goes through
// [driver.do] — which is what keeps the two halves separately reviewable.
type server struct {
	d *driver
}

// handler returns the routes of the automation interface, wrapped in the
// loopback guard.
//
// # The routes
//
//	GET  /health      counters and liveness
//	GET  /screenshot  a PNG of the real framebuffer
//	POST /input       a batch of input steps with waits, and frame bursts
//	GET  /tree        the retained tree
//	GET  /query       the nodes matching a filter, flat
//	GET  /diag        diagnostics, overflow, theme, focus
//	POST /theme       install a theme, for checking that roles are re-tintable
func (s *server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.health)
	mux.HandleFunc("/screenshot", s.screenshot)
	mux.HandleFunc("/input", s.input)
	mux.HandleFunc("/tree", s.tree)
	mux.HandleFunc("/query", s.query)
	mux.HandleFunc("/diag", s.diag)
	mux.HandleFunc("/theme", s.theme)
	return loopbackOnly(mux)
}

// loopbackOnly refuses every request that did not come from the loopback
// interface.
//
// The listener is already bound to a loopback address, so this is the second
// of two locks on the same door, and it is here because the first one is a
// configuration and this one is a property of the code. An operator who sets
// GIFT_AUTO_ADDR to :7391 out of habit widens the listener; they do not widen
// this. Anything that is not 127.0.0.0/8 or ::1 gets 403 and no information
// about the application whatsoever.
func loopbackOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopback(r.RemoteAddr) {
			http.Error(w, "gift/auto: refused: this interface answers on loopback only. "+
				"It is a debugging tool with no authentication that can synthesise any "+
				"input and read back every pixel, so it must never be reachable from a "+
				"network.", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopback reports whether addr, in the host:port form net/http fills
// RemoteAddr with, is a loopback address.
//
// An address that cannot be parsed is not loopback. That is the safe
// direction: a caller this function cannot identify is a caller it must not
// serve.
func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}

// writeJSON serialises v, or the error if there was one.
func (s *server) writeJSON(w http.ResponseWriter, v any, err error) {
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// fail answers with a diagnosis. A wait that expired because nothing is being
// drawn is 503 and not 500: the application is not broken, it is not on
// screen, and the text says which.
func (s *server) fail(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	if isNotDrawn(err) || isNoApp(err) {
		code = http.StatusServiceUnavailable
	}
	http.Error(w, "gift/auto: "+err.Error(), code)
}

// health is the cheapest answer: it takes no lock on the UI goroutine at all,
// because [gift.App.Diagnostics] is explicitly safe from any goroutine. A
// caller polls this while the application is wedged and still gets an answer.
func (s *server) health(w http.ResponseWriter, r *http.Request) {
	app, _, err := s.d.target()
	if err != nil {
		s.fail(w, err)
		return
	}
	d := app.Diagnostics()
	s.writeJSON(w, map[string]any{
		"ok":          true,
		"frames":      d.Frames,
		"updates":     d.Updates,
		"drawnFrames": s.d.Frames(),
	}, nil)
}

// screenshot answers with a PNG of the framebuffer the window actually drew.
//
// The image is read back inside Draw from the screen image Ebitengine hands
// the backend, so it is the picture on the display and not a second render of
// the same scene. The drawn frame count is in the X-Gift-Frames header of
// every answer, next to the density in X-Gift-Density, so that a caller can
// always tell a fresh frame from a stale one and can map pixels back onto the
// logical coordinates an input step uses.
func (s *server) screenshot(w http.ResponseWriter, r *http.Request) {
	if n, err := strconv.Atoi(r.URL.Query().Get("settle")); err == nil && n > 0 {
		if err := s.d.waitFrames(uint64(n)); err != nil {
			s.fail(w, err)
			return
		}
	}
	shot, err := s.d.screenshot()
	if err != nil {
		s.fail(w, err)
		return
	}
	density, _ := query(s.d, func(app *gift.App) float32 { return app.Density() })
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Gift-Frames", strconv.FormatUint(shot.count, 10))
	w.Header().Set("X-Gift-Drawn-Frames", strconv.FormatUint(s.d.Frames(), 10))
	w.Header().Set("X-Gift-Density", strconv.FormatFloat(float64(density), 'g', -1, 32))
	_ = png.Encode(w, shot.img)
}

// input executes a batch of steps and answers with the frame counters.
func (s *server) input(w http.ResponseWriter, r *http.Request) {
	var b Batch
	if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
		http.Error(w, "gift/auto: malformed batch: "+err.Error(), http.StatusBadRequest)
		return
	}
	res, err := s.d.runBatch(b)
	s.writeJSON(w, res, err)
}

// tree answers with the retained tree. depth limits how far down it goes; the
// default is the whole tree.
func (s *server) tree(w http.ResponseWriter, r *http.Request) {
	depth := maxTreeDepth
	if n, err := strconv.Atoi(r.URL.Query().Get("depth")); err == nil && n >= 0 {
		depth = n
	}
	t, err := query(s.d, func(app *gift.App) Tree { return buildTree(app, depth) })
	s.writeJSON(w, t, err)
}

// query answers with the nodes matching a filter, flat, so that a caller can
// find a target by key or by text and then aim at its centre without ever
// guessing a coordinate.
func (s *server) query(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := Filter{
		Key:         q.Get("key"),
		Text:        q.Get("text"),
		TextContain: q.Get("contains"),
		Type:        q.Get("type"),
		VisibleOnly: q.Get("visible") == "1",
		Interactive: q.Get("interactive") == "1",
	}
	limit, _ := strconv.Atoi(q.Get("limit"))
	out, err := query(s.d, func(app *gift.App) []Node {
		t := buildTree(app, maxTreeDepth)
		return flatten(t.Root, f, nil)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	s.writeJSON(w, map[string]any{"count": len(out), "nodes": out}, err)
}

// diag answers with the counters, the overflowing nodes, the installed theme
// and the focused node.
func (s *server) diag(w http.ResponseWriter, r *http.Request) {
	drawn := s.d.Frames()
	d, err := query(s.d, func(app *gift.App) Diag { return buildDiag(app, drawn) })
	s.writeJSON(w, d, err)
}

// theme installs a theme at runtime, which is how the claim that the semantic
// colour roles are genuinely re-tintable stops being a claim.
//
//	curl -XPOST localhost:7391/theme -d '{"mode":"dark"}'
//	curl -XPOST localhost:7391/theme -d '{"roles":{"accent":[255,0,255,255]}}'
//
// A role a screenshot does not change when it is tinted magenta is a role the
// application does not actually use.
func (s *server) theme(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mode  string              `json:"mode"`
		Roles map[string][4]uint8 `json:"roles"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "gift/auto: malformed theme: "+err.Error(), http.StatusBadRequest)
		return
	}
	info, err := query(s.d, func(app *gift.App) ThemeInfo {
		t := ui.CurrentTheme()
		switch req.Mode {
		case "light":
			t = ui.LightTheme()
		case "dark":
			t = ui.DarkTheme()
		}
		for name, v := range req.Roles {
			role, ok := roleByName(name)
			if !ok {
				continue
			}
			t = t.With(role, ui.RGBA(v[0], v[1], v[2], v[3]))
		}
		ui.SetTheme(app, t)
		return themeInfo()
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	// The theme is installed; the tree it changed is rebuilt in the update
	// after this one. Waiting for that update is what makes a screenshot
	// taken straight afterwards show the new colours.
	_ = s.d.waitTicks(2)
	s.writeJSON(w, info, nil)
}

// roleByName resolves the JSON spelling of a colour role.
func roleByName(name string) (ui.Color, bool) {
	for _, r := range themeRoles {
		if r.name == name {
			return r.c, true
		}
	}
	return ui.Color{}, false
}

// isNotDrawn and isNoApp classify the two failures that are not the
// application's fault.
func isNotDrawn(err error) bool { return errors.Is(err, errNotDrawn) }
func isNoApp(err error) bool    { return errors.Is(err, errNoApp) }

// describe is the line printed on startup.
func describe(addr string) string {
	return fmt.Sprintf("gift/auto: automation interface on http://%s "+
		"(loopback only, debugging tool, built with -tags giftauto)", addr)
}
