package cli

import (
	"net/http"
	"testing"

	"github.com/aries1980/lodo/internal/config"
	"github.com/aries1980/lodo/internal/logseq"
)

func TestHTTPClientInsecure(t *testing.T) {
	if got := httpClient(config.Config{}); got != nil {
		t.Fatalf("secure config returned %+v, want nil (adapter default)", got)
	}

	cfg := config.Config{}
	cfg.Logseq.Insecure = true
	got := httpClient(cfg)
	if got == nil {
		t.Fatal("insecure config returned nil, want a skipping client")
	}
	if got.Timeout != logseq.DefaultTimeout {
		t.Fatalf("timeout = %v, want %v", got.Timeout, logseq.DefaultTimeout)
	}
	transport, ok := got.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport is %T, want *http.Transport", got.Transport)
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("insecure transport must skip TLS verification")
	}

	// The shared default stays verifying: cloning must not flip the
	// process-wide transport to insecure.
	if def, ok := http.DefaultTransport.(*http.Transport); ok && def.TLSClientConfig != nil {
		if def.TLSClientConfig.InsecureSkipVerify {
			t.Fatal("DefaultTransport was left insecure")
		}
	}
}
