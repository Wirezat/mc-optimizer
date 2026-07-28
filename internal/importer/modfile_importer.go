package importer

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ModFileResult summarises one modfile ZIP import.
type ModFileResult struct {
	ModID          string   `json:"mod_id"`
	Machines       int      `json:"machines"`
	Recipes        int      `json:"recipes_imported"`
	RecipesSkip    int      `json:"recipes_skipped"`
	Items          int      `json:"items"`
	Fluids         int      `json:"fluids"`
	Translations   int      `json:"translations"`
	Tags           int      `json:"tags"`
	UpgradeTiers   int      `json:"upgrade_tiers"`
	Textures       int      `json:"textures"`
	BlockDrops     int      `json:"block_drops"`
	VillagerTrades int      `json:"villager_trades"`
	Warnings       []string `json:"warnings,omitempty"`
}

// RunModFile imports a single modfile ZIP.
// The ZIP must contain a mod.yml at its root and optionally an assets/ subtree.
func (imp *Importer) RunModFile(ctx context.Context, zipPath string) (ModFileResult, error) {
	data, err := os.ReadFile(zipPath)
	if err != nil {
		return ModFileResult{}, fmt.Errorf("modfile: read zip: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ModFileResult{}, fmt.Errorf("modfile: open zip: %w", err)
	}

	// Find mod.yml
	var yamlData []byte
	for _, f := range zr.File {
		if f.Name == "mod.yml" || f.Name == "mod.yaml" {
			rc, err := f.Open()
			if err != nil {
				return ModFileResult{}, fmt.Errorf("modfile: open mod.yml: %w", err)
			}
			yamlData, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return ModFileResult{}, fmt.Errorf("modfile: read mod.yml: %w", err)
			}
			break
		}
	}
	if yamlData == nil {
		return ModFileResult{}, fmt.Errorf("modfile: no mod.yml found in zip")
	}

	def, err := ParseModFile(yamlData)
	if err != nil {
		return ModFileResult{}, fmt.Errorf("modfile: parse: %w", err)
	}

	res := ModFileResult{ModID: def.ModID}
	warn := func(format string, args ...any) {
		res.Warnings = append(res.Warnings, fmt.Sprintf(format, args...))
	}

	// 1. Modrinth fetch — fills empty fields from Modrinth if slug is set.
	// mod_id is used as slug fallback.
	slug := def.ModrinthSlug
	if slug == "" {
		slug = def.ModID
	}
	if meta, err := FetchModrinthMetadata(def.ModID, def.Name, slug); err != nil {
		log.Printf("modfile: modrinth fetch %s: %v", def.ModID, err)
	} else if meta != nil {
		if def.Description == "" {
			def.Description = meta.Description
		}
		if def.Author == "" {
			def.Author = meta.Author
		}
		if def.License == "" {
			def.License = meta.License
		}
		if def.URLSource == "" {
			def.URLSource = meta.URLSource
		}
		if def.URLModrinth == "" {
			def.URLModrinth = meta.URLModrinth
		}
		if def.URLWiki == "" {
			def.URLWiki = meta.URLWiki
		}
		if def.URLIssues == "" {
			def.URLIssues = meta.URLIssues
		}
		if def.URLDiscord == "" {
			def.URLDiscord = meta.URLDiscord
		}
		if def.ModrinthSlug == "" {
			def.ModrinthSlug = meta.ModrinthSlug
		}
	}

	// 2. Write mod record.
	if err := imp.db.UpsertMod(ctx, *def); err != nil {
		return res, fmt.Errorf("modfile: upsert mod: %w", err)
	}

	// 3. Translations.
	for lang, entries := range def.Translations {
		if err := imp.db.UpsertTranslations(ctx, lang, entries); err != nil {
			return res, fmt.Errorf("modfile: upsert translations %s: %w", lang, err)
		}
		res.Translations += len(entries)
	}

	// 4. Items.
	if err := imp.db.BulkUpsertItems(ctx, def.ModID, def.Items); err != nil {
		return res, fmt.Errorf("modfile: upsert items: %w", err)
	}
	res.Items = len(def.Items)

	// 5. Fluids.
	fluidIDs := make([]string, 0, len(def.Fluids))
	for _, f := range def.Fluids {
		fluidIDs = append(fluidIDs, f.FluidID)
	}
	if err := imp.db.UpsertFluids(ctx, def.ModID, fluidIDs); err != nil {
		return res, fmt.Errorf("modfile: upsert fluids: %w", err)
	}
	res.Fluids = len(fluidIDs)

	// 6. Tags.
	for _, tag := range def.Tags {
		if err := imp.db.UpsertDirectTagMembers(ctx, tag.Name, tag.Members); err != nil {
			warn("tag %q: %v", tag.Name, err)
			continue
		}
		res.Tags++
	}

	// 6b. Upgrade tiers.
	for _, ut := range def.UpgradeTiers {
		if err := imp.db.UpsertUpgradeTier(ctx, def.ModID, ut.Name, ut.EUBonusPerSlot, ut.ItemRef); err != nil {
			warn("upgrade tier %q: %v", ut.Name, err)
			continue
		}
		res.UpgradeTiers++
	}

	// 7. Machines — resolve name from translations then write.
	enUS := def.Translations["en_us"]
	miModIDs := []string{}
	isVanilla := false
	for _, m := range def.Machines {
		name := m.LangKey
		if enUS != nil {
			if translated, ok := enUS[m.LangKey]; ok {
				name = translated
			}
		}
		m.Name = name
		if err := imp.db.UpsertMachineType(ctx, m); err != nil {
			warn("machine %s: %v", m.MachineID, err)
			continue
		}
		res.Machines++

		// Collect MI-ecosystem mods for post-processing.
		if m.Ecosystem == "modern_industrialization" {
			miModIDs = append(miModIDs, def.ModID)
		}
		if m.Ecosystem == "vanilla" {
			isVanilla = true
		}
	}

	// Vanilla post-processing: sets energy_type='fuel' on furnace/smoker/blast_furnace
	// (was silently blank before — display-only, doesn't affect solving, but wrong).
	if isVanilla {
		if err := imp.db.SeedVanillaMachineSlots(ctx); err != nil {
			warn("vanilla machine slots: %v", err)
		}
	}

	// 7b. Machine interfaces ("A implements B") — run after ALL machines are
	// upserted, since AddMachineInterface requires both sides to already exist.
	for _, m := range def.Machines {
		for _, ref := range m.Implements {
			baseModID, baseMachineID, err := splitRef(ref, def.ModID)
			if err != nil {
				warn("machine %s implements %q: %v", m.MachineID, ref, err)
				continue
			}
			if err := imp.db.AddMachineInterface(ctx, def.ModID, m.MachineID, baseModID, baseMachineID); err != nil {
				if !errors.Is(err, db.ErrConflict) {
					warn("machine interface %s -> %s:%s: %v", m.MachineID, baseModID, baseMachineID, err)
				}
			}
		}
	}

	// MI post-processing: default energy_type to 'eu' where the yaml didn't
	// override it. Deliberately NOT calling SetMachinesUpgradable here — that
	// blanket "upgradable=true for every eu machine" used to clobber the
	// per-machine `upgradable: false` already curated in the yaml for steam
	// tiers (bronze/steel) and fixed multiblocks (vacuum_freezer, etc). The
	// yaml's own value (already written by UpsertMachineType above) is authoritative.
	if len(miModIDs) > 0 {
		if err := imp.db.SetMIEnergyType(ctx, miModIDs); err != nil {
			warn("MI energy type: %v", err)
		}
	}

	// 8. Recipes.
	for _, r := range def.Recipes {
		norm := ModRecipeToNormalized(r, def.ModID)
		imported, err := imp.db.ImportRecipe(ctx, norm)
		if err != nil {
			warn("recipe %s:%s: %v", r.MachineModID, r.MachineID, err)
			res.RecipesSkip++
			continue
		}
		if imported {
			res.Recipes++
		} else {
			res.RecipesSkip++
		}
	}

	// 9. Block drops.
	if err := imp.db.UpsertBlockDrops(ctx, def.BlockDrops); err != nil {
		warn("block drops: %v", err)
	}
	res.BlockDrops = len(def.BlockDrops)

	// 10. Villager trades.
	if err := imp.db.UpsertVillagerTrades(ctx, def.VillagerTrades); err != nil {
		warn("villager trades: %v", err)
	}
	res.VillagerTrades = len(def.VillagerTrades)

	// 11. Extract assets from assets/{mod_id}/textures/** — strip the leading
	// "assets/" prefix so files land at <assetsDir>/<mod_id>/... instead of
	// doubling it to <assetsDir>/assets/<mod_id>/... (imp.assetsDir is itself
	// the "assets" directory).
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "assets/") || f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(f.Name, "assets/")
		dst := filepath.Join(imp.assetsDir, rel)
		// A zip entry names its own path, so a crafted "assets/../../x" would
		// have filepath.Join clean its way out of the asset tree and write
		// anywhere the process can reach. Refuse rather than warn: an entry
		// that tries this is not a mistake.
		if !underDir(imp.assetsDir, dst) {
			warn("asset %s: path escapes the assets directory, skipped", f.Name)
			continue
		}
		if err := extractZipFile(f, dst); err != nil {
			warn("asset %s: %v", f.Name, err)
			continue
		}
		res.Textures++
	}

	return res, nil
}

