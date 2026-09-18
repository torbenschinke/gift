//go:build giftauto

package auto

import (
	"strings"

	"github.com/worldiety/gift"
	"github.com/worldiety/gift/ui"
)

// maxTreeDepth bounds a tree walk. It is the same order as the scene's own
// depth limit and exists so that a cycle — which cannot happen, but a
// debugging tool is exactly where an impossible tree shows up — produces a
// truncated answer instead of a stack overflow in the process under test.
const maxTreeDepth = 256

// Node is one node of the retained tree as this interface reports it.
//
// It carries what a caller needs to find a target and aim at it without
// guessing coordinates: the type, the key, the label, where the node is on
// screen, whether any of it survives the clips above it, and the four
// interaction bits. Centre is the point a tap should use, so a shell script
// never does arithmetic.
type Node struct {
	// Path is the index path from the root, such as "0.2.1". It is stable
	// for as long as the tree shape is, and it is what /query hands back so
	// that a second request can talk about the same node.
	Path string `json:"path"`

	Type  string `json:"type"`
	Key   string `json:"key,omitempty"`
	Text  string `json:"text,omitempty"`
	Depth int    `json:"depth"`

	// Bounds is the device-space rectangle in logical pixels: x, y, width,
	// height, after the scroll offsets and transforms above the node.
	Bounds [4]float32 `json:"bounds"`
	// Centre is the middle of Bounds, which is the point to aim at.
	Centre [2]float32 `json:"centre"`
	// Visible reports whether any part of the node survives the clips of its
	// ancestors. A node in an inactive tab or scrolled out of its viewport
	// is not visible; it is still mounted and still in this answer.
	Visible bool `json:"visible"`

	Interactive bool `json:"interactive,omitempty"`
	Focusable   bool `json:"focusable,omitempty"`
	Hover       bool `json:"hover,omitempty"`
	Pressed     bool `json:"pressed,omitempty"`
	Focused     bool `json:"focused,omitempty"`
	Disabled    bool `json:"disabled,omitempty"`

	// Overflow is by how much the content of this node exceeds the size it
	// reported, or nil when it fits.
	Overflow *[2]float32 `json:"overflow,omitempty"`

	// Scroll describes the node when it is a scroll container.
	Scroll *Scroll `json:"scroll,omitempty"`

	Children []Node `json:"children,omitempty"`
}

// Scroll is the scroll state of a container, as far as a driver needs it.
type Scroll struct {
	Axis      string  `json:"axis"`
	Offset    float64 `json:"offset"`
	MaxOffset float64 `json:"maxOffset"`
	Content   float64 `json:"contentExtent"`
	Viewport  float32 `json:"viewportExtent"`
	Flinging  bool    `json:"flinging"`
	Dragging  bool    `json:"dragging"`
}

// Tree is the answer of /tree.
type Tree struct {
	// Viewport is the logical size the application was last laid out at, and
	// Density the factor between logical and physical pixels. A screenshot is
	// Viewport times Density pixels, which is the mapping a caller needs to
	// go from a pixel in a PNG to a coordinate in an input step.
	Viewport [2]float32 `json:"viewport"`
	Density  float32    `json:"density"`
	Frames   uint64     `json:"frames"`
	Updates  uint64     `json:"updates"`
	Root     Node       `json:"root"`
}

// buildTree walks the retained tree. It runs on the UI goroutine.
func buildTree(app *gift.App, maxDepth int) Tree {
	d := app.Diagnostics()
	t := Tree{
		Density: app.Density(),
		Frames:  d.Frames,
		Updates: d.Updates,
	}
	v := app.Viewport()
	t.Viewport = [2]float32{v.W, v.H}
	t.Root = buildNode(app, app.Root(), "0", 0, maxDepth)
	return t
}

