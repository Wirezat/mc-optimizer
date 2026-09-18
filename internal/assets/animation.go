package assets

import (
	"encoding/json"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

// tickMS is a Minecraft tick.
const tickMS = 50

// Sheet describes a texture that stacks several frames vertically instead of holding a
// single image.
type Sheet struct {
	Cells    int  // frames physically stacked in the file
	Play     int  // frames the animation shows
	FrameMS  int  // how long one frame is displayed
	PingPong bool // the order runs up and back down again
}

// ResolveAnimation reports how to play the texture at urlPath, which is a URL this package
// handed out — "/assets/<mod>/textures/...".
func ResolveAnimation(assetsDir, urlPath string) (Sheet, bool) {
	rel, ok := diskRelFromURL(urlPath)
	if !ok {
		return Sheet{}, false
	}
	return InspectAnimation(filepath.Join(assetsDir, rel))
}

// InspectAnimation reads a texture file and reports how to play it.
func InspectAnimation(pngPath string) (Sheet, bool) {
	file, err := os.Open(pngPath)
	if err != nil {
		return Sheet{}, false
	}
	defer file.Close()
	cfg, _, err := image.DecodeConfig(file)
	if err != nil || cfg.Width <= 0 {
		return Sheet{}, false
	}

	meta, hasMeta := readAnimationMeta(pngPath + ".mcmeta")

	cells := 0
	if hasMeta && meta.height > 0 && cfg.Height%meta.height == 0 {
		cells = cfg.Height / meta.height
	} else if cfg.Height > cfg.Width && cfg.Height%cfg.Width == 0 {
		cells = cfg.Height / cfg.Width
	}
	if cells < 2 {
		return Sheet{}, false
	}

	frameTicks := 1 // Minecraft's default when the metadata omits it
	if hasMeta && meta.frametime > 0 {
		frameTicks = meta.frametime
	}

	sheet := Sheet{Cells: cells, Play: cells, FrameMS: frameTicks * tickMS}
	if hasMeta && isPingPong(meta.frames, cells) {
		// The list walks up and back down, so only the way up is played.
		sheet.Play = len(meta.frames)/2 + 1
		sheet.PingPong = true
	}
	return sheet, true
}

// diskRelFromURL turns "/assets/<mod>/textures/x.png" back into the path below assetsDir,
// refusing anything that is not one of our own asset URLs or that tries to climb out of the
// tree.
func diskRelFromURL(urlPath string) (string, bool) {
	rel, ok := strings.CutPrefix(urlPath, "/assets/")
	if !ok || rel == "" || strings.Contains(rel, "..") {
		return "", false
	}
	return filepath.FromSlash(rel), true
}

type animationMeta struct {
	frametime int
	height    int
	frames    []int
}

func readAnimationMeta(path string) (animationMeta, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return animationMeta{}, false
	}
	var doc struct {
		Animation *struct {
			Frametime int   `json:"frametime"`
			Height    int   `json:"height"`
			Frames    []int `json:"frames"`
		} `json:"animation"`
	}
	if err := json.Unmarshal(data, &doc); err != nil || doc.Animation == nil {
		return animationMeta{}, false
	}
	return animationMeta{
		frametime: doc.Animation.Frametime,
		height:    doc.Animation.Height,
		frames:    doc.Animation.Frames,
	}, true
}

// isPingPong reports whether an explicit frame list is "0..n-1,n-2..1" — the order vanilla
// lava and one of MI's fluids use.
func isPingPong(list []int, sheetFrames int) bool {
	// A palindrome of this shape has one entry per frame up, then all but the endpoints back
	// down.
	if len(list) < 4 || len(list)%2 != 0 {
		return false
	}
	peak := len(list)/2 + 1
	if peak > sheetFrames {
		return false
	}
	for i := range peak {
		if list[i] != i {
			return false
		}
	}
	for i := 1; i < peak-1; i++ {
		if list[len(list)-i] != i {
			return false
		}
	}
	return true
}
