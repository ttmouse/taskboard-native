package main

import (
	"bytes"
	"encoding/binary"
	"math"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// A screenshot taken on a Retina display holds two device pixels per DIP, so
// drawing it at its pixel size shows it twice as large as the text it sits
// next to. Two sources say how dense an image is: the file itself (PNG pHYs,
// JPEG JFIF density) and, when the file says nothing, the display the board
// runs on — macOS copies screenshots to the clipboard without any density
// metadata, but they were captured at this screen's scale.
const (
	// minGuessedDensityPixels guards the display-scale guess. A screenshot
	// pasted into a card is at least this large on one side, while the small
	// unlabelled images that really are 1× (icons, logos, avatars) stay put.
	minGuessedDensityPixels = 400

	// maxDensity bounds the pixels-per-DIP ratio: 1×, 2× and 3× displays.
	maxDensity = 3

	// densityThreshold separates an image that is merely mislabelled from one
	// that is really denser than a DIP: 1.25 rounds up to 2×.
	densityThreshold = 1.25
)

// displayImage retains every source pixel. Retina sizing belongs to the image
// element's logical layout dimensions; resampling here throws away the extra
// pixels needed to render sharply on a high-density display.
func (b *board) displayImage(data []byte) (ui.ImageSource, error) {
	return ui.DecodeBitmap(data)
}

// displayDensity is how many pixels the image holds per DIP it should be
// drawn at: its declared density when the file states one, otherwise the
// screen scale for an image large enough to be a pasted screenshot, otherwise
// 1. screenScale is the main display's backing scale.
func displayDensity(data []byte, w, h int, screenScale float64) float64 {
	if declared := declaredDensity(data); declared > 0 {
		return clampDensity(declared)
	}
	if max(w, h) < minGuessedDensityPixels {
		return 1
	}
	return clampDensity(screenScale)
}

// clampDensity snaps a pixels-per-DIP ratio to 1, 2 or 3, so a slightly dense
// export is not shrunk by an odd fraction.
func clampDensity(density float64) float64 {
	if density < densityThreshold {
		return 1
	}
	if density = math.Round(density); density > maxDensity {
		return maxDensity
	}
	return density
}

// declaredDensity reads the density the image file carries, in pixels per
// DIP, or 0 when it carries none.
func declaredDensity(data []byte) float64 {
	if d := pngDensity(data); d > 0 {
		return d
	}
	return jpegDensity(data)
}

var pngSignature = []byte("\x89PNG\r\n\x1a\n")

// pngDensity reads the pHYs chunk, whose pixels per metre become dots per
// inch. A chunk that only states an aspect ratio says nothing about density.
func pngDensity(data []byte) float64 {
	if !bytes.HasPrefix(data, pngSignature) {
		return 0
	}
	for i := len(pngSignature); i+8 <= len(data); {
		size := int(binary.BigEndian.Uint32(data[i:]))
		kind := string(data[i+4 : i+8])
		if size < 0 || i+12+size > len(data) {
			return 0
		}
		switch kind {
		case "pHYs":
			if size < 9 || data[i+16] != 1 { // unit 1 is metres, 0 is aspect only
				return 0
			}
			perMetre := float64(binary.BigEndian.Uint32(data[i+8:]))
			return perMetre * 0.0254 / 72 // inches per metre over the 72 dpi a DIP is
		case "IDAT", "IEND":
			return 0 // pHYs must precede the pixels
		}
		i += 12 + size
	}
	return 0
}

// jpegDensity reads the JFIF header's density. The image's own pixels carry
// no density in any other segment, so an absent header means unknown.
func jpegDensity(data []byte) float64 {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 0
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xff {
			return 0
		}
		marker := data[i+1]
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if marker == 0xda || size < 2 || i+2+size > len(data) { // the image data starts
			return 0
		}
		seg := data[i+4 : i+2+size]
		if marker == 0xe0 && len(seg) >= 12 && string(seg[:5]) == "JFIF\x00" {
			per := float64(binary.BigEndian.Uint16(seg[8:]))
			switch seg[7] {
			case 1: // dots per inch
				return per / 72
			case 2: // dots per centimetre
				return per * 2.54 / 72
			default: // aspect ratio only
				return 0
			}
		}
		i += 2 + size
	}
	return 0
}

var (
	screenScaleBits atomic.Uint64 // float64 bits of the main display's scale
)

// syncDisplayScale records the main display's backing scale — 2 on a Retina
// screen — for the density guess below. The screen can only be asked on the
// main thread once the app is ready, so this runs from App.WhenReady; before
// that, and in tests, every image stays at its pixel size.
func syncDisplayScale() {
	defer func() { _ = recover() }()
	displays := mygo.Screen.Displays()
	if len(displays) == 0 || displays[0].ScaleFactor <= 0 {
		return
	}
	screenScaleBits.Store(math.Float64bits(displays[0].ScaleFactor))
}

func displayScale() float64 {
	if bits := screenScaleBits.Load(); bits != 0 {
		return math.Float64frombits(bits)
	}
	return 1
}
