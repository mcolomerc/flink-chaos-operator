package tc

import (
	"strings"
	"testing"
	"time"
)

func TestAddArgs_NetemDelayOnly(t *testing.T) {
	r := Rule{Interface: "eth0", Latency: 100 * time.Millisecond}
	got := AddArgs(r)
	want := []string{"tc", "qdisc", "add", "dev", "eth0", "root", "netem", "delay", "100ms"}
	assertSliceEqual(t, want, got)
}

func TestAddArgs_NetemDelayAndJitter(t *testing.T) {
	r := Rule{Interface: "eth0", Latency: 100 * time.Millisecond, Jitter: 20 * time.Millisecond}
	got := AddArgs(r)
	want := []string{"tc", "qdisc", "add", "dev", "eth0", "root", "netem", "delay", "100ms", "20ms"}
	assertSliceEqual(t, want, got)
}

func TestAddArgs_NetemLossOnly(t *testing.T) {
	r := Rule{Interface: "eth0", LossPct: 10}
	got := AddArgs(r)
	want := []string{"tc", "qdisc", "add", "dev", "eth0", "root", "netem", "loss", "10%"}
	assertSliceEqual(t, want, got)
}

func TestAddArgs_NetemDelayAndLoss(t *testing.T) {
	r := Rule{Interface: "eth0", Latency: 50 * time.Millisecond, LossPct: 5}
	got := AddArgs(r)
	want := []string{"tc", "qdisc", "add", "dev", "eth0", "root", "netem", "delay", "50ms", "loss", "5%"}
	assertSliceEqual(t, want, got)
}

func TestAddArgs_TBFBandwidth(t *testing.T) {
	r := Rule{Interface: "eth0", Bandwidth: "10mbit"}
	got := AddArgs(r)
	want := []string{
		"tc", "qdisc", "add", "dev", "eth0",
		"root", "tbf",
		"rate", "10mbit",
		"burst", "32kbit",
		"latency", "400ms",
	}
	assertSliceEqual(t, want, got)
}

func TestAddArgs_TBFIgnoresNetemFields(t *testing.T) {
	// When Bandwidth is set, Latency/Jitter/LossPct must be ignored.
	r := Rule{
		Interface: "eth0",
		Bandwidth: "5mbit",
		Latency:   200 * time.Millisecond,
		Jitter:    10 * time.Millisecond,
		LossPct:   20,
	}
	got := AddArgs(r)
	want := []string{
		"tc", "qdisc", "add", "dev", "eth0",
		"root", "tbf",
		"rate", "5mbit",
		"burst", "32kbit",
		"latency", "400ms",
	}
	assertSliceEqual(t, want, got)
}

func TestFilterArgs_WithCIDR(t *testing.T) {
	r := Rule{Interface: "eth0", PeerCIDR: "10.0.0.0/8"}
	got := FilterArgs(r, "1:")
	want := []string{
		"tc", "filter", "add", "dev", "eth0",
		"parent", "1:",
		"protocol", "ip",
		"u32", "match", "ip", "dst", "10.0.0.0/8",
		"flowid", "1:",
	}
	assertSliceEqual(t, want, got)
}

func TestFilterArgs_EmptyCIDR_ReturnsNil(t *testing.T) {
	r := Rule{Interface: "eth0", PeerCIDR: ""}
	got := FilterArgs(r, "1:")
	if got != nil {
		t.Errorf("FilterArgs with empty PeerCIDR: got %v, want nil", got)
	}
}

func TestDelArgs(t *testing.T) {
	got := DelArgs("eth0")
	want := []string{"tc", "qdisc", "del", "dev", "eth0", "root"}
	assertSliceEqual(t, want, got)
}

// TestFormatDuration_Seconds exercises formatDuration through AddArgs with a
// 1-second delay, which must be rendered as "1s".
func TestFormatDuration_Seconds(t *testing.T) {
	r := Rule{Interface: "eth0", Latency: 1 * time.Second}
	got := AddArgs(r)
	if got[len(got)-1] != "1s" {
		t.Errorf("expected last arg to be \"1s\", got %q (full: %v)", got[len(got)-1], got)
	}
}

