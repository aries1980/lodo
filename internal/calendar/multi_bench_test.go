package calendar

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/aries1980/lodo/internal/grid"
)

func BenchmarkMultiBusy(b *testing.B) {
	const reads = 8
	ids := make([]string, 0, reads)
	bodies := make(map[string]string, reads)
	for i := range reads {
		id := fmt.Sprintf("read%d@example.com", i)
		ids = append(ids, id)
		bodies[id] = fmt.Sprintf(
			`{"items":[{"id":"e%d","status":"confirmed","start":{"dateTime":"2026-11-09T10:00:00Z"},"end":{"dateTime":"2026-11-09T11:00:00Z"}}]}`,
			i)
	}
	srv := httptest.NewTestServer(b, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := calendarIDFromPath(r.URL.Path)
		if id != "primary" {
			time.Sleep(5 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "application/json")
		body, ok := bodies[id]
		if !ok {
			body = `{"items":[]}`
		}
		_, _ = w.Write([]byte(body))
	}))
	srv.Start()

	m, err := NewMultiClient(nil, "primary", ids, srv.URL, ReadOptions{})
	if err != nil {
		b.Fatalf("NewMultiClient: %v", err)
	}
	now := time.Date(2026, 11, 9, 8, 0, 0, 0, time.UTC)
	g := grid.Build(now, time.UTC, 15*time.Minute, 7)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, _, _, err := m.Busy(b.Context(), g, now); err != nil {
			b.Fatal(err)
		}
	}
}
