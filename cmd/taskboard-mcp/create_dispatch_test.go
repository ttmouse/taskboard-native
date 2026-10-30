package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The MCP create_task tool carries the same two modes as the HTTP API:
// without dispatchRuntime it only creates a card, with it the card is
// created and handed to that channel in the same call.
func TestMCPCreateTaskDispatchModes(t *testing.T) {
	var gotBodies []map[string]any
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/projects":
			_, _ = w.Write([]byte(`[{"id":"project-1","workspacePath":"/tmp/project"}]`))
		case r.Method == http.MethodPost && r.URL.Path == "/api/tasks":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decode create_task body: %v", err)
			}
			gotBodies = append(gotBodies, body)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"task-1","title":"A","version":1}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	})
	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, req)
		return recorder.Result(), nil
	})}
	s := &server{base: "http://board.test", http: httpClient}

	// 仅建卡: no channel on the wire.
	if _, err := s.callTool("create_task", json.RawMessage(`{"projectId":"project-1","title":"仅建卡"}`)); err != nil {
		t.Fatalf("create only: %v", err)
	}
	if len(gotBodies) != 1 {
		t.Fatalf("requests = %d, want 1", len(gotBodies))
	}
	if _, ok := gotBodies[0]["dispatchRuntime"]; ok {
		t.Fatalf("create-only body carries a channel: %v", gotBodies[0])
	}

	// 建卡并派发: the channel rides along in the same request.
	if _, err := s.callTool("create_task", json.RawMessage(`{"projectId":"project-1","title":"建卡并派发","dispatchRuntime":"codex"}`)); err != nil {
		t.Fatalf("create and dispatch: %v", err)
	}
	if len(gotBodies) != 2 {
		t.Fatalf("requests = %d, want 2", len(gotBodies))
	}
	if got := gotBodies[1]["dispatchRuntime"]; got != "codex" {
		t.Fatalf("dispatchRuntime = %v, want codex", got)
	}

	// The schema advertises the field, or a client cannot discover it.
	byName := map[string]map[string]any{}
	for _, definition := range toolDefinitions() {
		byName[definition["name"].(string)] = definition
	}
	properties := byName["create_task"]["inputSchema"].(map[string]any)["properties"].(map[string]any)
	if _, ok := properties["dispatchRuntime"]; !ok {
		t.Error("create_task schema missing dispatchRuntime")
	}
}
