// Package render draws Minecraft block models as inventory-style icons.
//
// Some things have no flat item texture to serve: a pipe's item model delegates
// to a block model the game renders in 3D, and a machine block's icon is its
// model seen from the front-above. This package renders those on demand instead
// of looking for a sprite that does not exist.
//
// The input is Minecraft's own block model JSON, so any model a mod ships can be
// drawn without a per-mod special case, and the geometry stays where modders
// already put it.
package render

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Model is the subset of Minecraft's block model format this renderer draws.
//
// One extension: Tint. Vanilla carries a face's tintindex but leaves the colour
// itself to code — that is how one grey sprite becomes every cable colour — so a
// model that needs a tint states it here. Vanilla would ignore the field.
type Model struct {
	Parent   string            `json:"parent"`
	Textures map[string]string `json:"textures"`
	Elements []Element         `json:"elements"`
	Tint     *uint32           `json:"tint"`
}

// Element is one box of the model, in Minecraft's 0..16 block space.
type Element struct {
	From     [3]float64      `json:"from"`
	To       [3]float64      `json:"to"`
	Faces    map[string]Face `json:"faces"`
	Rotation *ElementRot     `json:"rotation"`
	// Shade defaults to true when absent, so it is a pointer: an element that
	// opts out of directional shading is rare but meaningful.
	Shade *bool `json:"shade"`
}

// ElementRot rotates one element about an axis before projection.
type ElementRot struct {
	Origin [3]float64 `json:"origin"`
	Axis   string     `json:"axis"`
	Angle  float64    `json:"angle"`
}

// Face is one side of an element.
type Face struct {
	// UV is x1,y1,x2,y2 in 0..16 texture space. Absent means the face spans
	// its whole texture.
	UV        *[4]float64 `json:"uv"`
	Texture   string      `json:"texture"`
	Rotation  int         `json:"rotation"`
	TintIndex *int        `json:"tintindex"`
}

// Directions in Minecraft's own order, with the outward normal and the fixed
// brightness the game applies to that side. The shading is a constant, not a
// light source — it is the only reason a flat-lit block reads as 3D.
var (
	directions = []string{"down", "up", "north", "south", "west", "east"}

	normals = map[string][3]float64{
		"down":  {0, -1, 0},
		"up":    {0, 1, 0},
		"north": {0, 0, -1},
		"south": {0, 0, 1},
		"west":  {-1, 0, 0},
		"east":  {1, 0, 0},
	}

	faceShade = map[string]float64{
		"down": 0.5, "up": 1.0,
		"north": 0.8, "south": 0.8,
		"west": 0.6, "east": 0.6,
	}
)

// ParseModel decodes a block model JSON document.
func ParseModel(data []byte) (*Model, error) {
	var m Model
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("render: parse model: %w", err)
	}
	return &m, nil
}

// ResolveTexture follows a face's texture reference through the model's texture
// map to a namespaced path like "minecraft:block/stone".
//
// References chain ("#side" -> "#all" -> "minecraft:block/stone"), so this
// walks until it reaches a real path. A chain that loops or dead-ends yields
// ok=false rather than spinning.
func (m *Model) ResolveTexture(ref string) (path string, ok bool) {
	const maxHops = 8
	for hop := 0; hop < maxHops; hop++ {
		if !strings.HasPrefix(ref, "#") {
			return ref, ref != ""
		}
		next, exists := m.Textures[strings.TrimPrefix(ref, "#")]
		if !exists {
			return "", false
		}
		ref = next
	}
	return "", false
}

// TexturePath turns "minecraft:block/stone" into the mod id and the path below
// its textures directory, defaulting the namespace to minecraft as the game
// does.
func TexturePath(ref string) (modID, rel string) {
	modID = "minecraft"
	rel = ref
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		modID, rel = ref[:i], ref[i+1:]
	}
	return modID, rel
}

// shaded reports whether an element takes directional shading.
func (e Element) shaded() bool {
	return e.Shade == nil || *e.Shade
}
