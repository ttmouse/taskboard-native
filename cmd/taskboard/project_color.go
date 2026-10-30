package main

import (
	"hash/fnv"
	"strconv"
	"strings"

	"taskboard-native/internal/model"

	"github.com/egoist/mygo/ui"
)

// projectColorPalette is the swatch row the project form offers. Each color
// is a "#rrggbb" hex string the service accepts verbatim; the same palette
// also feeds the automatic slot assignment, so a project with no explicit
// color still paints with a stable, distinct hue in 全部项目.
var projectColorPalette = []string{
	"#2563eb", // blue
	"#0ea5e9", // sky
	"#10b981", // emerald
	"#f59e0b", // amber
	"#ef4444", // red
	"#ec4899", // pink
	"#8b5cf6", // violet
	"#64748b", // slate
	"#14b8a6", // teal
	"#84cc16", // lime
}

// projectColor resolves the color a project's cards wear in the global
// view. An explicit project color wins; otherwise the project's position in
// the sidebar list picks a stable palette slot, so the assignment survives
// relaunches and stays consistent while the list order is unchanged.
func (b *board) projectColor(p model.Project) ui.Color {
	if p.Color != "" {
		if parsed, err := parseHexColor(p.Color); err == nil {
			return parsed
		}
	}
	return paletteSlot(projectIndex(b.projects, p.ID))
}

// projectIndex is the project's position in b.projects, or a stable hash
// when it is not there yet (a pushed event can race the list load).
func projectIndex(projects []model.Project, id string) int {
	for i, p := range projects {
		if p.ID == id {
			return i
		}
	}
	return hashIndex(id)
}

// hashIndex gives an id a stable slot without consulting any list.
func hashIndex(id string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return int(h.Sum32() % uint32(len(projectColorPalette)))
}

// paletteSlot is the palette color for a stable index.
func paletteSlot(i int) ui.Color {
	if len(projectColorPalette) == 0 {
		return ui.Hex("#2563eb")
	}
	n := i % len(projectColorPalette)
	if n < 0 {
		n += len(projectColorPalette)
	}
	return ui.Hex(projectColorPalette[n])
}

// parseHexColor turns a "#rrggbb" string into a ui.Color; a malformed value
// fails, so callers fall back to the palette slot rather than paint black.
func parseHexColor(s string) (ui.Color, error) {
	s = strings.TrimSpace(s)
	if len(s) != 7 || s[0] != '#' {
		return ui.Color{}, strconv.ErrSyntax
	}
	r, err := hexByte(s[1:3])
	if err != nil {
		return ui.Color{}, err
	}
	g, err := hexByte(s[3:5])
	if err != nil {
		return ui.Color{}, err
	}
	b, err := hexByte(s[5:7])
	if err != nil {
		return ui.Color{}, err
	}
	return ui.Color{R: r, G: g, B: b, A: 255}, nil
}

func hexByte(s string) (uint8, error) {
	v, err := strconv.ParseUint(s, 16, 8)
	return uint8(v), err
}
