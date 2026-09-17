// Package assets resolves item and fluid texture files, extracted from mod
// ZIPs into ./assets/, by filename convention. No DB, no cache — every call
// is a live filesystem check.
package assets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type candidate struct {
	diskRel string
	urlRel  string
}

// safeSegment reports whether s is safe to use as a single path component:
// no separator, no "..", not empty.
func safeSegment(s string) bool {
	return s != "" && s != ".." && !strings.ContainsAny(s, `/\`)
}

// ResolveItemTexture returns the public URL path for itemID's icon under modID
// (both plain path segments, not namespaced refs), or ok=false if neither a
// texture nor a model exists on disk under assetsDir.
func ResolveItemTexture(assetsDir, modID, itemID string) (urlPath string, ok bool) {
	if !safeSegment(modID) || !safeSegment(itemID) {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(assetsDir, modID, "models", "item", itemID+".json")); err == nil {
		if url, ok := generatedSprite(assetsDir, modID+":item/"+itemID); ok {
			return url, true
		}
		return fmt.Sprintf("/assets/render/%s/item/%s.png", modID, itemID), true
	}
	if _, err := os.Stat(filepath.Join(assetsDir, modID, "blockstates", itemID+".json")); err == nil {
		return fmt.Sprintf("/assets/render/%s/item/%s.png", modID, itemID), true
	}
	if url, ok := resolveFirst(assetsDir, []candidate{
		{filepath.Join(modID, "textures", "item", itemID+".png"), fmt.Sprintf("%s/textures/item/%s.png", modID, itemID)},
		{filepath.Join(modID, "textures", "block", itemID+".png"), fmt.Sprintf("%s/textures/block/%s.png", modID, itemID)},
	}); ok {
		return url, true
	}
	return "", false
}

// ResolveFluidTexture returns the public URL path for fluidID's still-frame
// icon under modID, or ok=false if no texture file exists on disk under
// assetsDir.
func ResolveFluidTexture(assetsDir, modID, fluidID string) (urlPath string, ok bool) {
	if !safeSegment(modID) || !safeSegment(fluidID) {
		return "", false
	}
	return resolveFirst(assetsDir, []candidate{
		{filepath.Join(modID, "textures", "fluid", fluidID+"_still.png"), fmt.Sprintf("%s/textures/fluid/%s_still.png", modID, fluidID)},
		{filepath.Join(modID, "textures", "block", fluidID+"_still.png"), fmt.Sprintf("%s/textures/block/%s_still.png", modID, fluidID)},
	})
}

// ResolveMachineTexture returns the public URL path for machineID's icon
// under modID (block model, blockstate, item model, or generated fallback
// texture, in that order), or ok=false if none exist on disk under
// assetsDir. Item models cover machines with no block model (MI's
// block-entity-rendered casings) via vanilla's generic block/cube parent,
// no MI-specific rendering needed; only the filename needs a second,
// "electric_"-prefixed attempt for MI's one naming inconsistency.
func ResolveMachineTexture(assetsDir, modID, machineID string) (urlPath string, ok bool) {
	if !safeSegment(modID) || !safeSegment(machineID) {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(assetsDir, modID, "models", "block", machineID+".json")); err == nil {
		return fmt.Sprintf("/assets/render/%s/block/%s.png", modID, machineID), true
	}
	if _, err := os.Stat(filepath.Join(assetsDir, modID, "blockstates", machineID+".json")); err == nil {
		return fmt.Sprintf("/assets/render/%s/block/%s.png", modID, machineID), true
	}
	if _, err := os.Stat(filepath.Join(assetsDir, modID, "models", "item", machineID+".json")); err == nil {
		return fmt.Sprintf("/assets/render/%s/item/%s.png", modID, machineID), true
	}
	if _, err := os.Stat(filepath.Join(assetsDir, modID, "models", "item", "electric_"+machineID+".json")); err == nil {
		return fmt.Sprintf("/assets/render/%s/item/electric_%s.png", modID, machineID), true
	}
	return resolveFirst(assetsDir, []candidate{
		{filepath.Join(modID, "textures", "generated", "machine_icons", machineID+"_south.png"), fmt.Sprintf("%s/textures/generated/machine_icons/%s_south.png", modID, machineID)},
		{filepath.Join(modID, "textures", "generated", "machine_icons", "electric_"+machineID+"_south.png"), fmt.Sprintf("%s/textures/generated/machine_icons/electric_%s_south.png", modID, machineID)},
	})
}

// maxParentDepth caps a model's parent chain, mirroring internal/render.
const maxParentDepth = 8

// generatedParents are the game's built-in flat item models: a chain that
// ends in one of them describes a sprite, not a shape — nothing for the
// renderer to draw, the layer0 texture is the icon.
var generatedParents = map[string]bool{
	"item/generated":    true,
	"item/handheld":     true,
	"item/handheld_rod": true,
	"builtin/generated": true,
}

// generatedSprite follows ref's parent chain under assetsDir and, where it
// ends in a built-in flat item model, returns the public URL of its layer0
// sprite. ok=false for a shape (or an unreadable chain), which the renderer
// handles. Textures merge child-over-parent the way the game does, so a base
// model's layer0 can be overridden by the item.
func generatedSprite(assetsDir, ref string) (urlPath string, ok bool) {
	textures := map[string]string{}
	for depth := 0; ref != "" && depth <= maxParentDepth; depth++ {
		modID, rel := splitRef(ref)
		if !safeRel(modID) || !safeRel(rel) {
			return "", false
		}
		if generatedParents[rel] {
			return spriteURL(assetsDir, textures["layer0"])
		}
		data, err := os.ReadFile(filepath.Join(assetsDir, modID, "models", filepath.FromSlash(rel)+".json"))
		if err != nil {
			return "", false
		}
		var m struct {
			Parent   string            `json:"parent"`
			Textures map[string]string `json:"textures"`
		}
		if json.Unmarshal(data, &m) != nil {
			return "", false
		}
		for k, v := range m.Textures {
			if _, set := textures[k]; !set {
				textures[k] = v
			}
		}
		ref = m.Parent
	}
	return "", false
}

// spriteURL turns a texture ref ("mod:item/tools/wrench") into its public
// URL, ok=false if the file is not on disk.
func spriteURL(assetsDir, texRef string) (string, bool) {
	if texRef == "" {
		return "", false
	}
	modID, rel := splitRef(texRef)
	if !safeRel(modID) || !safeRel(rel) {
		return "", false
	}
	return resolveFirst(assetsDir, []candidate{
		{filepath.Join(modID, "textures", filepath.FromSlash(rel)+".png"), fmt.Sprintf("%s/textures/%s.png", modID, rel)},
	})
}

// splitRef splits a namespaced ref; the namespace defaults to minecraft, as
// in the game.
func splitRef(ref string) (modID, rel string) {
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		return ref[:i], ref[i+1:]
	}
	return "minecraft", ref
}

// safeRel reports whether a slash-separated ref component stays inside the
// assets tree: no empty segments, no "..", no backslashes.
func safeRel(rel string) bool {
	if rel == "" || strings.ContainsRune(rel, '\\') {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == ".." {
			return false
		}
	}
	return true
}

func resolveFirst(assetsDir string, candidates []candidate) (string, bool) {
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(assetsDir, c.diskRel)); err == nil {
			return "/assets/" + c.urlRel, true
		}
	}
	return "", false
}
