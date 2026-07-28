package render

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeBlockstateFixture(t *testing.T, root, rel, json string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(json), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// The common case: a block with no properties has exactly one variant, keyed
// by the empty string, and it always wins.
func TestResolveBlockState_NoPropertiesVariant(t *testing.T) {
	dir := t.TempDir()
	writeBlockstateFixture(t, dir, "mod/blockstates/plain_block.json",
		`{"variants":{"":{"model":"mod:block/plain_block"}}}`)

	l := NewLoader(dir)
	ref, ok := l.resolveBlockState("mod", "plain_block")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "mod:block/plain_block"; ref != want {
		t.Errorf("ref = %q, want %q", ref, want)
	}
}

// With no world state to match, several keyed variants pick the
// alphabetically-first — deterministic, not necessarily the "natural" one.
func TestResolveBlockState_MultipleVariantsPicksFirstKey(t *testing.T) {
	dir := t.TempDir()
	writeBlockstateFixture(t, dir, "mod/blockstates/stairs.json",
		`{"variants":{
			"facing=north":{"model":"mod:block/stairs_north"},
			"facing=east":{"model":"mod:block/stairs_east"}
		}}`)

	l := NewLoader(dir)
	ref, ok := l.resolveBlockState("mod", "stairs")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "mod:block/stairs_east"; ref != want {
		t.Errorf("ref = %q, want %q (alphabetically first key)", ref, want)
	}
}

// Vanilla allows a list of equally-valid options for random visual variety;
// only the first is deterministic enough for an icon.
func TestResolveBlockState_VariantListTakesFirst(t *testing.T) {
	dir := t.TempDir()
	writeBlockstateFixture(t, dir, "mod/blockstates/grass.json",
		`{"variants":{"":[{"model":"mod:block/grass1"},{"model":"mod:block/grass2"}]}}`)

	l := NewLoader(dir)
	ref, ok := l.resolveBlockState("mod", "grass")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "mod:block/grass1"; ref != want {
		t.Errorf("ref = %q, want %q", ref, want)
	}
}

func TestResolveBlockState_MultipartTakesFirstEntry(t *testing.T) {
	dir := t.TempDir()
	writeBlockstateFixture(t, dir, "mod/blockstates/shelf.json",
		`{"multipart":[
			{"apply":{"model":"mod:block/shelf"},"when":{"facing":"north"}},
			{"apply":{"model":"mod:block/shelf","y":90},"when":{"facing":"east"}}
		]}`)

	l := NewLoader(dir)
	ref, ok := l.resolveBlockState("mod", "shelf")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "mod:block/shelf"; ref != want {
		t.Errorf("ref = %q, want %q (first multipart entry)", ref, want)
	}
}

func TestResolveItemDefinition_ModelEntry(t *testing.T) {
	dir := t.TempDir()
	writeBlockstateFixture(t, dir, "mod/items/shelf.json",
		`{"model":{"type":"minecraft:model","model":"mod:block/shelf_inventory"}}`)

	l := NewLoader(dir)
	ref, ok := l.resolveItemDefinition("mod", "shelf")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if want := "mod:block/shelf_inventory"; ref != want {
		t.Errorf("ref = %q, want %q", ref, want)
	}
}

// Any entry type other than "minecraft:model" is left unresolved.
func TestResolveItemDefinition_UnsupportedEntryType(t *testing.T) {
	dir := t.TempDir()
	writeBlockstateFixture(t, dir, "mod/items/complex.json",
		`{"model":{"type":"minecraft:select","property":"minecraft:display_context","cases":[]}}`)

	l := NewLoader(dir)
	if _, ok := l.resolveItemDefinition("mod", "complex"); ok {
		t.Error("expected ok=false for a non-model entry type")
	}
}

func TestResolveBlockState_MissingFile(t *testing.T) {
	l := NewLoader(t.TempDir())
	if _, ok := l.resolveBlockState("mod", "nonexistent"); ok {
		t.Fatal("expected ok=false for a missing blockstate")
	}
}

func TestResolveBlockState_Malformed(t *testing.T) {
	dir := t.TempDir()
	writeBlockstateFixture(t, dir, "mod/blockstates/broken.json", `{not json`)

	l := NewLoader(dir)
	if _, ok := l.resolveBlockState("mod", "broken"); ok {
		t.Fatal("expected ok=false for malformed JSON")
	}
}

func TestResolveBlockState_NoVariants(t *testing.T) {
	dir := t.TempDir()
	writeBlockstateFixture(t, dir, "mod/blockstates/empty.json", `{"variants":{}}`)

	l := NewLoader(dir)
	if _, ok := l.resolveBlockState("mod", "empty"); ok {
		t.Fatal("expected ok=false for a blockstate with no variants")
	}
}

// End-to-end: a block-item that ships no models/item/<id>.json still
// resolves through its blockstate, only at the top of the chain.
func TestLoadScene_FallsBackToBlockState(t *testing.T) {
	dir := t.TempDir()
	writeBlockstateFixture(t, dir, "mod/blockstates/plain_block.json",
		`{"variants":{"":{"model":"mod:block/plain_block"}}}`)
	writeFixtureFile(t, dir, "mod/models/block/plain_block.json",
		`{"textures":{"all":"mod:block/plain"},"elements":[
			{"from":[0,0,0],"to":[16,16,16],"faces":{"up":{"texture":"#all"}}}
		]}`)
	writeFixturePNG(t, dir, "mod/textures/block/plain.png")

	l := NewLoader(dir)
	scene, err := l.LoadScene("mod:item/plain_block")
	if err != nil {
		t.Fatalf("LoadScene: %v", err)
	}
	if len(scene.Model.Elements) != 1 {
		t.Errorf("got %d elements, want 1 from the fallback block model", len(scene.Model.Elements))
	}
}

// A `parent` reference is content a mod wrote, never a block ID — the
// blockstate fallback must not silently substitute a match for it.
func TestLoadScene_ParentDoesNotFallBackToBlockState(t *testing.T) {
	dir := t.TempDir()
	// A blockstate that happens to share a name with the missing parent must
	// be ignored: only depth 0 may use it.
	writeBlockstateFixture(t, dir, "mod/blockstates/missing_parent.json",
		`{"variants":{"":{"model":"mod:block/plain_block"}}}`)
	writeFixtureFile(t, dir, "mod/models/item/child.json",
		`{"parent":"mod:missing_parent","textures":{},"elements":[]}`)

	l := NewLoader(dir)
	if _, err := l.LoadScene("mod:item/child"); err == nil {
		t.Fatal("expected an error: a missing parent must not resolve via blockstate")
	}
}

func writeFixtureFile(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// writeFixturePNG writes a real, decodable 1x1 PNG — LoadTexture decodes the
// file, so a placeholder string like the asset-resolution fixtures use won't
// do here.
func writeFixturePNG(t *testing.T, root, rel string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture png: %v", err)
	}
	writeFixtureFile(t, root, rel, buf.String())
}
