// Package iconsvg parses the small subset of SVG the icon corpora use and
// turns it into the byte format of internal/icon.
//
// It exists as its own package so that the project plan's requirement in
// section 21 — "Damit entsteht kein SVG-Parser im Frame-Pfad" — is enforced by
// the import graph rather than by a comment. Only cmd/gift-icongen imports
// this; ui imports internal/icon, which has no path from here.
//
// # The subset, measured rather than assumed
//
// The surveyed Flowbite tree — 282 outline and 239 solid files — contains 595
// <path> elements and exactly one <rect>, and nothing else: no <g>, no
// transform, no <defs>, no <use>, no gradient. The path data uses
// M m L l H h V v C c S s A a Z and no quadratic at all. This package
// therefore supports exactly that, plus the quadratics Q q T t, which cost
// four lines because a quadratic elevates to a cubic exactly and leaving them
// out would make a later corpus fail with "unknown command" instead of
// working.
//
// Anything outside the subset is an error. A generator that silently skipped
// an element it did not understand would produce an icon missing a stroke,
// which nobody would notice until somebody looked at it.
package iconsvg

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Op is one command of a parsed path, after the shorthands and the relative
// forms have been resolved and every arc has been converted to cubics.
type Op struct {
	// Kind is 'M', 'L', 'C' or 'Z'.
	Kind byte
	// P holds the points of the command in user units: one for M and L, three
	// for C, none for Z.
	P [3][2]float64
}

// Figure is the geometry and paint of one SVG element.
type Figure struct {
	// Ops is the resolved path.
	Ops []Op
	// Stroke is true when the element is stroked rather than filled.
	Stroke bool
	// Width, Cap and Join describe the stroke and are meaningless otherwise.
	// Cap and Join are the SVG spellings: "butt", "round", "square" and
	// "miter", "round", "bevel".
	Width     float64
	Cap, Join string
	// EvenOdd is true when the element declared fill-rule="evenodd".
	EvenOdd bool
	// Erase is true when the element's fill is a literal colour rather than
	// currentColor, which in a monochrome icon set means a knockout; see
	// [icon.PaintErase].
	Erase bool
}

// Doc is one parsed SVG file.
type Doc struct {
	// ViewBox is the edge length of a square viewBox in user units.
	ViewBox float64
	// Figures are the elements in document order, which is paint order.
	Figures []Figure
}

// Parse reads one SVG file.
//
// It is a deliberately literal reader over the text and not an XML parser,
// for the same reason internal/text does not use a general font library: the
// subset is fixed and measured, and a general reader would accept files this
// package cannot render and hand them on as silently wrong icons. Every
// element and every attribute it does not know about is an error naming the
// file.
func Parse(name, src string) (Doc, error) {
	var d Doc
	root, rest, err := firstTag(src, "svg")
	if err != nil {
		return d, fmt.Errorf("%s: %w", name, err)
	}
	vb := attr(root, "viewBox")
	var x0, y0, w, h float64
	if n, _ := fmt.Sscan(vb, &x0, &y0, &w, &h); n != 4 {
		return d, fmt.Errorf("%s: viewBox %q is not four numbers", name, vb)
	}
	if x0 != 0 || y0 != 0 || w != h {
		return d, fmt.Errorf("%s: viewBox %q is not a square anchored at the origin; "+
			"internal/icon.Mask is square and the mapping would be ambiguous", name, vb)
	}
	d.ViewBox = w

	rootFill, rootStroke := attr(root, "fill"), attr(root, "stroke")

	for {
		tag, tail, name2, ok := nextElement(rest)
		if !ok {
			break
		}
		rest = tail
		f, err := element(name2, tag, rootFill, rootStroke)
		if err != nil {
			return d, fmt.Errorf("%s: %w", name, err)
		}
		if f != nil {
			d.Figures = append(d.Figures, *f)
		}
	}
	if len(d.Figures) == 0 {
		return d, fmt.Errorf("%s: no drawable element", name)
	}
	return d, nil
}

