package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

type imageBoard struct {
	*fakeBoard
	data []byte
	gate chan struct{}
	err  error
}

func (f *imageBoard) UploadImage(_ string, _ []byte) (string, error) {
	if f.gate != nil {
		<-f.gate
	}
	return "/api/images/" + strings.Repeat("a", 64), f.err
}
func (f *imageBoard) Image(_ string) ([]byte, error) { return f.data, nil }

func makeTestImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 320, 120))
	for y := 0; y < 120; y++ {
		for x := 0; x < 320; x++ {
			img.Set(x, y, color.RGBA{uint8(x % 255), 100, 180, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func imageFixture(t *testing.T) (*board, *ui.Tester, *imageBoard) {
	t.Helper()
	f := &imageBoard{fakeBoard: seedBoard(), data: makeTestImage(t)}
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "详情", func() bool { return !b.detail.loading })
	if err := tt.Click("编辑任务正文"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	return b, tt, f
}

func TestDetailImageReducesEmptyBodyEditorSpace(t *testing.T) {
	b, tt, _ := imageFixture(t)
	settle(t, tt)
	regular, ok := tt.Find("正文编辑器")
	if !ok {
		t.Fatal("body text area missing")
	}

	b.detail.body = "![粘贴的图片](/api/images/" + strings.Repeat("a", 64) + ")"
	settle(t, tt)
	withImage, ok := tt.Find("正文编辑器")
	if !ok {
		t.Fatal("body text area missing with image")
	}
	if tt.HasText("图片预览") {
		t.Fatal("source edit mode must not duplicate the image preview")
	}
	if regular.H-withImage.H < 80 {
		t.Fatalf("image body still leaves too much editor space: regular=%v image=%v", regular.H, withImage.H)
	}
}

func TestDetailBodyRendersImageWithoutMarkdownSource(t *testing.T) {
	f := &imageBoard{fakeBoard: seedBoard()}
	f.data = makeTestImage(t)
	imageMarkdown := "![粘贴的图片](/api/images/" + strings.Repeat("a", 64) + ")"
	f.tasks[0].Description = imageMarkdown
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "看板载入", func() bool { return b.task("t-1") != nil })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "评论载入", func() bool { return !b.detail.loading })
	settle(t, tt)
	if tt.HasText(imageMarkdown) {
		t.Fatal("read mode shows image Markdown source beside the rendered image")
	}
	if tt.HasText("图片预览") {
		t.Fatal("read mode should render the image in the body, without a duplicate preview section")
	}
	if _, ok := tt.Find("编辑任务正文"); !ok {
		t.Fatal("clickable body region missing")
	}
	if err := tt.Click("编辑任务正文"); err != nil {
		t.Fatal(err)
	}
	settle(t, tt)
	if !tt.HasText("正文编辑器") || tt.HasText("完成编辑正文") || tt.HasText("图片预览") {
		t.Fatal("edit mode must expose the source editor without a duplicate image preview")
	}
}

func TestDetailImagePaste(t *testing.T) {
	b, tt, f := imageFixture(t)
	d := b.detail
	d.body = "前文选中后文"
	f.gate = make(chan struct{})
	if !b.pasteDetailImage(d, &d.body, &d.bodyImage, 2, 4, func() []byte { return f.data }) {
		t.Fatal("image paste unhandled")
	}
	if !d.dirty() {
		t.Fatal("upload must hold SSE updates")
	}
	b.saveDetail()
	if b.saving || d.body != "前文选中后文" {
		t.Fatal("submitted or changed text during upload")
	}
	close(f.gate)
	waitFor(t, tt, "上传", func() bool { return !d.bodyImage.uploading })
	if !strings.HasPrefix(d.body, "前文\n\n![") || !strings.HasSuffix(d.body, "\n\n后文") || strings.Contains(d.body, "选中") {
		t.Fatal(d.body)
	}
	if d.bodyImage.caret != nil {
		t.Fatal("caret was not applied")
	}
	// A second insertion in a comment follows the same path and can send
	// even when it contains only an image.
	d.comment = ""
	b.pasteDetailImage(d, &d.comment, &d.commentImage, 0, 0, func() []byte { return f.data })
	waitFor(t, tt, "评论图片", func() bool { return !d.commentImage.uploading })
	b.sendComment()
	waitFor(t, tt, "发送评论图片", func() bool { return !b.commenting })
	f.mu.Lock()
	comments := f.tasks[0].Comments
	f.mu.Unlock()
	if !strings.Contains(comments[len(comments)-1].Text, "/api/images/") {
		t.Fatal(comments)
	}
	b.saveDetail()
	waitFor(t, tt, "保存", func() bool { return !b.saving })
	f.mu.Lock()
	body := f.tasks[0].Description
	f.mu.Unlock()
	if !strings.Contains(body, "/api/images/") {
		t.Fatal(body)
	}
}

func TestDetailImagePasteFailureAndText(t *testing.T) {
	b, tt, f := imageFixture(t)
	d := b.detail
	before := d.body
	if b.pasteDetailImage(d, &d.body, &d.bodyImage, 0, 2, func() []byte { return nil }) {
		t.Fatal("ordinary text intercepted")
	}
	f.err = errors.New("磁盘不可写")
	b.pasteDetailImage(d, &d.body, &d.bodyImage, 0, 2, func() []byte { return f.data })
	waitFor(t, tt, "失败", func() bool { return !d.bodyImage.uploading })
	if d.body != before {
		t.Fatal("failed upload lost selected text")
	}
	waitFor(t, tt, "错误提示", func() bool { return tt.HasText("图片上传失败") })
	// Closing a dialog during upload must never insert into the next task.
	f.err = nil
	f.gate = make(chan struct{})
	b.pasteDetailImage(d, &d.body, &d.bodyImage, 0, 0, func() []byte { return f.data })
	b.openDetail(*b.task("t-2"))
	next := b.detail
	close(f.gate)
	waitFor(t, tt, "旧上传完成", func() bool { return !d.bodyImage.uploading })
	if next.body != next.snap.Description {
		t.Fatal("old upload changed new task")
	}
}

func TestImagePasteNativeEditor(t *testing.T) {
	b, _, f := imageFixture(t)
	d := b.detail
	d.body = "中文前后"
	var readData []byte
	tt := ui.NewTester(func(c *ui.Context) {
		b.pump()
		input := ui.TextArea(c, &d.body).Label("编辑正文").FillWidth()
		b.imageInput(input, d, &d.body, &d.bodyImage, func() []byte { return readData })
	}, 600, 250)
	if err := tt.Click("编辑正文"); err != nil {
		t.Fatal(err)
	}
	tt.Key(ui.Cmd, ui.KeyHome)
	tt.SetClipboard("文字")
	tt.Key(ui.Cmd, ui.KeyV)
	if d.body != "文字中文前后" {
		t.Fatalf("plain paste: %q", d.body)
	}
	readData = f.data
	tt.SetClipboard("")
	tt.Key(ui.Cmd, ui.KeyV)
	waitFor(t, tt, "原生粘贴图片", func() bool { return !d.bodyImage.uploading })
	tt.SetClipboard("紧接图片")
	readData = nil
	tt.Key(ui.Cmd, ui.KeyV)
	if !strings.Contains(d.body, ")\n\n紧接图片中文前后") {
		t.Fatalf("caret after upload: %q", d.body)
	}
	// Optional visual evidence, without rewriting the project's render assets.
	if path := os.Getenv("TASKBOARD_IMAGE_REVIEW"); path != "" {
		view := newBoardTester(b)
		for i := 0; i < 20; i++ {
			time.Sleep(5 * time.Millisecond)
			view.Frame()
		}
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if err := png.Encode(file, view.Image()); err != nil {
			t.Fatal(err)
		}
	}
}

func TestImageSummary(t *testing.T) {
	text := "![粘贴的图片](/api/images/" + strings.Repeat("a", 64) + ")\n\n说明"
	if got := firstLines(text, 2); got != "[图片] 说明" {
		t.Fatal(got)
	}
}

func TestRemoveDetailImageAndSave(t *testing.T) {
	b, tt, f := imageFixture(t)
	image := "![粘贴的图片](/api/images/" + strings.Repeat("a", 64) + ")"
	d := b.detail
	d.body = "前文\n\n" + image + "\n\n中间文字\n\n" + image + "\n\n后文"
	tt.Frame()
	if err := tt.Click("移除正文图片 1"); err != nil {
		t.Fatal(err)
	}
	if count := strings.Count(d.body, image); count != 1 {
		t.Fatalf("removed %d occurrences instead of one: %q", 2-count, d.body)
	}
	if !strings.Contains(d.body, "中间文字\n\n"+image) || !strings.Contains(d.body, "前文") || !strings.HasSuffix(d.body, "后文") {
		t.Fatalf("removed the wrong occurrence or surrounding text: %q", d.body)
	}
	b.saveDetail()
	waitFor(t, tt, "保存移除后的正文", func() bool { return !b.saving })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "重新打开详情", func() bool { return !b.detail.loading })
	if count := strings.Count(b.detail.body, image); count != 1 {
		t.Fatalf("saved removal did not persist: %q", b.detail.body)
	}
	if err := tt.Click("编辑任务正文"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("移除正文图片 1"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if strings.Contains(b.detail.body, "/api/images/") || tt.HasText("图片预览") {
		t.Fatal("last removed image still appears")
	}
	b.saveDetail()
	waitFor(t, tt, "保存最后一张图片的移除", func() bool { return !b.saving })
	saved, err := f.Task("t-1")
	if err != nil || strings.Contains(saved.Description, "/api/images/") {
		t.Fatalf("saved: %+v, %v", saved, err)
	}
}

func TestRemoveCommentDraftImage(t *testing.T) {
	b, tt, f := imageFixture(t)
	d := b.detail
	d.comment = "保留这条评论\n\n![粘贴的图片](/api/images/" + strings.Repeat("a", 64) + ")"
	tt.Frame()
	if err := tt.Click("移除评论图片 1"); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(d.comment) != "保留这条评论" {
		t.Fatalf("removed comment text: %q", d.comment)
	}
	if err := tt.Click("写评论"); err != nil {
		t.Fatal(err)
	}
	tt.Key(0, ui.KeyEnter)
	waitFor(t, tt, "发送移除图片后的评论", func() bool { return !b.commenting })
	comments, err := f.Comments(d.id)
	if err != nil || comments[len(comments)-1].Text != "保留这条评论" {
		t.Fatalf("comments: %+v, %v", comments, err)
	}
}
