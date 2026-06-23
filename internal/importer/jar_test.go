package importer

import (
	"io"
	"strings"
	"testing"
)

// openString wraps a string as a WalkEntry.Open function.
func openString(s string) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(s)), nil
	}
}

func TestResolveTagsRaw_basic(t *testing.T) {
	raw := map[string][]string{
		"forge:ingots":        {"minecraft:iron_ingot", "#forge:ingots/copper"},
		"forge:ingots/copper": {"minecraft:copper_ingot"},
	}
	got := ResolveTagsRaw(raw)

	copper := got["forge:ingots/copper"]
	if len(copper) != 1 || copper[0] != "minecraft:copper_ingot" {
		t.Errorf("forge:ingots/copper = %v, want [minecraft:copper_ingot]", copper)
	}

	ingots := got["forge:ingots"]
	if len(ingots) != 2 {
		t.Errorf("forge:ingots len = %d, want 2; got %v", len(ingots), ingots)
	}
}

func TestResolveTagsRaw_cycle(t *testing.T) {
	// Cycle must not hang; result is partial but defined.
	raw := map[string][]string{
		"a": {"#b", "item:x"},
		"b": {"#a", "item:y"},
	}
	got := ResolveTagsRaw(raw)
	_ = got
}

func TestResolveTagsRaw_dedup(t *testing.T) {
	raw := map[string][]string{
		"tag:items": {"mod:a", "mod:a", "mod:b"},
	}
	got := ResolveTagsRaw(raw)
	items := got["tag:items"]
	if len(items) != 2 {
		t.Errorf("dedup: want 2 items, got %d: %v", len(items), items)
	}
}

func TestReadTagFile(t *testing.T) {
	entry := WalkEntry{
		Namespace: "forge",
		Category:  "tag",
		RelPath:   "ingots/iron.json",
		FullPath:  "data/forge/tags/items/ingots/iron.json",
		Open: openString(`{
			"replace": false,
			"values": ["minecraft:iron_ingot", {"id":"mod:special_iron","required":false}]
		}`),
	}
	td, err := ReadTagFile(entry)
	if err != nil {
		t.Fatalf("ReadTagFile: %v", err)
	}
	if td.TagName != "forge:ingots/iron" {
		t.Errorf("TagName = %q, want forge:ingots/iron", td.TagName)
	}
	if td.Replace {
		t.Error("Replace should be false")
	}
	if len(td.Values) != 2 {
		t.Errorf("Values len = %d, want 2; got %v", len(td.Values), td.Values)
	}
}

func TestReadLangFile(t *testing.T) {
	entry := WalkEntry{
		Namespace: "minecraft",
		Category:  "lang",
		RelPath:   "en_us.json",
		FullPath:  "assets/minecraft/lang/en_us.json",
		Open:      openString(`{"item.minecraft.iron_ingot":"Iron Ingot","block.minecraft.stone":"Stone"}`),
	}
	lang, entries, err := ReadLangFile(entry)
	if err != nil {
		t.Fatalf("ReadLangFile: %v", err)
	}
	if lang != "en_us" {
		t.Errorf("lang = %q, want en_us", lang)
	}
	if entries["item.minecraft.iron_ingot"] != "Iron Ingot" {
		t.Errorf("iron ingot name = %q, want Iron Ingot", entries["item.minecraft.iron_ingot"])
	}
	if len(entries) != 2 {
		t.Errorf("entries len = %d, want 2", len(entries))
	}
}

func TestMatchPath(t *testing.T) {
	cases := []struct {
		s, prefix, middle, suffix string
		wantNS, wantRest          string
		wantOK                    bool
	}{
		{"data/minecraft/recipes/iron_ingot.json", "data/", "/recipes/", ".json", "minecraft", "iron_ingot.json", true},
		{"data/forge/tags/items/ingots.json", "data/", "/tags/items/", ".json", "forge", "ingots.json", true},
		{"assets/mod/lang/en_us.json", "assets/", "/lang/", ".json", "mod", "en_us.json", true},
		{"META-INF/manifest.mf", "data/", "/recipes/", ".json", "", "", false},
	}
	for _, c := range cases {
		var ns, rest string
		ok := matchPath(c.s, c.prefix, c.middle, c.suffix, &ns, &rest)
		if ok != c.wantOK {
			t.Errorf("matchPath(%q) ok=%v, want %v", c.s, ok, c.wantOK)
			continue
		}
		if ok && (ns != c.wantNS || rest != c.wantRest) {
			t.Errorf("matchPath(%q) ns=%q rest=%q, want ns=%q rest=%q",
				c.s, ns, rest, c.wantNS, c.wantRest)
		}
	}
}
