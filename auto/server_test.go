//go:build giftauto

package auto

import (
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/torbenschinke/gift"
	"github.com/torbenschinke/gift/ui"
)

// newTestServer mounts the fixture and returns the HTTP handler under test
// together with the loop driving it.
func newTestServer(t *testing.T) (http.Handler, *fakeLoop, *int) {
	t.Helper()
	count := new(int)
	l := newFakeLoop(t, func(*gift.Context) gift.View {
		return tapTarget{w: 100, h: 60, count: count, key: "hit"}
	}, func() byte { return byte(*count) })
	return (&server{d: l.d}).handler(), l, count
}

// get performs a request from a loopback peer, which is what net/http fills in
// for a real local connection.
func get(t *testing.T, h http.Handler, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:54321"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// TestEveryRouteRefusesANonLoopbackPeer is the security property of a tool
// that can synthesise any input and read back every pixel: it answers the
// machine it runs on and nothing else.
func TestEveryRouteRefusesANonLoopbackPeer(t *testing.T) {
	h, _, _ := newTestServer(t)
	routes := []string{"/health", "/screenshot", "/input", "/tree", "/query", "/diag", "/theme"}
	for _, route := range routes {
		r := httptest.NewRequest("GET", route, strings.NewReader("{}"))
		r.RemoteAddr = "192.168.1.14:41000"
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s from a LAN address: status %d, want 403", route, w.Code)
		}
		if strings.Contains(w.Body.String(), "frames") {
			t.Errorf("%s leaked application state to a refused peer: %s", route, w.Body.String())
		}
	}
	// And the loopback peer is served, so the guard is a guard and not a
	// wall.
	if w := get(t, h, "GET", "/health", ""); w.Code != http.StatusOK {
		t.Fatalf("/health from 127.0.0.1: status %d, want 200", w.Code)
	}
}

// TestAnUnparsableOrIPv6LoopbackPeerIsClassifiedCorrectly pins the two edges
// of the address check: ::1 is loopback, and an address that cannot be read is
// not.
func TestAnUnparsableOrIPv6LoopbackPeerIsClassifiedCorrectly(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:1":     true,
		"127.0.0.53:9999": true,
		"[::1]:7391":      true,
		"::1":             true,
		"10.0.0.1:80":     false,
		"":                false,
		"pipe":            false,
		"example.com:80":  false,
	}
	for addr, want := range cases {
		if got := isLoopback(addr); got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
}

