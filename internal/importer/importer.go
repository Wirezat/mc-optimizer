package importer

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// RecipeDB is the subset of the database the Importer needs.
// *db.DB satisfies this interface automatically.
type RecipeDB interface {
	ListValidRecipeTypes(ctx context.Context) ([]*model.ValidRecipeType, error)
	ImportRecipe(ctx context.Context, rec model.NormalizedRecipe) (bool, error)
}

// Result summarises the outcome of a batch import.
type Result struct {
	Imported int     `json:"imported"`
	Skipped  int     `json:"skipped"`
	Errors   []Error `json:"errors"`
}

// Error records a single recipe-level problem during import.
type Error struct {
	File    string `json:"file,omitempty"`
	Code    string `json:"error"`
	Message string `json:"message"`
}

// Importer validates and writes MI recipes to the database.
type Importer struct {
	db RecipeDB
}

// New creates a new Importer backed by the given database.
func New(database RecipeDB) *Importer {
	return &Importer{db: database}
}

// Run imports a batch of raw recipes.
// For each recipe it: validates format → resolves machine type → normalizes → writes to DB.
func (imp *Importer) Run(ctx context.Context, recipes []*MIRecipe) (Result, error) {
	// Load valid recipe types once for the whole batch.
	vrts, err := imp.db.ListValidRecipeTypes(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("importer: load valid recipe types: %w", err)
	}
	matcher := buildMatcher(vrts)

	var res Result
	for _, raw := range recipes {
		file := raw.SourceFile

		// 1. Format validation.
		if problems := ValidateFormat(raw); len(problems) != 0 {
			res.Errors = append(res.Errors, Error{
				File:    file,
				Code:    "INVALID_RECIPE_FORMAT",
				Message: fmt.Sprintf("format errors in %s: %s", file, strings.Join(problems, "; ")),
			})
			res.Skipped++
			continue
		}

		// 2. Resolve machine type.
		targetMod, targetMachine, ok := matcher.resolve(raw.Type)
		if !ok {
			modID, machineID, _ := SplitTypeField(raw.Type)
			res.Errors = append(res.Errors, Error{
				File:    file,
				Code:    "UNKNOWN_RECIPE_TYPE",
				Message: fmt.Sprintf("unknown recipe type %q — register via POST /api/valid-recipe-types (mod_id=%s, machine_id=%s)", raw.Type, modID, machineID),
			})
			res.Skipped++
			continue
		}

		// 3. Normalize.
		norm := Normalize(raw, targetMod, targetMachine)

		// 4. Write to DB.
		imported, err := imp.db.ImportRecipe(ctx, norm)
		if err != nil {
			res.Errors = append(res.Errors, Error{
				File:    file,
				Code:    "INTERNAL_SERVER_ERROR",
				Message: fmt.Sprintf("db error for %s: %v", file, err),
			})
			res.Skipped++
			continue
		}
		if imported {
			res.Imported++
		} else {
			res.Skipped++ // duplicate (same content_hash)
		}
	}
	return res, nil
}

type matcher struct {
	exact   map[string]resolved // pattern → resolved machine
	regexes []regexEntry
}

type resolved struct {
	modID     string
	machineID string
}

type regexEntry struct {
	re        *regexp.Regexp
	targetMod string // empty = use namespace from match
	targetMachine string
}

func buildMatcher(vrts []*model.ValidRecipeType) matcher {
	m := matcher{exact: make(map[string]resolved)}
	for _, v := range vrts {
		if v.IsRegex {
			re, err := regexp.Compile(v.Pattern)
			if err != nil {
				continue // skip invalid regex entries silently
			}
			var tMod, tMachine string
			if v.TargetModID != nil {
				tMod = *v.TargetModID
			}
			if v.TargetMachineID != nil {
				tMachine = *v.TargetMachineID
			}
			m.regexes = append(m.regexes, regexEntry{re, tMod, tMachine})
		} else {
			var tMod, tMachine string
			if v.TargetModID != nil {
				tMod = *v.TargetModID
			}
			if v.TargetMachineID != nil {
				tMachine = *v.TargetMachineID
			}
			m.exact[v.Pattern] = resolved{tMod, tMachine}
		}
	}
	return m
}

// resolve returns the canonical (modID, machineID) for a recipe type string.
// If target fields were empty, falls back to parsing them from the type itself.
func (m matcher) resolve(recipeType string) (modID, machineID string, ok bool) {
	// Exact match first.
	if r, found := m.exact[recipeType]; found {
		return resolveTarget(r, recipeType)
	}
	// Try regex entries in order.
	for _, entry := range m.regexes {
		if entry.re.MatchString(recipeType) {
			r := resolved{entry.targetMod, entry.targetMachine}
			return resolveTarget(r, recipeType)
		}
	}
	return "", "", false
}

// resolveTarget fills in mod/machine from the recipe type string if the entry has no explicit target.
func resolveTarget(r resolved, recipeType string) (modID, machineID string, ok bool) {
	if r.modID != "" && r.machineID != "" {
		return r.modID, r.machineID, true
	}
	// Auto-lookup: use the namespace and machine from the type field directly.
	mod, machine, err := SplitTypeField(recipeType)
	if err != nil {
		return "", "", false
	}
	if r.modID != "" {
		mod = r.modID
	}
	if r.machineID != "" {
		machine = r.machineID
	}
	return mod, machine, true
}
