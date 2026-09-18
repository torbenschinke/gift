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
	backend "github.com/worldiety/gift/backend/ebiten"
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

// once guards the listener, so that a second init or a manual Start cannot
// bind twice.
var once sync.Once

// init installs the seam into the backend's frame loop and starts the server.
//
// This is what the side-effect import does, and it is why the import is the
// opt-in: without `import _ "github.com/worldiety/gift/auto"` nothing here
// runs, and without `-tags giftauto` this file does not exist at all.
//
// The listener is opened when the frame loop hands the application over,
// which is inside [backend.Run] and still before the window opens, so a
// failure to bind is reported to a terminal somebody is still reading. It is
// deliberately not opened from this init: a test binary that happens to link
// this package would otherwise bind a fixed port for the whole test run, and
// two such binaries would fight over it.
func init() {
	backend.SetAutomation(seam{std})
}

// seam adapts the driver to the backend's automation interface.
//
// It exists so that driver.go imports no backend and can therefore be tested
// without one: the only thing the backend's own type contributes is the shape
// of a captured frame.
type seam struct{ d *driver }

func (s seam) Start(app *gift.App) {
	s.d.Start(app)
	start(s.d)
}
func (s seam) WantsFrame() bool { return s.d.WantsFrame() }
func (s seam) Frame(f backend.Frame) {
	s.d.Frame(f.Width, f.Height, f.Pix, f.Count)
}

// start opens the listener and serves in the background.
//
// The goroutine it starts is the one goroutine this package adds to a process,
// and it exists only in a giftauto build with the side-effect import present.
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
