package ui_test

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/worldiety/gift/font/ibmplexmono"
	"github.com/worldiety/gift/font/inter"
	"github.com/worldiety/gift/ui"
)

// The two bundled font packages are imported by name rather than for their
// side effect, because these tests assert what they registered. Importing them
// at all is the subject of TestTwoFontPackagesDoNotClobberEachOther.

// TestTwoFontPackagesDoNotClobberEachOther is the defect that motivated the
// register.
//
// With a single [ui.SetDefaultFont] slot, two font packages initialising in an
// order the language does not fix would each have written it, one of them
// would have won, and nothing would have said so. Here both are present at the
// same time, under their own families, and neither has touched the default.
func TestTwoFontPackagesDoNotClobberEachOther(t *testing.T) {
	sans, ok := ui.ResolveFont(ui.FontQuery{Family: inter.Family})
	if !ok {
		t.Fatal("Inter is not registered although font/inter is imported")
	}
	mono, ok := ui.ResolveFont(ui.FontQuery{Family: ibmplexmono.Family})
	if !ok {
		t.Fatal("IBM Plex Mono is not registered although font/ibmplexmono is imported")
	}
	if sans.IsZero() || mono.IsZero() {
		t.Fatal("a registered face resolved to the zero Font")
	}
	if sameFont(sans, mono) {
		t.Fatal("the two families resolved to the same face, so one overwrote the other")
	}

	// The important half: a font package registers and does not decide. No
	// face of either bundled package may have ended up in the default slot,
	// and the check has to cover every face rather than the two resolved
	// above — a package that installed its *last* registered face as the
	// default would otherwise slip through.
	def := ui.DefaultFont()
	if def.IsZero() {
		return
	}
	for _, f := range ui.RegisteredFonts(nil) {
		if f.Family != inter.Family && f.Family != ibmplexmono.Family {
			continue
		}
		if sameFont(def, f.Font) {
			t.Fatalf("importing a font package installed %s %d %v as the default font",
				f.Family, f.Weight, f.Style)
		}
	}
}

// sameFont reports whether two handles are the same loaded font. Two parses of
// the same bytes are deliberately two different fonts — see internal/text on
// FontID — so this compares identity and not content, which is exactly what
// "did one registration overwrite the other" asks.
func sameFont(a, b ui.Font) bool {
	if a.IsZero() || b.IsZero() {
		return a.IsZero() && b.IsZero()
	}
	return a == b
}

// TestBundledFacesAreRegisteredAsAdvertised pins what the two packages put in
// the register. A package that quietly grows a weight costs every one of its
// importers a few hundred kilobytes, and that should be a diff nobody can miss
// rather than a surprise in a binary size report.
func TestBundledFacesAreRegisteredAsAdvertised(t *testing.T) {
	want := map[string]bool{
		"Inter 400 normal":         true,
		"Inter 700 normal":         true,
		"IBM Plex Mono 400 normal": true,
		"IBM Plex Mono 700 normal": true,
		"IBM Plex Mono 400 italic": true,
		"IBM Plex Mono 700 italic": true,
	}
	for _, f := range ui.RegisteredFonts(nil) {
		name := ui.FontQuery{Family: f.Family, Weight: f.Weight, Style: f.Style}.String()
		switch {
		case want[name]:
			delete(want, name)
		case f.Family == inter.Family || f.Family == ibmplexmono.Family:
			t.Errorf("a bundled package registered an unexpected face: %s", name)
		default:
			// A face some other test registered. Not our business.
		}
	}
	for name := range want {
		t.Errorf("the face %s was not registered", name)
	}
}

