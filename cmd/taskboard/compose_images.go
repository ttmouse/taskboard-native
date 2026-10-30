package main

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// standaloneImageUploader uploads an image without binding it to a task.
// The real client exposes it; the quick-composer uses it because the task
// it is composing does not exist yet.
type standaloneImageUploader interface {
	UploadImageData(data []byte) (string, error)
}

// composePaste wires paste capture onto the quick-composer title input.
// A plain-text paste falls through to the native editor; an image on the
// clipboard is uploaded right away (no task row exists yet) and collected
// into f.composeImages, which becomes the task body on submit.
func (b *board) composePaste(input ui.Element, f *form) {
	input.OnPaste(func(_ string) bool {
		if f.composeImage.busy() {
			return true
		}
		data := mygo.Clipboard.ReadImage()
		if len(data) == 0 {
			return false
		}
		f.composeImage.pending = data
		return true
	})
	if len(f.composeImage.pending) != 0 {
		data := f.composeImage.pending
		f.composeImage.pending = nil
		b.uploadComposeImage(f, data)
	}
}

func (b *board) uploadComposeImage(f *form, data []byte) {
	if len(data) > 25<<20 {
		b.toast("无法粘贴图片", "图片超过 25 MiB，请缩小后重试。")
		return
	}
	up, ok := b.api.(standaloneImageUploader)
	if !ok {
		b.toast("无法粘贴图片", "当前服务不支持图片上传，请更新看板服务。")
		return
	}
	f.composeImage.uploading = true
	go func() {
		path, err := up.UploadImageData(data)
		b.post(func() {
			f.composeImage.uploading = false
			if b.form != f {
				return
			}
			if err != nil {
				b.toast("图片上传失败", err.Error())
				return
			}
			f.composeImages = append(f.composeImages, path)
		})
	}()
}

// composeThumbStrip renders a compact row of small image thumbnails above
// the input pill. Each thumbnail is a fixed-size rounded tile with a tiny
// remove affordance; images load lazily and are cached on the form.
func (b *board) composeThumbStrip(c *ui.Context, t *ui.Theme, f *form) {
	ui.Row(c).Gap(t.Space(1.5)).AlignItems(ui.Start).
		AlignSelf(ui.Start).
		Children(func() {
			for i, path := range f.composeImages {
				b.composeThumb(c, t, f, path, i)
			}
			if f.composeImage.uploading {
				ui.Text(c, "上传图片…").FontSize(t.FontSize - 2).TextColor(t.TextMuted).
					Padding(t.Space(1), 0)
			}
		})
}

func (b *board) composeThumb(c *ui.Context, t *ui.Theme, f *form, path string, i int) {
	h := t.Space(11)
	src, ok := f.thumbs[path]
	if !ok {
		if !f.thumbLoading[path] {
			if f.thumbLoading == nil {
				f.thumbLoading = map[string]bool{}
			}
			f.thumbLoading[path] = true
			go func() {
				a, has := b.api.(imageAPI)
				if !has {
					return
				}
				data, err := a.Image(path)
				if err != nil {
					return
				}
				bmp, err := ui.DecodeBitmap(data)
				if err != nil {
					return
				}
				b.post(func() {
					if f.thumbs == nil {
						f.thumbs = map[string]ui.ImageSource{}
					}
					f.thumbs[path] = bmp
				})
			}()
		}
		ui.Box(c.Key(fmt.Sprintf("thumb-ph-%d", i))).Size(h, h).Radius(t.Radius).
			Background(t.TextMuted.Alpha(0.15))
		return
	}
	removed := false
	ui.Box(c.Key(fmt.Sprintf("thumb-%d", i))).Size(h, h).Radius(t.Radius).
		Clip().Background(t.TextMuted.Alpha(0.1)).Children(func() {
		ui.Image(c, src).AspectRatio(1).Fit(ui.Cover)
		if ui.Button(c, "×").Label(fmt.Sprintf("移除待创建图片 %d", i+1)).
			Absolute().Attach(ui.AnchorTopRight, ui.AnchorTopRight).
			Top(t.Space(0.5)).Right(t.Space(0.5)).
			Size(t.Space(3), t.Space(3)).Padding(0).
			BorderWidth(0).Radius(t.Space(1.5)).
			TextColor(ui.RGB(255, 255, 255)).
			Background(ui.RGBA(0, 0, 0, 0.55)).
			Disabled(f.composeImage.busy() || f.creating).Clicked() {
			f.composeImages = append(f.composeImages[:i], f.composeImages[i+1:]...)
			delete(f.thumbs, path)
			delete(f.thumbLoading, path)
			removed = true
		}
	})
	if removed {
		return
	}
}

// buildComposeDescription assembles the task body from pasted images. It
// returns "" when the user only typed a title, keeping the existing empty
// body behaviour unchanged.
func buildComposeDescription(images []string) string {
	if len(images) == 0 {
		return ""
	}
	parts := make([]string, len(images))
	for i, p := range images {
		parts[i] = "![粘贴的图片](" + p + ")"
	}
	return strings.Join(parts, "\n\n")
}
