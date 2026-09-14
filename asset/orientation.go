package asset

import (
	"encoding/binary"
	"image"
)

// Orientation is the EXIF orientation of a picture: how the stored pixel grid
// has to be transformed to be shown the right way up.
//
// The values are the ones of EXIF tag 0x0112, and the names say what has to be
// *applied* to the stored pixels, not what the camera did.
//
//	1  TopLeft      no transform
//	2  TopRight     mirrored horizontally
//	3  BottomRight  rotated 180°
//	4  BottomLeft   mirrored vertically
//	5  LeftTop      transposed (mirror across the main diagonal)
//	6  RightTop     rotated 90° clockwise
//	7  RightBottom  transverse (mirror across the anti diagonal)
//	8  LeftBottom   rotated 270° clockwise
//
// [OrientationUnknown] is the zero value and is treated exactly like
// [OrientationTopLeft] when applying a transform. It is kept distinct because
// it is *not* the same thing in a cache key: "no EXIF was found" and "EXIF said
// 1" must produce the same picture, and they do, so they deliberately hash to
// the same key. See [Key].
type Orientation uint8

// The eight EXIF orientations plus the unknown zero value.
const (
	OrientationUnknown     Orientation = 0
	OrientationTopLeft     Orientation = 1
	OrientationTopRight    Orientation = 2
	OrientationBottomRight Orientation = 3
	OrientationBottomLeft  Orientation = 4
	OrientationLeftTop     Orientation = 5
	OrientationRightTop    Orientation = 6
	OrientationRightBottom Orientation = 7
	OrientationLeftBottom  Orientation = 8
)

// Normalised maps [OrientationUnknown] and any out of range value to
// [OrientationTopLeft].
func (o Orientation) Normalised() Orientation {
	if o < OrientationTopLeft || o > OrientationLeftBottom {
		return OrientationTopLeft
	}
	return o
}

// SwapsAxes reports whether applying this orientation exchanges width and
// height.
//
// This is the reason [Metadata.Width] is documented as the *oriented* width:
// a portrait photograph taken on a phone is stored as a landscape pixel grid
// with orientation 6, and a gallery that laid it out by the stored dimensions
// would give every such picture the wrong aspect ratio, reflow the whole
// masonry column when the real size arrived, and move the scroll anchor.
func (o Orientation) SwapsAxes() bool {
	switch o.Normalised() {
	case OrientationLeftTop, OrientationRightTop, OrientationRightBottom, OrientationLeftBottom:
		return true
	}
	return false
}

// Oriented returns the presentation dimensions of a stored grid of w by h.
func (o Orientation) Oriented(w, h int) (int, int) {
	if o.SwapsAxes() {
		return h, w
	}
	return w, h
}

func (o Orientation) String() string {
	switch o {
	case OrientationUnknown:
		return "unknown"
	case OrientationTopLeft:
		return "top-left"
	case OrientationTopRight:
		return "top-right"
	case OrientationBottomRight:
		return "bottom-right"
	case OrientationBottomLeft:
		return "bottom-left"
	case OrientationLeftTop:
		return "left-top"
	case OrientationRightTop:
		return "right-top"
	case OrientationRightBottom:
		return "right-bottom"
	case OrientationLeftBottom:
		return "left-bottom"
	}
	return "invalid"
}

