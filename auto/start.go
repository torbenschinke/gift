//go:build giftauto

package auto

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/worldiety/gift"
)

// DefaultAddr is where the interface listens when GIFT_AUTO_ADDR says nothing.
//
// It is a loopback address and not a bare port, so the socket is unreachable
// from the network even before the per-request guard in [loopbackOnly] sees
// anything. The port is high, fixed and unremarkable; a driver script needs to
// be able to guess it.
const DefaultAddr = "127.0.0.1:7391"

// The environment variables this package reads.
const (
	// EnvAddr overrides [DefaultAddr]. Setting it to a non-loopback address
	// widens the listener but not the interface: [loopbackOnly] still
	// refuses every non-loopback peer.
	EnvAddr = "GIFT_AUTO_ADDR"
	// EnvTimeout overrides [DefaultTimeout], in milliseconds.
	EnvTimeout = "GIFT_AUTO_TIMEOUT"
)

// std is the one driver of the process. The interface is a debugging seam into
// the running application and there is exactly one of those.
var std = newDriver(timeoutFromEnv())

// once guards the listener so repeated Start calls cannot bind twice.
var once sync.Once

// Start attaches the application and opens the automation listener. The
// giftauto-enabled backend calls it on the UI goroutine before opening the window.
//
// Merely importing the package does not bind a port, so test binaries can link
// it without competing for the default address.
func Start(app *gift.App) {
	std.Start(app)
	start(std)
}

// WantsFrame reports whether the driver needs the next drawn framebuffer.
// The backend calls it on the UI goroutine before reading pixels back.
func WantsFrame() bool { return std.WantsFrame() }

// Frame receives a requested framebuffer on the UI goroutine. Dimensions are
// physical pixels, pix is owned RGBA data, and count is the drawn frame number.
func Frame(width, height int, pix []byte, count uint64) {
	std.Frame(width, height, pix, count)
}

// start opens the listener and serves in the background.
//
// The goroutine it starts is the one goroutine this package adds to a process,
// and it exists only in a giftauto build after the backend starts.
func start(d *driver) {
	once.Do(func() {
		addr := os.Getenv(EnvAddr)
		if addr == "" {
			addr = DefaultAddr
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			fmt.Fprintln(os.Stderr, "gift/auto: cannot listen on", addr+":", err)
			return
		}
		fmt.Fprintln(os.Stderr, describe(ln.Addr().String()))
		srv := &http.Server{Handler: (&server{d: d}).handler()}
		go func() { _ = srv.Serve(ln) }()
	})
}

// timeoutFromEnv reads [EnvTimeout], in milliseconds.
func timeoutFromEnv() time.Duration {
	if v := os.Getenv(EnvTimeout); v != "" {
		if ms, err := strconv.Atoi(v); err == nil && ms > 0 {
			return time.Duration(ms) * time.Millisecond
		}
	}
	return DefaultTimeout
}
