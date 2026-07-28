package render

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

// solidTexture is a flat 16x16 texture, so a test asserts on geometry and
// shading rather than on texture content.
func solidTexture(r, g, b uint8) *Texture {
	t := &Texture{W: 16, H: 16, Cells: 1, Pix: make([]uint8, 16*16*4)}
	for i := 0; i < 16*16; i++ {
		t.Pix[i*4+0] = r
		t.Pix[i*4+1] = g
		t.Pix[i*4+2] = b
		t.Pix[i*4+3] = 255
	}
	return t
}

// fullCube builds a 0..16 cube whose six faces each point at their own texture.
func fullCube() *Scene {
	faces := map[string]Face{}
	for _, d := range directions {
		faces[d] = Face{Texture: "#" + d}
	}
	textures := map[string]TextureRef{}
	decoded := map[string]*Texture{}
	// A distinct primary per direction, so which face landed where is visible
	// from the pixel colour alone.
	colors := map[string][3]uint8{
		"up": {255, 255, 255}, "down": {64, 64, 64},
		"north": {255, 0, 0}, "south": {0, 255, 0},
		"west": {0, 0, 255}, "east": {255, 255, 0},
	}
	for d, c := range colors {
		ref := "test:block/" + d
		textures[d] = TextureRef(ref)
		decoded[ref] = solidTexture(c[0], c[1], c[2])
	}
	return &Scene{
		Model: &Model{
			Textures: textures,
			Elements: []Element{{From: [3]float64{0, 0, 0}, To: [3]float64{16, 16, 16}, Faces: faces}},
		},
		Textures: decoded,
	}
}

func at(img *image.RGBA, x, y int) (r, g, b, a uint8) {
	i := img.PixOffset(x, y)
	return img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]
}

// The gui transform's whole purpose is a specific three-quarter view. If the
// rotation order or handedness drifts, the wrong faces show and every icon is
// silently wrong, so pin down which faces are visible and where.
func TestRenderCubeShowsGuiFaces(t *testing.T) {
	img := Render(fullCube(), 32)

	// Top centre is the up face: full brightness, so pure white.
	if r, g, b, a := at(img, 16, 6); a == 0 || r < 250 || g < 250 || b < 250 {
		t.Errorf("top of cube: want opaque white (up face, shade 1.0), got %d,%d,%d a=%d", r, g, b, a)
	}

	// Lower right is north, shaded 0.8 — red at 80%.
	r, g, b, a := at(img, 22, 22)
	if a == 0 {
		t.Fatalf("lower right of cube is transparent, expected the north face")
	}
	if !(r > 180 && r < 225 && g < 20 && b < 20) {
		t.Errorf("north face: want red at shade 0.8 (~204,0,0), got %d,%d,%d", r, g, b)
	}

	// Lower left is east, shaded 0.6 — yellow at 60%.
	r, g, b, a = at(img, 9, 22)
	if a == 0 {
		t.Fatalf("lower left of cube is transparent, expected the east face")
	}
	if !(r > 130 && r < 175 && g > 130 && g < 175 && b < 20) {
		t.Errorf("east face: want yellow at shade 0.6 (~153,153,0), got %d,%d,%d", r, g, b)
	}

	// South and west point away and must be culled; nothing green or blue.
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			r, g, b, a := at(img, x, y)
			if a == 0 {
				continue
			}
			if g > 200 && r < 60 && b < 60 {
				t.Fatalf("pixel %d,%d is the south face — it faces away and should be culled", x, y)
			}
			if b > 200 && r < 60 && g < 60 {
				t.Fatalf("pixel %d,%d is the west face — it faces away and should be culled", x, y)
			}
		}
	}
}

// Pins the silhouette a full cube produces, which encodes the gui scale and
// rotation together. The numbers are not arbitrary: at scale 0.625 the rotated
// cube reaches |y| = 0.866*0.3125 + 0.354*0.625 = 0.492 of the half-frame, so it
// runs the full height and leaves a one-pixel margin left and right — a block in
// a vanilla inventory slot fills it the same way. The reference Python
// implementation this was ported from yields exactly these bounds.
func TestRenderCubeSilhouette(t *testing.T) {
	const size = 32
	img := Render(fullCube(), size)

	minX, minY, maxX, maxY := size, size, -1, -1
	for y := range size {
		for x := range size {
			if _, _, _, a := at(img, x, y); a > 0 {
				minX, minY = min(minX, x), min(minY, y)
				maxX, maxY = max(maxX, x), max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		t.Fatal("nothing was drawn")
	}
	if minX != 1 || maxX != 30 || minY != 0 || maxY != 31 {
		t.Errorf("cube silhouette x %d..%d, y %d..%d; want x 1..30, y 0..31 — gui transform changed",
			minX, maxX, minY, maxY)
	}
}

// image.RGBA is alpha-premultiplied and png.Encode divides the colour back out.
// Store a plain average and every partially covered edge pixel comes out of the
// encoder brightened and hue-shifted — grey pipes grew green fringes. Encoding
// and decoding has to give back the colour that was drawn.
func TestRenderEdgePixelsSurvivePNGRoundTrip(t *testing.T) {
	img := Render(fullCube(), 32)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}

	// The cube is white, red and yellow only; nothing green or cyan may appear.
	// Those are what an un-premultiply of a too-bright edge pixel produces.
	bounds := decoded.Bounds()
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r16, g16, b16, a16 := decoded.At(x, y).RGBA()
			if a16 == 0 {
				continue
			}
			// Un-premultiply to compare against the source colours.
			r := r16 * 0xFFFF / a16
			g := g16 * 0xFFFF / a16
			b := b16 * 0xFFFF / a16
			if g > r+0x2000 && g > b+0x2000 {
				t.Fatalf("pixel %d,%d decodes to a green cast (%d,%d,%d a=%d) — premultiply is wrong",
					x, y, r>>8, g>>8, b>>8, a16>>8)
			}
		}
	}
}

