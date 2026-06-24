package importer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// MIParser handles Modern Industrialization datapack recipes.
type MIParser struct{}

func (p *MIParser) Skip(_ string) bool { return false }

func (p *MIParser) KnownFields() []string {
	return []string{
		"type", "eu", "duration", "item_inputs", "item_outputs", "fluid_inputs", "fluid_outputs",
		"ingredient", "result", "count",
		"neoforge:conditions",
	}
}

func (p *MIParser) Accepts(recipeType string) bool {
	return !isVanillaType(recipeType) && !IsVanillaSkip(recipeType)
}

func (p *MIParser) Decode(data []byte, modID, machineID, sourceFile string) (model.NormalizedRecipe, error) {
	// forge_hammer uses a completely different JSON schema.
	if strings.HasSuffix(machineID, "forge_hammer") {
		return decodeMIForgeHammer(data, modID, machineID, sourceFile)
	}

	var rec MIRecipe
	if err := json.Unmarshal(data, &rec); err != nil {
		return model.NormalizedRecipe{}, err
	}
	rec.SourceFile = sourceFile

	// Flatten NeoForge extended ingredient types before validation.
	for i := range rec.ItemInputs {
		rec.ItemInputs[i] = resolveNeoForge(rec.ItemInputs[i])
	}
	for i := range rec.ItemOutputs {
		rec.ItemOutputs[i] = resolveNeoForge(rec.ItemOutputs[i])
	}

	// Default missing amount fields to 1 (MI convention: omitting amount = 1).
	fixMIAmounts(rec.ItemInputs)
	fixMIAmounts(rec.ItemOutputs)
	fixMIAmounts(rec.FluidInputs)
	fixMIAmounts(rec.FluidOutputs)

	if problems := validateMI(&rec); len(problems) != 0 {
		return model.NormalizedRecipe{}, fmt.Errorf("%s", strings.Join(problems, "; "))
	}

	return normalizeMI(&rec, modID, machineID), nil
}

func fixMIAmounts(ios RawIOList) {
	for i := range ios {
		if ios[i].Amount == 0 {
			ios[i].Amount = 1
		}
	}
}

// decodeMIForgeHammer handles the forge_hammer recipe format:
// { ingredient: {item|tag}, result: {id, count}, count: N (optional input count) }
type forgeHammerRaw struct {
	Type       string                `json:"type"`
	Ingredient vanillaIngredientValue `json:"ingredient"`
	Result     vanillaResult         `json:"result"`
	Count      int                   `json:"count"` // ingredient quantity; 0 = 1
}

func decodeMIForgeHammer(data []byte, modID, machineID, sourceFile string) (model.NormalizedRecipe, error) {
	var fh forgeHammerRaw
	if err := json.Unmarshal(data, &fh); err != nil {
		return model.NormalizedRecipe{}, fmt.Errorf("forge_hammer parse: %w", err)
	}

	ing := fh.Ingredient.toIngredient()
	if ing.Item == "" && ing.Tag == "" {
		return model.NormalizedRecipe{}, fmt.Errorf("forge_hammer: missing ingredient")
	}
	id := fh.Result.itemID()
	if id == "" {
		return model.NormalizedRecipe{}, fmt.Errorf("forge_hammer: missing result")
	}

	inCount := fh.Count
	if inCount <= 0 {
		inCount = 1
	}

	norm := model.NormalizedRecipe{
		SourceFile: sourceFile,
		RecipeType: fh.Type,
		ModID:      modID,
		MachineID:  machineID,
		EUPerTick:  0,
		Duration:   1,
	}
	norm.ItemInputs = append(norm.ItemInputs, ingToNormIO(ing, inCount))
	norm.ItemOutputs = append(norm.ItemOutputs, idToNormIO(id, fh.Result.count()))
	norm.ContentHash = ContentHash(norm)
	return norm, nil
}

func validateMI(rec *MIRecipe) []string {
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
		problems = append(problems, validateMIItemIO(io, fmt.Sprintf("item_inputs[%d]", i))...)
	}
	for i, io := range rec.ItemOutputs {
		problems = append(problems, validateMIItemIO(io, fmt.Sprintf("item_outputs[%d]", i))...)
	}
	for i, io := range rec.FluidInputs {
		problems = append(problems, validateMIFluidIO(io, fmt.Sprintf("fluid_inputs[%d]", i))...)
	}
	for i, io := range rec.FluidOutputs {
		problems = append(problems, validateMIFluidIO(io, fmt.Sprintf("fluid_outputs[%d]", i))...)
	}
	return problems
}

func validateMIItemIO(io RawIO, prefix string) []string {
	var p []string
	if io.Item == "" && io.Tag == "" {
		p = append(p, prefix+": must have item or tag")
	}
	if io.Probability == nil || *io.Probability != 0 {
		// Only validate amount for items that are actually consumed.
		if io.Amount <= 0 {
			p = append(p, fmt.Sprintf("%s.amount must be > 0, got %d", prefix, io.Amount))
		}
	}
	return p
}