// TestQueryFindsANodeByKeyAndReportsAPointToAimAt is the promise that a caller
// using curl never has to guess a coordinate.
func TestQueryFindsANodeByKeyAndReportsAPointToAimAt(t *testing.T) {
	h, l, count := newTestServer(t)

	w := get(t, h, "GET", "/query?key=hit", "")
	if w.Code != http.StatusOK {
		t.Fatalf("/query: status %d: %s", w.Code, w.Body.String())
	}
	var res struct {
		Count int    `json:"count"`
		Nodes []Node `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decoding the answer: %v", err)
	}
	if res.Count != 1 {
		t.Fatalf("matched %d nodes, want 1: %s", res.Count, w.Body.String())
	}
	n := res.Nodes[0]
	if n.Type != "auto.Tap" || n.Text != "tap me" || !n.Interactive {
		t.Fatalf("node = %+v, want the interactive auto.Tap labelled \"tap me\"", n)
	}

	// Aim at the centre it reported and the node fires, which is the whole
	// claim: query, then tap what it returned.
	body, _ := json.Marshal(Batch{Steps: []Step{{Op: "tap", X: &n.Centre[0], Y: &n.Centre[1]}}})
	if w := get(t, h, "POST", "/input", string(body)); w.Code != http.StatusOK {
		t.Fatalf("/input: status %d: %s", w.Code, w.Body.String())
	}
	got, err := query(l.d, func(*gift.App) int { return *count })
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("the centre reported by /query did not hit the node: activations = %d", got)
	}
}

// TestAScreenshotCarriesTheFrameCountSoAStaleFrameCannotPassForAFreshOne.
func TestAScreenshotCarriesTheFrameCountSoAStaleFrameCannotPassForAFreshOne(t *testing.T) {
	h, _, _ := newTestServer(t)
	w := get(t, h, "GET", "/screenshot", "")
	if w.Code != http.StatusOK {
		t.Fatalf("/screenshot: status %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if _, err := png.Decode(w.Body); err != nil {
		t.Fatalf("the body is not a PNG: %v", err)
	}
	first := w.Header().Get("X-Gift-Frames")
	if first == "" || first == "0" {
		t.Fatalf("X-Gift-Frames = %q, want the count of the frame that was captured", first)
	}
	if w.Header().Get("X-Gift-Density") == "" {
		t.Fatal("X-Gift-Density is missing, so a caller cannot map pixels onto logical coordinates")
	}
	w2 := get(t, h, "GET", "/screenshot", "")
	if second := w2.Header().Get("X-Gift-Frames"); second == first {
		t.Fatalf("two screenshots report the same frame %s; one of them is stale", second)
	}
}

// TestAScreenshotOfAnInvisibleWindowAnswers503WithTheDiagnosis keeps the one
// case that is not a defect from looking like one.
func TestAScreenshotOfAnInvisibleWindowAnswers503WithTheDiagnosis(t *testing.T) {
	h, l, _ := newTestServer(t)
	l.d.timeout = 100 * time.Millisecond
	l.setDrawing(false)

	w := get(t, h, "GET", "/screenshot", "")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "not visible") {
		t.Fatalf("the body does not name the cause: %s", w.Body.String())
	}
	// /health still answers, because it never touches the UI goroutine.
	if hw := get(t, h, "GET", "/health", ""); hw.Code != http.StatusOK {
		t.Fatalf("/health while nothing is drawn: status %d", hw.Code)
	}
}

// TestDiagReportsTheThemeAndTheFocusedNode covers the diagnostics endpoint's
// two answers that are not counters.
func TestDiagReportsTheThemeAndTheFocusedNode(t *testing.T) {
	h, _, _ := newTestServer(t)
	// A tap focuses the target, because the fixture requests the focus on a
	// press exactly as a control does.
	body := `{"steps":[{"op":"tap","x":10,"y":10}]}`
	if w := get(t, h, "POST", "/input", body); w.Code != http.StatusOK {
		t.Fatalf("/input: %s", w.Body.String())
	}
	w := get(t, h, "GET", "/diag", "")
	if w.Code != http.StatusOK {
		t.Fatalf("/diag: status %d: %s", w.Code, w.Body.String())
	}
	var d Diag
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatalf("decoding /diag: %v", err)
	}
	if d.Focus == nil || d.Focus.Type != "auto.Tap" {
		t.Fatalf("Focus = %+v, want the tapped node", d.Focus)
	}
	if len(d.Theme.Roles) < 12 {
		t.Fatalf("the theme table has %d roles, want every one of them", len(d.Theme.Roles))
	}
	if d.Updates == 0 || d.DrawnFrames == 0 {
		t.Fatalf("Updates = %d, DrawnFrames = %d, want both to be moving", d.Updates, d.DrawnFrames)
	}
}

// TestTintingARoleThroughTheThemeEndpointChangesTheReportedPalette is the
// mechanism behind the re-tintability check: a role that cannot be changed
// from here cannot be checked at all.
func TestTintingARoleThroughTheThemeEndpointChangesTheReportedPalette(t *testing.T) {
	h, _, _ := newTestServer(t)
	w := get(t, h, "POST", "/theme", `{"mode":"light","roles":{"accent":[255,0,255,255]}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("/theme: status %d: %s", w.Code, w.Body.String())
	}
	var info ThemeInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if got := info.Roles["accent"]; got != [4]uint8{255, 0, 255, 255} {
		t.Fatalf("accent = %v, want the magenta that was installed", got)
	}
	// Restore, so that a later test in this binary sees the default palette.
	get(t, h, "POST", "/theme", `{"mode":"light"}`)
}

// TestAMalformedBatchIsRejectedWithBadRequestAndNotWithAPanic.
func TestAMalformedBatchIsRejectedWithBadRequestAndNotWithAPanic(t *testing.T) {
	h, _, _ := newTestServer(t)
	if w := get(t, h, "POST", "/input", "{not json"); w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
}

// TestEveryColourRoleOfTheUIPackageIsReachable is the completeness gate this
// package did not have.
//
// /diag reported its palette from a hand written slice and /theme resolved a
// role name from the same one. A role added to ui would therefore have been
// invisible here and untintable from here — and this is the interface the role
// audit uses to decide whether a role is used at all, so an omission would
// have presented as "the application does not use that colour" rather than as
// "the tool does not know that colour". That is the worst way round.
//
// The assertion is against [ui.SemanticColors], which is the enum itself, so
// it cannot fall behind it. It fails if a role is missing from the report, and
// it fails if a role cannot be tinted through /theme, because those are two
// different tables in every implementation that has them as tables.
func TestEveryColourRoleOfTheUIPackageIsReachable(t *testing.T) {
	h, _, _ := newTestServer(t)
	defer get(t, h, "POST", "/theme", `{"mode":"light"}`)

	w := get(t, h, "GET", "/diag", "")
	if w.Code != http.StatusOK {
		t.Fatalf("/diag: status %d", w.Code)
	}
	var d Diag
	if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
		t.Fatalf("decoding /diag: %v", err)
	}
	roles := ui.SemanticColors()
	if len(roles) == 0 {
		t.Fatal("ui.SemanticColors is empty; the fixture is wrong")
	}
	if len(d.Theme.Roles) != len(roles) {
		t.Errorf("/diag reports %d roles and ui has %d", len(d.Theme.Roles), len(roles))
	}
	for _, c := range roles {
		name := jsonRoleName(c.Name)
		if _, ok := d.Theme.Roles[name]; !ok {
			t.Errorf("/diag does not report the role %s, which it calls %q", c.Name, name)
			continue
		}
		body := `{"roles":{"` + name + `":[255,0,255,255]}}`
		tw := get(t, h, "POST", "/theme", body)
		if tw.Code != http.StatusOK {
			t.Errorf("/theme %s: status %d", name, tw.Code)
			continue
		}
		var info ThemeInfo
		if err := json.Unmarshal(tw.Body.Bytes(), &info); err != nil {
			t.Errorf("decoding /theme for %s: %v", name, err)
			continue
		}
		if got := info.Roles[name]; got != [4]uint8{255, 0, 255, 255} {
			t.Errorf("after tinting %s the theme reports %v, want the magenta that was installed",
				name, got)
		}
	}
}

// TestTheJSONSpellingOfARoleNameDropsTheColorPrefix pins the mapping
// [jsonRoleName] performs, because it is the one place the wire format is
// decided and a change to it breaks every driver script in existence.
func TestTheJSONSpellingOfARoleNameDropsTheColorPrefix(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"ColorAccent", "accent"},
		{"ColorSecondaryLabel", "secondaryLabel"},
		{"ColorControlPressed", "controlPressed"},
		{"ColorBackground", "background"},
		{"Color", "Color"},
		{"Whatever", "whatever"},
	} {
		if got := jsonRoleName(c.in); got != c.want {
			t.Errorf("jsonRoleName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
