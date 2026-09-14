//go:build giftmetrics

package metrics

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// devNull is a writer for the warning path of readEnv. The function takes an
// *os.File because that is what it warns to in production; /dev/null keeps the
// test output clean without weakening the assertions, which are about the
// parsed configuration and not about the wording.
func devNull(t *testing.T) *os.File {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestReadEnv(t *testing.T) {
	null := devNull(t)
	cases := []struct {
		name string
		in   map[string]string
		want config
	}{
		{"unset", nil, config{}},
		{"on", map[string]string{"GIFT_METRICS": "1"}, config{enabled: true}},
		{"true", map[string]string{"GIFT_METRICS": "TRUE"}, config{enabled: true}},
		{"off", map[string]string{"GIFT_METRICS": "0"}, config{}},
		{"garbage", map[string]string{"GIFT_METRICS": "maybe"}, config{}},
		{"interval", map[string]string{"GIFT_METRICS": "on", "GIFT_METRICS_INTERVAL": "5s"},
			config{enabled: true, interval: 5 * time.Second}},
		{"bad interval", map[string]string{"GIFT_METRICS": "on", "GIFT_METRICS_INTERVAL": "5 seconds"},
			config{enabled: true}},
		{"negative interval", map[string]string{"GIFT_METRICS": "on", "GIFT_METRICS_INTERVAL": "-5s"},
			config{enabled: true}},
		{"out", map[string]string{"GIFT_METRICS": "on", "GIFT_METRICS_OUT": " /tmp/x "},
			config{enabled: true, out: "/tmp/x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := readEnv(envOf(c.in), null); got != c.want {
				t.Fatalf("readEnv = %+v, want %+v", got, c.want)
			}
		})
	}
}

// TestMalformedInputWarnsAndCarriesOn is the promise of the package
// documentation: a wrong variable never stops the program.
func TestMalformedInputWarnsAndCarriesOn(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	got := readEnv(envOf(map[string]string{"GIFT_METRICS": "yes", "GIFT_METRICS_INTERVAL": "nope"}), w)
	_ = w.Close()
	warned := <-done

	if !got.enabled || got.interval != 0 {
		t.Fatalf("config = %+v, want enabled with no interval", got)
	}
	if !strings.Contains(warned, "GIFT_METRICS_INTERVAL") {
		t.Fatalf("nothing useful was reported on stderr: %q", warned)
	}
}

// TestReportCarriesTheBoundFields pins the field names the project plan,
// section 13, makes binding. A reporter that quietly drops one of them turns
// a measurement into an unverifiable number.
func TestReportCarriesTheBoundFields(t *testing.T) {
	rec := &Recorder{
		timer: NewFrameTimerWith(FrameTimerOptions{Capacity: 16, Warmup: 2}),
		start: time.Now(),
	}
	var sb strings.Builder
	rec.enc = json.NewEncoder(&sb)
	rec.timer.RecordInterval(140 * time.Millisecond)
	rec.timer.RecordInterval(140 * time.Millisecond)
	rec.timer.RecordInterval(100 * time.Microsecond)
	rec.timer.RecordInterval(16 * time.Millisecond)
	rec.timer.RecordUpdate(time.Millisecond)
	rec.timer.RecordDraw(2 * time.Millisecond)
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	var m map[string]any
	if err := json.Unmarshal([]byte(sb.String()), &m); err != nil {
		t.Fatalf("the report is not one JSON object per line: %v (%q)", err, sb.String())
	}
	for _, k := range []string{
		"kind", "elapsed_s", "update_cpu", "draw_cpu", "frame_interval",
		"nominal_interval_ms", "interval_tolerance_ms", "missed_threshold_ms",
		"missed_intervals", "missed_ratio",
		"warmup_frames", "warmup_dropped", "min_interval_ms", "sub_frame_intervals",
		"updates", "draws", "mem",
	} {
		if _, ok := m[k]; !ok {
			t.Errorf("the report has no %q field", k)
		}
	}
	if m["kind"] != "final" {
		t.Errorf("kind = %v, want final", m["kind"])
	}
	if m["warmup_dropped"] != float64(2) {
		t.Errorf("warmup_dropped = %v, want 2", m["warmup_dropped"])
	}
	if m["sub_frame_intervals"] != float64(1) {
		t.Errorf("sub_frame_intervals = %v, want 1", m["sub_frame_intervals"])
	}
	if m["missed_threshold_ms"] != 17.167 {
		t.Errorf("missed_threshold_ms = %v, want 17.167", m["missed_threshold_ms"])
	}
	// Nothing supplied an App, a renderer or a shaper, so those objects are
	// absent rather than present and zero. A zero counter that was never
	// read is a lie in a measurement.
	for _, k := range []string{"gift", "renderer", "shaper"} {
		if _, ok := m[k]; ok {
			t.Errorf("the report invented a %q object with no source", k)
		}
	}
}

// TestNilRecorderIsInert mirrors the stub: even in the measured build, a
// recorder that was never started does nothing rather than panicking.
func TestNilRecorderIsInert(t *testing.T) {
	var rec *Recorder
	rec.RecordUpdate(time.Millisecond)
	rec.RecordDraw(time.Millisecond)
	rec.RecordInterval(time.Millisecond)
	rec.Tick()
	if rec.Timer() != nil || rec.Snapshot() != (FrameTimes{}) {
		t.Fatal("a nil recorder produced something")
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}
}
