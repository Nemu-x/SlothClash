package main

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
)

func TestCloseConnectionsDeletesOnlyGivenIDs(t *testing.T) {
	var mu sync.Mutex
	var deleted []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || !strings.HasPrefix(r.URL.Path, "/connections/") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		id := strings.TrimPrefix(r.URL.Path, "/connections/")
		if id == "broken" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		mu.Lock()
		deleted = append(deleted, id)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	a := &App{core: coreProcess{listen: strings.TrimPrefix(server.URL, "http://")}}

	n, err := a.CloseConnections([]string{" a ", "", "b", "a", "broken"})
	if n != 2 {
		t.Fatalf("closed %d, want 2", n)
	}
	if err == nil || !strings.Contains(err.Error(), "close broken") {
		t.Fatalf("want error naming the failed id, got %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(deleted, []string{"a", "b"}) {
		t.Fatalf("deleted %v, want [a b] (trimmed, deduped, no blanks)", deleted)
	}
}

func TestCloseConnectionsNoIDsIsNoop(t *testing.T) {
	a := &App{} // no controller: must not even try to reach it
	if n, err := a.CloseConnections([]string{"", "  "}); n != 0 || err != nil {
		t.Fatalf("got %d %v, want 0 nil", n, err)
	}
}

func TestCloseConnectionsRejectsOversizedBatch(t *testing.T) {
	ids := make([]string, maxCloseConnectionIDs+1)
	for i := range ids {
		ids[i] = strings.Repeat("x", i+1)
	}
	a := &App{core: coreProcess{listen: "127.0.0.1:1"}}
	if _, err := a.CloseConnections(ids); err == nil {
		t.Fatal("oversized batch accepted")
	}
}
