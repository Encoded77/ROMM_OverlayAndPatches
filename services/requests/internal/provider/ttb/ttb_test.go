package ttb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestReason(t *testing.T) {
	in := "prowlarr /api/v1/search: HTTP 400: {\n  \"message\": \"Search failed due to all selected indexers being unavailable\",\n  \"description\": \"NzbDrone...\"\n}"
	if got := reason(in); got != "Search failed due to all selected indexers being unavailable" {
		t.Fatalf("got %q", got)
	}
	if got := reason("timeout"); got != "timeout" {
		t.Fatalf("got %q", got)
	}
}

func TestCancelRemovesAFinishedJobTTBWillNotPurge(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		mu.Unlock()
		switch {
		case r.Method == http.MethodPost:
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":{"message":"job is done"}}`))
		case r.URL.Query().Get("purge") == "true":
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte(`{"error":{"message":"only a cancelled or infected job's files can be deleted"}}`))
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer srv.Close()
	if err := New(Config{URL: srv.URL}).Cancel(context.Background(), "j1", true); err != nil {
		t.Fatal(err)
	}
	if last := calls[len(calls)-1]; last != "DELETE /api/jobs/j1" {
		t.Fatalf("calls = %s", strings.Join(calls, ", "))
	}
}
