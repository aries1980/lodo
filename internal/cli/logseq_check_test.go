package cli

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/logseq"
	"github.com/aries1980/lodo/internal/run"
)

// pingClient is the smallest Client for the gate tests: Ping either
// succeeds or fails; every other method is unreachable.
type pingClient struct {
	pingErr error
	calls   atomic.Int32
}

func (c *pingClient) Ping(context.Context) error {
	c.calls.Add(1)
	return c.pingErr
}

func (c *pingClient) Pages(context.Context) ([]*logseq.Page, error) {
	return nil, errors.New("unreachable")
}

func (c *pingClient) Page(context.Context, string) (*logseq.Page, error) {
	return nil, errors.New("unreachable")
}

func (c *pingClient) Tree(context.Context, string) ([]*logseq.Block, error) {
	return nil, errors.New("unreachable")
}

func (c *pingClient) CreatePage(context.Context, string) (*logseq.Page, error) {
	return nil, errors.New("unreachable")
}

func (c *pingClient) AppendBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("unreachable")
}

func (c *pingClient) UpdateBlock(context.Context, string, string) error {
	return errors.New("unreachable")
}

func (c *pingClient) InsertBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("unreachable")
}

func (c *pingClient) SetBlockProperty(context.Context, string, string, any) error {
	return errors.New("unreachable")
}

// flakyClient fails the first n Pings, then answers, like an app that was
// momentarily busy.
type flakyClient struct {
	failures int
	calls    atomic.Int32
	pingClient
}

func (c *flakyClient) Ping(ctx context.Context) error {
	if c.calls.Add(1) <= int32(c.failures) {
		return errors.New("logseq api: logseq.Editor.get_all_pages: context deadline exceeded")
	}
	return nil
}

func TestProbeScheduleStartsAtTwoSecondsAndDoubles(t *testing.T) {
	oldInitial, oldAttempts := logseqProbeInitialTimeout, logseqProbeAttempts
	logseqProbeInitialTimeout, logseqProbeAttempts = 2*time.Second, 3
	t.Cleanup(func() { logseqProbeInitialTimeout, logseqProbeAttempts = oldInitial, oldAttempts })

	got := []time.Duration{
		logseqProbeTimeoutForAttempt(0),
		logseqProbeTimeoutForAttempt(1),
		logseqProbeTimeoutForAttempt(2),
	}
	want := []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("timeout[%d] = %v, want %v (exponential doubling)", i, got[i], want[i])
		}
	}
}

func TestCheckLogseqAvailablePassesWhenAllAnswer(t *testing.T) {
	sources := []run.Source{
		{Client: &pingClient{}, APIURL: "http://127.0.0.1:12315/api"},
		{Name: "work", Order: 0, Client: &pingClient{}, APIURL: "http://127.0.0.1:12316/api"},
	}
	if err := checkLogseqAvailable(context.Background(), sources, nil); err != nil {
		t.Fatalf("check = %v, want nil", err)
	}
}

func TestCheckLogseqAvailableNamesUnreachableEndpoint(t *testing.T) {
	c := &pingClient{pingErr: errors.New("logseq api: logseq.Editor.get_all_pages: connection refused")}
	sources := []run.Source{
		{Client: c, APIURL: "http://127.0.0.1:12315/api"},
	}
	var notices []logseqProbeAttempt
	err := checkLogseqAvailable(context.Background(), sources, func(a logseqProbeAttempt) {
		notices = append(notices, a)
	})
	if err == nil {
		t.Fatal("check succeeded against a dead app")
	}
	msg := err.Error()
	for _, want := range []string{"logseq unavailable", "http://127.0.0.1:12315/api", "connection refused", "HTTP API enabled", "logseq.api_url"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want %q", msg, want)
		}
	}
	// Refused is retried (cheap) and each retry is announced.
	if int(c.calls.Load()) != logseqProbeAttempts {
		t.Fatalf("pings = %d, want %d attempts", c.calls.Load(), logseqProbeAttempts)
	}
	if len(notices) != logseqProbeAttempts-1 {
		t.Fatalf("notices = %d, want one per retry", len(notices))
	}
	if notices[0].Attempt != 1 || notices[0].Attempts != logseqProbeAttempts {
		t.Fatalf("notice = %+v, want attempt 1 of %d", notices[0], logseqProbeAttempts)
	}
}

func TestCheckLogseqAvailableNamesGraph(t *testing.T) {
	sources := []run.Source{
		{Name: "work", Order: 0, Client: &pingClient{}, APIURL: "http://127.0.0.1:12315/api"},
		{Name: "family", Order: 1, Client: &pingClient{pingErr: errors.New("logseq api: boom")}, APIURL: "http://127.0.0.1:12316/api"},
	}
	err := checkLogseqAvailable(context.Background(), sources, nil)
	if err == nil {
		t.Fatal("check succeeded with the second graph down")
	}
	msg := err.Error()
	for _, want := range []string{`graph "family"`, "http://127.0.0.1:12316/api"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want %q", msg, want)
		}
	}
	if strings.Contains(msg, `"work"`) {
		t.Fatalf("error = %q, must name the failing graph, not the healthy one", msg)
	}
}

