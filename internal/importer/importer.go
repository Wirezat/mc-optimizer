package importer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// ImporterDB is the subset of the database the JAR importer needs.
type ImporterDB interface {
	ImportRecipe(ctx context.Context, rec model.NormalizedRecipe) (bool, error)
	UpsertTranslations(ctx context.Context, lang string, entries map[string]string) error
	UpsertTagValues(ctx context.Context, tagName string, values []string, replace bool) error
	LoadAllTagValues(ctx context.Context) (map[string][]string, error)
	UpsertTagMembers(ctx context.Context, resolved map[string][]string) error
	BulkUpsertItems(ctx context.Context, modID string, itemIDs []string) error
	MirrorFluidTranslations(ctx context.Context) error
	UpsertBlockDrops(ctx context.Context, drops []model.BlockDrop) error
	UpsertVillagerTrades(ctx context.Context, trades []model.VillagerTrade) error
	UpsertUpgradeTiers(ctx context.Context, tiers []model.UpgradeTier) error
	SetMIEnergyType(ctx context.Context, modIDs []string) error
	SetMISteamMachines(ctx context.Context, modIDs []string, machineIDs []string) error
	SetMachinesUpgradable(ctx context.Context, modIDs []string) error
	LocalizeMachineNames(ctx context.Context) error
	SeedVanillaMachineSlots(ctx context.Context) error
	UpdateModDisplayNames(ctx context.Context, names map[string]string) error
	UpdateModMetadata(ctx context.Context, meta model.ModMetadata) error
}

// SkipWarning describes a group of recipes that were skipped because their type is not auto-importable.
type SkipWarning struct {
	Code       string `json:"code"`
	RecipeType string `json:"recipe_type"`
	Count      int    `json:"count"`
}

// FieldWarning describes a recipe file that contains JSON fields the parser doesn't handle.
type FieldWarning struct {
	RecipeType    string   `json:"recipe_type"`
	SourceFile    string   `json:"source_file"`
	UnknownFields []string `json:"unknown_fields"`
	RawJSON       string   `json:"raw_json"`
}

// Result summarises the outcome of importing one or more JARs.
type Result struct {
	JARs           []string       `json:"jars"`
	Recipes        int            `json:"recipes_imported"`
	RecipesSkip    int            `json:"recipes_skipped"`
	Items          int            `json:"items_seeded"`
	Translations   int            `json:"translations"`
	Tags           int            `json:"tags"`
	TagMembers     int            `json:"tag_members"`
	Textures       int            `json:"textures"`
	BlockDrops     int            `json:"block_drops"`
	VillagerTrades int            `json:"villager_trades"`
	UpgradeTiers   int            `json:"upgrade_tiers"`
	Warnings       []string       `json:"warnings,omitempty"`
	SkipWarnings   []SkipWarning  `json:"skip_warnings,omitempty"`
	FieldWarnings  []FieldWarning `json:"field_warnings,omitempty"`
}

// Importer reads data from JAR files and writes it to the database.
type Importer struct {
	db        ImporterDB
	assetsDir string
}

// New creates an Importer.
func New(db ImporterDB, assetsDir string) *Importer {
	return &Importer{db: db, assetsDir: assetsDir}
}

