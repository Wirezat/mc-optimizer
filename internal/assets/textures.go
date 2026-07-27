// Package assets resolves item and fluid texture files, extracted from mod
// ZIPs into ./assets/, by filename convention. No DB, no cache — every call
// is a live filesystem check.
package assets

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type candidate struct {
	diskRel string
	urlRel  string
}

// safeSegment reports whether s is safe to use as a single path component —
// no separator, no "..", not empty. modID/itemID/fluidID come from imported
// mod data via the DB, not straight from a request, so this is defense in
// depth rather than a response to a known unvalidated path.
func safeSegment(s string) bool {
	return s != "" && s != ".." && !strings.ContainsAny(s, `/\`)
}

// ResolveItemTexture returns the public URL path for itemID's icon under modID,
// or ok=false if neither a texture nor a model exists on disk under assetsDir.
//
// Block-items (whose inventory icon comes from the block texture, not a separate
// item texture) are handled by falling back to textures/block/ when
// textures/item/ has no matching file.
//
// Some items have no flat sprite at all, because the game draws a 3D model for
// them — MI's pipes and cables are the case that forced this, and machine blocks
// are the same story. For those the URL points at the render endpoint instead of
// a file, so a caller can put it in an <img> either way without caring which it
// got.
func ResolveItemTexture(assetsDir, modID, itemID string) (urlPath string, ok bool) {
	if !safeSegment(modID) || !safeSegment(itemID) {
		return "", false
	}
	if url, ok := resolveFirst(assetsDir, []candidate{
		{filepath.Join(modID, "textures", "item", itemID+".png"), fmt.Sprintf("%s/textures/item/%s.png", modID, itemID)},
		{filepath.Join(modID, "textures", "block", itemID+".png"), fmt.Sprintf("%s/textures/block/%s.png", modID, itemID)},
	}); ok {
		return url, true
	}
	// Minecraft's own layout: an item's model lives at models/item/<id>.json.
	if _, err := os.Stat(filepath.Join(assetsDir, modID, "models", "item", itemID+".json")); err == nil {
		return fmt.Sprintf("/assets/render/%s/item/%s.png", modID, itemID), true
	}
	// A mod's block-item can omit its own item model and rely on the block's
	// blockstate the way vanilla never does; the render endpoint resolves
	// that fallback itself (render.Loader.resolveBlockState), so the same
	// URL shape still applies.
	if _, err := os.Stat(filepath.Join(assetsDir, modID, "blockstates", itemID+".json")); err == nil {
		return fmt.Sprintf("/assets/render/%s/item/%s.png", modID, itemID), true
	}
	return "", false
}

// ResolveFluidTexture returns the public URL path for fluidID's still-frame
// icon under modID, or ok=false if no texture file exists on disk under
// assetsDir. Mods keep these under textures/fluid/, but vanilla's own water and
// lava live in textures/block/, so that layout is tried second — the same
// item/block split ResolveItemTexture already has to cope with.
func ResolveFluidTexture(assetsDir, modID, fluidID string) (urlPath string, ok bool) {
	if !safeSegment(modID) || !safeSegment(fluidID) {
		return "", false
	}
	return resolveFirst(assetsDir, []candidate{
		{filepath.Join(modID, "textures", "fluid", fluidID+"_still.png"), fmt.Sprintf("%s/textures/fluid/%s_still.png", modID, fluidID)},
		{filepath.Join(modID, "textures", "block", fluidID+"_still.png"), fmt.Sprintf("%s/textures/block/%s_still.png", modID, fluidID)},
	})
}

func resolveFirst(assetsDir string, candidates []candidate) (string, bool) {
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(assetsDir, c.diskRel)); err == nil {
			return "/assets/" + c.urlRel, true
		}
	}
	return "", false
}