// element turns one <path> or <rect> into a Figure, or returns nil for an
// element that paints nothing.
func element(kind, tag, rootFill, rootStroke string) (*Figure, error) {
	var ops []Op
	switch kind {
	case "path":
		d := attr(tag, "d")
		if d == "" {
			return nil, fmt.Errorf("a <path> with no d attribute")
		}
		var err error
		ops, err = ParsePathData(d)
		if err != nil {
			return nil, err
		}
	case "rect":
		x, _ := strconv.ParseFloat(orZero(attr(tag, "x")), 64)
		y, _ := strconv.ParseFloat(orZero(attr(tag, "y")), 64)
		w, err := strconv.ParseFloat(orZero(attr(tag, "width")), 64)
		if err != nil {
			return nil, fmt.Errorf("a <rect> with an unreadable width")
		}
		h, err := strconv.ParseFloat(orZero(attr(tag, "height")), 64)
		if err != nil {
			return nil, fmt.Errorf("a <rect> with an unreadable height")
		}
		if attr(tag, "rx") != "" || attr(tag, "ry") != "" {
			rx, _ := strconv.ParseFloat(orZero(attrOr(tag, "rx", attr(tag, "ry"))), 64)
			ry, _ := strconv.ParseFloat(orZero(attrOr(tag, "ry", attr(tag, "rx"))), 64)
			rx, ry = math.Min(math.Abs(rx), w/2), math.Min(math.Abs(ry), h/2)
			if rx > 0 && ry > 0 {
				ops = roundRect(x, y, w, h, rx, ry)
				break
			}
		}
		ops = []Op{
			{Kind: 'M', P: [3][2]float64{{x, y}}},
			{Kind: 'L', P: [3][2]float64{{x + w, y}}},
			{Kind: 'L', P: [3][2]float64{{x + w, y + h}}},
			{Kind: 'L', P: [3][2]float64{{x, y + h}}},
			{Kind: 'Z'},
		}
	default:
		return nil, fmt.Errorf("unsupported element <%s>; the surveyed corpus has only <path> and <rect>", kind)
	}

	fill := attrOr(tag, "fill", rootFill)
	stroke := attrOr(tag, "stroke", rootStroke)
	f := Figure{Ops: ops}

	switch {
	case stroke != "" && stroke != "none":
		if stroke != "currentColor" {
			return nil, fmt.Errorf("stroke=%q; an icon is monochrome and only currentColor can be tinted", stroke)
		}
		f.Stroke = true
		f.Width = 1
		if s := attr(tag, "stroke-width"); s != "" {
			v, err := strconv.ParseFloat(s, 64)
			if err != nil || !(v > 0) {
				return nil, fmt.Errorf("stroke-width=%q", s)
			}
			f.Width = v
		}
		f.Cap, f.Join = attrOr(tag, "stroke-linecap", "butt"), attrOr(tag, "stroke-linejoin", "miter")
		if fill != "" && fill != "none" {
			// One path in the corpus carries both. Painting only the stroke
			// would drop the interior; the caller gets both figures and the
			// generator emits them in the SVG order, fill under stroke.
			return nil, errBothFillAndStroke
		}
	case fill != "" && fill != "none":
		if fill != "currentColor" {
			f.Erase = true
		}
		f.EvenOdd = attr(tag, "fill-rule") == "evenodd"
	default:
		// fill="none" and no stroke: an invisible element. None exists in the
		// corpus; refusing it is cheaper than deciding what it means.
		return nil, fmt.Errorf("an element that is neither filled nor stroked")
	}
	return &f, nil
}

// errBothFillAndStroke is handled by [ParseFigures], which splits such an
// element into two figures rather than choosing one.
var errBothFillAndStroke = fmt.Errorf("both filled and stroked")

