package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// tb comment --mention sends the address as a field, so the dispatcher can
// route the comment to that channel instead of parsing the body text.
func TestCommentCLISendsMention(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/api/tasks/task-1/comments" {
			t.Errorf("bad call: %s %s", r.Method, r.URL)
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("body %s: %v", raw, err)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"m-1","author":"human","text":"请复核"}`))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if code := cmdComment([]string{"task-1", "请复核", "--mention", "claude", "--addr", addr, "--agent", "human"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	if got["text"] != "请复核" || got["mention"] != "claude" {
		t.Fatalf("body = %v", got)
	}
}

// Without --mention the request keeps its old shape.
func TestCommentCLIWithoutMentionOmitsField(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/health" {
			w.Write([]byte(`{"ok":true}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Errorf("body %s: %v", raw, err)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"m-1","author":"human","text":"普通评论"}`))
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if code := cmdComment([]string{"task-1", "普通评论", "--addr", addr, "--agent", "human"}); code != exitOK {
		t.Fatalf("code = %d", code)
	}
	if _, ok := got["mention"]; ok {
		t.Fatalf("body = %v", got)
	}
}