// Run imports all provided JAR files.
func (imp *Importer) Run(ctx context.Context, jarPaths []string) (Result, error) {
	res := Result{JARs: jarPaths}

	// Determine which mods are present in this import set before importing any
	// recipes. Conditional recipe-type mappings (e.g. vanilla smelting also
	// running in the MI furnace) must only apply when the target mod is
	// actually being imported — otherwise we'd create recipes for machines
	// that don't exist. minecraft is always considered present.
	presentMods := map[string]bool{"minecraft": true}
	for _, jarPath := range jarPaths {
		_ = WalkJAR(jarPath, func(e WalkEntry) error {
			if e.Category != "mod_meta" {
				return nil
			}
			if mid, _, _, err := ReadModMeta(e); err == nil && mid != "" {
				presentMods[mid] = true
			}
			return nil
		})
	}

	rawTags := make(map[string]*TagFileData)

	// langData accumulates ALL language entries keyed by lang code.
	// We keep en_us for item seeding; others are written to DB.
	langData := make(map[string]map[string]string)

	// modDisplayNames accumulates mod_id → display name from mod metadata files.
	modDisplayNames := make(map[string]string)
	// modDeps accumulates mod_id → list of dependency mod IDs.
	modDeps := make(map[string][]string)

	// blockDrops, villager trades, and upgrade tiers are accumulated across all JARs.
	var allDrops []model.BlockDrop
	var allTrades []model.VillagerTrade
	// upgradeTiers is keyed by "mod_id:item_id" to deduplicate across JARs.
	upgradeTierMap := make(map[string]model.UpgradeTier)


	for _, jarPath := range jarPaths {
		warn := func(format string, args ...any) {
			res.Warnings = append(res.Warnings, fmt.Sprintf("["+jarPath+"] "+format, args...))
		}

		skippedUnsupported := map[string]int{}

		err := WalkJAR(jarPath, func(e WalkEntry) error {
			switch e.Category {
			case "recipe":
				imported, skip, fw, w := imp.importRecipe(ctx, e, presentMods)
				if fw != nil {
					res.FieldWarnings = append(res.FieldWarnings, *fw)
				}
				if skip && w != "" {
					skippedUnsupported[w]++
					res.RecipesSkip++
					return nil
				}
				if w != "" {
					warn("%s", w)
					return nil
				}
				if skip {
					res.RecipesSkip++
				} else if imported {
					res.Recipes++
				} else {
					res.RecipesSkip++
				}

			case "tag":
				td, err := ReadTagFile(e)
				if err != nil {
					warn("tag parse %s: %v", e.FullPath, err)
					return nil
				}
				if existing, ok := rawTags[td.TagName]; ok && !td.Replace {
					existing.Values = append(existing.Values, td.Values...)
				} else {
					rawTags[td.TagName] = td
				}

			case "lang":
				lang, entries, err := ReadLangFile(e)
				if err != nil {
					warn("lang parse %s: %v", e.FullPath, err)
					return nil
				}
				if entries == nil {
					return nil // non-map lang file (e.g. deprecated.json), skip silently
				}
				if existing, ok := langData[lang]; ok {
					for k, v := range entries {
						existing[k] = v
					}
				} else {
					langData[lang] = entries
				}

			case "texture":
				if _, err := ExtractTexture(e, imp.assetsDir); err != nil {
					warn("texture %s: %v", e.FullPath, err)
					return nil
				}
				res.Textures++

			case "loot_block":
				rc, err := e.Open()
				if err != nil {
					warn("loot open %s: %v", e.FullPath, err)
					return nil
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					warn("loot read %s: %v", e.FullPath, err)
					return nil
				}
				// relPath is "<block_id>.json" or "<sub/dir/block_id>.json"
				blockID := strings.TrimSuffix(e.RelPath, ".json")
				drops := ParseBlockLoot(data, e.Namespace, blockID)
				allDrops = append(allDrops, drops...)

			case "villager_trade":
				rc, err := e.Open()
				if err != nil {
					warn("trade open %s: %v", e.FullPath, err)
					return nil
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					warn("trade read %s: %v", e.FullPath, err)
					return nil
				}
				trades := ParseVillagerTrade(data, e.RelPath)
				allTrades = append(allTrades, trades...)

			case "machine_datamap":
				rc, err := e.Open()
				if err != nil {
					warn("datamap open %s: %v", e.FullPath, err)
					return nil
				}
				data, err := io.ReadAll(rc)
				rc.Close()
				if err != nil {
					warn("datamap read %s: %v", e.FullPath, err)
					return nil
				}
				tiers, parseErr := parseMachineUpgradesDatamap(data, e.Namespace)
				if parseErr != nil {
					warn("datamap parse %s: %v", e.FullPath, parseErr)
					return nil
				}
				for _, t := range tiers {
					upgradeTierMap[t.ItemModID+":"+t.ItemID] = t
				}

			case "mod_meta":
				mid, dname, deps, err := ReadModMeta(e)
				if err != nil {
					warn("mod_meta read %s: %v", e.FullPath, err)
					return nil
				}
				if mid != "" && dname != "" {
					modDisplayNames[mid] = dname
				}
				if mid != "" && len(deps) > 0 {
					modDeps[mid] = append(modDeps[mid], deps...)
				}
			}
			return nil
		})
		if err != nil {
			return res, fmt.Errorf("importer: walk jar %s: %w", jarPath, err)
		}
		for recipeType, count := range skippedUnsupported {
			res.SkipWarnings = append(res.SkipWarnings, SkipWarning{
				Code:       "unsupported_type",
				RecipeType: recipeType,
				Count:      count,
			})
		}
	}

	// Flush all lang data to DB and seed items from en_us.
	for lang, entries := range langData {
		if err := imp.db.UpsertTranslations(ctx, lang, entries); err != nil {
			return res, fmt.Errorf("importer: upsert translations %s: %w", lang, err)
		}
		res.Translations += len(entries)
	}

	// Mirror fluid translations: block.* keys that match a fluid are also written as fluid.* keys.
	if err := imp.db.MirrorFluidTranslations(ctx); err != nil {
		return res, fmt.Errorf("importer: mirror fluid translations: %w", err)
	}

	// Seed items from all language files (use union of all namespaces found).
	// en_us is primary; fall back to any available language.
	seedLang := langData["en_us"]
	if seedLang == nil {
		for _, v := range langData {
			seedLang = v
			break
		}
	}
	if seedLang != nil {
		byNS := ExtractItemsFromLang(seedLang)
		for ns, ids := range byNS {
			if err := imp.db.BulkUpsertItems(ctx, ns, ids); err != nil {
				return res, fmt.Errorf("importer: seed items for %s: %w", ns, err)
			}
			res.Items += len(ids)
		}
	}

	// Register tag raw values.
	for name, td := range rawTags {
		if err := imp.db.UpsertTagValues(ctx, name, td.Values, td.Replace); err != nil {
			return res, fmt.Errorf("importer: upsert tag values for %q: %w", name, err)
		}
	}

	// Resolve tags globally (including prior imports).
	allRaw, err := imp.db.LoadAllTagValues(ctx)
	if err != nil {
		return res, fmt.Errorf("importer: load all tag values: %w", err)
	}
	resolvedTags := ResolveTagsRaw(allRaw)
	if err := imp.db.UpsertTagMembers(ctx, resolvedTags); err != nil {
		return res, fmt.Errorf("importer: upsert tag members: %w", err)
	}
	res.Tags = len(resolvedTags)
	for _, items := range resolvedTags {
		res.TagMembers += len(items)
	}

	// Store block drops (items must exist — seeded above).
	if err := imp.db.UpsertBlockDrops(ctx, allDrops); err != nil {
		return res, fmt.Errorf("importer: upsert block drops: %w", err)
	}
	res.BlockDrops = len(allDrops)

	// Store villager trades.
	if err := imp.db.UpsertVillagerTrades(ctx, allTrades); err != nil {
		return res, fmt.Errorf("importer: upsert villager trades: %w", err)
	}
	res.VillagerTrades = len(allTrades)

	// Store upgrade tiers.
	allTiers := make([]model.UpgradeTier, 0, len(upgradeTierMap))
	for _, t := range upgradeTierMap {
		allTiers = append(allTiers, t)
	}
	if err := imp.db.UpsertUpgradeTiers(ctx, allTiers); err != nil {
		return res, fmt.Errorf("importer: upsert upgrade tiers: %w", err)
	}
	res.UpgradeTiers = len(allTiers)

	// Update mod display names from mod metadata files (only if not already manually set).
	if err := imp.db.UpdateModDisplayNames(ctx, modDisplayNames); err != nil {
		return res, fmt.Errorf("importer: update mod display names: %w", err)
	}

	// Enrich mods with metadata from Modrinth (best-effort, errors are logged not fatal).
	imp.EnrichModsFromModrinth(ctx, modDisplayNames)

	// Mark EU machines as upgradable for mods that are or depend on Modern Industrialization.
	miRelated := []string{"modern_industrialization"}
	for modID, deps := range modDeps {
		for _, dep := range deps {
			if dep == "modern_industrialization" {
				miRelated = append(miRelated, modID)
				break
			}
		}
	}
	if err := imp.db.SetMIEnergyType(ctx, miRelated); err != nil {
		return res, fmt.Errorf("importer: set MI energy type: %w", err)
	}
	// coke_oven and steam_blast_furnace are steam-only multiblocks — no EU, no upgrades.
	miSteamMachines := []string{"coke_oven", "steam_blast_furnace"}
	if err := imp.db.SetMISteamMachines(ctx, miRelated, miSteamMachines); err != nil {
		return res, fmt.Errorf("importer: set MI steam machines: %w", err)
	}
	if err := imp.db.SetMachinesUpgradable(ctx, miRelated); err != nil {
		return res, fmt.Errorf("importer: set machines upgradable: %w", err)
	}

	if err := imp.db.LocalizeMachineNames(ctx); err != nil {
		return res, fmt.Errorf("importer: localize machine names: %w", err)
	}

	// Seed vanilla machine slot definitions (idempotent).
	if err := imp.db.SeedVanillaMachineSlots(ctx); err != nil {
		return res, fmt.Errorf("importer: seed machine slots: %w", err)
	}

	return res, nil
}