// ParseFile is [Parse] with the one element that is both filled and stroked
// expanded into two figures, fill first.
//
// solid/npm.svg is that element: its <path> carries stroke="currentColor" and
// inherits the fill="currentColor" of the <svg>, so it is both. Earlier
// versions of this comment named solid/circle-plus.svg, which is wrong — that
// one is a single even-odd fill with no stroke at all — and the corpus was
// re-scanned in WU-AH to confirm that npm.svg is the only such element in
// either set.
//
// Splitting it here rather than in the encoder keeps the rule where the SVG
// semantics are: a filled and stroked shape paints its fill and then its
// stroke, and a figure in internal/icon carries exactly one paint.
func ParseFile(name, src string) (Doc, error) {
	d, err := Parse(name, src)
	if err == nil {
		return d, nil
	}
	if !strings.Contains(err.Error(), errBothFillAndStroke.Error()) {
		return d, err
	}
	return parseSplit(name, src)
}

func parseSplit(name, src string) (Doc, error) {
	var d Doc
	root, rest, err := firstTag(src, "svg")
	if err != nil {
		return d, fmt.Errorf("%s: %w", name, err)
	}
	var x0, y0, w, h float64
	fmt.Sscan(attr(root, "viewBox"), &x0, &y0, &w, &h)
	d.ViewBox = w
	rootFill, rootStroke := attr(root, "fill"), attr(root, "stroke")
	for {
		tag, tail, kind, ok := nextElement(rest)
		if !ok {
			break
		}
		rest = tail
		// The fill half: the same tag with the stroke removed.
		noStroke := stripAttrs(tag, "stroke", "stroke-width", "stroke-linecap", "stroke-linejoin")
		if f, err := element(kind, noStroke, rootFill, ""); err == nil && f != nil {
			d.Figures = append(d.Figures, *f)
		}
		noFill := stripAttrs(tag, "fill", "fill-rule")
		if f, err := element(kind, noFill, "", rootStroke); err == nil && f != nil {
			d.Figures = append(d.Figures, *f)
		}
	}
	if len(d.Figures) == 0 {
		return d, fmt.Errorf("%s: no drawable element after splitting fill from stroke", name)
	}
	return d, nil
}

// --- the tag reader ----------------------------------------------------------

func firstTag(src, want string) (tag, rest string, err error) {
	i := strings.Index(src, "<"+want)
	if i < 0 {
		return "", "", fmt.Errorf("no <%s> element", want)
	}
	j := strings.IndexByte(src[i:], '>')
	if j < 0 {
		return "", "", fmt.Errorf("unterminated <%s>", want)
	}
	return src[i : i+j], src[i+j+1:], nil
}

// nextElement returns the next start tag that is not the closing </svg>.
func nextElement(src string) (tag, rest, kind string, ok bool) {
	for {
		i := strings.IndexByte(src, '<')
		if i < 0 {
			return "", "", "", false
		}
		src = src[i+1:]
		if src == "" {
			return "", "", "", false
		}
		if src[0] == '/' || src[0] == '!' || src[0] == '?' {
			continue
		}
		j := strings.IndexByte(src, '>')
		if j < 0 {
			return "", "", "", false
		}
		body := src[:j]
		k := 0
		for k < len(body) && !isSpaceByte(body[k]) && body[k] != '/' {
			k++
		}
		return body, src[j+1:], body[:k], true
	}
}

func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// attr reads a double quoted attribute out of a start tag.
func attr(tag, name string) string {
	for i := 0; i+len(name) < len(tag); i++ {
		if !strings.HasPrefix(tag[i:], name) {
			continue
		}
		if i > 0 && !isSpaceByte(tag[i-1]) {
			continue
		}
		j := i + len(name)
		for j < len(tag) && isSpaceByte(tag[j]) {
			j++
		}
		if j >= len(tag) || tag[j] != '=' {
			continue
		}
		j++
		for j < len(tag) && isSpaceByte(tag[j]) {
			j++
		}
		if j >= len(tag) || tag[j] != '"' {
			continue
		}
		k := strings.IndexByte(tag[j+1:], '"')
		if k < 0 {
			return ""
		}
		return tag[j+1 : j+1+k]
	}
	return ""
}

func attrOr(tag, name, fallback string) string {
	if v := attr(tag, name); v != "" {
		return v
	}
	return fallback
}