// buildNode converts one node and its subtree.
func buildNode(app *gift.App, r gift.NodeRef, path string, depth, maxDepth int) Node {
	n := Node{Path: path, Depth: depth}
	if !app.NodeValid(r) {
		return n
	}
	n.Type = gift.TypeName(app.NodeType(r))
	n.Key = app.NodeKey(r)
	n.Text = app.NodeLabel(r)
	b := app.NodeDeviceBounds(r)
	n.Bounds = [4]float32{b.Min.X, b.Min.Y, b.Width(), b.Height()}
	n.Centre = [2]float32{b.Min.X + b.Width()/2, b.Min.Y + b.Height()/2}
	_, n.Visible = app.NodeVisibleBounds(r)
	n.Interactive = app.NodeInteractive(r)
	n.Focusable = app.NodeFocusable(r)
	ia := app.NodeInteraction(r)
	n.Hover, n.Pressed, n.Focused, n.Disabled = ia.Hover, ia.Pressed, ia.Focused, ia.Disabled
	if over, ok := app.NodeOverflow(r); ok {
		n.Overflow = &[2]float32{over.W, over.H}
	}
	if si, ok := app.ScrollInfo(r); ok {
		n.Scroll = &Scroll{
			Axis: axisName(si.Axis), Offset: si.Offset, MaxOffset: si.MaxOffset,
			Content: si.ContentExtent, Viewport: si.ViewportExtent,
			Flinging: si.Flinging, Dragging: si.Dragging,
		}
	}
	if depth >= maxDepth || depth >= maxTreeDepth {
		return n
	}
	var kids []gift.NodeRef
	kids = app.NodeChildren(r, kids)
	for i, c := range kids {
		n.Children = append(n.Children, buildNode(app, c, path+"."+itoa(i), depth+1, maxDepth))
	}
	return n
}

// axisName is the JSON spelling of a scroll axis.
func axisName(a gift.ScrollAxis) string {
	if a == gift.ScrollHorizontal {
		return "horizontal"
	}
	return "vertical"
}

// Filter is the query of /query: every field that is set has to match.
type Filter struct {
	Key         string
	Text        string
	TextContain string
	Type        string
	VisibleOnly bool
	Interactive bool
	Limit       int
}

// matches reports whether n satisfies the filter.
func (f Filter) matches(n Node) bool {
	switch {
	case f.Key != "" && n.Key != f.Key:
		return false
	case f.Text != "" && n.Text != f.Text:
		return false
	case f.TextContain != "" && !strings.Contains(n.Text, f.TextContain):
		return false
	case f.Type != "" && n.Type != f.Type:
		return false
	case f.VisibleOnly && !n.Visible:
		return false
	case f.Interactive && !n.Interactive:
		return false
	}
	return true
}

// flatten collects every node of the tree that matches the filter, without its
// children, so that the answer of a query is a flat list of aiming points.
func flatten(n Node, f Filter, out []Node) []Node {
	if f.matches(n) {
		m := n
		m.Children = nil
		out = append(out, m)
	}
	for _, c := range n.Children {
		out = flatten(c, f, out)
	}
	return out
}

// Diag is the answer of /diag.
type Diag struct {
	gift.Diagnostics
	// Overflowing names the nodes the OverflowNodes counter counts.
	Overflowing []Node `json:"overflowing,omitempty"`
	// Theme is the installed colour theme, role by role.
	Theme ThemeInfo `json:"theme"`
	// Focus is the node holding the keyboard focus, if any.
	Focus *Node `json:"focus,omitempty"`
	// SoftKeyboard reports whether the application is asking for gift's own
	// on-screen keyboard.
	SoftKeyboard bool       `json:"softKeyboard"`
	Viewport     [2]float32 `json:"viewport"`
	Density      float32    `json:"density"`
	// DrawnFrames is the number of frames the *backend* drew since the
	// automation interface started. It stands next to Frames, which is the
	// number of times gift painted, so a backend that stopped drawing is
	// visible from this answer alone.
	DrawnFrames uint64 `json:"drawnFrames"`
}

// ThemeInfo is the installed theme as a table a caller can compare against a
// screenshot.
type ThemeInfo struct {
	Dark bool `json:"dark"`
	// Roles maps the name of a semantic colour role onto its literal RGBA,
	// in the 0..255 non-premultiplied spelling a human reads.
	Roles map[string][4]uint8 `json:"roles"`
}

