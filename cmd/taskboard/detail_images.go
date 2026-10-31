package main

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
	"github.com/egoist/mygo/ui/markdown/parser"
)

type imageAPI interface {
	UploadImage(taskID string, data []byte) (string, error)
	Image(path string) ([]byte, error)
}

var localImageMarkdown = regexp.MustCompile(`!\[[^\]]*\]\(/api/images/[a-f0-9]{64}\)`)

type markdownImageLoad struct {
	mu      sync.Mutex
	image   *ui.Bitmap
	density float64
	err     error
	done    bool
}

func detailBodyMinHeight(body string, t *ui.Theme) float32 {
	if localImageMarkdown.MatchString(body) {
		// The image is previewed below the editor. Keeping the regular
		// description height here leaves a large empty block above that preview.
		return t.Space(4)
	}
	return t.Space(30)
}

type imageEdit struct {
	uploading bool
	pending   []byte
	caret     *int
}

func (edit *imageEdit) busy() bool { return edit.uploading || len(edit.pending) != 0 }

// pasteDetailImage runs only for a paste into the body or comment input.
// Plain text returns to the native editor unchanged.
func (b *board) pasteDetailImage(d *detail, value *string, edit *imageEdit, start, end int, read func() []byte) bool {
	if edit.uploading {
		return true
	}
	data := read()
	if len(data) == 0 {
		return false
	}
	if len(data) > 25<<20 {
		b.toast("无法粘贴图片", "图片超过 25 MiB，请缩小后重试。")
		return true
	}
	a, ok := b.api.(imageAPI)
	if !ok {
		b.toast("无法粘贴图片", "当前服务不支持图片上传，请更新看板服务。")
		return true
	}
	before := *value
	edit.uploading = true
	go func() {
		path, err := a.UploadImage(d.id, data)
		b.post(func() {
			edit.uploading = false
			if b.detail != d {
				return
			}
			if err != nil {
				b.toast("图片上传失败", err.Error()+"；原文已保留，请重新粘贴。")
				return
			}
			// A pushed update cannot replace a field while uploading. If some
			// other action changed it, preserve that newer text as well.
			if *value != before {
				b.toast("图片未插入", "正文已变化，请在新位置重新粘贴。")
				return
			}
			*value, start = insertImageMarkdown(before, start, end, path)
			edit.caret = &start
		})
	}()
	return true
}

func insertImageMarkdown(text string, start, end int, path string) (string, int) {
	runes := []rune(text)
	start = min(max(start, 0), len(runes))
	end = min(max(end, start), len(runes))
	insert := "\n\n![粘贴的图片](" + path + ")\n\n"
	return string(runes[:start]) + insert + string(runes[end:]), start + len([]rune(insert))
}

func (b *board) imageInput(input ui.Element, d *detail, value *string, edit *imageEdit, read func() []byte) {
	// Element handles only live during view construction. OnPaste can run
	// between frames: capture bytes there, inspect the editor next frame.
	input.OnPaste(func(_ string) bool {
		if edit.busy() {
			return true
		}
		data := read()
		if len(data) == 0 {
			return false
		}
		edit.pending = data
		return true
	})
	if len(edit.pending) != 0 {
		data := edit.pending
		edit.pending = nil
		start, end := input.TextSelection()
		b.pasteDetailImage(d, value, edit, start, end, func() []byte { return data })
	}
	input.ReadOnly(edit.uploading)
	if edit.caret != nil {
		input.SetTextSelection(*edit.caret, *edit.caret)
		edit.caret = nil
	}
}

func (b *board) detailMarkdown(c *ui.Context, text string) {
	d := b.detail
	if d.markdown == nil {
		d.markdown = &markdown.Options{
			LoadImage: func(path string) (ui.ImageSource, error) {
				a, ok := b.api.(imageAPI)
				if !ok {
					return nil, fmt.Errorf("图片服务不可用")
				}
				data, err := a.Image(path)
				if err != nil {
					return nil, err
				}
				return b.displayImage(data)
			},
			Block: func(c *ui.Context, doc *parser.Document, index int) bool {
				return b.retinaImageBlock(c, d, doc, index)
			},
			OnLink: func(target string) {
				if external, ok := markdownExternalTarget(target); ok {
					go mygo.Shell.OpenExternal(external)
				}
			},
		}
	}
	workspace := ""
	if p, ok := b.projectByID(d.snap.ProjectID); ok {
		workspace = p.WorkspacePath
	}
	markdown.View(c, linkifyAbsolutePaths(expandWorkspaceMentions(text, workspace)), d.markdown)
}

