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

func resolveFirst(assetsDir string, candidates []candidate) (string, bool) {
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(assetsDir, c.diskRel)); err == nil {
			return "/assets/" + c.urlRel, true
		}
	}
	return "", false
}
