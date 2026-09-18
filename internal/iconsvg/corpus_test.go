package iconsvg_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/worldiety/gift/internal/iconsvg"
)

// corpusDirs are the two sets that ship in gift/icon.
var corpusDirs = []struct {
	dir  string
	want int
}{
	{"testdata/flowbite/outline", 282},
	{"testdata/flowbite/solid", 239},
}

// TestEverySvgOfTheCorpusParses is the acceptance test of the parser, and it
// is written to fail on a *count* rather than on a sample.
//
// The brief that commissioned this work said "525 files", which is the number
// `ls` prints for the two source directories. It is wrong by four: each
// directory also holds a LICENSE and a generated Go file. There are 521 SVGs,
// 282 outline and 239 solid, and the numbers are written down here so that a
// file added to or lost from the corpus fails this test instead of quietly
// changing what "every file" means.
func TestEverySvgOfTheCorpusParses(t *testing.T) {
	total := 0
	for _, c := range corpusDirs {
		files, err := filepath.Glob(filepath.Join(c.dir, "*.svg"))
		if err != nil {
			t.Fatal(err)
		}
		if len(files) != c.want {
			t.Errorf("%s holds %d SVG files, expected %d. If the corpus really changed, update "+
				"the number here and regenerate gift/icon; a test that counts whatever it finds "+
				"would pass on an empty directory", c.dir, len(files), c.want)
		}
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			doc, err := iconsvg.ParseFile(filepath.Base(f), string(src))
			if err != nil {
				t.Errorf("%s: %v", f, err)
				continue
			}
			if doc.ViewBox != 24 {
				t.Errorf("%s: viewBox %v, the whole corpus is 24", f, doc.ViewBox)
			}
			if len(doc.Figures) == 0 {
				t.Errorf("%s: parsed to no figures at all, which is a silent empty icon", f)
			}
			for i, fig := range doc.Figures {
				if len(fig.Ops) == 0 {
					t.Errorf("%s: figure %d has no geometry", f, i)
				}
				for _, op := range fig.Ops {
					if strings.IndexByte("MLCZ", op.Kind) < 0 {
						t.Fatalf("%s: op %q survived the parser; only M, L, C and Z may reach "+
							"the encoder", f, op.Kind)
					}
				}
			}
			total++
		}
	}
	if total != 521 {
		t.Errorf("%d files parsed, want 521", total)
	}
	t.Logf("%d SVG files parsed", total)
}

// TestOnlyOneElementOfTheCorpusIsBothFilledAndStroked names the exception that
// [iconsvg.ParseFile] exists for.
//
// It is a test and not a comment because the comment was *wrong*: it named
// solid/circle-plus.svg, which is a plain even-odd fill with no stroke at all,
// and so did the documentation of gift/icon/solid. A named exception that
// nothing checks drifts from the corpus it describes, and the next reader
// looks at circle-plus.svg, sees no stroke, and has to re-derive the rule.
func TestOnlyOneElementOfTheCorpusIsBothFilledAndStroked(t *testing.T) {
	var found []string
	for _, c := range corpusDirs {
		files, _ := filepath.Glob(filepath.Join(c.dir, "*.svg"))
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			// Parse, not ParseFile: ParseFile is the function that resolves
			// this case, so it is the raw parser that reports it.
			if _, err := iconsvg.Parse(filepath.Base(f), string(src)); err != nil {
				found = append(found, filepath.Base(f)+": "+err.Error())
			}
		}
	}
	if len(found) != 1 || !strings.HasPrefix(found[0], "npm.svg:") {
		t.Errorf("the elements the plain parser refuses are %q; want exactly one, npm.svg, "+
			"which is filled and stroked at once. If the corpus changed, update this test and "+
			"the comments on iconsvg.ParseFile and on gift/icon/solid, which name the file",
			found)
	}
	// And the resolution: two figures, the fill before the stroke.
	src, err := os.ReadFile("testdata/flowbite/solid/npm.svg")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := iconsvg.ParseFile("npm.svg", string(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Figures) != 2 || doc.Figures[0].Stroke || !doc.Figures[1].Stroke {
		t.Errorf("npm.svg parsed to %d figures with stroke flags %v; want two, fill first",
			len(doc.Figures), func() []bool {
				var v []bool
				for _, f := range doc.Figures {
					v = append(v, f.Stroke)
				}
				return v
			}())
	}
}