// retinaImageBlock gives standalone task images half their pixel dimensions
// as layout size while keeping the full-resolution bitmap for Retina drawing.
// Resampling the bitmap first leaves only one source pixel per two screen
// pixels and makes screenshots, especially their text, visibly soft.
func (b *board) retinaImageBlock(c *ui.Context, d *detail, doc *parser.Document, index int) bool {
	block := doc.Block(index)
	if block.Kind != parser.Paragraph {
		return false
	}
	inlines := doc.Inlines(index)
	if len(inlines) == 0 || inlines[0].Kind != parser.Image || inlines[0].Next != len(inlines) {
		return false
	}
	inline := inlines[0]
	if !strings.HasPrefix(inline.Dest, "/api/images/") {
		return false
	}
	alt := parser.PlainText(inlines[1:inline.Next])
	value, loaded := d.markdownImages.LoadOrStore(inline.Dest, &markdownImageLoad{})
	entry := value.(*markdownImageLoad)
	if !loaded {
		services := c.Services()
		go func() {
			a, ok := b.api.(imageAPI)
			var bmp *ui.Bitmap
			var density float64
			var err error
			if !ok {
				err = fmt.Errorf("图片服务不可用")
			} else {
				var data []byte
				data, err = a.Image(inline.Dest)
				if err == nil {
					var src ui.ImageSource
					src, err = b.displayImage(data)
					if err == nil {
						bmp, _ = src.(*ui.Bitmap)
						if bmp == nil {
							err = fmt.Errorf("不支持的图片格式")
						} else {
							w, h := bmp.Size()
							density = displayDensity(data, w, h, displayScale())
						}
					}
				}
			}
			entry.mu.Lock()
			entry.image, entry.density, entry.err, entry.done = bmp, density, err, true
			entry.mu.Unlock()
			services.Invalidate()
		}()
	}
	entry.mu.Lock()
	bmp, density, err, done := entry.image, entry.density, entry.err, entry.done
	entry.mu.Unlock()
	if !done || err != nil || bmp == nil || density <= 0 {
		if alt == "" {
			alt = "图片"
		}
		ui.Text(c, alt).TextColor(c.Theme().TextMuted)
		return true
	}
	w, h := bmp.Size()
	img := ui.Image(c, bmp).Size(float32(w)/float32(density), float32(h)/float32(density)).
		AspectRatio(float32(w) / float32(h)).MaxWidthPercent(100).Label(alt)
	img.Radius(c.Theme().Radius)
	return true
}

// markdownExternalTarget permits ordinary external links and local file links.
// Absolute paths are converted to file URLs so macOS opens them with their
// registered default application instead of treating them as app routes.
func markdownExternalTarget(target string) (string, bool) {
	if filepath.IsAbs(target) {
		return (&url.URL{Scheme: "file", Path: filepath.Clean(target)}).String(), true
	}
	parsed, err := url.Parse(target)
	if err != nil {
		return "", false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "file", "http", "https", "mailto":
		return parsed.String(), true
	default:
		return "", false
	}
}

// removeImageReference removes one occurrence, so pasting the same image
// twice does not make removing either preview erase both references.
func removeImageReference(value *string, edit *imageEdit, index int) bool {
	if edit.busy() {
		return false
	}
	images := localImageMarkdown.FindAllStringIndex(*value, -1)
	if index < 0 || index >= len(images) {
		return false
	}
	span := images[index]
	before := (*value)[:span[0]]
	*value = before + (*value)[span[1]:]
	caret := len([]rune(before))
	edit.caret = &caret
	return true
}

func (b *board) imagePreview(c *ui.Context, value *string, edit *imageEdit, field string) {
	if edit.uploading {
		ui.Text(c, "正在上传图片…").TextColor(c.Theme().TextMuted)
	}
	if images := localImageMarkdown.FindAllString(*value, -1); len(images) > 0 {
		ui.Text(c, "图片预览").FontSize(c.Theme().FontSize - 2).TextColor(c.Theme().TextMuted)
		for i, image := range images {
			removed := false
			ui.Box(c.Key(fmt.Sprintf("%s-image-%d", field, i))).AlignSelf(ui.Start).MaxWidthPercent(100).Children(func() {
				b.detailMarkdown(c, image)
				if ui.Button(c, "移除").Label(fmt.Sprintf("移除%s图片 %d", field, i+1)).
					Absolute().Attach(ui.AnchorTopRight, ui.AnchorTopRight).
					Top(c.Theme().Space(1)).Right(c.Theme().Space(1)).
					TextColor(c.Theme().Danger).Background(c.Theme().Surface).
					Disabled(edit.busy() || b.saving || b.commenting).Clicked() {
					removed = removeImageReference(value, edit, i)
				}
			})
			if removed {
				return
			}
		}
	}
}

// imageRemoveControls keeps body attachments removable while the body is in
// source-edit mode, where the rendered preview is deliberately hidden.
func (b *board) imageRemoveControls(c *ui.Context, value *string, edit *imageEdit, field string) {
	if edit.uploading {
		ui.Text(c, "正在上传图片…").TextColor(c.Theme().TextMuted)
	}
	images := localImageMarkdown.FindAllStringIndex(*value, -1)
	for i := range images {
		removed := false
		ui.Row(c).Key(fmt.Sprintf("%s-image-source-%d", field, i)).FillWidth().AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf("图片 %d", i+1)).FontSize(c.Theme().FontSize - 2).TextColor(c.Theme().TextMuted)
			ui.Spacer(c)
			if ui.Button(c, "移除").Label(fmt.Sprintf("移除%s图片 %d", field, i+1)).
				TextColor(c.Theme().Danger).
				Disabled(edit.busy() || b.saving || b.commenting).Clicked() {
				removed = removeImageReference(value, edit, i)
			}
		})
		if removed {
			return
		}
	}
}
