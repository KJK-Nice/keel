package session

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
	"time"

	"keel/internal/store"
)

// ProbeTimeout bounds a Test connection run.
const ProbeTimeout = 10 * time.Second

// Probe outcomes (ProbeResult.Kind).
const (
	ProbeOK        = "ok"
	ProbeAuth      = "auth"
	ProbeUnknown   = "unknown-host"
	ProbeMismatch  = "host-key-mismatch"
	ProbeTimedOut  = "timeout"
	ProbeDNS       = "dns"
	ProbeRefused   = "refused"
	ProbeCancelled = "cancelled"
	ProbeError     = "error"
)

// ProbeResult is what Test connection shows. Hop is "jump" or "target"
// when a jump host was used and the connect failed. Hostname, Fingerprint,
// KeyType and Expected are set for an unknown or changed host key.
type ProbeResult struct {
	OK          bool   `json:"ok"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
	LatencyMs   int64  `json:"latencyMs,omitempty"`
	Hop         string `json:"hop,omitempty"`
	HopName     string `json:"hopName,omitempty"`
	Hostname    string `json:"hostname,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
	KeyType     string `json:"keyType,omitempty"`
	Expected    string `json:"expected,omitempty"`
}

// Probe connects and authenticates to host (through its jump host, if
// any), then closes the connection. It opens no session or PTY and runs
// no command. An unknown host key is reported, never trusted: checkKey
// only reads the store.
func Probe(ctx context.Context, host store.Host, checkKey func(hostname, fingerprint string) error) ProbeResult {
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	start := time.Now()
	client, err := DialContext(ctx, host, checkKey)
	if err == nil {
		ms := time.Since(start).Milliseconds()
		_ = client.Close()
		return ProbeResult{OK: true, Kind: ProbeOK, Message: "Connected and authenticated", LatencyMs: ms}
	}
	return ClassifyProbe(ctx, err)
}

// ClassifyProbe turns a connect error into a ProbeResult.
func ClassifyProbe(ctx context.Context, err error) ProbeResult {
	out := ProbeResult{Kind: ProbeError, Message: err.Error()}
	var hop *HopError
	if errors.As(err, &hop) {
		out.Hop = hop.Hop
		out.HopName = hop.Name
		out.Message = hop.Err.Error()
	}
	var unk *store.UnknownHostError
	var mm *store.HostKeyMismatchError
	var dns *net.DNSError
	var ne net.Error
	switch {
	case errors.As(err, &unk):
		out.Kind = ProbeUnknown
		out.Hostname, out.Fingerprint, out.KeyType = unk.Host, unk.Fingerprint, unk.KeyType
	case errors.As(err, &mm):
		out.Kind = ProbeMismatch
		out.Hostname, out.Fingerprint, out.KeyType, out.Expected = mm.Host, mm.Got, mm.KeyType, mm.Expected
	case errors.Is(err, context.Canceled) || (ctx != nil && errors.Is(ctx.Err(), context.Canceled)):
		out.Kind = ProbeCancelled
	case errors.Is(err, context.DeadlineExceeded) || (ctx != nil && errors.Is(ctx.Err(), context.DeadlineExceeded)):
		out.Kind = ProbeTimedOut
	case errors.As(err, &dns):
		out.Kind = ProbeDNS
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(strings.ToLower(err.Error()), "connection refused"):
		out.Kind = ProbeRefused
	case errors.As(err, &ne) && ne.Timeout():
		out.Kind = ProbeTimedOut
	case isAuthError(err):
		out.Kind = ProbeAuth
	}
	return out
}

func isAuthError(err error) bool {
	msg := err.Error()
	for _, s := range []string{"unable to authenticate", "password is required", "private key is required", "parse private key", "no supported methods remain"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}