// underDir reports whether path stays inside dir once both are cleaned, so a
// zip entry cannot write outside the tree it is supposed to land in.
func underDir(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ModRecipeToNormalized converts a ModRecipeDef to the NormalizedRecipe the DB expects.
// sourceModID is the modfile the recipe was defined in, which may differ from r.MachineModID.
func ModRecipeToNormalized(r model.ModRecipeDef, sourceModID string) model.NormalizedRecipe {
	norm := model.NormalizedRecipe{
		SourceFile:  sourceModID + ".yml",
		ModID:       r.MachineModID,
		SourceModID: sourceModID,
		MachineID:   r.MachineID,
		Duration:    r.DurationTicks,
		Shape:       r.Shape,
	}
	if r.EnergyPerTick != nil {
		norm.EUPerTick = *r.EnergyPerTick
	}
	for _, io := range r.ItemInputs {
		norm.ItemInputs = append(norm.ItemInputs, model.NormalizedIO{
			ModID:     io.ItemModID,
			ID:        io.ItemID,
			TagName:   io.TagName,
			AmountNum: io.AmountNum,
			AmountDen: io.AmountDen,
			ProbNum:   io.ProbNum,
			ProbDen:   io.ProbDen,
		})
	}
	for _, io := range r.ItemOutputs {
		norm.ItemOutputs = append(norm.ItemOutputs, model.NormalizedIO{
			ModID:     io.ItemModID,
			ID:        io.ItemID,
			TagName:   io.TagName,
			AmountNum: io.AmountNum,
			AmountDen: io.AmountDen,
			ProbNum:   io.ProbNum,
			ProbDen:   io.ProbDen,
		})
	}
	for _, io := range r.FluidInputs {
		norm.FluidInputs = append(norm.FluidInputs, model.NormalizedIO{
			ModID:    io.FluidModID,
			ID:       io.FluidID,
			TagName:  io.TagName,
			AmountMB: io.AmountMB,
			ProbNum:  io.ProbNum,
			ProbDen:  io.ProbDen,
		})
	}
	for _, io := range r.FluidOutputs {
		norm.FluidOutputs = append(norm.FluidOutputs, model.NormalizedIO{
			ModID:    io.FluidModID,
			ID:       io.FluidID,
			TagName:  io.TagName,
			AmountMB: io.AmountMB,
			ProbNum:  io.ProbNum,
			ProbDen:  io.ProbDen,
		})
	}
	norm.ContentHash = ContentHash(norm)
	return norm
}

func extractZipFile(f *zip.File, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	rc, err := f.Open()
	if err != nil {
		return err
	}
	defer rc.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
}