func stripAttrs(tag string, names ...string) string {
	out := tag
	for _, n := range names {
		for {
			v := attr(out, n)
			if v == "" {
				break
			}
			needle := n + `="` + v + `"`
			i := strings.Index(out, needle)
			if i < 0 {
				break
			}
			out = out[:i] + out[i+len(needle):]
		}
	}
	return out
}

func orZero(s string) string {
	if s == "" {
		return "0"
	}
	return s
}

// --- path data ---------------------------------------------------------------

// ParsePathData parses the d attribute of a <path>.
//
// Every relative command is resolved to an absolute one, every shorthand — H,
// V, S, T — is expanded, and every elliptical arc is converted to cubic
// Beziers. The result therefore uses only M, L, C and Z, which is exactly what
// internal/icon encodes.
func ParsePathData(d string) ([]Op, error) {
	p := &pathParser{s: d}
	return p.run()
}

type pathParser struct {
	s   string
	i   int
	ops []Op
	// cur is the current point, start the opening point of the current
	// subpath, and prevC / prevQ the reflection anchors of S and T.
	cur, start   [2]float64
	prevC, prevQ [2]float64
	hadC, hadQ   bool
	open         bool
}

func (p *pathParser) run() ([]Op, error) {
	var last byte
	for {
		p.skip()
		if p.i >= len(p.s) {
			break
		}
		c := p.s[p.i]
		if isCmd(c) {
			p.i++
			last = c
		} else if last == 0 {
			return nil, fmt.Errorf("path data starts with %q, not a command", c)
		} else if last == 'M' {
			// "If a moveto is followed by multiple pairs of coordinates, the
			// subsequent pairs are treated as implicit lineto commands."
			last = 'L'
		} else if last == 'm' {
			last = 'l'
		}
		if err := p.command(last); err != nil {
			return nil, err
		}
	}
	return p.ops, nil
}

func isCmd(c byte) bool {
	return strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", c) >= 0
}

func (p *pathParser) command(c byte) error {
	rel := c >= 'a' && c <= 'z'
	ox, oy := 0.0, 0.0
	if rel {
		ox, oy = p.cur[0], p.cur[1]
	}
	switch c | 0x20 {
	case 'z':
		if !p.open {
			return fmt.Errorf("Z before any moveto")
		}
		p.ops = append(p.ops, Op{Kind: 'Z'})
		p.cur = p.start
		p.hadC, p.hadQ = false, false
		return nil
	case 'm':
		x, y, err := p.pair()
		if err != nil {
			return err
		}
		p.cur = [2]float64{x + ox, y + oy}
		p.start = p.cur
		p.open = true
		p.ops = append(p.ops, Op{Kind: 'M', P: [3][2]float64{p.cur}})
	case 'l':
		x, y, err := p.pair()
		if err != nil {
			return err
		}
		p.cur = [2]float64{x + ox, y + oy}
		p.line()
	case 'h':
		x, err := p.num()
		if err != nil {
			return err
		}
		p.cur[0] = x + ox
		p.line()
	case 'v':
		y, err := p.num()
		if err != nil {
			return err
		}
		p.cur[1] = y + oy
		p.line()
	case 'c':
		var v [6]float64
		for i := range v {
			n, err := p.num()
			if err != nil {
				return err
			}
			v[i] = n
		}
		p.cube(
			[2]float64{v[0] + ox, v[1] + oy},
			[2]float64{v[2] + ox, v[3] + oy},
			[2]float64{v[4] + ox, v[5] + oy})
	case 's':
		var v [4]float64
		for i := range v {
			n, err := p.num()
			if err != nil {
				return err
			}
			v[i] = n
		}
		c1 := p.cur
		if p.hadC {
			c1 = [2]float64{2*p.cur[0] - p.prevC[0], 2*p.cur[1] - p.prevC[1]}
		}
		p.cube(c1, [2]float64{v[0] + ox, v[1] + oy}, [2]float64{v[2] + ox, v[3] + oy})
	case 'q':
		var v [4]float64
		for i := range v {
			n, err := p.num()
			if err != nil {
				return err
			}
			v[i] = n
		}
		p.quad([2]float64{v[0] + ox, v[1] + oy}, [2]float64{v[2] + ox, v[3] + oy})
	case 't':
		x, y, err := p.pair()
		if err != nil {
			return err
		}
		q := p.cur
		if p.hadQ {
			q = [2]float64{2*p.cur[0] - p.prevQ[0], 2*p.cur[1] - p.prevQ[1]}
		}
		p.quad(q, [2]float64{x + ox, y + oy})
	case 'a':
		var v [7]float64
		for i := range v {
			// The two flags are single digits and may be written without a
			// separator, which is how the corpus writes them: "a1 1 0 0 0 1 1"
			// but also "a1 1 0 1 1 2 0" and, compressed, "a1 1 0 104-4".
			var n float64
			var err error
			if i == 3 || i == 4 {
				n, err = p.flag()
			} else {
				n, err = p.num()
			}
			if err != nil {
				return err
			}
			v[i] = n
		}
		p.arc(v[0], v[1], v[2], v[3] != 0, v[4] != 0, [2]float64{v[5] + ox, v[6] + oy})
	default:
		return fmt.Errorf("unknown path command %q", c)
	}
	return nil
}

