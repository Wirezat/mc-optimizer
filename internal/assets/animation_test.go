package assets

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// writePNG lays down a real PNG, since these paths decode the image header to
// work out how many frames a sheet holds.
func writePNG(t *testing.T, root, rel string, w, h int) string {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return full
}

func writeMeta(t *testing.T, pngPath, body string) {
	t.Helper()
	if err := os.WriteFile(pngPath+".mcmeta", []byte(body), 0o644); err != nil {
		t.Fatalf("write mcmeta: %v", err)
	}
}

// A square texture is a single image, whatever else is lying around.
func TestInspectAnimation_SquareIsNotAnimated(t *testing.T) {
	path := writePNG(t, t.TempDir(), "minecraft/textures/item/stone.png", 16, 16)
	if _, ok := InspectAnimation(path); ok {
		t.Error("a 16x16 texture must not be reported as animated")
	}
}

// Mods ship animation metadata inconsistently, so the frame count is inferred
// from the proportions when no .mcmeta says otherwise.
func TestInspectAnimation_InfersFramesFromProportions(t *testing.T) {
	path := writePNG(t, t.TempDir(), "mod/textures/fluid/x_still.png", 16, 512)

	sheet, ok := InspectAnimation(path)
	if !ok {
		t.Fatal("expected a 16x512 strip to be animated")
	}
	if sheet.Cells != 32 || sheet.Play != 32 {
		t.Errorf("got Cells=%d Play=%d, want 32 and 32", sheet.Cells, sheet.Play)
	}
	// Minecraft's default frametime is one tick when the metadata omits it.
	if sheet.FrameMS != 50 {
		t.Errorf("FrameMS = %d, want 50 (one tick)", sheet.FrameMS)
	}
	if sheet.PingPong {
		t.Error("PingPong should be false without an explicit frame list")
	}
}

// frametime is the field that actually matters: vanilla and MI both use 2, so
// defaulting to 1 would play every fluid at twice its real speed.
func TestInspectAnimation_UsesFrametimeFromMeta(t *testing.T) {
	path := writePNG(t, t.TempDir(), "mod/textures/fluid/x_still.png", 16, 512)
	writeMeta(t, path, `{"animation":{"frametime":2}}`)

	sheet, ok := InspectAnimation(path)
	if !ok {
		t.Fatal("expected animated")
	}
	if sheet.Cells != 32 || sheet.FrameMS != 100 {
		t.Errorf("got Cells=%d FrameMS=%d, want 32 and 100", sheet.Cells, sheet.FrameMS)
	}
}

// An explicit height overrides the proportions, which is the only way to
// describe non-square cells.
func TestInspectAnimation_MetaHeightOverridesProportions(t *testing.T) {
	path := writePNG(t, t.TempDir(), "mod/textures/fluid/x_still.png", 16, 64)
	writeMeta(t, path, `{"animation":{"height":32}}`)

	sheet, ok := InspectAnimation(path)
	if !ok {
		t.Fatal("expected animated")
	}
	if sheet.Cells != 2 {
		t.Errorf("Cells = %d, want 2 (64/32), not 4 from the aspect ratio", sheet.Cells)
	}
}

// Vanilla lava and one MI fluid list their frames running up and back down.
// That is a palindrome, so it plays by alternating direction over half the
// list — getting this wrong would run the animation backwards through a jump.
func TestInspectAnimation_DetectsPingPong(t *testing.T) {
	path := writePNG(t, t.TempDir(), "minecraft/textures/block/lava_still.png", 16, 320)
	writeMeta(t, path, `{"animation":{"frametime":2,"frames":[
		0,1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,
		18,17,16,15,14,13,12,11,10,9,8,7,6,5,4,3,2,1]}}`)

	sheet, ok := InspectAnimation(path)
	if !ok {
		t.Fatal("expected animated")
	}
	if !sheet.PingPong {
		t.Error("a list running 0..19,18..1 is a ping-pong")
	}
	if sheet.Play != 20 {
		t.Errorf("Play = %d, want 20 — only the way up is played", sheet.Play)
	}
	if sheet.Cells != 20 {
		t.Errorf("Cells = %d, want 20 — the file still holds 20 frames", sheet.Cells)
	}
	if sheet.FrameMS != 100 {
		t.Errorf("FrameMS = %d, want 100", sheet.FrameMS)
	}
}

