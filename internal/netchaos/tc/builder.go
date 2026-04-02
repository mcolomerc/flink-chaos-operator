// Package tc builds tc(8) command argument slices and shell scripts for
// injecting network chaos into a container's network interface. It has zero
// Kubernetes dependencies and operates purely on Go values.
package tc

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// shellSafeRe matches strings that are safe to interpolate into a shell script:
// alphanumerics, dots, colons, slashes, percent signs, hyphens, and spaces only.
// All user-controlled values (PeerCIDR, Bandwidth, durations) must match this
// pattern before they are written into the entrypoint script.
var shellSafeRe = regexp.MustCompile(`^[a-zA-Z0-9.:/% -]+$`)

// validateShellSafe returns an error when s contains characters outside the
// safe set for shell interpolation. Call this for every user-controlled value
// before including it in EntrypointScript.
func validateShellSafe(s string) error {
	if s == "" {
		return nil
	}
	if !shellSafeRe.MatchString(s) {
		return fmt.Errorf("value %q contains characters unsafe for shell interpolation", s)
	}
	return nil
}

// Rule describes a single tc injection to apply to a network interface.
type Rule struct {
	// Interface is the network interface to apply the rule to (e.g. "eth0").
	Interface string
	// Latency is the fixed one-way delay (e.g. 100ms). Zero means no delay.
	Latency time.Duration
	// Jitter is the random variation added to the delay. Zero means no jitter.
	// Requires Latency > 0.
	Jitter time.Duration
	// LossPct is the packet drop percentage (0–100). Zero means no loss.
	LossPct int32
	// Bandwidth is the tc tbf rate string (e.g. "10mbit"). Empty means no
	// bandwidth limit. When set, Latency/Jitter/LossPct are ignored.
	Bandwidth string
	// PeerCIDR optionally restricts the netem rule to traffic destined for
	// a specific CIDR block. When non-empty, a tc filter is added after the
	// qdisc. Only supported for netem rules (not tbf bandwidth).
	PeerCIDR string
}

// AddArgs returns the argv for the tc qdisc add command described by r.
// For bandwidth rules (r.Bandwidth != ""), it uses the tbf discipline.
// Otherwise it uses the netem discipline with optional delay, jitter, and loss.
// PeerCIDR filtering requires a separate tc filter command — see FilterArgs.
func AddArgs(r Rule) []string {
	if r.Bandwidth != "" {
		return []string{
			"tc", "qdisc", "add", "dev", r.Interface,
			"root", "tbf",
			"rate", r.Bandwidth,
			"burst", "32kbit",
			"latency", "400ms",
		}
	}

	args := []string{"tc", "qdisc", "add", "dev", r.Interface, "root", "netem"}

	if r.Latency > 0 {
		args = append(args, "delay", formatDuration(r.Latency))
		if r.Jitter > 0 {
			args = append(args, formatDuration(r.Jitter))
		}
	}

	if r.LossPct > 0 {
		args = append(args, "loss", fmt.Sprintf("%d%%", r.LossPct))
	}

	return args
}

// FilterArgs returns the argv for tc filter add to restrict a qdisc to the
// CIDR in r.PeerCIDR. handle is the parent handle string (e.g. "1:").
// Returns nil when r.PeerCIDR is empty — no filter is needed.
func FilterArgs(r Rule, handle string) []string {
	if r.PeerCIDR == "" {
		return nil
	}
	return []string{
		"tc", "filter", "add", "dev", r.Interface,
		"parent", handle,
		"protocol", "ip",
		"u32", "match", "ip", "dst", r.PeerCIDR,
		"flowid", handle,
	}
}

// DelArgs returns the argv to delete the root qdisc from iface.
func DelArgs(iface string) []string {
	return []string{"tc", "qdisc", "del", "dev", iface, "root"}
}

// formatDuration formats d as a tc-compatible duration string using the
// largest unit that represents d exactly.
func formatDuration(d time.Duration) string {
	switch {
	case d%time.Second == 0:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d%time.Millisecond == 0:
		return fmt.Sprintf("%dms", int(d.Milliseconds()))
	default:
		return fmt.Sprintf("%dus", int(d.Microseconds()))
	}
}

// sleepSeconds returns the sleep duration in whole seconds, with a minimum of 1.
func sleepSeconds(d time.Duration) int {
	s := int(d.Seconds())
	if s < 1 {
		return 1
	}
	return s
}

// EntrypointScript returns a POSIX sh script that applies the tc rule described
// by r, sleeps for duration, then cleans up. The script is self-contained and
// suitable for use as an ephemeral container command. It traps SIGTERM/INT so
// the cleanup runs even when the container is stopped early.
//
// All user-controlled values (Interface, Bandwidth, PeerCIDR) are validated
// against a strict allowlist before interpolation to prevent shell injection.
// The function panics on validation failure because callers are expected to
// validate inputs before construction; a panic surfaces programmer errors early.
func EntrypointScript(r Rule, duration time.Duration) string {
	// Validate all user-controlled values that will be interpolated into the
	// shell script. These must not contain shell metacharacters.
	for _, v := range []string{r.Interface, r.Bandwidth, r.PeerCIDR} {
		if err := validateShellSafe(v); err != nil {
			panic(fmt.Sprintf("tc.EntrypointScript: %v", err))
		}
	}

	var b strings.Builder

	b.WriteString("#!/bin/sh\n")
	b.WriteString("set -e\n")
	b.WriteString("_cleanup() {\n")
	fmt.Fprintf(&b, "  tc qdisc del dev %s root 2>/dev/null || true\n", r.Interface)
	b.WriteString("}\n")
	b.WriteString("trap '_cleanup' TERM INT\n")

	// Build tc qdisc add command inline.
	b.WriteString(strings.Join(AddArgs(r), " "))
	b.WriteString("\n")

	// Optionally add the filter command for PeerCIDR (netem only).
	if r.Bandwidth == "" && r.PeerCIDR != "" {
		filter := FilterArgs(r, "1:")
		if filter != nil {
			b.WriteString(strings.Join(filter, " "))
			b.WriteString("\n")
		}
	}

	fmt.Fprintf(&b, "sleep %d &\n", sleepSeconds(duration))
	b.WriteString("SLEEP_PID=$!\n")
	b.WriteString("wait $SLEEP_PID\n")
	b.WriteString("_cleanup\n")

	return b.String()
}