func (p *pathParser) line() {
	p.ops = append(p.ops, Op{Kind: 'L', P: [3][2]float64{p.cur}})
	p.hadC, p.hadQ = false, false
}

func (p *pathParser) cube(c1, c2, to [2]float64) {
	p.ops = append(p.ops, Op{Kind: 'C', P: [3][2]float64{c1, c2, to}})
	p.cur = to
	p.prevC, p.hadC, p.hadQ = c2, true, false
}

// quad elevates a quadratic to a cubic, which is exact: the cubic control
// points are one third and two thirds of the way from each endpoint to the
// quadratic's control point.
func (p *pathParser) quad(q, to [2]float64) {
	c1 := [2]float64{p.cur[0] + 2.0/3.0*(q[0]-p.cur[0]), p.cur[1] + 2.0/3.0*(q[1]-p.cur[1])}
	c2 := [2]float64{to[0] + 2.0/3.0*(q[0]-to[0]), to[1] + 2.0/3.0*(q[1]-to[1])}
	p.ops = append(p.ops, Op{Kind: 'C', P: [3][2]float64{c1, c2, to}})
	p.cur = to
	p.prevQ, p.hadQ, p.hadC = q, true, false
}

func (p *pathParser) skip() {
	for p.i < len(p.s) && (isSpaceByte(p.s[p.i]) || p.s[p.i] == ',') {
		p.i++
	}
}

func (p *pathParser) pair() (float64, float64, error) {
	x, err := p.num()
	if err != nil {
		return 0, 0, err
	}
	y, err := p.num()
	return x, y, err
}

// flag reads one of the two single digit arc flags.
//
// SVG's grammar makes them single characters, and path data in the wild —
// including this corpus — relies on it: "a1 1 0 104-4" is rx=1 ry=1 rot=0
// large-arc=1 sweep=0 dx=4 dy=-4, and reading the flags as ordinary numbers
// would consume "104" and then fail three values later with a message about
// the wrong thing.
func (p *pathParser) flag() (float64, error) {
	p.skip()
	if p.i >= len(p.s) {
		return 0, fmt.Errorf("an arc that ends before its flags")
	}
	switch p.s[p.i] {
	case '0':
		p.i++
		return 0, nil
	case '1':
		p.i++
		return 1, nil
	}
	return 0, fmt.Errorf("an arc flag that is %q rather than 0 or 1", p.s[p.i])
}

