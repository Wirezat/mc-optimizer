package importer

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// TagFileData holds the raw content of one tag JSON file before sub-tag resolution.
type TagFileData struct {
	TagName string   // e.g. "minecraft:planks"
	Replace bool
	Values  []string // "mod:item_id" or "#mod:tag_path"
}

// WalkEntry is passed to the WalkJAR callback for each relevant file.
type WalkEntry struct {
	Namespace string // e.g. "minecraft"
	Category  string // "recipe", "tag", "lang", "texture", "loot_block", "villager_trade"
	RelPath   string // path within the category dir, e.g. "ingots/iron.json"
	FullPath  string // full path inside ZIP, for logging
	Open      func() (io.ReadCloser, error)
}

// WalkJAR opens a JAR (ZIP) and calls fn for each relevant data entry.
// Transparently handles Minecraft bundler JARs (has nested inner JAR under META-INF/versions/).
// fn is called synchronously; the ReadCloser from Open() must be consumed before fn returns.
func WalkJAR(jarPath string, fn func(WalkEntry) error) error {
	zr, err := zip.OpenReader(jarPath)
	if err != nil {
		return fmt.Errorf("open jar %s: %w", jarPath, err)
	}

	// Detect Minecraft bundler JAR (MC 1.21+): has a nested server-*.jar under META-INF/versions/.
	var innerFile *zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "META-INF/versions/") && strings.HasSuffix(f.Name, ".jar") {
			innerFile = f
			break
		}
	}
	if innerFile != nil {
		tmpPath, extractErr := extractZipEntry(innerFile)
		zr.Close()
		if extractErr != nil {
			return fmt.Errorf("bundler %s: extract inner jar: %w", jarPath, extractErr)
		}
		defer os.Remove(tmpPath)
		return WalkJAR(tmpPath, fn)
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		name := f.Name

		var ns, category, rel string

		switch {
		// Recipes: support both new singular path (1.21+) and old plural path.
		case matchPath(name, "data/", "/recipe/", ".json", &ns, &rel):
			category = "recipe"
		case matchPath(name, "data/", "/recipes/", ".json", &ns, &rel):
			category = "recipe"
		// Item tags: support both new singular and old plural directory names.
		case matchPath(name, "data/", "/tags/item/", ".json", &ns, &rel):
			category = "tag"
		case matchPath(name, "data/", "/tags/items/", ".json", &ns, &rel):
			category = "tag"
		// Block loot tables (also handle datapack-nested paths).
		case matchDeep(name, "/loot_table/blocks/", ".json", &ns, &rel):
			category = "loot_block"
		// Villager trades (also handle datapack-nested paths like datapacks/trade_rebalance/...).
		case matchDeep(name, "/villager_trade/", ".json", &ns, &rel):
			category = "villager_trade"
		// Language files.
		case matchPath(name, "assets/", "/lang/", ".json", &ns, &rel):
			category = "lang"
		// All textures (item, block, entity, …).
		case matchPath(name, "assets/", "/textures/", ".png", &ns, &rel):
			category = "texture"
		// Machine upgrade datamaps: data/<ns>/data_maps/item/machine_upgrades.json
		case strings.HasPrefix(name, "data/") && strings.HasSuffix(name, "/data_maps/item/machine_upgrades.json"):
			middle := name[len("data/") : len(name)-len("/data_maps/item/machine_upgrades.json")]
			if strings.Contains(middle, "/") {
				continue
			}
			ns = middle
			rel = "machine_upgrades.json"
			category = "machine_datamap"
		// Mod metadata (NeoForge/Forge or Fabric) for display name extraction.
		case name == "META-INF/neoforge.mods.toml", name == "META-INF/mods.toml",
			name == "fabric.mod.json", name == "quilt.mod.json":
			category = "mod_meta"
			rel = name
		default:
			continue
		}

		fc := f
		if err := fn(WalkEntry{
			Namespace: ns,
			Category:  category,
			RelPath:   rel,
			FullPath:  name,
			Open:      func() (io.ReadCloser, error) { return fc.Open() },
		}); err != nil {
			return err
		}
	}
	return nil
}

// extractZipEntry writes a zip.File to a temporary file and returns its path.
// The caller must remove the temp file when done.
func extractZipEntry(f *zip.File) (string, error) {
	tmp, err := os.CreateTemp("", "mc-inner-*.jar")
	if err != nil {
		return "", err
	}
	rc, err := f.Open()
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", err
	}
	_, err = io.Copy(tmp, rc)
	rc.Close()
	tmp.Close()
	if err != nil {
		os.Remove(tmp.Name())
		return "", err
	}
	return tmp.Name(), nil
}