// themeRoles is the table /diag reports and /theme accepts: every semantic
// colour of the ui package, under the lowerCamel spelling this interface uses
// in JSON.
//
// It is derived from [ui.SemanticColors] and is deliberately no longer a
// literal slice. It used to be one, with the comment "it is written out rather
// than derived because the role enum is unexported in ui, and a debugging tool
// that silently omitted a role would be the worst possible place to learn
// that" — which named the hazard exactly and then walked into it, because
// nothing checked the list against the enum. A role added to ui would have
// been absent from /diag and unreachable from /theme, so the one tool that can
// prove a role is used would have reported it as unused. ui exports the
// enumeration now; see TestEveryColourRoleOfTheUIPackageIsReachable.
var themeRoles = buildThemeRoles()

type themeRole struct {
	name string
	c    ui.Color
}

func buildThemeRoles() []themeRole {
	all := ui.SemanticColors()
	out := make([]themeRole, 0, len(all))
	for _, c := range all {
		out = append(out, themeRole{jsonRoleName(c.Name), c.Color})
	}
	return out
}

// jsonRoleName turns the Go identifier of a semantic colour into the spelling
// this interface uses on the wire: "ColorSecondaryLabel" becomes
// "secondaryLabel".
//
// The mapping is mechanical rather than a second table, for the reason
// themeRoles is derived: a table is a thing that falls behind. A name that
// does not start with "Color" is passed through with its first letter lowered,
// so an unexpected addition is still reachable under a name rather than
// silently dropped.
func jsonRoleName(goName string) string {
	s := strings.TrimPrefix(goName, "Color")
	if s == "" {
		return goName
	}
	return strings.ToLower(s[:1]) + s[1:]
}

// themeInfo reads the installed theme. It runs on the UI goroutine, like
// everything else this package asks for.
func themeInfo() ThemeInfo {
	t := ui.CurrentTheme()
	info := ThemeInfo{Dark: t.IsDark(), Roles: make(map[string][4]uint8, len(themeRoles))}
	for _, r := range themeRoles {
		info.Roles[r.name] = rgba8(t.Color(r.c))
	}
	return info
}

// rgba8 converts a premultiplied float colour into the eight bit,
// non-premultiplied form a human compares against a screenshot.
func rgba8(c ui.Color) [4]uint8 {
	if c.A <= 0 {
		return [4]uint8{}
	}
	f := func(v float32) uint8 {
		v = v / c.A * 255
		switch {
		case v < 0:
			return 0
		case v > 255:
			return 255
		}
		return uint8(v + 0.5)
	}
	return [4]uint8{f(c.R), f(c.G), f(c.B), uint8(c.A*255 + 0.5)}
}

// buildDiag collects everything /diag answers. It runs on the UI goroutine.
func buildDiag(app *gift.App, drawn uint64) Diag {
	d := Diag{
		Diagnostics:  app.Diagnostics(),
		Theme:        themeInfo(),
		SoftKeyboard: app.SoftKeyboardRequested(),
		Density:      app.Density(),
		DrawnFrames:  drawn,
	}
	v := app.Viewport()
	d.Viewport = [2]float32{v.W, v.H}
	if r, ok := app.Focus(); ok {
		n := buildNode(app, r, "focus", 0, 0)
		d.Focus = &n
	}
	if d.OverflowNodes > 0 {
		d.Overflowing = collectOverflow(app, app.Root(), "0", 0, nil)
	}
	return d
}

// collectOverflow walks the tree for the nodes the overflow counter counts.
func collectOverflow(app *gift.App, r gift.NodeRef, path string, depth int, out []Node) []Node {
	if !app.NodeValid(r) || depth > maxTreeDepth {
		return out
	}
	if _, ok := app.NodeOverflow(r); ok {
		out = append(out, buildNode(app, r, path, depth, 0))
	}
	var kids []gift.NodeRef
	for i, c := range app.NodeChildren(r, kids) {
		out = collectOverflow(app, c, path+"."+itoa(i), depth+1, out)
	}
	return out
}

// itoa is strconv.Itoa without the import in a file that needs nothing else
// from it.
func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}