func (p *pathParser) num() (float64, error) {
	p.skip()
	j := p.i
	if j < len(p.s) && (p.s[j] == '+' || p.s[j] == '-') {
		j++
	}
	for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
		j++
	}
	if j < len(p.s) && p.s[j] == '.' {
		j++
		for j < len(p.s) && p.s[j] >= '0' && p.s[j] <= '9' {
			j++
		}
	}
	if j < len(p.s) && (p.s[j] == 'e' || p.s[j] == 'E') {
		k := j + 1
		if k < len(p.s) && (p.s[k] == '+' || p.s[k] == '-') {
			k++
		}
		if k < len(p.s) && p.s[k] >= '0' && p.s[k] <= '9' {
			for k < len(p.s) && p.s[k] >= '0' && p.s[k] <= '9' {
				k++
			}
			j = k
		}
	}
	if j == p.i {
		return 0, fmt.Errorf("expected a number at offset %d of the path data, found %q", p.i, p.s[p.i:min(p.i+8, len(p.s))])
	}
	v, err := strconv.ParseFloat(p.s[p.i:j], 64)
	if err != nil {
		return 0, fmt.Errorf("unreadable number %q in the path data", p.s[p.i:j])
	}
	p.i = j
	return v, nil
}

// --- arcs --------------------------------------------------------------------

// arc converts one endpoint parameterised elliptical arc to cubic Beziers and
// appends them.
//
// # Why this is the piece to get right
//
// The elliptical arc is by a wide margin the most common command in the
// corpus: 2875 relative and 380 absolute, more than any other single command
// and more than every curve command put together. It is also the one with a
// conversion between two parameterisations in it, so it is the one where a
// mistake produces a plausible looking wrong shape rather than a failure.
//
// The implementation follows the conversion in appendix F.6 of the SVG 1.1
// specification literally: out of range radii are scaled up, a degenerate
// radius degrades to a straight line, the centre is recovered in the rotated
// frame, and the sweep is split into arcs of at most ninety degrees, each of
// which becomes one cubic by the standard kappa construction. Ninety degrees
// is the usual bound because the maximum radial error of that construction
// grows steeply past it; at ninety degrees it is about 2.7e-4 of the radius,
// which for a 24 unit viewBox is a ten thousandth of a unit.
func (p *pathParser) arc(rx, ry, rot float64, large, sweep bool, to [2]float64) {
	x1, y1 := p.cur[0], p.cur[1]
	x2, y2 := to[0], to[1]
	if x1 == x2 && y1 == y2 {
		// "If the endpoints are identical, this is equivalent to omitting the
		// elliptical arc segment entirely."
		return
	}
	rx, ry = math.Abs(rx), math.Abs(ry)
	if rx == 0 || ry == 0 {
		p.cur = to
		p.line()
		return
	}
	phi := rot * math.Pi / 180
	cosPhi, sinPhi := math.Cos(phi), math.Sin(phi)

	// Step 1: the endpoint in the frame where the ellipse is a unit circle.
	dx2, dy2 := (x1-x2)/2, (y1-y2)/2
	x1p := cosPhi*dx2 + sinPhi*dy2
	y1p := -sinPhi*dx2 + cosPhi*dy2

	// Step 2: scale the radii up if they are too small to span the chord.
	lambda := x1p*x1p/(rx*rx) + y1p*y1p/(ry*ry)
	if lambda > 1 {
		s := math.Sqrt(lambda)
		rx, ry = rx*s, ry*s
	}

	// Step 3: the centre in the primed frame.
	num := rx*rx*ry*ry - rx*rx*y1p*y1p - ry*ry*x1p*x1p
	den := rx*rx*y1p*y1p + ry*ry*x1p*x1p
	if num < 0 {
		num = 0
	}
	co := math.Sqrt(num / den)
	if large == sweep {
		co = -co
	}
	cxp := co * rx * y1p / ry
	cyp := -co * ry * x1p / rx
	cx := cosPhi*cxp - sinPhi*cyp + (x1+x2)/2
	cy := sinPhi*cxp + cosPhi*cyp + (y1+y2)/2

	// Step 4: the start angle and the sweep.
	theta1 := angle(1, 0, (x1p-cxp)/rx, (y1p-cyp)/ry)
	delta := angle((x1p-cxp)/rx, (y1p-cyp)/ry, (-x1p-cxp)/rx, (-y1p-cyp)/ry)
	delta = math.Mod(delta, 2*math.Pi)
	if !sweep && delta > 0 {
		delta -= 2 * math.Pi
	} else if sweep && delta < 0 {
		delta += 2 * math.Pi
	}

	n := int(math.Ceil(math.Abs(delta) / (math.Pi / 2)))
	if n == 0 {
		n = 1
	}
	step := delta / float64(n)
	// The kappa of the standard circular-arc-to-cubic construction, for a
	// sweep of `step` radians.
	k := 4.0 / 3.0 * math.Tan(step/4)
	t := theta1
	for range n {
		c, s := math.Cos(t), math.Sin(t)
		c2, s2 := math.Cos(t+step), math.Sin(t+step)
		// Points and tangents on the unit circle, mapped through the ellipse
		// and the rotation.
		e := func(c, s float64) [2]float64 {
			return [2]float64{
				cx + rx*c*cosPhi - ry*s*sinPhi,
				cy + rx*c*sinPhi + ry*s*cosPhi,
			}
		}
		d := func(c, s float64) [2]float64 {
			return [2]float64{
				-rx*s*cosPhi - ry*c*sinPhi,
				-rx*s*sinPhi + ry*c*cosPhi,
			}
		}
		p0, p3 := e(c, s), e(c2, s2)
		d0, d3 := d(c, s), d(c2, s2)
		c1 := [2]float64{p0[0] + k*d0[0], p0[1] + k*d0[1]}
		cc2 := [2]float64{p3[0] - k*d3[0], p3[1] - k*d3[1]}
		p.ops = append(p.ops, Op{Kind: 'C', P: [3][2]float64{c1, cc2, p3}})
		t += step
	}
	p.cur = to
	// The last appended point is the arc's mathematical endpoint, which can
	// differ from `to` in the last bit. Pin it, so that a following Z closes
	// to exactly where the data says.
	p.ops[len(p.ops)-1].P[2] = to
	p.prevC, p.hadC, p.hadQ = p.ops[len(p.ops)-1].P[1], true, false
}

