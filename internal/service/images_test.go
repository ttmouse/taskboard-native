package service

import (
	"bytes"
	"image"
	"image/png"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"taskboard-native/internal/client"
	"taskboard-native/internal/model"
)

func TestImagePersistence(t *testing.T) {
	db := filepath.Join(t.TempDir(), "tb.db")
	srv, err := Start("127.0.0.1:0", db)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	c := client.New(&client.Discover{Addr: srv.Addr}, "image-test")
	// A newly opened service starts projectless; create the board this
	// persistence scenario restarts against.
	proj, cerr := c.CreateProject("local")
	if cerr != nil {
		t.Fatal(cerr)
	}
	task, err := c.CreateTask(proj.ID, "", "图片测试", "", "")
	if err != nil {
		t.Fatal(err)
	}
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 12, 8))); err != nil {
		t.Fatal(err)
	}
	path, err := c.UploadImage(task.ID, data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.UploadImage(task.ID, data.Bytes())
	if err != nil || again != path {
		t.Fatalf("dedup: %q, %v", again, err)
	}
	body := "截图\n\n![图片](" + path + ")"
	if _, err := c.Update(task.ID, task.Version, nil, &body, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddComment(task.ID, body); err != nil {
		t.Fatal(err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	srv, err = Start("127.0.0.1:0", db)
	if err != nil {
		t.Fatal(err)
	}
	c = client.New(&client.Discover{Addr: srv.Addr}, "image-test")
	got, err := c.Image(path)
	if err != nil || !bytes.Equal(got, data.Bytes()) {
		t.Fatalf("restart image: %v", err)
	}
	saved, err := c.Task(task.ID)
	if err != nil || saved.Description != body || len(saved.Comments) != 1 || saved.Comments[0].Text != body {
		t.Fatalf("saved: %+v %v", saved, err)
	}
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "image/png" {
		t.Fatal(resp.Header)
	}
}

func TestImageValidation(t *testing.T) {
	srv := start(t)
	fc := client.New(&client.Discover{Addr: srv.Addr}, "image-test")
	projects, err := fc.Projects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("projects: %v", err)
	}
	_, raw := request(t, srv, "GET", "/api/projects/"+projects[0].ID+"/board", "", nil)
	task := decode[Board](t, raw).Tasks[0]
	for _, tc := range []struct {
		name, id string
		data     []byte
		status   int
	}{
		{"invalid", task.ID, []byte("not an image"), 400},
		{"oversized", task.ID, bytes.Repeat([]byte("x"), maxImageBytes+1), 413},
		{"missing task", "missing", []byte("x"), 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Post(srv.URL+"/api/tasks/"+tc.id+"/images", "application/octet-stream", bytes.NewReader(tc.data))
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != tc.status {
				body, _ := io.ReadAll(resp.Body)
				t.Fatalf("%d: %s", resp.StatusCode, body)
			}
		})
	}
	status, _ := request(t, srv, "GET", "/api/images/not-a-hash", "", nil)
	if status != 404 {
		t.Fatal(status)
	}
	c := client.New(&client.Discover{Addr: srv.Addr}, "test")
	for _, path := range []string{"file:///etc/passwd", "https://example.com/img.png", "/api/images/" + strings.Repeat("../", 22)} {
		if _, err := c.Image(path); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	_, raw = request(t, srv, "GET", "/api/tasks/"+task.ID, "", nil)
	if got := decode[model.Task](t, raw); got.Version != task.Version {
		t.Fatal("upload changed version")
	}
}

// TestStandaloneImageUpload exercises the unbound upload endpoint the
// quick-composer uses: no task exists yet, but the image must still be
// stored and readable, and embeddable in a task body created afterward.
func TestStandaloneImageUpload(t *testing.T) {
	srv := start(t)
	c := client.New(&client.Discover{Addr: srv.Addr}, "standalone-test")
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 16, 9))); err != nil {
		t.Fatal(err)
	}
	path, err := c.UploadImageData(data.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	again, err := c.UploadImageData(data.Bytes())
	if err != nil || again != path {
		t.Fatalf("dedup: %q, %v", again, err)
	}
	got, err := c.Image(path)
	if err != nil || !bytes.Equal(got, data.Bytes()) {
		t.Fatalf("readback: %v", err)
	}
	// A bad image must be rejected the same way as the bound endpoint.
	if status, _ := request(t, srv, "POST", "/api/images", "application/octet-stream", []byte("nope")); status != 400 {
		t.Fatalf("non-image status = %d", status)
	}
	// The URL can be embedded in a task created afterwards.
	body := "先贴图后建任务\n\n![粘贴的图片](" + path + ")"
	projects, err := c.Projects()
	if err != nil || len(projects) == 0 {
		t.Fatalf("projects: %v", err)
	}
	if _, err := c.CreateTask(projects[0].ID, "", "暂存贴图", body, ""); err != nil {
		t.Fatal(err)
	}
}
