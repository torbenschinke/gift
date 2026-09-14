package asset

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"sync"
)

// headerBytes is how much of a file the pipeline buffers to answer "how big is
// it and which way up". It covers a JPEG's SOI, its EXIF APP1 and its first
// SOF marker, and a PNG's IHDR, for everything a camera or an editor produces.
const headerBytes = 64 << 10

// Decoder turns encoded bytes into an image.
//
// The two built in decoders are image/jpeg and image/png. Further formats are
// added with [RegisterDecoder], which the project plan, section 12, step 4,
// requires to be an explicit act: "weitere Formate nur ueber klar registrierte
// Decoder. Kein RAW-/Video-Support."
//
// A Decoder is used from several goroutines at once and must be safe for
// concurrent use.
type Decoder interface {
	// DecodeConfig reads the dimensions and colour model from the head of
	// the stream, without decoding pixels.
	DecodeConfig(r io.Reader) (image.Config, error)

	// Decode decodes the whole picture.
	Decode(r io.Reader) (image.Image, error)

	// MemoryFactor is how many bytes of working memory the decoder needs per
	// pixel of the declared picture size. It is the number the pipeline
	// reserves its decode budget with, and the project plan, section 9, is
	// explicit that it is computed from the *picture's* dimensions and the
	// codec's risk, not from the size of the thumbnail that comes out.
	MemoryFactor() float64
}

// Sniffer is an optional extension of [Decoder]: a decoder that can recognise
// its own format from the first bytes. Without it a decoder is only selected by
// media type.
type Sniffer interface {
	Sniff(header []byte) bool
}

type decoderEntry struct {
	mime string
	dec  Decoder
}

var (
	decoderMu sync.RWMutex
	decoders  = []decoderEntry{
		{MIMEJPEG, jpegDecoder{}},
		{MIMEPNG, pngDecoder{}},
	}
)

// RegisterDecoder registers a decoder for a media type, replacing any earlier
// registration of the same type.
//
// It is process wide, like image.RegisterFormat, and is meant to be called
// during start up, before a pipeline runs. Registering while decodes are in
// flight is safe but which decoder a request in flight uses is unspecified.
func RegisterDecoder(mimeType string, d Decoder) {
	if mimeType == "" || d == nil {
		panic("gift/asset: RegisterDecoder needs a media type and a decoder")
	}
	decoderMu.Lock()
	defer decoderMu.Unlock()
	for i := range decoders {
		if decoders[i].mime == mimeType {
			decoders[i].dec = d
			return
		}
	}
	decoders = append(decoders, decoderEntry{mimeType, d})
}

// decoderFor picks a decoder by sniffing the header first and by media type
// second. Sniffing wins because a server's Content-Type is a claim and the
// magic bytes are a fact.
func decoderFor(header []byte, mimeType string) (Decoder, string, bool) {
	decoderMu.RLock()
	defer decoderMu.RUnlock()
	for _, e := range decoders {
		if s, ok := e.dec.(Sniffer); ok && s.Sniff(header) {
			return e.dec, e.mime, true
		}
	}
	for _, e := range decoders {
		if e.mime == mimeType && mimeType != "" {
			return e.dec, e.mime, true
		}
	}
	return nil, "", false
}

type jpegDecoder struct{}

func (jpegDecoder) DecodeConfig(r io.Reader) (image.Config, error) { return jpeg.DecodeConfig(r) }
func (jpegDecoder) Decode(r io.Reader) (image.Image, error)        { return jpeg.Decode(r) }
func (jpegDecoder) Sniff(h []byte) bool {
	return len(h) >= 3 && h[0] == 0xFF && h[1] == 0xD8 && h[2] == 0xFF
}

// MemoryFactor for JPEG.
//
// image/jpeg allocates a YCbCr image, which is 1.5 bytes per pixel at 4:2:0 and
// 3 at 4:4:4, plus the block scratch and, for a progressive file, one int32
// coefficient plane per component, which is 12 bytes per pixel and the real
// risk in this codec. Six is the honest reservation for a progressive worst
// case; a baseline 4:2:0 file will use a quarter of it and give the rest back
// immediately.
func (jpegDecoder) MemoryFactor() float64 { return 6 }

type pngDecoder struct{}

func (pngDecoder) DecodeConfig(r io.Reader) (image.Config, error) { return png.DecodeConfig(r) }
func (pngDecoder) Decode(r io.Reader) (image.Image, error)        { return png.Decode(r) }
func (pngDecoder) Sniff(h []byte) bool {
	return len(h) >= 8 && string(h[:8]) == "\x89PNG\r\n\x1a\n"
}

// MemoryFactor for PNG.
//
// image/png allocates the destination image — up to 8 bytes per pixel for
// 16 bit RGBA — plus two row buffers. Nine covers the widest pixel format with
// room for the scratch.
func (pngDecoder) MemoryFactor() float64 { return 9 }

// header is the buffered head of a stream plus the reader that continues it.
type header struct {
	buf []byte
	// rest replays buf and then continues with the original stream.
	rest io.Reader
}

// readHeader buffers up to headerBytes from r without consuming them: the
// returned reader replays everything.
func readHeader(r io.Reader) (header, error) {
	buf := make([]byte, headerBytes)
	n, err := io.ReadFull(r, buf)
	switch {
	case err == nil || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF):
	default:
		return header{}, err
	}
	buf = buf[:n]
	return header{buf: buf, rest: io.MultiReader(bytes.NewReader(buf), r)}, nil
}

// probeInfo is what the buffered head of a stream reveals.
type probeInfo struct {
	// W and H are the *oriented* dimensions, because that is what
	// [Metadata] is defined to carry and what the gallery lays out with.
	// See [Orientation.SwapsAxes].
	W, H int
	// StoredW and StoredH are the dimensions of the stored pixel grid, which
	// is what the decoder will allocate and therefore what the decode budget
	// is computed from.
	StoredW, StoredH int
	Orientation      Orientation
	MIME             string
	Dec              Decoder
}

// probeHeader answers dimensions, media type and orientation from the buffered
// head of a stream.
//
// allowStream permits a fallback that reads past the buffered head when the
// head was too small to hold the configuration, which happens for a file with
// a very large EXIF block. The fallback *consumes* the stream, so only the
// probing path, which re-opens the source afterwards, may allow it.
func probeHeader(h header, mimeType string, allowStream bool) (probeInfo, error) {
	dec, mt, ok := decoderFor(h.buf, mimeType)
	if !ok {
		return probeInfo{}, ErrNotAPicture
	}
	info := probeInfo{MIME: mt, Dec: dec}
	cfg, err := dec.DecodeConfig(bytes.NewReader(h.buf))
	if err != nil {
		if !allowStream {
			return info, fmt.Errorf("%w: %v", ErrNotAPicture, err)
		}
		cfg, err = dec.DecodeConfig(bufio.NewReader(h.rest))
		if err != nil {
			return info, fmt.Errorf("%w: %v", ErrNotAPicture, err)
		}
	}
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return info, ErrNotAPicture
	}
	if mt == MIMEJPEG {
		info.Orientation = exifOrientation(h.buf)
	}
	info.StoredW, info.StoredH = cfg.Width, cfg.Height
	info.W, info.H = info.Orientation.Oriented(cfg.Width, cfg.Height)
	return info, nil
}