func validateMIFluidIO(io RawIO, prefix string) []string {
	var p []string
	if io.Fluid == "" && io.Tag == "" {
		p = append(p, prefix+": missing fluid field")
	}
	if io.Amount <= 0 {
		p = append(p, fmt.Sprintf("%s.amount must be > 0, got %d", prefix, io.Amount))
	}
	return p
}

func normalizeMI(rec *MIRecipe, resolvedModID, resolvedMachineID string) model.NormalizedRecipe {
	norm := model.NormalizedRecipe{
		SourceFile: rec.SourceFile,
		RecipeType: rec.Type,
		ModID:      resolvedModID,
		MachineID:  resolvedMachineID,
		EUPerTick:  rec.EU,
		Duration:   rec.Duration,
		Shape:      rec.Shape,
	}
	for _, io := range rec.ItemInputs {
		norm.ItemInputs = append(norm.ItemInputs, miItemIO(io))
	}
	for _, io := range rec.ItemOutputs {
		norm.ItemOutputs = append(norm.ItemOutputs, miItemIO(io))
	}
	for _, io := range rec.FluidInputs {
		norm.FluidInputs = append(norm.FluidInputs, miFluidIO(io))
	}
	for _, io := range rec.FluidOutputs {
		norm.FluidOutputs = append(norm.FluidOutputs, miFluidIO(io))
	}
	norm.ContentHash = ContentHash(norm)
	return norm
}

func miItemIO(io RawIO) model.NormalizedIO {
	nonConsuming := io.Probability != nil && *io.Probability == 0
	prob := 1.0
	if io.Probability != nil && !nonConsuming {
		prob = *io.Probability
	}
	n := model.NormalizedIO{AmountNum: io.Amount, AmountDen: 1, NonConsuming: nonConsuming}
	n.ProbNum, n.ProbDen = ProbToRational(prob)
	if io.Tag != "" {
		n.TagName = &io.Tag
	} else {
		mod, item := splitID(io.Item)
		n.ModID = &mod
		n.ID = &item
	}
	return n
}

func miFluidIO(io RawIO) model.NormalizedIO {
	prob := 1.0
	if io.Probability != nil {
		prob = *io.Probability
	}
	probNum, probDen := ProbToRational(prob)
	n := model.NormalizedIO{AmountMB: int64(io.Amount), ProbNum: probNum, ProbDen: probDen}
	if io.Tag != "" {
		n.TagName = &io.Tag
	} else {
		mod, fluid := splitID(io.Fluid)
		n.ModID = &mod
		n.ID = &fluid
	}
	return n
}

// MIRecipe is the raw JSON shape of a Modern Industrialization datapack recipe.
// item_inputs/outputs and fluid_inputs/outputs can be a single object or an array;
// RawIOList handles both via a custom unmarshaler.
type MIRecipe struct {
	Type         string    `json:"type"`
	EU           int64     `json:"eu"`
	Duration     int       `json:"duration"`
	ItemInputs   RawIOList `json:"item_inputs"`
	ItemOutputs  RawIOList `json:"item_outputs"`
	FluidInputs  RawIOList `json:"fluid_inputs"`
	FluidOutputs RawIOList `json:"fluid_outputs"`

	// SourceFile is set by the caller and is not part of the JSON schema.
	SourceFile string `json:"-"`
	// Shape is set by convertShaped: 9-element row-major 3×3 grid.
	Shape []string `json:"-"`
}

// RawIO is one entry in an item_inputs / item_outputs / fluid_inputs / fluid_outputs list.
type RawIO struct {
	Item        string   `json:"item"`
	Items       string   `json:"items"`       // neoforge:components uses "items" instead of "item"
	Tag         string   `json:"tag"`
	Fluid       string   `json:"fluid"`
	Amount      int      `json:"amount"`
	Probability *float64 `json:"probability"` // nil = absent (treat as 1.0); explicit 0.0 = tool item (not consumed)
	// NeoForge extended ingredient fields
	IngType  string  `json:"type"`     // e.g. "neoforge:compound", "neoforge:components"
	Children []RawIO `json:"children"` // neoforge:compound alternatives
}

// resolveNeoForge flattens NeoForge extended ingredient types into a plain RawIO.
// neoforge:components → use "items" as the item ID.
// neoforge:compound   → pick the first child as a representative alternative.
func resolveNeoForge(io RawIO) RawIO {
	switch io.IngType {
	case "neoforge:components":
		if io.Item == "" && io.Items != "" {
			io.Item = io.Items
		}
	case "neoforge:compound":
		if len(io.Children) > 0 {
			first := io.Children[0]
			if io.Item == "" && io.Tag == "" {
				io.Item = first.Item
				io.Tag = first.Tag
			}
		}
	}
	io.Children = nil
	return io
}

// RawIOList unmarshals both a single RawIO object and a JSON array of them.
type RawIOList []RawIO

func (r *RawIOList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	if data[0] == '[' {
		var arr []RawIO
		if err := json.Unmarshal(data, &arr); err != nil {
			return err
		}
		*r = arr
		return nil
	}
	var obj RawIO
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*r = RawIOList{obj}
	return nil
}