// TestWeightResolution pins the rule of [ui.ResolveFont]: exact family, exact
// style, nearest weight with a tie going up.
func TestWeightResolution(t *testing.T) {
	family := testFamily(t)
	light := loadTestFont(t)
	// Three distinct parses, so that the three faces are three identities and
	// a wrong pick is visible rather than accidentally equal.
	heavy := parseTestFont(t)
	middle := parseTestFont(t)
	ui.RegisterFont(family, ui.WeightLight, ui.StyleNormal, light)   // 300
	ui.RegisterFont(family, ui.WeightMedium, ui.StyleNormal, middle) // 500
	ui.RegisterFont(family, ui.WeightBlack, ui.StyleNormal, heavy)   // 900

	for _, tc := range []struct {
		name   string
		weight ui.FontWeight
		want   ui.Font
		why    string
	}{
		{"exact", ui.WeightMedium, middle, "an exact match wins"},
		{"zero means regular", 0, middle, "0 means 400, which ties between 300 and 500 and goes up"},
		{"below the lightest", 100, light, "nothing lighter exists"},
		{"above the heaviest", 1000, heavy, "nothing heavier exists"},
		{"nearest below", 350, light, "50 from 300, 150 from 500"},
		{"nearest above", 450, middle, "50 from 500, 150 from 300"},
		{"tie goes up", 400, middle, "100 from both 300 and 500, the heavier wins"},
		{"far tie goes up", 700, heavy, "200 from both 500 and 900"},
	} {
		got, ok := ui.ResolveFont(ui.FontQuery{Family: family, Weight: tc.weight})
		if !ok {
			t.Errorf("%s: weight %d resolved to nothing", tc.name, tc.weight)
			continue
		}
		if !sameFont(got, tc.want) {
			t.Errorf("%s: weight %d picked the wrong face (%s)", tc.name, tc.weight, tc.why)
		}
	}

	// The zero weight case above reads oddly on purpose, so state it plainly:
	// FontQuery{} with no weight asks for 400, and against {300, 500, 900} the
	// tie rule sends it to 500.
	if got, _ := ui.ResolveFont(ui.FontQuery{Family: family}); !sameFont(got, middle) {
		t.Error("the zero weight did not mean WeightRegular")
	}
}

// TestLoadFontErrorsAreClassifiable checks the sentinel [ui.ErrBadFont] that
// this unit re-exported from internal/text.
//
// Without it the only available test is err != nil, and a caller cannot tell
// "the file is not where I looked" from "the file is there and is not a font".
// The first invites looking elsewhere and the second does not, which is the
// rule the project plan, section 15, states for an error two packages have to
// agree about.
func TestLoadFontErrorsAreClassifiable(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"not a font", []byte("this is a text file, not a font at all")},
		{"truncated", parseTestFontBytes(t)[:512]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ui.LoadFont(tc.data)
			if err == nil {
				t.Fatal("this was accepted as a font")
			}
			if !errors.Is(err, ui.ErrBadFont) {
				t.Fatalf("the error does not wrap ui.ErrBadFont: %v", err)
			}
		})
	}
}

func parseTestFontBytes(t testing.TB) []byte {
	t.Helper()
	data, err := os.ReadFile(testFontPath)
	if err != nil {
		t.Fatalf("read the test font: %v", err)
	}
	return data
}

// parseTestFont parses the test font afresh, so that every call yields a
// distinct identity. internal/text hands out a new FontID per parse precisely
// because two parses have two glyph caches, and this test needs the distinct
// identities to tell three registered faces apart.
func parseTestFont(t testing.TB) ui.Font {
	t.Helper()
	data, err := os.ReadFile(testFontPath)
	if err != nil {
		t.Fatalf("read the test font: %v", err)
	}
	f, err := ui.LoadFont(data)
	if err != nil {
		t.Fatalf("parse the test font: %v", err)
	}
	return f
}

// TestStyleIsNeverSubstituted is the other half of the resolution rule and the
// one that is a decision rather than an implementation detail: gift does not
// slant an upright face to stand in for a missing italic.
func TestStyleIsNeverSubstituted(t *testing.T) {
	family := testFamily(t)
	ui.RegisterFont(family, ui.WeightRegular, ui.StyleNormal, loadTestFont(t))

	if _, ok := ui.ResolveFont(ui.FontQuery{Family: family, Style: ui.StyleItalic}); ok {
		t.Fatal("an italic was resolved from a family that has only an upright face")
	}
	if _, ok := ui.ResolveFont(ui.FontQuery{Family: family + " (nope)"}); ok {
		t.Fatal("an unknown family resolved to something")
	}
}