// A reordered or repeating list is not expressible as an alternating play, so
// it must fall back to a plain loop rather than be mistaken for one.
func TestInspectAnimation_NonPalindromeIsNotPingPong(t *testing.T) {
	path := writePNG(t, t.TempDir(), "mod/textures/fluid/x_still.png", 16, 64)
	writeMeta(t, path, `{"animation":{"frames":[0,2,1,3]}}`)

	sheet, ok := InspectAnimation(path)
	if !ok {
		t.Fatal("expected animated")
	}
	if sheet.PingPong {
		t.Error("0,2,1,3 is not a ping-pong")
	}
	if sheet.Cells != 4 || sheet.Play != 4 {
		t.Errorf("got Cells=%d Play=%d, want 4 and 4", sheet.Cells, sheet.Play)
	}
}

// A malformed .mcmeta must not break the icon; inference still applies.
func TestInspectAnimation_BrokenMetaFallsBackToInference(t *testing.T) {
	path := writePNG(t, t.TempDir(), "mod/textures/fluid/x_still.png", 16, 128)
	writeMeta(t, path, `{not json`)

	sheet, ok := InspectAnimation(path)
	if !ok {
		t.Fatal("expected animated despite the broken metadata")
	}
	if sheet.Cells != 8 || sheet.FrameMS != 50 {
		t.Errorf("got Cells=%d FrameMS=%d, want 8 and 50", sheet.Cells, sheet.FrameMS)
	}
}

// ResolveAnimation takes a URL this package handed out. It must not be usable
// to walk out of the asset tree.
func TestResolveAnimation_RejectsPathsOutsideAssets(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "mod/textures/fluid/x_still.png", 16, 512)

	for _, url := range []string{
		"/assets/../../etc/passwd",
		"/etc/passwd",
		"assets/mod/textures/fluid/x_still.png", // no leading slash: not one of ours
		"",
	} {
		if _, ok := ResolveAnimation(dir, url); ok {
			t.Errorf("ResolveAnimation accepted %q", url)
		}
	}
}

func TestResolveAnimation_ResolvesOwnURL(t *testing.T) {
	dir := t.TempDir()
	writePNG(t, dir, "mod/textures/fluid/x_still.png", 16, 512)

	sheet, ok := ResolveAnimation(dir, "/assets/mod/textures/fluid/x_still.png")
	if !ok || sheet.Cells != 32 {
		t.Errorf("got Cells=%d ok=%v, want 32 and true", sheet.Cells, ok)
	}
}

// A frame list may cover fewer cells than the file holds. Cells and Play then
// differ, and conflating them makes a renderer slice the file at the wrong
// height — it would treat two stacked frames as one.
func TestInspectAnimation_PartialPingPongKeepsCellCount(t *testing.T) {
	// 8 cells of 16px; the list plays only the first four, up and back down.
	path := writePNG(t, t.TempDir(), "mod/textures/fluid/x_still.png", 16, 128)
	writeMeta(t, path, `{"animation":{"frames":[0,1,2,3,2,1]}}`)

	sheet, ok := InspectAnimation(path)
	if !ok {
		t.Fatal("expected animated")
	}
	if sheet.Cells != 8 {
		t.Errorf("Cells = %d, want 8 — the file holds eight frames", sheet.Cells)
	}
	if sheet.Play != 4 || !sheet.PingPong {
		t.Errorf("got Play=%d PingPong=%v, want 4 and true", sheet.Play, sheet.PingPong)
	}
}