// importRecipe parses and imports one recipe entry.
// Returns (imported, skipped, fieldWarning, warning).
func (imp *Importer) importRecipe(ctx context.Context, e WalkEntry, presentMods map[string]bool) (imported, skipped bool, fw *FieldWarning, warn string) {
	rc, err := e.Open()
	if err != nil {
		return false, false, nil, fmt.Sprintf("open %s: %v", e.FullPath, err)
	}
	data, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return false, false, nil, fmt.Sprintf("read %s: %v", e.FullPath, err)
	}

	recipeType := peekType(data)

	if strings.HasSuffix(recipeType, ":forge_hammer") {
		return false, true, nil, recipeType
	}

	for _, parser := range parsers {
		if parser.Skip(recipeType) {
			return false, true, nil, ""
		}
		if !parser.Accepts(recipeType) {
			continue
		}
		mappings, ok := ResolveRecipeType(recipeType)
		if !ok {
			return false, false, nil, fmt.Sprintf("no machine mapping for %q in %s", recipeType, e.FullPath)
		}
		// A recipe type can belong to several machines (e.g. vanilla smelting
		// is consumed by both the furnace and the MI furnace). Emit one recipe
		// per mapping; the recipe counts as imported if any mapping imported it.
		// Skip cross-mod mappings whose target mod isn't part of this import set;
		// the recipe's own namespace is always kept as the primary resolution.
		for _, m := range mappings {
			if m.modID != e.Namespace && !presentMods[m.modID] {
				continue
			}
			norm, err := parser.Decode(data, m.modID, m.machineID, e.FullPath)
			if err != nil {
				return false, false, nil, fmt.Sprintf("decode %s: %v", e.FullPath, err)
			}
			didImport, err := imp.db.ImportRecipe(ctx, norm)
			if err != nil {
				return false, false, nil, fmt.Sprintf("db error for %s: %v", e.FullPath, err)
			}
			if didImport {
				imported = true
			}
		}
		if unknown := unknownFields(data, parser); len(unknown) > 0 {
			var pretty []byte
			if p, err2 := json.MarshalIndent(json.RawMessage(data), "", "  "); err2 == nil {
				pretty = p
			} else {
				pretty = data
			}
			fw = &FieldWarning{
				RecipeType:    recipeType,
				SourceFile:    e.FullPath,
				UnknownFields: unknown,
				RawJSON:       string(pretty),
			}
		}
		return imported, false, fw, ""
	}

	return false, false, nil, fmt.Sprintf("unknown type %q in %s", recipeType, e.FullPath)
}

