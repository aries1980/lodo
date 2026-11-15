package logseq_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/aries1980/lodo/internal/logseq"
)

// TestClientConcurrentTrees pins the adapter's concurrent-read contract: each
// response stays with the page that asked for it, and no decode state is
// shared between calls.
func TestClientConcurrentTrees(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Args   []any  `json:"args"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		name, _ := req.Args[0].(string)
		if req.Method != "logseq.Editor.get_page_blocks_tree" {
			http.Error(w, "unexpected method "+req.Method, http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode([]*logseq.Block{{
			UUID:    "uuid-" + name,
			Content: "TODO " + name,
		}})
	}))
	t.Cleanup(srv.Close)

	client, err := logseq.NewClient(srv.URL+"/api", token, srv.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	const calls = 8
	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for i := range calls {
		wg.Go(func() {
			name := fmt.Sprintf("page-%d", i)
			tree, err := client.Tree(t.Context(), name)
			if err != nil {
				errs <- err
				return
			}
			if len(tree) != 1 || tree[0].UUID != "uuid-"+name {
				errs <- fmt.Errorf("%s: tree = %+v", name, tree)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
