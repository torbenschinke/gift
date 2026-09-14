//go:build giftmetrics

package metrics

import (
	"fmt"
	"os"
	"strings"
	"time"
)

// config is the environment configuration, read once. See the package
// documentation for the list of variables.
type config struct {
	enabled  bool
	interval time.Duration
	out      string
}

var env = readEnv(os.Getenv, os.Stderr)

// readEnv parses the configuration. It is a function of an accessor so that a
// test can drive it without touching the process environment.
//
// Nothing in here can fail. A malformed value is reported once on warn and
// then ignored: refusing to start would mean that setting a debugging
// variable wrongly takes the application down, and silently substituting a
// default would mean that a measurement ran with settings nobody asked for.
func readEnv(get func(string) string, warn *os.File) config {
	c := config{}
	switch strings.ToLower(strings.TrimSpace(get("GIFT_METRICS"))) {
	case "", "0", "false", "no", "off":
	case "1", "true", "yes", "on":
		c.enabled = true
	default:
		fmt.Fprintf(warn, "gift/metrics: ignoring GIFT_METRICS=%q, expected one of 1, true, yes, on, 0, false, no, off\n",
			get("GIFT_METRICS"))
	}

	if s := strings.TrimSpace(get("GIFT_METRICS_INTERVAL")); s != "" {
		d, err := time.ParseDuration(s)
		switch {
		case err != nil:
			fmt.Fprintf(warn, "gift/metrics: ignoring GIFT_METRICS_INTERVAL=%q: %v\n", s, err)
		case d < 0:
			fmt.Fprintf(warn, "gift/metrics: ignoring negative GIFT_METRICS_INTERVAL=%q\n", s)
		default:
			c.interval = d
		}
	}

	c.out = strings.TrimSpace(get("GIFT_METRICS_OUT"))
	return c
}

// Enabled reports whether measurement is compiled in and switched on.
//
// In a build without the giftmetrics tag this is a function whose body is the
// constant false, so a caller's `if metrics.Enabled() { ... }` is removed
// entirely. In a build with the tag it additionally consults GIFT_METRICS.
func Enabled() bool { return env.enabled }