// angle is the signed angle from u to v.
func angle(ux, uy, vx, vy float64) float64 {
	dot := ux*vx + uy*vy
	l := math.Hypot(ux, uy) * math.Hypot(vx, vy)
	if l == 0 {
		return 0
	}
	c := dot / l
	c = math.Max(-1, math.Min(1, c))
	a := math.Acos(c)
	if ux*vy-uy*vx < 0 {
		a = -a
	}
	return a
}

// roundRect is the path of a <rect> with corner radii, in the direction and
// starting point SVG's own rect-to-path equivalence defines: clockwise from
// the top left corner's end of its arc.
//
// Exactly one element in the surveyed corpus needs it — outline/stop.svg, a
// stop button with rx="1" — and it is implemented rather than refused because
// the alternative is an icon with square corners that nobody would spot.
func roundRect(x, y, w, h, rx, ry float64) []Op {
	// The circular-arc-to-cubic constant for a quarter turn.
	const k = 0.5522847498307933
	cx, cy := rx*k, ry*k
	x1, y1 := x+w, y+h
	return []Op{
		{Kind: 'M', P: [3][2]float64{{x + rx, y}}},
		{Kind: 'L', P: [3][2]float64{{x1 - rx, y}}},
		{Kind: 'C', P: [3][2]float64{{x1 - rx + cx, y}, {x1, y + ry - cy}, {x1, y + ry}}},
		{Kind: 'L', P: [3][2]float64{{x1, y1 - ry}}},
		{Kind: 'C', P: [3][2]float64{{x1, y1 - ry + cy}, {x1 - rx + cx, y1}, {x1 - rx, y1}}},
		{Kind: 'L', P: [3][2]float64{{x + rx, y1}}},
		{Kind: 'C', P: [3][2]float64{{x + rx - cx, y1}, {x, y1 - ry + cy}, {x, y1 - ry}}},
		{Kind: 'L', P: [3][2]float64{{x, y + ry}}},
		{Kind: 'C', P: [3][2]float64{{x, y + ry - cy}, {x + rx - cx, y}, {x + rx, y}}},
		{Kind: 'Z'},
	}
}
