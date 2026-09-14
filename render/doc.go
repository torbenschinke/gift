// Package render defines the renderer neutral drawing contract of gift.
//
// # Scope
//
// The package contains the display list ([List]), the drawing operation
// ([Op]), the positioned glyph ([Glyph]), the colour convention ([Color]) and
// the [Backend] contract that a concrete renderer such as backend/ebiten
// implements. It deliberately
// contains no widgets, no application state and no transport protocol; see
// the project plan, section 3.
//
// # Memory model
//
// A display list is a set of flat slices of plain old data. There are no
// pointers, slices or interfaces inside [Op], so building a list never chases
// a pointer graph and never allocates once the backing arrays have grown to
// their working size. Lists are reused between frames: [List.Reset] drops the
// contents but keeps the capacity. This is the basis of the zero allocation
// frame path required by the project plan, section 11.
//
// # Lifetime of borrowed lists
//
// A list handed to a consumer is borrowed, never owned. It is valid only
// until the producer calls [List.Reset] again, which normally happens at the
// start of the next frame. See [List.Ops], [List.Glyphs] and
// [Backend.Submit].
//
// The rule runs the other way too. The shaping result a text view draws from
// is itself borrowed from a cache with a shorter lifetime than a frame, so
// [List.AppendGlyph] copies: what is in the display list is owned by the
// display list, and no lifetime has to be reconciled with another.
//
// # Completeness
//
// Step 1 covers rectangles, rounded rectangles and strokes together with
// clipping and transforms; step 2 adds text as [OpGlyphs] and the glyph side
// table. Images and material regions are part of later steps. [Op] is a struct
// with a discriminating [OpKind] precisely so that those can be added without
// changing the shape of the list.
package render