// A tint only applies where the face asks for one. Getting this backwards would
// colour every icon, or none of the ones that need it.
func TestRenderTintAppliesOnlyToTintedFaces(t *testing.T) {
	tint := uint32(0xFF0000)
	zero := 0

	build := func(tinted bool) *Scene {
		face := Face{Texture: "#all"}
		if tinted {
			face.TintIndex = &zero
		}
		return &Scene{
			Model: &Model{
				Tint:     &tint,
				Textures: map[string]TextureRef{"all": "test:block/all"},
				Elements: []Element{{
					From:  [3]float64{0, 0, 0},
					To:    [3]float64{16, 16, 16},
					Faces: map[string]Face{"up": face},
				}},
			},
			Textures: map[string]*Texture{"test:block/all": solidTexture(255, 255, 255)},
		}
	}

	// Untinted: white texture stays white on the up face (shade 1.0).
	if r, g, b, _ := at(Render(build(false), 32), 16, 10); g < 250 || b < 250 || r < 250 {
		t.Errorf("untinted up face: want white, got %d,%d,%d", r, g, b)
	}
	// Tinted red: green and blue multiplied away.
	if r, g, b, _ := at(Render(build(true), 32), 16, 10); r < 250 || g > 5 || b > 5 {
		t.Errorf("tinted up face: want pure red, got %d,%d,%d", r, g, b)
	}
}

// Fluid textures are vertical animation strips. Only the first cell is the icon
// the game shows; sampling the whole sheet squeezes 32 frames into the box.
func TestTextureAtReadsFirstFrameOnly(t *testing.T) {
	// Two frames: first all red, second all blue.
	tex := &Texture{W: 16, H: 32, Cells: 2, Pix: make([]uint8, 16*32*4)}
	for y := 0; y < 32; y++ {
		for x := 0; x < 16; x++ {
			i := (y*16 + x) * 4
			if y < 16 {
				tex.Pix[i], tex.Pix[i+3] = 255, 255
			} else {
				tex.Pix[i+2], tex.Pix[i+3] = 255, 255
			}
		}
	}
	for _, v := range []float64{0, 0.5, 0.99} {
		r, _, b, _ := tex.At(0.5, v)
		if r != 255 || b != 0 {
			t.Errorf("At(0.5,%v): want the first frame's red, got r=%d b=%d", v, r, b)
		}
	}
}

// Renders are cached and served by URL, so the same input must give the same
// bytes — otherwise every request busts the cache.
func TestRenderIsDeterministic(t *testing.T) {
	a := Render(fullCube(), 32)
	b := Render(fullCube(), 32)
	for i := range a.Pix {
		if a.Pix[i] != b.Pix[i] {
			t.Fatalf("two renders of the same scene differ at byte %d", i)
		}
	}
}

// A chain of texture references has to resolve, and a broken one must fail
// rather than loop.
func TestResolveTexture(t *testing.T) {
	m := &Model{Textures: map[string]TextureRef{
		"side": "#all",
		"all":  "minecraft:block/stone",
		"loop": "#loop",
	}}
	if got, ok := m.ResolveTexture("#side"); !ok || got != "minecraft:block/stone" {
		t.Errorf(`ResolveTexture("#side") = %q, %v; want "minecraft:block/stone", true`, got, ok)
	}
	if _, ok := m.ResolveTexture("#missing"); ok {
		t.Error(`ResolveTexture("#missing") should fail`)
	}
	if _, ok := m.ResolveTexture("#loop"); ok {
		t.Error(`ResolveTexture("#loop") should fail rather than spin`)
	}
}

func TestParseModelTextureWithTranslucencyHint(t *testing.T) {
	data := []byte(`{
		"parent": "minecraft:block/cube_all",
		"textures": {
			"all": {"force_translucent": true, "sprite": "minecraft:block/glass"}
		}
	}`)
	m, err := ParseModel(data)
	if err != nil {
		t.Fatalf("ParseModel: %v", err)
	}
	if got, ok := m.ResolveTexture("#all"); !ok || got != "minecraft:block/glass" {
		t.Errorf(`ResolveTexture("#all") = %q, %v; want "minecraft:block/glass", true`, got, ok)
	}
}

func TestTexturePath(t *testing.T) {
	for _, tc := range []struct{ in, mod, rel string }{
		{"minecraft:block/stone", "minecraft", "block/stone"},
		{"modern_industrialization:block/pipes/item", "modern_industrialization", "block/pipes/item"},
		{"block/stone", "minecraft", "block/stone"}, // namespace defaults like the game's
	} {
		mod, rel := TexturePath(tc.in)
		if mod != tc.mod || rel != tc.rel {
			t.Errorf("TexturePath(%q) = %q,%q; want %q,%q", tc.in, mod, rel, tc.mod, tc.rel)
		}
	}
}
