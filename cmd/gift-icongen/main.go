// Command gift-icongen turns a directory of SVG icons into an embedded blob
// and a Go file of package level variables.
//
// It is the offline step the project plan, section 21, requires: "Die SVGs
// werden zur Bauzeit per go:generate zu Pfadsegmenten vorgeparst und
// eingebettet. Damit entsteht kein SVG-Parser im Frame-Pfad und keine neue
// Abhaengigkeit."
//
// Usage:
//
//	gift-icongen -src DIR -out DIR -pkg NAME -set NAME
//
// It writes two files into -out: icons.bin, the concatenated encoded icons,
// and icons.gen.go, which embeds it and declares one [ui.Symbol] per file. The
// names are the file names in Go's spelling, so address-book.svg becomes
// AddressBook, which is the ergonomics the surveyed Nago tree offers and the
// one the brief asked for.
//
// # What it refuses
//
// Everything it does not understand. An element outside the measured subset, a
// stroke or fill that is not currentColor where one is required, a coordinate
// outside the range of the encoded form, and — the one that matters most — an
// even-odd path whose reorientation does not reproduce the original fill. Each
// of those is a wrong icon rather than a missing one if it is waved through,
// and a wrong icon is not visible in any test that does not look at it.
package main

import (
	"flag"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/worldiety/gift/internal/icon"
	"github.com/worldiety/gift/internal/iconsvg"
)

// sampleGrid is the edge of the sampling grid the even-odd analysis uses; see
// [iconsvg.DiffersUnderNonzero]. 240 over a 24 unit viewBox is ten samples per
// unit, that is ten per pixel at the nominal icon size.
const sampleGrid = 240

func main() {
	src := flag.String("src", "", "directory of .svg files to read")
	out := flag.String("out", "", "directory to write icons.bin and icons.gen.go into")
	pkg := flag.String("pkg", "", "package name of the generated file")
	set := flag.String("set", "", "human readable name of the icon set, for the generated doc comment")
	flag.Parse()
	if *src == "" || *out == "" || *pkg == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := run(*src, *out, *pkg, *set); err != nil {
		fmt.Fprintln(os.Stderr, "gift-icongen:", err)
		os.Exit(1)
	}
}

type entry struct {
	ident      string
	file       string
	off, n     int
	box        float64
	evenOdd    int
	evenOddBad int
}

func run(src, out, pkg, set string) error {
	names, err := filepath.Glob(filepath.Join(src, "*.svg"))
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("no .svg files in %s", src)
	}
	sort.Strings(names)

	var blob []byte
	var entries []entry
	stats := struct{ paths, evenOdd, evenOddDiffer, strokes, erases int }{}

	for _, path := range names {
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		base := strings.TrimSuffix(filepath.Base(path), ".svg")
		doc, err := iconsvg.ParseFile(base+".svg", string(raw))
		if err != nil {
			return err
		}
		var enc icon.Encoder
		e := entry{ident: identOf(base), file: base + ".svg", off: len(blob), box: doc.ViewBox}
		for _, f := range doc.Figures {
			stats.paths++
			ops := f.Ops
			if f.EvenOdd {
				stats.evenOdd++
				differs, bad := iconsvg.DiffersUnderNonzero(ops, doc.ViewBox, sampleGrid)
				if differs {
					stats.evenOddDiffer++
					e.evenOdd++
					e.evenOddBad += bad
					fixed, ok := iconsvg.Orient(ops, doc.ViewBox, sampleGrid)
					if !ok {
						return fmt.Errorf("%s: an even-odd path differs from nonzero in %d of %d "+
							"samples and could not be reoriented to agree; emitting it would fill "+
							"a hole", base, bad, sampleGrid*sampleGrid)
					}
					ops = fixed
				}
			}
			switch {
			case f.Stroke:
				stats.strokes++
				enc.StrokeFigure(float32(f.Width), capOf(f.Cap), joinOf(f.Join))
			case f.Erase:
				stats.erases++
				enc.Figure(icon.PaintErase)
			default:
				enc.Figure(icon.PaintFill)
			}
			if err := emit(&enc, ops); err != nil {
				return fmt.Errorf("%s: %w", base, err)
			}
		}
		data, err := enc.Bytes()
		if err != nil {
			return fmt.Errorf("%s: %w", base, err)
		}
		// Verify the round trip here rather than hoping a test does. Decoding
		// what was just encoded and comparing the two point streams is the
		// one check that covers the whole format at once, and it costs
		// microseconds in a program that runs by hand.
		if err := verify(data, ops(doc)); err != nil {
			return fmt.Errorf("%s: %w", base, err)
		}
		e.n = len(data)
		blob = append(blob, data...)
		entries = append(entries, e)
	}

	if err := os.WriteFile(filepath.Join(out, "icons.bin"), blob, 0o644); err != nil {
		return err
	}
	code, err := render(pkg, set, entries, len(blob))
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "icons.gen.go"), code, 0o644); err != nil {
		return err
	}
	fmt.Printf("%s: %d icons, %d figures (%d stroked, %d even-odd of which %d differ under nonzero, %d knockouts), %d bytes\n",
		pkg, len(entries), stats.paths, stats.strokes, stats.evenOdd, stats.evenOddDiffer, stats.erases, len(blob))
	return nil
}

