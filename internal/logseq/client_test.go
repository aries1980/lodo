package logseq_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/aries1980/lodo/internal/logseq"
)

// testdataBody loads a captured response fixture. The shapes were probed
// against the running app (Logseq 0.10.x); the contents are anonymised.
func testdataBody(t *testing.T, name string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// token is the stub app's bearer token.
const token = "st-token"

type stubCall struct {
	Method string
	Args   []any
}

// newStubApp runs a minimal Logseq API server: it authenticates the bearer
// token, requires POST /api, records every call, and routes each method to
// the response the research probe captured for it.
func newStubApp(t *testing.T, route func(call stubCall) (string, int)) (logseq.Client, *[]stubCall) {
	t.Helper()
	var calls []stubCall
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Bearer "+token {
			http.Error(w, "Access Denied", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		var req struct {
			Method string `json:"method"`
			Args   []any  `json:"args"`
		}
		if err := json.Unmarshal(raw, &req); err != nil {
			t.Fatalf("stub received a request not in the documented call shape: %v (%s)", err, raw)
		}
		calls = append(calls, stubCall{Method: req.Method, Args: req.Args})
		body, status := route(stubCall{Method: req.Method, Args: req.Args})
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)

	client, err := logseq.NewClient(srv.URL+"/api", token, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client, &calls
}

func TestPages(t *testing.T) {
	client, calls := newStubApp(t, func(_ stubCall) (string, int) {
		return testdataBody(t, "pages.json"), http.StatusOK
	})

	pages, err := client.Pages(context.Background())
	if err != nil {
		t.Fatalf("Pages: %v", err)
	}
	if len(pages) != 3 {
		t.Fatalf("Pages returned %d pages, want 3", len(pages))
	}
	if pages[2].Name != "Time Maps" || pages[2].UUID != "22222222-2222-4222-8222-222222222222" {
		t.Fatalf("page 2 = %+v", pages[2])
	}
	if pages[0].Properties != nil {
		t.Fatalf("pages carry no properties here; got %+v", pages[0].Properties)
	}

	if len(*calls) != 1 || (*calls)[0].Method != "logseq.Editor.get_all_pages" {
		t.Fatalf("calls = %+v, want one get_all_pages", *calls)
	}
}

func TestPageFoundAndMissing(t *testing.T) {
	client, calls := newStubApp(t, func(c stubCall) (string, int) {
		if c.Method != "logseq.Editor.get_page" {
			t.Fatalf("unexpected method %q", c.Method)
		}
		if c.Args[0] == "ghost" {
			return "null", http.StatusOK
		}
		return testdataBody(t, "page-journal.json"), http.StatusOK
	})

	page, err := client.Page(context.Background(), "2026-10-15")
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page == nil || page.Name != "2026-10-15" || page.Properties != nil {
		t.Fatalf("page = %+v, want the captured shape with nil properties", page)
	}

	page, err = client.Page(context.Background(), "ghost")
	if err != nil {
		t.Fatalf("Page(ghost): %v", err)
	}
	if page != nil {
		t.Fatalf("Page(ghost) = %+v, want nil for a JSON null", page)
	}

	if len(*calls) != 2 || (*calls)[1].Args[0].(string) != "ghost" {
		t.Fatalf("calls = %+v, want the page name passed verbatim in args", *calls)
	}
}

func TestTree(t *testing.T) {
	client, _ := newStubApp(t, func(c stubCall) (string, int) {
		switch c.Args[0].(string) {
		case "empty":
			return "[]", http.StatusOK
		case "missing":
			return "null", http.StatusOK
		default:
			return testdataBody(t, "tree-journal.json"), http.StatusOK
		}
	})

	tree, err := client.Tree(context.Background(), "2026-10-15")
	if err != nil {
		t.Fatalf("Tree: %v", err)
	}
	want := []*logseq.Block{
		{
			UUID:    "33333333-3333-4333-8333-333333333333",
			Content: "Assorted notes from today",
		},
		{
			UUID:    "34444444-4444-4444-8444-444444444444",
			Content: "TODO Review the draft\nid:: 34444444-4444-4444-8444-444444444444",
			Marker:  "TODO",
			Properties: map[string]any{
				"id": "34444444-4444-4444-8444-444444444444",
			},
			PropsText: map[string]any{
				"id": "34444444-4444-4444-8444-444444444444",
			},
			Children: []*logseq.Block{
				{
					UUID:    "35555555-5555-4555-8555-555555555555",
					Content: "Details are notes, not Tasks",
					Marker:  "",
				},
			},
		},
		{
			UUID:    "36666666-6666-4666-8666-666666666666",
			Content: "DOING File the expenses",
			Marker:  "DOING",
		},
		{
			UUID:    "37777777-7777-4777-8777-777777777777",
			Content: "",
		},
	}
	if diff := cmp.Diff(want, tree); diff != "" {
		t.Fatalf("Tree mismatch (-want +got):\n%s", diff)
	}

	if tree, _ := client.Tree(context.Background(), "empty"); tree != nil {
		t.Fatalf("Tree(empty) = %+v, want nil", tree)
	}
	if tree, _ := client.Tree(context.Background(), "missing"); tree != nil {
		t.Fatalf("Tree(missing) = %+v, want nil", tree)
	}
}

func TestAppErrorSurfaces(t *testing.T) {
	client, _ := newStubApp(t, func(_ stubCall) (string, int) {
		return `{"error": "Access Denied"}`, http.StatusInternalServerError
	})

	_, err := client.Pages(context.Background())
	if err == nil {
		t.Fatal("Pages succeeded against an erroring app")
	}
	if !strings.Contains(err.Error(), "Access Denied") {
		t.Fatalf("error = %q, want the app's own message", err.Error())
	}
}

func TestHTTPErrorSurfacesBody(t *testing.T) {
	client, _ := newStubApp(t, func(_ stubCall) (string, int) {
		return "the graph is closed", http.StatusBadGateway
	})

	_, err := client.Pages(context.Background())
	if err == nil {
		t.Fatal("Pages succeeded against a broken app")
	}
	if !strings.Contains(err.Error(), "502") || !strings.Contains(err.Error(), "the graph is closed") {
		t.Fatalf("error = %q, want status and body", err.Error())
	}
}

func TestUnreachableApp(t *testing.T) {
	// A closed port: the adapter reports the cause, not the HTTP plumbing.
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL + "/api"
	srv.Close()

	client, err := logseq.NewClient(url, token, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Pages(context.Background())
	if err == nil {
		t.Fatal("Pages succeeded against a dead app")
	}
	if !strings.Contains(err.Error(), "logseq api") {
		t.Fatalf("error = %q, want the logseq api prefix", err.Error())
	}
}

func TestAccessTokenRequired(t *testing.T) {
	var denied bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			denied = true
			http.Error(w, "Access Denied", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "[]")
	}))
	t.Cleanup(srv.Close)

	client, err := logseq.NewClient(srv.URL+"/api", "", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Pages(context.Background()); err == nil || !denied {
		t.Fatalf("Pages err = %v, denied=%v; a missing token must reach the app as no token", err, denied)
	}
}

func TestNewClientRejectsBadURL(t *testing.T) {
	for _, bad := range []string{"", "not a url", "logseq.local/api", "ftp://x/api"} {
		if _, err := logseq.NewClient(bad, token, nil); err == nil {
			t.Fatalf("NewClient(%q) succeeded", bad)
		}
	}
}

func TestTLSUnknownAuthorityHintsAtInsecure(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "[]")
	}))
	t.Cleanup(srv.Close)

	client, err := logseq.NewClient(srv.URL+"/api", token, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Pages(context.Background())
	if err == nil {
		t.Fatal("Pages succeeded against a self-signed TLS server with verification on")
	}
	msg := err.Error()
	if !strings.Contains(msg, "logseq api") {
		t.Fatalf("error = %q, want the logseq api prefix", msg)
	}
	for _, want := range []string{"logseq.api_url", "logseq.insecure"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error = %q, want hint containing %q", msg, want)
		}
	}
}

func TestInsecureTransportSkipsVerify(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"name":"Time Maps","uuid":"tm"}]`)
	}))
	t.Cleanup(srv.Close)

	// Same shape as the insecure config path: a default transport clone
	// that skips verification, without the test server's RootCAs.
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Skip("DefaultTransport is not *http.Transport")
	}
	cloned := base.Clone()
	if cloned.TLSClientConfig == nil {
		t.Fatal("cloned transport has no TLS config")
	}
	cloned.TLSClientConfig.InsecureSkipVerify = true
	insecure := &http.Client{Timeout: logseq.DefaultTimeout, Transport: cloned}

	client, err := logseq.NewClient(srv.URL+"/api", token, insecure)
	if err != nil {
		t.Fatal(err)
	}
	pages, err := client.Pages(context.Background())
	if err != nil {
		t.Fatalf("Pages with skipped verification: %v", err)
	}
	if len(pages) != 1 || pages[0].Name != "Time Maps" {
		t.Fatalf("pages = %+v, want the one stub page", pages)
	}
}
