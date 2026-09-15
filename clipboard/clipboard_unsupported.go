//go:build !darwin && !(linux && !android && !faketime)

package clipboard

import (
	"fmt"
	"log/slog"
	"runtime"
)

// This is the platform layer for every operating system this package does not
// support: Windows, the BSDs, Android, iOS, js/wasm, and a linux build with
// the faketime tag, whose purego has no dlopen.
//
// It fails to open, with a message that names the platform, and the portable
// half then logs that once and turns every copy into a no-op and every paste
// into an empty clipboard. That is the honest degradation the work order asks
// for: [ui.MemoryClipboard] keeps working inside the process, so copy and
// paste inside one gift application still do what the user expects, and
// nothing pretends to have reached the system clipboard.
//
// On Windows this is a decision rather than an omission; the package
// documentation says why.
func openPlatform(log *slog.Logger) (platform, error) {
	return nil, fmt.Errorf("%w: %s/%s has no implementation in this package", ErrUnsupported, runtime.GOOS, runtime.GOARCH)
}
