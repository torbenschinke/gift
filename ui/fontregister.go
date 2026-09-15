package ui

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// FontWeight is the visual thickness of a typeface on the usual 100..900
// scale, where 400 is the upright text weight and 700 is the bold one.
//
// The scale is the one OpenType's OS/2 usWeightClass and CSS both use, so a
// designer's "Medium 500" and a font file's name mean the same number here.
// Values outside 1..1000 are rejected at registration; a face that claims
// weight 0 is a typo, not a very light font.
type FontWeight int

// The named weights of the 100..900 scale. They are ordinary constants, not a
// closed set: [RegisterFont] takes any value in 1..1000, because a variable
// font instanced at 450 is a real face and refusing to name it would only
// push the caller into a cast.
const (
	WeightThin       FontWeight = 100
	WeightExtraLight FontWeight = 200
	WeightLight      FontWeight = 300
	WeightRegular    FontWeight = 400
	WeightMedium     FontWeight = 500
	WeightSemiBold   FontWeight = 600
	WeightBold       FontWeight = 700
	WeightExtraBold  FontWeight = 800
	WeightBlack      FontWeight = 900
)

// FontStyle distinguishes an upright face from an italic one.
//
// There is no oblique: gift does not synthesise a slant from an upright face
// and never will, because a sheared upright is not what the type designer drew
// and a toolkit that fakes it hides the fact that the italic was never
// installed. A request for an italic that is not registered fails; see
// [ResolveFont].
type FontStyle uint8

// The font styles. The zero value is [StyleNormal], so a [FontQuery] that
// omits the style asks for the upright face.
const (
	StyleNormal FontStyle = iota
	StyleItalic
)

func (s FontStyle) String() string {
	switch s {
	case StyleNormal:
		return "normal"
	case StyleItalic:
		return "italic"
	default:
		return fmt.Sprintf("FontStyle(%d)", uint8(s))
	}
}

// FontQuery names a face in the font register: a family, a weight and a style.
//
// The zero query is invalid — a family is required — but the zero weight is
// not: it means [WeightRegular], so ui.FontQuery{Family: inter.Family} asks
// for the ordinary upright text face and reads like it.
type FontQuery struct {
	// Family is the family name, matched exactly and case sensitively
	// against the name a face was registered under.
	//
	// Case sensitive because the names come from exported constants —
	// inter.Family, ibmplexmono.Family — and a case insensitive match would
	// have to lower case the query on every lookup, which allocates on a path
	// that runs during build. An unknown family is a missing import, and the
	// diagnosis [MustFont] prints names the families that are there.
	Family string

	// Weight is the desired thickness. Zero means [WeightRegular]. If the
	// exact weight is not registered, the nearest one is used; see
	// [ResolveFont].
	Weight FontWeight

	// Style is upright or italic. There is no substitution between the two.
	Style FontStyle
}

func (q FontQuery) weight() FontWeight {
	if q.Weight == 0 {
		return WeightRegular
	}
	return q.Weight
}

func (q FontQuery) String() string {
	return fmt.Sprintf("%s %d %s", q.Family, q.weight(), q.Style)
}

// FontFace is one registered face: what it was registered as, and the font
// itself.
type FontFace struct {
	Family string
	Weight FontWeight
	Style  FontStyle
	Font   Font
}

// fontKey is the exact identity of a registered face. It is a comparable
// struct of a string and two integers, so a map lookup with it does not
// allocate — which is what keeps [ResolveFont] out of the way of the
// allocation contract of the project plan, section 11.
type fontKey struct {
	family string
	weight FontWeight
	style  FontStyle
}

// fonts is the register. See [RegisterFont] for why it is process wide.
var fonts struct {
	mu sync.RWMutex
	m  map[fontKey]Font
	// families keeps, per family and style, the weights that are registered,
	// sorted ascending. It exists so that the nearest weight search is a walk
	// over a handful of integers instead of a scan of the whole register.
	families map[familyKey][]FontWeight
}

type familyKey struct {
	family string
	style  FontStyle
}

