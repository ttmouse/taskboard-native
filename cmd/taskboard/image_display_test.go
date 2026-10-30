package main

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// pngWithDensity encodes a PNG and inserts a pHYs chunk, which Go's encoder
// never writes: unit 1 is metres, so 5669 pixels per metre is 144 dpi.
func pngWithDensity(t *testing.T, w, h, perMetre int, unit byte) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{uint8(x % 255), 100, 180, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	raw := buf.Bytes()
	chunk := make([]byte, 21)
	binary.BigEndian.PutUint32(chunk, 9)
	copy(chunk[4:8], "pHYs")
	binary.BigEndian.PutUint32(chunk[8:], uint32(perMetre))
	binary.BigEndian.PutUint32(chunk[12:], uint32(perMetre))
	chunk[16] = unit
	binary.BigEndian.PutUint32(chunk[17:], crc32.ChecksumIEEE(chunk[4:17]))
	// Right after IHDR: 8 signature bytes + 25 IHDR bytes.
	at := 8 + 25
	out := make([]byte, 0, len(raw)+len(chunk))
	out = append(out, raw[:at]...)
	out = append(out, chunk...)
	return append(out, raw[at:]...)
}

func TestDisplayDensityUsesDeclaredDensity(t *testing.T) {
	retina := pngWithDensity(t, 64, 32, 5669, 1) // 144 dpi
	if got := displayDensity(retina, 64, 32, 1); got != 2 {
		t.Fatalf("144 dpi density = %v, want 2", got)
	}
	plain := pngWithDensity(t, 800, 400, 2835, 1) // 72 dpi
	if got := displayDensity(plain, 800, 400, 2); got != 1 {
		t.Fatalf("declared 72 dpi density = %v, want 1 even on a Retina screen", got)
	}
	aspectOnly := pngWithDensity(t, 800, 400, 5669, 0)
	if got := displayDensity(aspectOnly, 800, 400, 2); got != 2 {
		t.Fatalf("aspect-only pHYs density = %v, want the screen scale", got)
	}
}

func TestDisplayDensityGuessesScreenScaleOnlyForScreenshots(t *testing.T) {
	plain := pngWithDensity(t, 800, 400, 0, 0)
	if got := displayDensity(plain, 800, 400, 2); got != 2 {
		t.Fatalf("density without metadata = %v, want 2 on a Retina screen", got)
	}
	if got := displayDensity(plain, 800, 400, 1); got != 1 {
		t.Fatalf("density without metadata = %v, want 1 on a 1× screen", got)
	}
	small := pngWithDensity(t, 64, 64, 0, 0)
	if got := displayDensity(small, 64, 64, 2); got != 1 {
		t.Fatalf("small unlabelled density = %v, want 1", got)
	}
	if got := displayDensity(plain, 800, 400, 1.75); got != 2 {
		t.Fatalf("density = %v, want the ratio rounded to whole pixels per DIP", got)
	}
}

func TestDisplayImageKeepsRetinaSourcePixels(t *testing.T) {
	data := pngWithDensity(t, 640, 240, 5669, 1)
	src, err := (&board{}).displayImage(data)
	if err != nil {
		t.Fatal(err)
	}
	bmp, ok := src.(*ui.Bitmap)
	if !ok {
		t.Fatalf("displayImage returned %T, want a bitmap", src)
	}
	if w, h := bmp.Size(); w != 640 || h != 240 {
		t.Fatalf("Retina source decoded at %dx%d, want full-resolution 640x240", w, h)
	}
}

func TestDisplayImageKeepsPlainImageAtPixelSize(t *testing.T) {
	data := makeTestImage(t) // 320×120, no density metadata
	src, err := (&board{}).displayImage(data)
	if err != nil {
		t.Fatal(err)
	}
	bmp, ok := src.(*ui.Bitmap)
	if !ok {
		t.Fatalf("displayImage returned %T, want a bitmap", src)
	}
	if w, h := bmp.Size(); w != 320 || h != 120 {
		t.Fatalf("plain image shown at %dx%d, want its own 320x120", w, h)
	}
}

func TestDisplayDensityUsesScreenScaleOnceRecorded(t *testing.T) {
	previous := screenScaleBits.Load()
	screenScaleBits.Store(math.Float64bits(2))
	t.Cleanup(func() { screenScaleBits.Store(previous) })

	data := pngWithDensity(t, 800, 400, 0, 0) // a clipboard screenshot: no metadata
	src, err := (&board{}).displayImage(data)
	if err != nil {
		t.Fatal(err)
	}
	bmp, ok := src.(*ui.Bitmap)
	if !ok {
		t.Fatalf("displayImage returned %T, want a bitmap", src)
	}
	if w, h := bmp.Size(); w != 800 || h != 400 {
		t.Fatalf("paste without metadata decoded at %dx%d, want full-resolution 800x400", w, h)
	}
}

// A body image is loaded through the detail's markdown options, so this is
// the wiring that decides what the card actually shows.
func TestDetailMarkdownLoadsRetinaImageAtLogicalSize(t *testing.T) {
	f := &imageBoard{fakeBoard: seedBoard(), data: pngWithDensity(t, 640, 240, 5669, 1)}
	b := newBoard(f)
	tt := newBoardTester(b)
	waitFor(t, tt, "载入", func() bool { return len(b.tasks) > 0 })
	b.openDetail(*b.task("t-1"))
	waitFor(t, tt, "详情", func() bool { return !b.detail.loading })
	b.detail.body = "![粘贴的图片](/api/images/" + strings.Repeat("a", 64) + ")"
	settle(t, tt)

	if b.detail.markdown == nil || b.detail.markdown.LoadImage == nil {
		t.Fatal("detail body did not build an image loader")
	}
	src, err := b.detail.markdown.LoadImage("/api/images/" + strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	bmp, ok := src.(*ui.Bitmap)
	if !ok {
		t.Fatalf("loader returned %T, want a bitmap", src)
	}
	if w, h := bmp.Size(); w != 640 || h != 240 {
		t.Fatalf("body bitmap loaded at %dx%d, want full-resolution 640x240", w, h)
	}
	waitFor(t, tt, "Retina 图片按逻辑尺寸显示", func() bool {
		box, ok := tt.Find("粘贴的图片")
		return ok && box.W == 320 && box.H == 120
	})
}