// TestRegisterFontRejectsADuplicate follows gift.RegisterType: a slot claimed
// twice is two packages that both think they own it, and last-writer-wins is
// the silent clobbering this register exists to prevent.
func TestRegisterFontRejectsADuplicate(t *testing.T) {
	family := testFamily(t)
	ui.RegisterFont(family, ui.WeightRegular, ui.StyleNormal, loadTestFont(t))

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("registering the same family, weight and style twice did not panic")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "already registered") {
			t.Errorf("the diagnosis does not say what went wrong:\n%v", r)
		}
	}()
	ui.RegisterFont(family, ui.WeightRegular, ui.StyleNormal, loadTestFont(t))
}

// testFamily returns a family name no other registration in this process has
// used, so that a test which registers a face can be run twice.
//
// It is needed because the register is process wide and a duplicate panics —
// both on purpose, see [ui.RegisterFont] — and neither of those has an undo.
// A test whose family was a constant therefore passed on the first run and
// panicked on the second, which is what `go test -count=3` is for and how this
// was found. The counter is not for concurrency: the project plan, section 13,
// records that no test in this repository calls t.Parallel, and this one does
// not either.
func testFamily(t *testing.T) string {
	t.Helper()
	testFamilySeq++
	return fmt.Sprintf("%s#%d", t.Name(), testFamilySeq)
}

var testFamilySeq int

// TestRegisterFontRejectsNonsense checks the argument validation. A weight of
// zero is a caller who forgot the field, and the zero Font is a caller whose
// load failed; both would otherwise sit in the register until a label
// somewhere rendered wrongly or not at all.
func TestRegisterFontRejectsNonsense(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func()
	}{
		{"empty family", func() { ui.RegisterFont("  ", ui.WeightRegular, ui.StyleNormal, loadTestFont(t)) }},
		{"zero weight", func() { ui.RegisterFont("X", 0, ui.StyleNormal, loadTestFont(t)) }},
		{"absurd weight", func() { ui.RegisterFont("X", 5000, ui.StyleNormal, loadTestFont(t)) }},
		{"unknown style", func() { ui.RegisterFont("X", ui.WeightRegular, ui.FontStyle(42), loadTestFont(t)) }},
		{"zero font", func() { ui.RegisterFont("X", ui.WeightRegular, ui.StyleNormal, ui.Font{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("this was accepted and should not have been")
				}
			}()
			tc.call()
		})
	}
}

// TestMustFontNamesWhatIsThere checks the failure message, which is the whole
// value of MustFont over ResolveFont. The two causes of a miss — a forgotten
// side effect import and a misspelled family — are both answered by a list of
// what is registered.
func TestMustFontNamesWhatIsThere(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("MustFont of an unregistered family did not panic")
		}
		msg, _ := r.(string)
		for _, want := range []string{"Nonexistent Sans", "Registered faces:", inter.Family, "import _"} {
			if !strings.Contains(msg, want) {
				t.Errorf("the diagnosis does not mention %q:\n%s", want, msg)
			}
		}
		t.Logf("diagnosis:\n%s", msg)
	}()
	ui.MustFont(ui.FontQuery{Family: "Nonexistent Sans"})
}

// BenchmarkResolveFont guards the allocation contract of the project plan,
// section 11, at the one point where a font lookup could sneak into a
// measurement path. Resolution happens during build today, which the contract
// exempts, but a lookup that allocated would be trivial to introduce — a
// strings.ToLower for a case insensitive family match, say — and impossible to
// notice afterwards.
func BenchmarkResolveFont(b *testing.B) {
	q := ui.FontQuery{Family: inter.Family, Weight: ui.WeightBold}
	miss := ui.FontQuery{Family: inter.Family, Weight: 450}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, ok := ui.ResolveFont(q); !ok {
			b.Fatal("exact lookup missed")
		}
		if _, ok := ui.ResolveFont(miss); !ok {
			b.Fatal("nearest weight lookup missed")
		}
	}
}
