// Package asset is the catalogue and the image pipeline of gift: stable
// identities, metadata, the ordered collection a gallery is a view of, and the
// application wide service that turns a path or a URL into a decoded,
// oriented, cached thumbnail.
//
// # What is here and what is not
//
// The catalogue half is step 3 of the project plan, section 12. The pipeline
// half is the CPU part of step 4, sections 9 and 12: sources, probing,
// decoding, orientation, thumbnails, the persistent cache, the bounded CPU
// pixel cache, byte budgets, cancellation and prioritisation.
//
// What is deliberately absent is everything with a GPU in it. There is no
// texture upload, no backend resource and no view. The package imports nothing
// from ui, gift, render or backend, which is what the project plan, section 3,
// requires of it, and every line of it is testable without a window, without a
// graphics context and without a network.
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
//   - [Collection.StructureVersion] is the *structure* version of the
//     catalogue. It changes when entries are added, removed or reordered, and
//     it is what makes a consumer throw away anything derived from positions.
//   - [Collection.MetadataVersion] is the version of the entries' contents.
//     It changes for the above and for a batch of corrections, which moves no
//     entry, and it is what tells a consumer to reflow *without* throwing
//     anything away.
//
// The last two were one counter until WU-O, and that is how the confusion this
// section guards against got in anyway: with one number a gallery could not
// tell a reorder from a probed dimension, took the safe reaction to both —
// unbind every tile — and its tile generation stopped meaning "this slot
// stands for a different picture". See [Collection.StructureVersion].
//
// The other two live where they belong: the request generation with whoever
// issues requests, and the tile generation with the view that recycles tiles.
// The pipeline carries a generation through [Request.Generation] and
// [Result.Generation] and *never looks at it*: it is an opaque number that
// comes back unchanged, and the consumer compares it before accepting a
// result. Interpreting it here would be this package deciding what a tile is.
//
// # Ownership and threading
//
// A [Collection] and a [Selection] belong to one goroutine, the UI executor,
// like everything else a frame reads. They are plain mutable objects with no
// locking.
//
// A [Pipeline] is the opposite: every method on it is safe to call from any
// goroutine, and it runs its own workers. It never touches gift state and
// never calls into the user interface directly. Results travel as closures
// through [Config.Deliver], which the consumer wires to the UI executor.
//
// # Driving the pipeline
//
// The seam is one function field. An application writes:
//
//	app := gift.NewApp(...)
//	pipe := asset.NewPipeline(asset.Config{
//	    Deliver: app.Post,
//	    Disk:    asset.DiskCacheConfig{Dir: cacheDir},
//	    Logger:  logger,
//	})
//	defer pipe.Close()
//
// and then, from the gallery's layout or paint pass, for every visible tile:
//
//	for _, b := range gallery.Bindings(buf[:0]) {
//	    if t, ok := pipe.Lookup(b.ID, int(b.DocW)); ok {
//	        drawThumbnail(t)   // valid until the matching Release
//	        t.Release()
//	        continue
//	    }
//	    src := sources[b.ID]
//	    gen := b.Generation    // the tile's generation, not the pipeline's
//	    pipe.Request(asset.Request{
//	        Source:     src,
//	        Size:       int(b.DocW),
//	        Priority:   asset.Visible,
//	        Generation: gen,
//	        OnResult: func(r asset.Result) {
//	            // Runs on the UI executor, because Deliver is app.Post.
//	            cur, ok := gallery.BindingOf(r.ID)
//	            if !ok || cur.Generation != r.Generation {
//	                return // the tile was recycled; the answer is stale
//	            }
//	            if r.Err != nil {
//	                markError(r.ID, r.Err)
//	                return
//	            }
//	            corrections = append(corrections, r.Correction())
//	            keep(r.ID, r.Image) // Retain first if it outlives the call
//	        },
//	    })
//	}
//	gallery.ApplyCorrections(corrections)
//
// Tiles that are about to become visible are requested the same way with
// [Prefetch], which never delays visible work.
//
// # Budgets
//
// Every stage has a byte budget, because the project plan, section 9, says a
// channel limit is not enough, and it is right: eight queue slots hold eight
// thumbnails or eight 8000 by 6000 decodes.
//
//	Stage              Bounded by                 At saturation
//	-----------------  -------------------------  --------------------------
//	request queue      Config.QueueLimit (count)  prefetch refused with
//	                                              ErrQueueFull; a visible
//	                                              request evicts the oldest
//	                                              waiting prefetch
//	encoded input      Config.InputBudget         worker waits
//	decode scratch     Config.DecodeBudget        worker waits; a picture
//	                                              larger than the whole
//	                                              budget is refused with
//	                                              ErrTooLarge, never queued
//	                                              behind a wait it cannot win
//	thumbnails         Config.PixelBudget         CPU cache is evicted first,
//	                                              then the worker waits
//	ready results      Config.ReadyLimit (count)  a prefetch result is
//	                                              delivered without its image
//	                                              and with ErrQueueFull; a
//	                                              visible one waits
//	disk cache         DiskCacheConfig.Budget     least recently used entries
//	                                              are deleted
//
// All of them are reported by [Pipeline.Stats], including the high water mark
// of each byte budget, so that saturation is an assertable property and not a
// comment. A budget that is only a comment is worthless.
//
// # What cancellation can and cannot do
//
// [Ticket.Cancel] removes work that has not started and suppresses the
// publication of work that has. It does not stop a running decode: the
// standard decoders are not interruptible and they allocate, and the project
// plan, section 9, is explicit that CPU decoding is therefore not GC free and
// that a context cancellation "kann bereits laufende Codec-Berechnungen nicht
// generell sofort stoppen". What is guaranteed is the property the view needs:
// a cancelled result never reaches it.
//
// # Errors and logging
//
// Failures of the outside world are values. A request always gets exactly one
// [Result], and a failed one carries an [Result.Err] and no image; nothing in
// this package panics for an I/O, HTTP, decode or cache failure, and nothing
// calls os.Exit or log.Fatal. A source that failed gets a backoff and is not
// hammered; see [BackoffPolicy].
//
// Logging goes to [Config.Logger] and never to slog.Default. Nothing on a path
// that can run during layout logs at all: [Pipeline.Lookup], the one method a
// frame calls, writes no log record and allocates nothing beyond the retain.
// Everything else is counters, read out of band through [Pipeline.Stats]; see
// the project plan, section 15.
//
// # Formats
//
// JPEG and PNG, from image/jpeg and image/png. EXIF orientation is read from
// JPEG and applied; see [Orientation] for exactly what is and is not handled.
// Further formats require an explicit [RegisterDecoder]. No RAW, no video.
package asset