// TestFormatDuration_Milliseconds exercises formatDuration with a 100ms delay.
func TestFormatDuration_Milliseconds(t *testing.T) {
	r := Rule{Interface: "eth0", Latency: 100 * time.Millisecond}
	got := AddArgs(r)
	if got[len(got)-1] != "100ms" {
		t.Errorf("expected last arg to be \"100ms\", got %q (full: %v)", got[len(got)-1], got)
	}
}

// TestFormatDuration_Microseconds exercises formatDuration with a 500µs delay
// which cannot be expressed in whole milliseconds.
func TestFormatDuration_Microseconds(t *testing.T) {
	r := Rule{Interface: "eth0", Latency: 500 * time.Microsecond}
	got := AddArgs(r)
	if got[len(got)-1] != "500us" {
		t.Errorf("expected last arg to be \"500us\", got %q (full: %v)", got[len(got)-1], got)
	}
}

func TestEntrypointScript_NetemRule(t *testing.T) {
	r := Rule{Interface: "eth0", Latency: 100 * time.Millisecond, Jitter: 20 * time.Millisecond}
	script := EntrypointScript(r, 60*time.Second)

	mustContain(t, script, "tc qdisc add dev eth0 root netem delay 100ms 20ms")
	mustContain(t, script, "sleep 60")
	mustContain(t, script, "_cleanup")
	mustContain(t, script, "trap '_cleanup' TERM INT")
}

func TestEntrypointScript_BandwidthRule(t *testing.T) {
	r := Rule{Interface: "eth0", Bandwidth: "10mbit"}
	script := EntrypointScript(r, 30*time.Second)

	mustContain(t, script, "tc qdisc add dev eth0 root tbf rate 10mbit burst 32kbit latency 400ms")
	mustContain(t, script, "sleep 30")
}

func TestEntrypointScript_PeerCIDR(t *testing.T) {
	r := Rule{Interface: "eth0", Latency: 100 * time.Millisecond, PeerCIDR: "10.0.0.0/8"}
	script := EntrypointScript(r, 60*time.Second)

	mustContain(t, script, "tc qdisc add dev eth0 root netem delay 100ms")
	mustContain(t, script, "tc filter add dev eth0 parent 1: protocol ip u32 match ip dst 10.0.0.0/8 flowid 1:")
}

func TestEntrypointScript_MinimumSleepOneSecond(t *testing.T) {
	r := Rule{Interface: "eth0", Latency: 50 * time.Millisecond}
	// duration less than 1 second should still sleep for at least 1 second.
	script := EntrypointScript(r, 0)
	mustContain(t, script, "sleep 1")
}

func TestEntrypointScript_NoPeerCIDRFilter_WhenBandwidth(t *testing.T) {
	// PeerCIDR should not produce a filter line when Bandwidth is set.
	r := Rule{Interface: "eth0", Bandwidth: "5mbit", PeerCIDR: "192.168.0.0/16"}
	script := EntrypointScript(r, 60*time.Second)

	if strings.Contains(script, "tc filter") {
		t.Errorf("expected no tc filter command when Bandwidth is set, got:\n%s", script)
	}
}

// assertSliceEqual fails the test if got does not match want element-by-element.
func assertSliceEqual(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("length mismatch: want %d elements %v, got %d elements %v", len(want), want, len(got), got)
		return
	}
	for i := range want {
		if want[i] != got[i] {
			t.Errorf("element[%d]: want %q, got %q (full want: %v, full got: %v)", i, want[i], got[i], want, got)
		}
	}
}

// mustContain fails the test if script does not contain substr.
func mustContain(t *testing.T, script, substr string) {
	t.Helper()
	if !strings.Contains(script, substr) {
		t.Errorf("script does not contain %q\nscript:\n%s", substr, script)
	}
}
