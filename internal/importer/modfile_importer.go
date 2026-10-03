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

// ErrInvalidModFile marks an import refused because its mod.yml cannot be parsed.
var ErrInvalidModFile = errors.New("invalid modfile")

type invalidModFileError struct{ err error }

func (e invalidModFileError) Error() string        { return e.err.Error() }
func (e invalidModFileError) Unwrap() error        { return e.err }
func (e invalidModFileError) Is(target error) bool { return target == ErrInvalidModFile }

// ModFileResult summarises one modfile ZIP import.
type ModFileResult struct {
	ModID           string   `json:"mod_id"`
	Machines        int      `json:"machines"`
	Recipes         int      `json:"recipes_imported"`
	RecipesSkip     int      `json:"recipes_skipped"`
	Items           int      `json:"items"`
	Fluids          int      `json:"fluids"`
	Energies        int      `json:"energies"`
	Translations    int      `json:"translations"`
	Tags            int      `json:"tags"`
	Textures        int      `json:"textures"`
	BlockDrops      int      `json:"block_drops"`
	VillagerTrades  int      `json:"villager_trades"`
	PluginInstalled bool     `json:"plugin_installed"`
	Warnings        []string `json:"warnings,omitempty"`
}

// RunModFile imports a single modfile ZIP.
func (imp *Importer) RunModFile(ctx context.Context, zipPath string) (ModFileResult, error) {
	data, err := os.ReadFile(zipPath)
	if err != nil {
		return ModFileResult{}, fmt.Errorf("modfile: read zip: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ModFileResult{}, fmt.Errorf("modfile: open zip: %w", err)
	}

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
		return ModFileResult{}, invalidModFileError{err}
	}

	bundle, err := findPluginBundle(zr.File)
	if err != nil {
		return ModFileResult{}, fmt.Errorf("modfile: %w", err)
	}
	var pluginDef *model.PluginDef
	var pluginHasWizard bool
	if bundle != nil {
		pluginDef, err = ParsePluginFile(bundle.PluginYML)
		if err != nil {
			return ModFileResult{}, fmt.Errorf("modfile: %w", err)
		}
		pluginHasWizard, err = validatePluginJS(string(bundle.PluginJS))
		if err != nil {
			return ModFileResult{}, fmt.Errorf("modfile: %w", err)
		}
	}

	res := ModFileResult{ModID: def.ModID}
	warn := func(format string, args ...any) {
		res.Warnings = append(res.Warnings, fmt.Sprintf(format, args...))
	}

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

	if err := imp.db.ReplaceEnergies(ctx, def.ModID, def.Energies); err != nil {
		return res, fmt.Errorf("modfile: replace energies: %w", err)
	}
	res.Energies = len(def.Energies)

	// 6. Tags.
	for _, tag := range def.Tags {
		skipped, err := imp.db.UpsertDirectTagMembers(ctx, def.ModID, tag.Kind, tag.Name, tag.Members)
		if err != nil {
			warn("tag %q: %v", tag.Name, err)
			continue
		}
		if skipped > 0 {
			warn("tag %q: %d of %d members are not in the catalog", tag.Name, skipped, len(tag.Members))
		}
		res.Tags++
	}

	// 7. Machines — resolve name from translations then write.
	enUS := def.Translations["en_us"]
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
	}

	// 7b. Machine interfaces ("A implements B").
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

	// 11.
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "assets/") || f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(f.Name, "assets/")
		dst := filepath.Join(imp.assetsDir, rel)
		// A crafted entry path such as "assets/../../x" escapes the asset tree once filepath.Join
		// cleans it.
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

	// 12. Record the plugin, now that everything else has succeeded.
	if bundle != nil && pluginDef != nil {
		if err := imp.db.UpsertModPlugin(ctx, db.ModPlugin{
			ModID:       def.ModID,
			DisplayName: pluginDef.DisplayName,
			Version:     pluginDef.Version,
			APIVersion:  pluginDef.APIVersion,
			Source:      string(bundle.PluginJS),
			HasWizard:   pluginHasWizard,
			UploadedBy:  imp.UploadedBy,
		}); err != nil {
			return res, fmt.Errorf("modfile: record plugin: %w", err)
		}
		res.PluginInstalled = true
	}

	return res, nil
}

// underDir reports whether path stays inside dir once both are cleaned.
func underDir(dir, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// ModRecipeToNormalized converts a ModRecipeDef to the NormalizedRecipe the DB expects.
func ModRecipeToNormalized(r model.ModRecipeDef, sourceModID string) model.NormalizedRecipe {
	norm := model.NormalizedRecipe{
		SourceFile:  sourceModID + ".yml",
		ModID:       r.MachineModID,
		SourceModID: sourceModID,
		MachineID:   r.MachineID,
		Duration:    r.DurationTicks,
		Shape:       r.Shape,
		ModData:     r.ModData,
	}
	norm.Inputs = r.Inputs
	norm.Outputs = r.Outputs
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