// matchDeep finds paths containing marker anywhere under data/, regardless of datapack nesting.
// It uses the LAST occurrence of marker, and derives the namespace from the path segment
// immediately before that marker. Handles both:
//   data/<ns>/<marker>/<rel>.json                            (direct)
//   data/<ns>/datapacks/<pack>/data/<ns>/<marker>/<rel>.json (datapack)
func matchDeep(s, marker, suffix string, ns, rel *string) bool {
	if !strings.HasPrefix(s, "data/") || !strings.HasSuffix(s, suffix) {
		return false
	}
	idx := strings.LastIndex(s, marker)
	if idx < 0 {
		return false
	}
	before := s[len("data/"):idx] // e.g. "minecraft" or "minecraft/datapacks/trade_rebalance/data/minecraft"
	lastSlash := strings.LastIndex(before, "/")
	if lastSlash < 0 {
		*ns = before
	} else {
		*ns = before[lastSlash+1:]
	}
	*rel = s[idx+len(marker):]
	return *ns != "" && !strings.Contains(*ns, "/") && *rel != ""
}

// matchPath checks if s has the form "<prefix><ns><middle><rest><suffix>"
// and fills ns and rest if it matches.
// ns must not contain "/" — this prevents subdirectory paths like
// "minecraft/advancement" from being treated as a namespace.
func matchPath(s, prefix, middle, suffix string, ns, rest *string) bool {
	after, ok := strings.CutPrefix(s, prefix)
	if !ok {
		return false
	}
	before, after2, found := strings.Cut(after, middle)
	if !found {
		return false
	}
	if strings.Contains(before, "/") {
		return false // not a direct namespace, e.g. "minecraft/advancement"
	}
	*ns = before
	*rest = after2
	return strings.HasSuffix(*rest, suffix)
}

// ReadTagFile parses a tag JSON file entry.
func ReadTagFile(e WalkEntry) (*TagFileData, error) {
	rc, err := e.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}

	var raw struct {
		Replace bool              `json:"replace"`
		Values  []json.RawMessage `json:"values"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	// Tag name: namespace + ":" + path-without-extension.
	tagPath := strings.TrimSuffix(e.RelPath, ".json")
	tagName := e.Namespace + ":" + tagPath

	td := &TagFileData{TagName: tagName, Replace: raw.Replace}
	for _, v := range raw.Values {
		// Values can be plain strings or {"id": "...", "required": false}.
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			td.Values = append(td.Values, s)
			continue
		}
		var obj struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(v, &obj); err == nil && obj.ID != "" {
			td.Values = append(td.Values, obj.ID)
		}
	}
	return td, nil
}

// ReadLangFile parses a lang JSON file entry.
// Returns the lang code (e.g. "en_us") and the key→name map.
// Returns (lang, nil, nil) for lang files that aren't key-value objects (e.g. deprecated.json arrays).
func ReadLangFile(e WalkEntry) (lang string, entries map[string]string, err error) {
	lang = strings.TrimSuffix(filepath.Base(e.RelPath), ".json")

	rc, err := e.Open()
	if err != nil {
		return "", nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", nil, err
	}

	entries = make(map[string]string)
	if err := json.Unmarshal(data, &entries); err != nil {
		// Not a key-value map (e.g. deprecated.json is an array) — skip silently.
		return lang, nil, nil
	}
	return lang, entries, nil
}

// ReadModMeta extracts (modID, displayName, deps) from a mod metadata file.
// Supports META-INF/neoforge.mods.toml, META-INF/mods.toml (first [[mods]] block),
// and fabric.mod.json / quilt.mod.json.
// deps is the list of mod IDs this mod depends on (required dependencies).
// Returns ("", "", nil, nil) if the file doesn't contain the expected fields.
func ReadModMeta(e WalkEntry) (modID, displayName string, deps []string, err error) {
	rc, err := e.Open()
	if err != nil {
		return "", "", nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", "", nil, err
	}

	base := filepath.Base(e.RelPath)
	switch base {
	case "fabric.mod.json", "quilt.mod.json":
		var m struct {
			ID      string         `json:"id"`
			Name    string         `json:"name"`
			Depends map[string]any `json:"depends"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			return "", "", nil, nil
		}
		for depID := range m.Depends {
			deps = append(deps, depID)
		}
		return m.ID, m.Name, deps, nil

	case "neoforge.mods.toml", "mods.toml":
		modID, displayName, deps = parseFirstModTOML(string(data))
		return modID, displayName, deps, nil
	}
	return "", "", nil, nil
}