// unknownFields returns top-level JSON keys in data that are not in parser.KnownFields().
func unknownFields(data []byte, parser RecipeParser) []string {
	known := make(map[string]struct{}, 16)
	for _, k := range parser.KnownFields() {
		known[k] = struct{}{}
	}
	var raw map[string]json.RawMessage
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	var result []string
	for k := range raw {
		if _, ok := known[k]; !ok {
			result = append(result, k)
		}
	}
	return result
}

// parseMachineUpgradesDatamap parses a machine_upgrades.json datamap file.
// Format: { "values": { "mod:item_id": { "extraMaxEu": N } } }
func parseMachineUpgradesDatamap(data []byte, _ string) ([]model.UpgradeTier, error) {
	var raw struct {
		Values map[string]struct {
			ExtraMaxEu int64 `json:"extraMaxEu"`
		} `json:"values"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	tiers := make([]model.UpgradeTier, 0, len(raw.Values))
	for itemRef, v := range raw.Values {
		colon := strings.IndexByte(itemRef, ':')
		if colon <= 0 || colon == len(itemRef)-1 {
			continue // malformed ref
		}
		modID := itemRef[:colon]
		itemID := itemRef[colon+1:]
		tiers = append(tiers, model.UpgradeTier{
			ModID:          modID,
			Name:           itemID,
			EUBonusPerSlot: v.ExtraMaxEu,
			ItemModID:      modID,
			ItemID:         itemID,
		})
	}
	return tiers, nil
}

// peekType extracts the "type" field from a recipe JSON without full unmarshaling.
func peekType(data []byte) string {
	var t struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(data, &t)
	return t.Type
}
