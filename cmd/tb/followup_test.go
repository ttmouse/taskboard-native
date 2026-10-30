package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFollowupCLIUsesSavedComment(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/tasks/task-1/comments/comment-1/continue" || r.Header.Get("X-Agent") != "reviewer" {
			t.Errorf("bad followup: %s %s agent=%s", r.Method, r.URL, r.Header.Get("X-Agent"))
		}
		called = true
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if code := cmdFollowup([]string{"task-1", "comment-1", "--addr", strings.TrimPrefix(srv.URL, "http://"), "--agent", "reviewer"}); code != exitOK || !called {
		t.Fatalf("code=%d called=%v", code, called)
	}
}