// RegisterFont adds a face to the process wide font register under a family, a
// weight and a style.
//
// It is meant to be called from the init function of a package that embeds a
// typeface, once per face:
//
//	func init() {
//		ui.RegisterFont(Family, ui.WeightRegular, ui.StyleNormal, mustLoad(regularTTF))
//	}
//
// and an application then pulls that package in for its side effect:
//
//	import _ "github.com/torbenschinke/gift/font/inter"
//
// # Why a register and not a second global
//
// Because [SetDefaultFont] is a single slot, and a single slot cannot hold two
// font packages. Two side effect imports would both have called it, in an init
// order the language does not fix, and the winner would have been whichever
// package the linker happened to order last — silently, with no error, and
// differently between builds. A register keyed by family, weight and style has
// room for both, so importing two typefaces is additive and the *application*
// decides which one is the default.
//
// # This is not a fallback chain
//
// The register resolves among the faces registered in it and does nothing
// else. If the chosen face has no glyph for a rune, no second face is
// consulted and the shaper emits the font's own notdef glyph. The project
// plan, section 14, keeps font fallback chains out of the project, and the
// shaper depends on it: it shapes a paragraph as a single run under a single
// script (internal/text/shaper.go, scriptOf), which is only honest *because*
// there is exactly one face per paragraph. Choosing a face and covering for a
// face are different problems and only the first one is solved here.
//
// # Duplicates panic
//
// Registering the same family, weight and style twice panics, following
// [gift.RegisterType]. A duplicate means two packages each believe they own a
// slot, and the alternative — last writer wins — is precisely the silent
// clobbering this register exists to prevent. A caller who really wants to
// replace a face resolves it and passes it explicitly.
//
// It is process wide and mutable state, with the caveat the project plan,
// section 13, records about such state: registering from init is safe because
// init runs before any test does, but a test that calls RegisterFont while
// another goroutine resolves is a data race that `go test -race` will report.
func RegisterFont(family string, weight FontWeight, style FontStyle, f Font) {
	if strings.TrimSpace(family) == "" {
		panic("gift/ui: RegisterFont needs a family name")
	}
	if weight < 1 || weight > 1000 {
		panic(fmt.Sprintf("gift/ui: RegisterFont(%q, %d): weight must be in 1..1000", family, weight))
	}
	if style != StyleNormal && style != StyleItalic {
		panic(fmt.Sprintf("gift/ui: RegisterFont(%q): unknown style %v", family, style))
	}
	if f.IsZero() {
		panic(fmt.Sprintf("gift/ui: RegisterFont(%q, %d, %v): the font is the zero Font", family, weight, style))
	}

	k := fontKey{family: family, weight: weight, style: style}
	fonts.mu.Lock()
	defer fonts.mu.Unlock()
	if fonts.m == nil {
		fonts.m = make(map[fontKey]Font, 8)
		fonts.families = make(map[familyKey][]FontWeight, 4)
	}
	if _, dup := fonts.m[k]; dup {
		panic(fmt.Sprintf("gift/ui: the font %q %d %v is already registered", family, weight, style))
	}
	fonts.m[k] = f

	fk := familyKey{family: family, style: style}
	ws := append(fonts.families[fk], weight)
	sort.Slice(ws, func(i, j int) bool { return ws[i] < ws[j] })
	fonts.families[fk] = ws
}

