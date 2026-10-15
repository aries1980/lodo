package calendar

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// writeFake records mutating calls and serves list from its map.
type writeFake struct {
	events map[string]Event
	calls  []string
}

func (f *writeFake) mount(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/events"):
			w.Header().Set("Content-Type", "application/json")
			items := make([]Event, 0, len(f.events))
			for _, e := range f.events {
				items = append(items, e)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": items})
		case r.Method == http.MethodPost:
			f.calls = append(f.calls, "POST")
			body, _ := io.ReadAll(r.Body)
			var e Event
			if err := json.Unmarshal(body, &e); err != nil {
				http.Error(w, "bad", http.StatusBadRequest)
				return
			}
			f.events[e.ID] = e
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(e)
		case r.Method == http.MethodPatch:
			f.calls = append(f.calls, "PATCH")
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			body, _ := io.ReadAll(r.Body)
			var p Event
			_ = json.Unmarshal(body, &p)
			cur := f.events[id]
			if p.Summary != "" {
				cur.Summary = p.Summary
			}
			if p.Start.DateTime != "" {
				cur.Start = p.Start
			}
			if p.End.DateTime != "" {
				cur.End = p.End
			}
			f.events[id] = cur
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cur)
		case r.Method == http.MethodDelete:
			f.calls = append(f.calls, "DELETE")
			id := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
			delete(f.events, id)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestInsertPatchDeleteRoundTrip(t *testing.T) {
	f := &writeFake{events: map[string]Event{}}
	c, err := NewClient(nil, "primary", f.mount(t))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	ctx := context.Background()
	loc := london(t)
	start := at(9, 9, 0, loc)
	end := at(9, 9, 30, loc)

	in := Event{
		ID: "lodoabc123", Summary: "Write the note",
		Start: EventTime{DateTime: start.Format(time.RFC3339), TimeZone: "Europe/London"},
		End:   EventTime{DateTime: end.Format(time.RFC3339), TimeZone: "Europe/London"},
		ExtendedProperties: Extended{Private: map[string]string{
			"lodo": "true", "task": "t1", "block": "0",
		}},
		Reminders: &Reminders{UseDefault: true},
	}
	got, err := c.Insert(ctx, in)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if got.ID != in.ID || got.Summary != in.Summary {
		t.Fatalf("Insert = %+v", got)
	}
	if got.ExtendedProperties.Private["lodo"] != "true" || got.Reminders == nil || !got.Reminders.UseDefault {
		t.Fatalf("insert must carry ownership + default reminders: %+v", got)
	}

	patched, err := c.Patch(ctx, in.ID, Event{
		Summary: "Renamed",
		Start:   EventTime{DateTime: start.Add(time.Hour).Format(time.RFC3339)},
		End:     EventTime{DateTime: end.Add(time.Hour).Format(time.RFC3339)},
	})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if patched.Summary != "Renamed" {
		t.Fatalf("Patch summary = %q", patched.Summary)
	}

	if err := c.Delete(ctx, in.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := f.events[in.ID]; ok {
		t.Fatalf("event still present after delete")
	}
	if len(f.calls) != 3 || f.calls[0] != "POST" || f.calls[1] != "PATCH" || f.calls[2] != "DELETE" {
		t.Fatalf("calls = %v", f.calls)
	}
}

func TestWriteErrorSurfacesMessage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"calendar is read-only"}}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewClient(nil, "primary", srv.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.Insert(context.Background(), Event{ID: "x"}); err == nil ||
		!strings.Contains(err.Error(), "calendar is read-only") {
		t.Fatalf("Insert err = %v, want the API message", err)
	}
}
