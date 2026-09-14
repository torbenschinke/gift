package asset

import (
	"errors"
	"fmt"
)

// Media types the pipeline decodes out of the box. Anything else needs a
// registered decoder; see [RegisterDecoder]. The project plan, section 12,
// step 4, limits the first cut to these two and rules out RAW and video.
const (
	MIMEJPEG = "image/jpeg"
	MIMEPNG  = "image/png"
)

// The pipeline's error values. They are ordinary values and never panics: the
// project plan, section 15, reserves panics for violations of gift's own
// contract and requires failures of the outside world — I/O, HTTP, decode,
// cache — to travel as values and end in a visible error state on the affected
// tile.
var (
	// ErrNotAPicture is returned for a path that is not a regular file or a
	// body that is not in a format gift decodes.
	ErrNotAPicture = errors.New("not a decodable picture")

	// ErrBadURL is returned by [HTTPSource] for a URL that could not be
	// parsed or does not use http or https. The message never repeats the
	// URL, because it may contain a credential.
	ErrBadURL = errors.New("unusable URL")

	// ErrTooLarge means the picture was refused before its pixels were
	// allocated: its header declares more pixels than [Config.MaxPixels], or
	// more than the whole decode budget could ever hold, or its encoded form
	// exceeds [Config.MaxEncodedBytes].
	//
	// Refusing early is the point. A decoder that is handed a 60000 by 60000
	// PNG allocates 14 GB before it fails, and on a Raspberry Pi that is not
	// an error, it is a dead process.
	ErrTooLarge = errors.New("picture exceeds the configured limits")

	// ErrQueueFull means the request was refused because the request queue
	// was at its limit and nothing of lower priority could be evicted for
	// it. It is the documented saturation behaviour and not a defect; see
	// [Config.QueueLimit].
	ErrQueueFull = errors.New("request queue is full")

	// ErrBackoff means the source failed recently and is not being retried
	// yet. The project plan, section 15: "Ein Fehler erzeugt keinen
	// Retry-Sturm." See [BackoffPolicy].
	ErrBackoff = errors.New("source is in backoff after an earlier failure")

	// ErrQuarantined means the source failed in a way a retry cannot fix and
	// is out of service until its revision changes or [Pipeline.Forget] is
	// called for it.
	//
	// It wraps [ErrBackoff], because it is the terminal state of the same
	// policy and an existing test for "is this a backoff" should keep
	// working. The difference that matters is [Retryable]: a timed backoff
	// lifts by itself and a quarantine does not, so a consumer that polls
	// the first one recovers and a consumer that polls the second one polls
	// for ever.
	ErrQuarantined = fmt.Errorf("source is quarantined until its revision changes: %w", ErrBackoff)

	// ErrClosed means the pipeline was shut down while the request was in
	// flight, or the request was made after shutdown.
	ErrClosed = errors.New("pipeline is closed")

	// ErrNoRevision is reported in diagnostics, never to a requester: it is
	// why a result was not written to the disk cache. A source without a
	// validator has no content version to key an entry by, and the project
	// plan, section 9, refuses to treat the URL itself as one.
	ErrNoRevision = errors.New("source has no revision, disk caching skipped")
)

// Retryable classifies a pipeline error for a consumer.
//
// True means "nothing is wrong with this picture, the pipeline was merely
// busy": [ErrQueueFull] from a saturated queue, [ErrBackoff] while a source
// waits out its delay. The condition lifts by itself, so the right response is
// to forget it and ask again on a later frame — never to remember it.
//
// False means the same request would fail the same way: [ErrNotAPicture],
// [ErrTooLarge], [ErrBadURL], [ErrQuarantined], [ErrClosed], an I/O error, a
// timeout. The way back is a new revision, [Pipeline.Forget] or
// [Pipeline.Invalidate].
//
// A [Result] already carries this classification in its two error fields and a
// consumer of one does not need this function; it is for an error that arrived
// by some other route, and for a third consumer that is tempted to invent the
// distinction itself.
func Retryable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrQuarantined) {
		return false
	}
	return errors.Is(err, ErrQueueFull) || errors.Is(err, ErrBackoff)
}

// temporary reports whether an error is worth retrying after a delay, as
// opposed to one that will fail the same way until the revision changes.
//
// It is not [Retryable] and the two ask different questions. This one is about
// the *source*: should the pipeline itself try this URL again, or quarantine
// it. [Retryable] is about the *result*: may a consumer forget this error and
// ask again. A 503 is temporary here and its delivered ErrBackoff is retryable
// there; a 404 is neither.
func temporary(err error) bool {
	if err == nil {
		return false
	}
	var t interface{ Temporary() bool }
	if errors.As(err, &t) {
		return t.Temporary()
	}
	switch {
	case errors.Is(err, ErrTooLarge), errors.Is(err, ErrNotAPicture),
		errors.Is(err, ErrBadURL):
		return false
	}
	return true
}