// TestTheCorpusIsTheSubsetThisParserClaims re-measures the facts the design of
// this package rests on, so that a later corpus that quietly stops matching
// them is a failure here rather than a wrong icon somewhere else.
//
// Every one of these numbers was taken from the source and then used as a
// design premise: that there are no quadratics, that the arc is the most
// common command, that half the corpus is strokes and half is fills, that
// round joins and caps dominate. If the set is ever updated the premises have
// to be rechecked, and this is what rechecks them.
func TestTheCorpusIsTheSubsetThisParserClaims(t *testing.T) {
	cmds := map[byte]int{}
	elements := map[string]int{}
	var paths, strokes, fills, evenOdd, erases int
	caps, joins, widths := map[string]int{}, map[string]int{}, map[float64]int{}

	for _, c := range corpusDirs {
		files, _ := filepath.Glob(filepath.Join(c.dir, "*.svg"))
		for _, f := range files {
			src, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			s := string(src)
			for _, tag := range []string{"<path", "<rect", "<g ", "<g>", "<use", "<defs", "<circle", "<polygon"} {
				elements[tag] += strings.Count(s, tag)
			}
			for _, d := range dAttrs(s) {
				for i := range d {
					if isPathCmd(d[i]) {
						cmds[d[i]]++
					}
				}
			}
			doc, err := iconsvg.ParseFile(filepath.Base(f), s)
			if err != nil {
				t.Fatal(err)
			}
			for _, fig := range doc.Figures {
				paths++
				switch {
				case fig.Stroke:
					strokes++
					caps[fig.Cap]++
					joins[fig.Join]++
					widths[fig.Width]++
				case fig.Erase:
					erases++
				default:
					fills++
				}
				if fig.EvenOdd {
					evenOdd++
				}
			}
		}
	}

	for _, tag := range []string{"<g ", "<g>", "<use", "<defs", "<circle", "<polygon"} {
		if elements[tag] != 0 {
			t.Errorf("%d occurrences of %q; the parser does not implement it and would have "+
				"to", elements[tag], tag)
		}
	}
	if cmds['Q']+cmds['q']+cmds['T']+cmds['t'] != 0 {
		t.Errorf("the corpus now contains quadratics (%d); the parser elevates them to cubics, "+
			"so nothing is broken, but the claim in the package documentation is not true any more",
			cmds['Q']+cmds['q']+cmds['T']+cmds['t'])
	}
	if got := cmds['a'] + cmds['A']; got < cmds['m']+cmds['M'] {
		t.Errorf("arcs are no longer the most common command (%d arcs against %d movetos); "+
			"the emphasis the arc conversion gets in this package was justified by that", got, cmds['m']+cmds['M'])
	}
	for _, want := range []struct {
		what string
		got  int
		n    int
	}{
		{"figures", paths, 597},
		{"stroked figures", strokes, 292},
		{"filled figures", fills, 304},
		{"even-odd figures", evenOdd, 211},
		{"knockout figures", erases, 1},
	} {
		if want.got != want.n {
			t.Errorf("%s: %d, expected %d", want.what, want.got, want.n)
		}
	}
	t.Logf("commands: a=%d A=%d m=%d M=%d h=%d v=%d l=%d c=%d s=%d Z=%d",
		cmds['a'], cmds['A'], cmds['m'], cmds['M'], cmds['h'], cmds['v'], cmds['l'], cmds['c'], cmds['s'], cmds['Z'])
	t.Logf("caps: %v", caps)
	t.Logf("joins: %v", joins)
	t.Logf("stroke widths: %v", widths)

	// The premise the stroker leans on, checked rather than assumed. The
	// brief that commissioned this said there were zero miter joins, and that
	// is true of the joins that are *written down*; miter is the SVG default,
	// and the paths that say nothing ask for it.
	if joins["miter"] == 0 {
		t.Errorf("no figure defaults to a miter join any more. The miter implementation in " +
			"internal/icon exists for exactly those; if this is really true it can go")
	}
	t.Logf("%d of %d stroked figures fall back to the default miter join", joins["miter"], strokes)
}

func dAttrs(s string) []string {
	var out []string
	for {
		i := strings.Index(s, ` d="`)
		if i < 0 {
			return out
		}
		s = s[i+4:]
		j := strings.IndexByte(s, '"')
		if j < 0 {
			return out
		}
		out = append(out, s[:j])
		s = s[j:]
	}
}

func isPathCmd(c byte) bool { return strings.IndexByte("MmLlHhVvCcSsQqTtAaZz", c) >= 0 }