// ResolveFont returns the registered face that best matches q.
//
// The match is exact on family and on style and approximate on weight only:
//
//   - The family must be registered under exactly that name. Nothing is
//     substituted for an unknown family; ok is false.
//   - The style must be registered for that family. An italic is never
//     synthesised from an upright face and an upright is never substituted
//     for a missing italic; ok is false.
//   - The weight is the registered weight of that family and style with the
//     smallest absolute distance to the requested one. A tie goes to the
//     heavier face.
//
// # Why nearest, and why the tie goes up
//
// Because a register holds a handful of faces on purpose. Asking for Medium
// from a package that ships Regular and Bold has exactly two useful answers,
// and "no font, so the text does not render" is not one of them: the
// application asked for a shade of emphasis, not for a specific file. The tie
// goes to the heavier face because a weight is normally asked for in order to
// stand out from the surrounding text, and the lighter of two equidistant
// candidates is the one that fails to do that.
//
// This is deliberately *not* the CSS Fonts 4 matching algorithm, which
// additionally prefers lighter faces below 400 and has a special case for
// 400 and 500. That algorithm is right for a system with hundreds of installed
// faces and several fallbacks behind it; here it would be four extra branches
// to describe a situation — a family with many registered weights — that a
// register fed by go:embed does not have. One sentence a reader can hold in
// their head is worth more here than spec fidelity nobody can observe.
//
// ResolveFont takes a read lock and does not allocate. It is nevertheless not
// a frame path function: a [Text] resolves its font during build, once, and
// the allocation contract of the project plan, section 11, exempts build. The
// absence of allocations is asserted by a benchmark anyway, because a font
// lookup that allocated would be very easy to introduce and very hard to
// notice.
func ResolveFont(q FontQuery) (Font, bool) {
	w := q.weight()
	fonts.mu.RLock()
	defer fonts.mu.RUnlock()
	if f, ok := fonts.m[fontKey{family: q.Family, weight: w, style: q.Style}]; ok {
		return f, true
	}
	best, ok := nearestWeight(fonts.families[familyKey{family: q.Family, style: q.Style}], w)
	if !ok {
		return Font{}, false
	}
	return fonts.m[fontKey{family: q.Family, weight: best, style: q.Style}], true
}

// nearestWeight picks the entry of the ascending slice ws closest to w, with a
// tie going to the heavier one. It does not allocate.
func nearestWeight(ws []FontWeight, w FontWeight) (FontWeight, bool) {
	if len(ws) == 0 {
		return 0, false
	}
	best, bestDist := ws[0], abs(ws[0]-w)
	for _, cand := range ws[1:] {
		// <= and not <, walking ascending, is how the tie goes up.
		if d := abs(cand - w); d <= bestDist {
			best, bestDist = cand, d
		}
	}
	return best, true
}

func abs(v FontWeight) FontWeight {
	if v < 0 {
		return -v
	}
	return v
}

// MustFont is [ResolveFont] for the case where the caller has just imported
// the package that registers the face, and a miss is therefore a programming
// error rather than a condition to handle.
//
// It panics with a message listing every registered face, because the two
// causes of a miss — a forgotten side effect import and a misspelled family —
// are both answered by that list:
//
//	ui.SetDefaultFont(ui.MustFont(ui.FontQuery{Family: inter.Family}))
func MustFont(q FontQuery) Font {
	f, ok := ResolveFont(q)
	if ok {
		return f
	}
	panic(fmt.Sprintf(
		"gift/ui: no font registered for %s.\n"+
			"Registered faces: %s\n"+
			"A font package registers its faces from init, so it has to be imported:\n"+
			"\n"+
			"\timport _ \"github.com/torbenschinke/gift/font/inter\"\n"+
			"\n"+
			"or register your own with ui.RegisterFont.",
		q, describeFonts()))
}

// RegisteredFonts appends every registered face to dst and returns the
// extended slice, sorted by family, then style, then weight.
//
// It exists for diagnostics and for the one test that has to notice that a
// package registered something it did not mean to. The order is stable so
// that such a test can be written as a comparison and not as a set.
func RegisteredFonts(dst []FontFace) []FontFace {
	fonts.mu.RLock()
	start := len(dst)
	for k, f := range fonts.m {
		dst = append(dst, FontFace{Family: k.family, Weight: k.weight, Style: k.style, Font: f})
	}
	fonts.mu.RUnlock()
	out := dst[start:]
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Family != b.Family {
			return a.Family < b.Family
		}
		if a.Style != b.Style {
			return a.Style < b.Style
		}
		return a.Weight < b.Weight
	})
	return dst
}

// describeFonts renders the register for a failure message.
func describeFonts() string {
	faces := RegisteredFonts(nil)
	if len(faces) == 0 {
		return "none"
	}
	var b strings.Builder
	for i, f := range faces {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s %d %s", f.Family, f.Weight, f.Style)
	}
	return b.String()
}
