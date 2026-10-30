package service

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"

	_ "golang.org/x/image/webp"
)

const maxImageBytes = 25 << 20

// handleUploadImage attaches an image to an existing task. The task must
// already exist; the standalone endpoint below is for images pasted while
// composing a task that has not been created yet.
func (s *Server) handleUploadImage(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.Task(r.PathValue("id")); err != nil {
		http.Error(w, "任务不存在", http.StatusNotFound)
		return
	}
	s.serveUploadImage(w, r)
}

// handleUploadStandaloneImage stores an image without binding it to a task.
// It exists so the quick-composer can paste and preview images before the
// task row is created; the resulting /api/images/{hash} URL is then embedded
// in the task body when it is created.
func (s *Server) handleUploadStandaloneImage(w http.ResponseWriter, r *http.Request) {
	s.serveUploadImage(w, r)
}

func (s *Server) serveUploadImage(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImageBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "图片读取失败或超过 25 MiB", http.StatusRequestEntityTooLarge)
		return
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (format != "png" && format != "jpeg" && format != "gif" && format != "webp") || cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 40_000_000 {
		http.Error(w, "图片无效或超过 4000 万像素", http.StatusBadRequest)
		return
	}
	if _, _, err := image.Decode(bytes.NewReader(data)); err != nil {
		http.Error(w, "图片内容损坏", http.StatusBadRequest)
		return
	}
	sum := sha256.Sum256(data)
	id := hex.EncodeToString(sum[:])
	dir := s.DBPath + ".images"
	if err := os.MkdirAll(dir, 0700); err != nil {
		http.Error(w, "无法创建图片目录", http.StatusInternalServerError)
		return
	}
	if err := writeImageFile(dir, id, data); err != nil {
		http.Error(w, "图片保存失败", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusCreated, struct {
		URL string `json:"url"`
	}{"/api/images/" + id})
}

// Publish complete files atomically so an existing preview never reads a
// partially written duplicate upload.
func writeImageFile(dir, id string, data []byte) error {
	file, err := os.CreateTemp(dir, ".upload-")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, err = file.Write(data)
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(file.Name(), filepath.Join(dir, id))
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("hash")
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != sha256.Size || hex.EncodeToString(decoded) != id {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	http.ServeFile(w, r, filepath.Join(s.DBPath+".images", id))
}
