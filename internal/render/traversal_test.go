package render

import (
	"os"
	"path/filepath"
	"testing"
)

// A model's parent and texture names are content from an imported ZIP.
func TestLoadSceneRejectsEscapingRefs(t *testing.T) {
	root := t.TempDir()
	assetsDir := filepath.Join(root, "assets")
	outside := filepath.Join(root, "outside.json")
	os.WriteFile(outside, []byte(`{"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{}}]}`), 0o644)

	dir := filepath.Join(assetsDir, "mod", "models", "item")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "evil.json"),
		[]byte(`{"parent":"mod:../../../outside"}`), 0o644)
	os.WriteFile(filepath.Join(dir, "evilTex.json"),
		[]byte(`{"textures":{"a":"mod:../../../../secret"},"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{"up":{"texture":"#a"}}}]}`), 0o644)

	l := NewLoader(assetsDir)
	if _, err := l.LoadScene("mod:item/evil"); err == nil {
		t.Error("a parent escaping the assets tree must be refused")
	}
	if _, err := l.LoadTexture("mod:../../../../secret"); err == nil {
		t.Error("a texture ref escaping the assets tree must be refused")
	}
}

// ResolveModel is the seam a future client-side renderer would call instead of LoadScene:
// same parent-merge, no texture decode.
func TestResolveModel_MergesParentChain(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "mod/models/block/parent.json",
		`{"textures":{"all":"mod:block/parent_tex"},"elements":[
			{"from":[0,0,0],"to":[16,16,16],"faces":{"up":{"texture":"#all"}}}
		]}`)
	writeFixtureFile(t, dir, "mod/models/item/child.json",
		`{"parent":"mod:block/parent","textures":{"all":"mod:block/child_tex"}}`)

	l := NewLoader(dir)
	model, err := l.ResolveModel("mod:item/child")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if len(model.Elements) != 1 {
		t.Fatalf("got %d elements, want 1 inherited from the parent", len(model.Elements))
	}
	if got := model.Textures["all"]; got != "mod:block/child_tex" {
		t.Errorf(`Textures["all"] = %q, want the child's own override`, got)
	}
}

// items/<id>.json wins over the blockstate when a block ships both.
func TestResolveModel_ItemDefinitionWinsOverBlockState(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "mod/blockstates/shelf.json",
		`{"multipart":[{"apply":{"model":"mod:block/shelf_body"}}]}`)
	writeFixtureFile(t, dir, "mod/items/shelf.json",
		`{"model":{"type":"minecraft:model","model":"mod:block/shelf_inventory"}}`)
	writeFixtureFile(t, dir, "mod/models/block/shelf_body.json",
		`{"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{"up":{"texture":"#all"}}}]}`)
	writeFixtureFile(t, dir, "mod/models/block/shelf_inventory.json",
		`{"elements":[{"from":[0,0,0],"to":[16,16,16],"faces":{"down":{"texture":"#all"}}}]}`)

	l := NewLoader(dir)
	model, err := l.ResolveModel("mod:item/shelf")
	if err != nil {
		t.Fatalf("ResolveModel: %v", err)
	}
	if _, ok := model.Elements[0].Faces["down"]; !ok {
		t.Errorf("got faces %v, want the shelf_inventory model's \"down\" face, not shelf_body's", model.Elements[0].Faces)
	}
}

func TestTextureURL_ResolvesExistingTexture(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "mod/textures/block/stone.png", "fake-png")

	l := NewLoader(dir)
	url, ok := l.TextureURL("mod:block/stone")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "/assets/mod/textures/block/stone.png"; url != want {
		t.Errorf("url = %q, want %q", url, want)
	}
}

func TestTextureURL_MissingFile(t *testing.T) {
	l := NewLoader(t.TempDir())
	if _, ok := l.TextureURL("mod:block/nonexistent"); ok {
		t.Fatal("expected ok=false for a texture that doesn't exist")
	}
}

func TestTextureURL_RejectsEscapingRef(t *testing.T) {
	l := NewLoader(t.TempDir())
	if _, ok := l.TextureURL("mod:../../../secret"); ok {
		t.Fatal("a texture ref escaping the assets tree must be refused")
	}
}