// parseFirstModTOML extracts the first modId, displayName, and dependency mod IDs
// from a NeoForge/Forge mods.toml. Uses simple line scanning.
//
// Dependency sections look like:
//
//	[[dependencies.yourmodid]]
//	    modId = "some_dep"
func parseFirstModTOML(content string) (modID, displayName string, deps []string) {
	inDeps := false
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)

		// Detect section headers.
		if strings.HasPrefix(line, "[[dependencies.") {
			inDeps = true
			continue
		}
		if strings.HasPrefix(line, "[[") || (strings.HasPrefix(line, "[") && !strings.HasPrefix(line, "[[")) {
			inDeps = false
		}

		if inDeps {
			if v, ok := tomlStringValue(line, "modId"); ok {
				deps = append(deps, v)
			}
			continue
		}

		if modID == "" {
			if v, ok := tomlStringValue(line, "modId"); ok {
				modID = v
			}
		}
		if displayName == "" {
			if v, ok := tomlStringValue(line, "displayName"); ok {
				displayName = v
			}
		}
	}
	return modID, displayName, deps
}

// tomlStringValue parses a line of the form `key = "value"` or `key="value"`.
func tomlStringValue(line, key string) (string, bool) {
	rest, ok := strings.CutPrefix(line, key)
	if !ok {
		return "", false
	}
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, "=") {
		return "", false
	}
	rest = strings.TrimLeft(rest[1:], " \t")
	if !strings.HasPrefix(rest, `"`) {
		return "", false
	}
	rest = rest[1:]
	val, _, found := strings.Cut(rest, `"`)
	if !found {
		return "", false
	}
	return val, true
}

// ExtractTexture writes a texture from the JAR entry to assetsDir/<ns>/textures/<relPath>.
// Returns the relative path stored in the textures table (e.g. "minecraft/textures/item/iron_ingot.png").
func ExtractTexture(e WalkEntry, assetsDir string) (texPath string, err error) {
	texPath = filepath.Join(e.Namespace, "textures", e.RelPath)
	destPath := filepath.Join(assetsDir, texPath)
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return "", err
	}
	rc, err := e.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	out, err := os.Create(destPath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return texPath, err
}

// ResolveTags resolves sub-tag references (#tag_name) in rawTags to concrete item lists.
// Returns map[tagName][]"mod:item_id".
func ResolveTags(rawTags map[string]*TagFileData) map[string][]string {
	flat := make(map[string][]string, len(rawTags))
	for k, v := range rawTags {
		flat[k] = v.Values
	}
	return ResolveTagsRaw(flat)
}

// ResolveTagsRaw resolves sub-tag references in a plain value map.
// Returns map[tagName][]"mod:item_id".
func ResolveTagsRaw(raw map[string][]string) map[string][]string {
	resolved := make(map[string][]string, len(raw))
	inProgress := make(map[string]bool)

	var resolve func(name string) []string
	resolve = func(name string) []string {
		if items, ok := resolved[name]; ok {
			return items
		}
		if inProgress[name] {
			return nil // cycle guard
		}
		values, ok := raw[name]
		if !ok {
			return nil
		}
		inProgress[name] = true
		defer func() { delete(inProgress, name) }()

		seen := make(map[string]bool)
		var items []string
		for _, v := range values {
			if strings.HasPrefix(v, "#") {
				for _, sub := range resolve(v[1:]) {
					if !seen[sub] {
						seen[sub] = true
						items = append(items, sub)
					}
				}
			} else {
				if !seen[v] {
					seen[v] = true
					items = append(items, v)
				}
			}
		}
		resolved[name] = items
		return items
	}

	for name := range raw {
		resolve(name)
	}
	return resolved
}

// nsIDRe matches valid Minecraft namespace/item-id segments: lowercase letters, digits, underscores, hyphens.
// Uppercase letters, dots, or special chars indicate a non-item lang key (e.g. "canUse", "modifiers").
var nsIDRe = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// ExtractItemsFromLang extracts item/block IDs from a translation map.
// Keys of the form "item.{ns}.{id}" or "block.{ns}.{id}" are extracted.
// Returns map[namespace][]itemID, deduplicated (item.* preferred over block.* for same id).
// Keys with non-lowercase namespace or id (e.g. Mojang UI strings) are ignored.
func ExtractItemsFromLang(entries map[string]string) map[string][]string {
	type key struct{ ns, id string }
	seen := make(map[key]bool)

	addKey := func(k string) {
		var rest string
		if strings.HasPrefix(k, "item.") {
			rest = k[5:]
		} else if strings.HasPrefix(k, "block.") {
			rest = k[6:]
		} else {
			return
		}
		parts := strings.SplitN(rest, ".", 2)
		if len(parts) != 2 {
			return
		}
		ns, id := parts[0], parts[1]
		if !nsIDRe.MatchString(ns) || !nsIDRe.MatchString(id) {
			return
		}
		seen[key{ns, id}] = true
	}

	for k := range entries {
		addKey(k)
	}

	result := make(map[string][]string)
	for kk := range seen {
		result[kk.ns] = append(result[kk.ns], kk.id)
	}
	return result
}