// applyOrientation rewrites src into the presentation orientation.
//
// It works on the *thumbnail*, never on the full decode: rotating a 6000 by
// 4000 picture costs 96 MB of destination pixels and three cache misses per
// pixel, while rotating its 256 pixel thumbnail costs 256 kB. The only thing
// that has to happen before scaling is the swap of the target dimensions, and
// that is arithmetic.
//
// [OrientationTopLeft] and [OrientationUnknown] return src unchanged, without
// copying.
func applyOrientation(src *image.RGBA, o Orientation) *image.RGBA {
	o = o.Normalised()
	if o == OrientationTopLeft {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	ow, oh := o.Oriented(w, h)
	dst := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for y := range h {
		srow := src.Pix[y*src.Stride+4*0 : y*src.Stride+4*w : y*src.Stride+4*w]
		for x := range w {
			var dx, dy int
			switch o {
			case OrientationTopRight:
				dx, dy = w-1-x, y
			case OrientationBottomRight:
				dx, dy = w-1-x, h-1-y
			case OrientationBottomLeft:
				dx, dy = x, h-1-y
			case OrientationLeftTop:
				dx, dy = y, x
			case OrientationRightTop:
				dx, dy = h-1-y, x
			case OrientationRightBottom:
				dx, dy = h-1-y, w-1-x
			case OrientationLeftBottom:
				dx, dy = y, w-1-x
			}
			di := dy*dst.Stride + dx*4
			copy(dst.Pix[di:di+4], srow[x*4:x*4+4])
		}
	}
	return dst
}

// exifOrientation extracts the orientation from a JPEG header.
//
// # What is handled
//
//   - JPEG only, and only the APP1 segment with the "Exif\0\0" identifier, in
//     the header bytes the pipeline has already buffered.
//   - Both byte orders, IFD0 only, tag 0x0112 with SHORT or LONG type.
//   - All eight values, including the four mirrored ones.
//
// # What is not handled, deliberately
//
//   - PNG. The eXIf chunk exists since PNG 1.5, but no encoder in the standard
//     library writes one and no camera produces a rotated PNG. A PNG is always
//     treated as [OrientationTopLeft].
//   - The EXIF sub-IFD, the GPS IFD, IFD1 and the embedded thumbnail. Only
//     IFD0 carries the orientation of the main picture.
//   - MakerNote, XMP and IPTC rotation hints. Where they disagree with EXIF,
//     EXIF wins here.
//   - Multi-Picture Format, EXIF beyond the first APP1 segment, and an APP1
//     that starts further into the file than the buffered header. Those yield
//     [OrientationUnknown] and therefore no rotation, which is the same answer
//     as a picture without EXIF and never a wrong rotation.
//
// It reads only from the byte slice it is given and allocates nothing.
func exifOrientation(header []byte) Orientation {
	// SOI
	if len(header) < 4 || header[0] != 0xFF || header[1] != 0xD8 {
		return OrientationUnknown
	}
	i := 2
	for i+4 <= len(header) {
		if header[i] != 0xFF {
			// Not at a marker: fill bytes are 0xFF, anything else means
			// the structure is not what we assume. Stop rather than
			// guess.
			return OrientationUnknown
		}
		marker := header[i+1]
		if marker == 0xFF { // fill byte
			i++
			continue
		}
		// Standalone markers without a length field.
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			i += 2
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // start of scan / end of image
			return OrientationUnknown
		}
		if i+4 > len(header) {
			return OrientationUnknown
		}
		size := int(binary.BigEndian.Uint16(header[i+2 : i+4]))
		if size < 2 {
			return OrientationUnknown
		}
		seg := i + 4
		end := i + 2 + size
		if end > len(header) {
			return OrientationUnknown
		}
		if marker == 0xE1 && end-seg > 6 && string(header[seg:seg+6]) == "Exif\x00\x00" {
			return tiffOrientation(header[seg+6 : end])
		}
		i = end
	}
	return OrientationUnknown
}

// tiffOrientation reads tag 0x0112 out of IFD0 of a TIFF header.
func tiffOrientation(b []byte) Orientation {
	if len(b) < 8 {
		return OrientationUnknown
	}
	var bo binary.ByteOrder
	switch {
	case b[0] == 'I' && b[1] == 'I':
		bo = binary.LittleEndian
	case b[0] == 'M' && b[1] == 'M':
		bo = binary.BigEndian
	default:
		return OrientationUnknown
	}
	if bo.Uint16(b[2:4]) != 42 {
		return OrientationUnknown
	}
	off := int(bo.Uint32(b[4:8]))
	if off < 8 || off+2 > len(b) {
		return OrientationUnknown
	}
	n := int(bo.Uint16(b[off : off+2]))
	off += 2
	for range n {
		if off+12 > len(b) {
			return OrientationUnknown
		}
		tag := bo.Uint16(b[off : off+2])
		typ := bo.Uint16(b[off+2 : off+4])
		count := bo.Uint32(b[off+4 : off+8])
		if tag == 0x0112 && count == 1 {
			var v uint32
			switch typ {
			case 3: // SHORT, stored in the first two bytes of the value field
				v = uint32(bo.Uint16(b[off+8 : off+10]))
			case 4: // LONG
				v = bo.Uint32(b[off+8 : off+12])
			default:
				return OrientationUnknown
			}
			if v >= 1 && v <= 8 {
				return Orientation(v)
			}
			return OrientationUnknown
		}
		off += 12
	}
	return OrientationUnknown
}
