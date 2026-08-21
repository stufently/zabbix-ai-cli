package mcp

import (
	"context"
	"strings"
	"testing"
)

// A listen address that reaches beyond this machine must not be served without
// an explicit opt-in and a bearer token. ":8000" is the trap: it looks local
// and is not.
func TestRemoteListenAddressesAreRefusedWithoutOptInAndToken(t *testing.T) {
	cases := []struct {
		name  string
		opts  HTTPOptions
		wants string
	}{
		{name: ":8000", wants: "not a loopback address"},
		{name: "0.0.0.0:8000", wants: "not a loopback address"},
		{name: "[::]:8000", wants: "not a loopback address"},
		{name: "192.0.2.1:8000", wants: "not a loopback address"},
		{
			name:  "0.0.0.0:8000 with the opt-in but no token",
			opts:  HTTPOptions{Addr: "0.0.0.0:8000", AllowNonLoopback: true},
			wants: "bearer token is required",
		},
		{name: "127.0.0.1", wants: "--http must be host:port"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := tc.opts
			if opts.Addr == "" {
				opts.Addr = tc.name
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := ServeHTTP(ctx, nil, opts)
			if err == nil {
				t.Fatalf("%s was served without an opt-in", opts.Addr)
			}
			if !strings.Contains(err.Error(), tc.wants) {
				t.Errorf("error = %q, want it to mention %q", err, tc.wants)
			}
		})
	}
}

func TestLoopbackIsRecognisedByAddressNotByAppearance(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "127.0.0.2", "::1", "localhost"} {
		if !isLoopback(host) {
			t.Errorf("%q should count as loopback", host)
		}
	}
	// An empty host is every interface. It reads as local and is not.
	for _, host := range []string{"", "0.0.0.0", "::", "192.0.2.1"} {
		if isLoopback(host) {
			t.Errorf("%q must not count as loopback", host)
		}
	}
}