// ops flattens a document back into one op list per figure, for [verify].
func ops(d iconsvg.Doc) [][]iconsvg.Op {
	out := make([][]iconsvg.Op, len(d.Figures))
	for i, f := range d.Figures {
		out[i] = f.Ops
	}
	return out
}

func emit(enc *icon.Encoder, o []iconsvg.Op) error {
	for _, op := range o {
		switch op.Kind {
		case 'M':
			enc.MoveTo(float32(op.P[0][0]), float32(op.P[0][1]))
		case 'L':
			enc.LineTo(float32(op.P[0][0]), float32(op.P[0][1]))
		case 'C':
			enc.CubeTo(
				float32(op.P[0][0]), float32(op.P[0][1]),
				float32(op.P[1][0]), float32(op.P[1][1]),
				float32(op.P[2][0]), float32(op.P[2][1]))
		case 'Z':
			enc.Close()
		default:
			return fmt.Errorf("op %q survived the parser", op.Kind)
		}
	}
	return nil
}

// checker counts the segments of each figure as they come back out of the
// decoder.
type checker struct {
	figures []int
	cur     int
	started bool
}

func (c *checker) Figure(icon.Paint, float32, icon.Cap, icon.Join) {
	if c.started {
		c.figures = append(c.figures, c.cur)
	}
	c.started, c.cur = true, 0
}
func (c *checker) MoveTo(float32, float32)         { c.cur++ }
func (c *checker) LineTo(float32, float32)         { c.cur++ }
func (c *checker) CubeTo(_, _, _, _, _, _ float32) { c.cur++ }
func (c *checker) Close()                          { c.cur++ }
func (c *checker) done() []int                     { return append(c.figures, c.cur) }

// verify decodes data and checks that every figure has the number of segments
// it was given.
func verify(data []byte, want [][]iconsvg.Op) error {
	var c checker
	if err := icon.Walk(data, &c); err != nil {
		return err
	}
	got := c.done()
	if len(got) != len(want) {
		return fmt.Errorf("encoded %d figures, decoded %d", len(want), len(got))
	}
	for i := range got {
		if got[i] != len(want[i]) {
			return fmt.Errorf("figure %d: encoded %d segments, decoded %d", i, len(want[i]), got[i])
		}
	}
	return nil
}

func capOf(s string) icon.Cap {
	switch s {
	case "round":
		return icon.CapRound
	case "square":
		return icon.CapSquare
	default:
		return icon.CapButt
	}
}

func joinOf(s string) icon.Join {
	switch s {
	case "round":
		return icon.JoinRound
	case "bevel":
		return icon.JoinBevel
	default:
		return icon.JoinMiter
	}
}

// identOf turns a file name into an exported Go identifier, in the spelling
// the surveyed Nago tree uses: address-book becomes AddressBook, and a leading
// digit is prefixed so that 4g.svg becomes N4G rather than a syntax error.
func identOf(name string) string {
	var b strings.Builder
	up := true
	for _, r := range name {
		switch {
		case r == '-' || r == '_' || r == '.' || r == ' ':
			up = true
		case up:
			b.WriteString(strings.ToUpper(string(r)))
			up = false
		default:
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return "Unnamed"
	}
	if s[0] >= '0' && s[0] <= '9' {
		return "N" + s
	}
	return s
}

func render(pkg, set string, entries []entry, total int) ([]byte, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "// Code generated by cmd/gift-icongen. DO NOT EDIT.\n\n")
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	fmt.Fprintf(&b, "import (\n\t_ \"embed\"\n\n\t\"github.com/worldiety/gift/ui\"\n)\n\n")
	fmt.Fprintf(&b, "// blob is the encoded geometry of all %d icons of the %s set, %d bytes.\n",
		len(entries), set, total)
	fmt.Fprintf(&b, "// Every symbol below is a window into it; no icon owns a slice of its own.\n")
	fmt.Fprintf(&b, "//\n//go:embed icons.bin\nvar blob []byte\n\n")
	fmt.Fprintf(&b, "// Count is the number of icons in this package.\nconst Count = %d\n\n", len(entries))
	for _, e := range entries {
		note := ""
		if e.evenOdd > 0 {
			note = fmt.Sprintf(
				"\n// Its fill rule was even-odd and genuinely differs from nonzero (%d of %d samples);\n"+
					"// the contours were reoriented offline so that the hole stays a hole.",
				e.evenOddBad, sampleGrid*sampleGrid)
		}
		fmt.Fprintf(&b, "// %s is %s.%s\nvar %s = ui.NewSymbol(blob[%d:%d], %g)\n\n",
			e.ident, e.file, note, e.ident, e.off, e.off+e.n, e.box)
	}
	return format.Source([]byte(b.String()))
}