func TestCheckLogseqAvailableHintsTokenOnAuthFailure(t *testing.T) {
	c := &pingClient{pingErr: errors.New("logseq api: logseq.Editor.get_all_pages: HTTP 401 Unauthorized: Access Denied")}
	sources := []run.Source{
		{Client: c, APIURL: "http://127.0.0.1:12315/api"},
	}
	notified := false
	err := checkLogseqAvailable(context.Background(), sources, func(logseqProbeAttempt) { notified = true })
	if err == nil {
		t.Fatal("check succeeded against a denying app")
	}
	msg := err.Error()
	for _, want := range []string{"denied access", "logseq.token", "--logseq-token"} {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(want)) {
			t.Fatalf("error = %q, want %q", msg, want)
		}
	}
	// A wrong token never fixes itself: one attempt, no retry notices.
	if got := c.calls.Load(); got != 1 {
		t.Fatalf("pings = %d, want 1 (no retry on auth)", got)
	}
	if notified {
		t.Fatal("auth failure must not send retry notices")
	}
}

func TestCheckLogseqAvailableRetriesThenRecovers(t *testing.T) {
	c := &flakyClient{failures: 1}
	sources := []run.Source{
		{Client: c, APIURL: "http://127.0.0.1:12315/api"},
	}
	var notices []logseqProbeAttempt
	if err := checkLogseqAvailable(context.Background(), sources, func(a logseqProbeAttempt) {
		notices = append(notices, a)
	}); err != nil {
		t.Fatalf("check = %v, want nil after the retry answers", err)
	}
	if got := c.calls.Load(); got != 2 {
		t.Fatalf("pings = %d, want 2 (fail once, recover)", got)
	}
	if len(notices) != 1 {
		t.Fatalf("notices = %d, want the one retry announced", len(notices))
	}
	if notices[0].Timeout != logseqProbeTimeoutForAttempt(0) || notices[0].NextTimeout != logseqProbeTimeoutForAttempt(1) {
		t.Fatalf("notice = %+v, want the failed and next timeouts", notices[0])
	}
}

// hungClient blocks until the probe context gives up, like an app that
// accepts the connection but never answers.
type hungClient struct{}

func (c *hungClient) Ping(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func (c *hungClient) Pages(context.Context) ([]*logseq.Page, error) {
	return nil, errors.New("unreachable")
}

func (c *hungClient) Page(context.Context, string) (*logseq.Page, error) {
	return nil, errors.New("unreachable")
}

func (c *hungClient) Tree(context.Context, string) ([]*logseq.Block, error) {
	return nil, errors.New("unreachable")
}

func (c *hungClient) CreatePage(context.Context, string) (*logseq.Page, error) {
	return nil, errors.New("unreachable")
}

func (c *hungClient) AppendBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("unreachable")
}

func (c *hungClient) UpdateBlock(context.Context, string, string) error {
	return errors.New("unreachable")
}

func (c *hungClient) InsertBlock(context.Context, string, string) (*logseq.Block, error) {
	return nil, errors.New("unreachable")
}

func (c *hungClient) SetBlockProperty(context.Context, string, string, any) error {
	return errors.New("unreachable")
}

func TestCheckLogseqAvailableTimesOutEarly(t *testing.T) {
	oldInitial, oldAttempts := logseqProbeInitialTimeout, logseqProbeAttempts
	logseqProbeInitialTimeout, logseqProbeAttempts = 50*time.Millisecond, 2
	t.Cleanup(func() { logseqProbeInitialTimeout, logseqProbeAttempts = oldInitial, oldAttempts })

	sources := []run.Source{
		{Client: &hungClient{}, APIURL: "http://127.0.0.1:12315/api"},
	}
	start := time.Now()
	var notices []logseqProbeAttempt
	err := checkLogseqAvailable(context.Background(), sources, func(a logseqProbeAttempt) {
		notices = append(notices, a)
	})
	took := time.Since(start)
	if err == nil {
		t.Fatal("check succeeded against a hung app")
	}
	// Schedule sum at 50ms/2 attempts is ~129ms; the 30s read bound must
	// never be approached.
	if took > 5*time.Second {
		t.Fatalf("gate took %v, want the short probe schedule, not the 30s read bound", took)
	}
	msg := err.Error()
	for _, want := range []string{"logseq unavailable", "timed out after"} {
		if !strings.Contains(strings.ToLower(msg), strings.ToLower(want)) {
			t.Fatalf("error = %q, want %q", msg, want)
		}
	}
	if len(notices) != 1 {
		t.Fatalf("notices = %d, want the first attempt's retry announced", len(notices))
	}
}

func TestProbeRetryMessageNamesGraphAndTimeouts(t *testing.T) {
	a := logseqProbeAttempt{
		Source:      run.Source{Name: "family", APIURL: "http://127.0.0.1:12316/api"},
		Attempt:     1,
		Attempts:    3,
		Timeout:     2 * time.Second,
		NextTimeout: 4 * time.Second,
		Err:         errors.New("boom"),
	}
	msg := logseqProbeRetryMessage(a)
	for _, want := range []string{`graph "family"`, "http://127.0.0.1:12316/api", "2s", "attempt 1 of 3", "retrying"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message = %q, want %q", msg, want)
		}
	}
}
