package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

func (c *Client) imageRequest(method, path string, data []byte) ([]byte, error) {
	req, err := http.NewRequest(method, c.Base+path, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Agent", c.currentAgent())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/octet-stream")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, (25<<20)+1))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("图片请求失败 (%d)：%s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if len(raw) > 25<<20 {
		return nil, fmt.Errorf("图片超过 25 MiB")
	}
	return raw, nil
}

func (c *Client) UploadImage(taskID string, data []byte) (string, error) {
	raw, err := c.imageRequest(http.MethodPost, "/api/tasks/"+url.PathEscape(taskID)+"/images", data)
	if err != nil {
		return "", err
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if !validImagePath(result.URL) {
		return "", fmt.Errorf("图片地址无效")
	}
	return result.URL, nil
}

func validImagePath(path string) bool {
	const prefix = "/api/images/"
	if !strings.HasPrefix(path, prefix) || len(path) != len(prefix)+64 {
		return false
	}
	for _, ch := range path[len(prefix):] {
		if !(ch >= '0' && ch <= '9' || ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

// UploadImageData stores an image without binding it to a task. It powers
// the quick-composer, where the image is pasted before the task exists; the
// returned URL is later embedded in the task body.
func (c *Client) UploadImageData(data []byte) (string, error) {
	raw, err := c.imageRequest(http.MethodPost, "/api/images", data)
	if err != nil {
		return "", err
	}
	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return "", err
	}
	if !validImagePath(result.URL) {
		return "", fmt.Errorf("图片地址无效")
	}
	return result.URL, nil
}

// Image only reads this service's image URLs, never arbitrary remote or local files.
func (c *Client) Image(path string) ([]byte, error) {
	if !validImagePath(path) {
		return nil, fmt.Errorf("图片地址无效")
	}
	return c.imageRequest(http.MethodGet, path, nil)
}
