// Package asset is the catalogue half of gift: stable identities, metadata
// and the ordered collection a gallery is a view of.
//
// # What is here and what is not
//
// This is step 3 of the project plan, section 12, "Galerie ohne I/O". The
// package therefore contains no loader, no decoder, no cache and no HTTP: it
// performs no input or output at all and imports nothing outside the standard
// library. [Collection] is a value the application fills in and gift reads;
// where the values came from is the application's business until step 4 adds
// the pipeline of section 9.
//
// That is not a placeholder. The split the project plan draws in section 10 —
// "Die Collection liefert geordnete, stabile IDs, Revisionen und
// Metadatenzugriff ohne I/O im Frame" — is precisely a catalogue that answers
// synchronously and a pipeline that does not, and the catalogue is useful,
// testable and complete on its own.
//
// # Three versions, three meanings
//
// The project plan, section 9, insists that content version, request
// generation and tile generation stay separate concepts, and the confusion it
// guards against is the one that puts the wrong picture in a recycled tile.
// This package owns the first of them and names it twice:
//
//   - [Metadata.Revision] is the *content* version of one entry. It changes
//     when the bytes behind the entry change, and it is what a cache key and a
//     reload decision are made of.
//   - [Collection.Version] is the *structure* version of the catalogue. It
//     changes when entries are added, removed, reordered or corrected, and it
//     is what makes a consumer throw away anything derived from positions.
//
// The other two live where they belong: the request generation with whoever
// issues requests, and the tile generation with the view that recycles tiles.
// Nothing in this package knows about either.
//
// # Ownership and threading
//
// A [Collection] belongs to one goroutine, the UI executor, like everything
// else a frame reads. It is a plain mutable object with no locking; a worker
// that has produced new metadata posts it to the UI executor and the UI
// executor calls [Collection.ApplyCorrections]. See the project plan,
// section 5.
package asset
