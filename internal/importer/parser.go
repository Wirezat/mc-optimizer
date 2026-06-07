package importer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// ParseFile reads and parses a single MI recipe JSON file.
func ParseFile(path string) (*MIRecipe, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	rec, err := ParseBytes(data)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	rec.SourceFile = filepath.Base(path)
	return rec, nil
}

// ParseBytes parses raw JSON bytes into an MIRecipe.
// Vanilla recipe types (minecraft:smelting, crafting_shaped, etc.) are transparently
// converted to the MI field layout so the rest of the pipeline stays unchanged.
func ParseBytes(data []byte) (*MIRecipe, error) {
	var rec MIRecipe
	if err := json.Unmarshal(data, &rec); err != nil {
		return nil, err
	}
	if isVanillaType(rec.Type) {
		if err := convertVanillaInPlace(data, &rec); err != nil {
			return nil, err
		}
	}
	return &rec, nil
}

// ParseDir walks dir recursively and parses every *.json file found.
func ParseDir(dir string) ([]*MIRecipe, []ParseError) {
	var recipes []*MIRecipe
	var errs []ParseError

	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		rec, parseErr := ParseFile(path)
		if parseErr != nil {
			errs = append(errs, ParseError{File: filepath.Base(path), Err: parseErr})
			return nil
		}
		recipes = append(recipes, rec)
		return nil
	})
	if err != nil {
		errs = append(errs, ParseError{File: dir, Err: err})
	}
	return recipes, errs
}

// ParseError records a parse failure for a specific file.
type ParseError struct {
	File string
	Err  error
}

// ValidateFormat checks minimum required fields.
// Returns a list of human-readable problems; empty = valid.
func ValidateFormat(rec *MIRecipe) []string {
	var problems []string

	if rec.Type == "" {
		problems = append(problems, "missing type field")
	} else if _, _, err := SplitTypeField(rec.Type); err != nil {
		problems = append(problems, err.Error())
	}
	if rec.Duration <= 0 {
		problems = append(problems, fmt.Sprintf("duration must be > 0, got %d", rec.Duration))
	}
	if rec.EU < 0 {
		problems = append(problems, fmt.Sprintf("eu must be >= 0, got %d", rec.EU))
	}
	if len(rec.ItemOutputs) == 0 && len(rec.FluidOutputs) == 0 {
		problems = append(problems, "recipe must have at least one output")
	}
	for i, io := range rec.ItemInputs {
		problems = append(problems, validateItemIO(io, fmt.Sprintf("item_inputs[%d]", i))...)
	}
	for i, io := range rec.ItemOutputs {
		problems = append(problems, validateItemIO(io, fmt.Sprintf("item_outputs[%d]", i))...)
	}
	for i, io := range rec.FluidInputs {
		problems = append(problems, validateFluidIO(io, fmt.Sprintf("fluid_inputs[%d]", i))...)
	}
	for i, io := range rec.FluidOutputs {
		problems = append(problems, validateFluidIO(io, fmt.Sprintf("fluid_outputs[%d]", i))...)
	}
	return problems
}

func validateItemIO(io RawIO, prefix string) []string {
	var p []string
	if io.Item == "" && io.Tag == "" {
		p = append(p, prefix+": must have item or tag")
	} else if io.Item != "" {
		if _, _, err := SplitTypeField(io.Item); err != nil {
			p = append(p, prefix+".item: "+err.Error())
		}
	}
	if io.Amount <= 0 {
		p = append(p, fmt.Sprintf("%s.amount must be > 0, got %d", prefix, io.Amount))
	}
	return p
}

func validateFluidIO(io RawIO, prefix string) []string {
	var p []string
	if io.Fluid == "" {
		p = append(p, prefix+": missing fluid field")
	} else if _, _, err := SplitTypeField(io.Fluid); err != nil {
		p = append(p, prefix+".fluid: "+err.Error())
	}
	if io.Amount <= 0 {
		p = append(p, fmt.Sprintf("%s.amount must be > 0, got %d", prefix, io.Amount))
	}
	return p
}

// Normalize converts a validated MIRecipe to a model.NormalizedRecipe.
func Normalize(rec *MIRecipe, resolvedModID, resolvedMachineID string) model.NormalizedRecipe {
	norm := model.NormalizedRecipe{
		SourceFile: rec.SourceFile,
		RecipeType: rec.Type,
		ModID:      resolvedModID,
		MachineID:  resolvedMachineID,
		EUPerTick:  rec.EU,
		Duration:   rec.Duration,
	}
	for _, io := range rec.ItemInputs {
		norm.ItemInputs = append(norm.ItemInputs, toItemIO(io))
	}
	for _, io := range rec.ItemOutputs {
		norm.ItemOutputs = append(norm.ItemOutputs, toItemIO(io))
	}
	for _, io := range rec.FluidInputs {
		norm.FluidInputs = append(norm.FluidInputs, toFluidIO(io))
	}
	for _, io := range rec.FluidOutputs {
		norm.FluidOutputs = append(norm.FluidOutputs, toFluidIO(io))
	}
	norm.ContentHash = ContentHash(norm)
	return norm
}

func toItemIO(io RawIO) model.NormalizedItemIO {
	n := model.NormalizedItemIO{AmountNum: io.Amount, AmountDen: 1}
	prob := io.Probability
	if prob == 0 {
		prob = 1.0
	}
	n.ProbNum, n.ProbDen = ProbToRational(prob)
	if io.Tag != "" {
		n.TagName = &io.Tag
	} else {
		parts := strings.SplitN(io.Item, ":", 2)
		modID, itemID := parts[0], parts[1]
		n.ItemModID = &modID
		n.ItemID = &itemID
	}
	return n
}

func toFluidIO(io RawIO) model.NormalizedFluidIO {
	parts := strings.SplitN(io.Fluid, ":", 2)
	prob := io.Probability
	if prob == 0 {
		prob = 1.0
	}
	probNum, probDen := ProbToRational(prob)
	return model.NormalizedFluidIO{
		FluidModID: parts[0],
		FluidID:    parts[1],
		AmountMB:   int64(io.Amount),
		ProbNum:    probNum,
		ProbDen:    probDen,
	}
}
