package importer

import (
	"fmt"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/model"
	"gopkg.in/yaml.v3"
)

const exampleSentinel = "_example_"

// rawModFile mirrors the YAML structure exactly for unmarshaling.
type rawModFile struct {
	ModID        string `yaml:"mod_id"`
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	Author       string `yaml:"author"`
	License      string `yaml:"license"`
	ModrinthSlug string `yaml:"modrinth_slug"`
	URLSource    string `yaml:"url_source"`
	URLModrinth  string `yaml:"url_modrinth"`
	URLWiki      string `yaml:"url_wiki"`
	URLIssues    string `yaml:"url_issues"`
	URLDiscord   string `yaml:"url_discord"`

	Translations map[string]map[string]string `yaml:"translations"`

	Items []struct {
		ID       string `yaml:"id"`
		MaxStack *int   `yaml:"max_stack"`
		LangKey  string `yaml:"lang_key"`
	} `yaml:"items"`

	Fluids []struct {
		ID      string `yaml:"id"`
		LangKey string `yaml:"lang_key"`
	} `yaml:"fluids"`

	Tags []rawTag `yaml:"tags"`

	Machines []struct {
		ID         string   `yaml:"id"`
		LangKey    string   `yaml:"lang_key"`
		Implements []string `yaml:"implements"`
		// Ecosystem names which plugin evaluates this machine; empty means the machine's own
		// mod_id.
		Ecosystem string `yaml:"ecosystem"`
		Slots     []struct {
			Index int     `yaml:"index"`
			Type  string  `yaml:"type"`
			X     *int16  `yaml:"x"`
			Y     *int16  `yaml:"y"`
			Label *string `yaml:"label"`
		} `yaml:"slots"`
		// ModData collects every key not claimed by a field above.
		ModData map[string]any `yaml:",inline"`
	} `yaml:"machines"`

	Recipes []rawRecipe `yaml:"recipes"`

	BlockDrops []struct {
		Block string `yaml:"block"`
		Drops []struct {
			Item      string `yaml:"item"`
			MinCount  int    `yaml:"min_count"`
			MaxCount  int    `yaml:"max_count"`
			Condition string `yaml:"condition"`
		} `yaml:"drops"`
	} `yaml:"block_drops"`

	VillagerTrades []struct {
		// key identifies the offer within this mod.
		Key        string `yaml:"key"`
		Profession string `yaml:"profession"`
		Tier       int    `yaml:"tier"`
		Cost       struct {
			Item  string `yaml:"item"`
			Count int    `yaml:"count"`
		} `yaml:"cost"`
		// Optional second item the offer also charges.
		Cost2 struct {
			Item  string `yaml:"item"`
			Count int    `yaml:"count"`
		} `yaml:"cost2"`
		Result struct {
			Item  string `yaml:"item"`
			Count int    `yaml:"count"`
		} `yaml:"result"`
		ResultModified bool `yaml:"result_modified"`
		// The data does not fix the price — an enchanted book's cost is derived at runtime.
		CostVariable bool `yaml:"cost_variable"`
		MaxUses      *int `yaml:"max_uses"`
		XP           *int `yaml:"xp"`
	} `yaml:"villager_trades"`
}

type rawTag struct {
	Name    string   `yaml:"name"`
	Kind    string   `yaml:"kind"`
	Members []string `yaml:"members"`
	line    int
}

func (r *rawTag) UnmarshalYAML(n *yaml.Node) error {
	type plain rawTag
	if err := n.Decode((*plain)(r)); err != nil {
		return err
	}
	r.line = n.Line
	return nil
}

type rawRecipe struct {
	Machine       string   `yaml:"machine"`
	DurationTicks int      `yaml:"duration_ticks"`
	Inputs        rawIO    `yaml:"inputs"`
	Outputs       rawIO    `yaml:"outputs"`
	Shape         []string `yaml:"shape"`
	// ModData collects every key not claimed by a field above.
	ModData map[string]any `yaml:",inline"`
	line    int
}

func (r *rawRecipe) UnmarshalYAML(n *yaml.Node) error {
	type plain rawRecipe
	if err := n.Decode((*plain)(r)); err != nil {
		return err
	}
	r.line = n.Line
	return nil
}

type rawIO struct {
	Items  []rawItemIO  `yaml:"items"`
	Fluids []rawFluidIO `yaml:"fluids"`
}

type rawItemIO struct {
	Item        string       `yaml:"item"`
	Tag         string       `yaml:"tag"`
	Amount      *exactNumber `yaml:"amount"`
	AmountNum   *int         `yaml:"amount_num"`
	AmountDen   *int         `yaml:"amount_den"`
	Probability *exactNumber `yaml:"probability"`
	line        int
}

func (r *rawItemIO) UnmarshalYAML(n *yaml.Node) error {
	type plain rawItemIO
	if err := n.Decode((*plain)(r)); err != nil {
		return err
	}
	r.line = n.Line
	return nil
}

type rawFluidIO struct {
	Fluid       string       `yaml:"fluid"`
	Tag         string       `yaml:"tag"`
	AmountMB    *int64       `yaml:"amount_mb"`
	Probability *exactNumber `yaml:"probability"`
	line        int
}

func (r *rawFluidIO) UnmarshalYAML(n *yaml.Node) error {
	type plain rawFluidIO
	if err := n.Decode((*plain)(r)); err != nil {
		return err
	}
	r.line = n.Line
	return nil
}

// ParseModFile parses YAML bytes into a ModDef.
func ParseModFile(data []byte) (*model.ModDef, error) {
	var raw rawModFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("modfile: yaml parse: %w", err)
	}
	if raw.ModID == "" {
		return nil, fmt.Errorf("modfile: mod_id is required")
	}
	if raw.ModID == exampleSentinel {
		return nil, fmt.Errorf("modfile: mod_id is still the example sentinel %q", exampleSentinel)
	}
	def := &model.ModDef{
		ModID:        raw.ModID,
		Name:         strOrFallback(raw.Name, raw.ModID),
		Description:  raw.Description,
		Author:       raw.Author,
		License:      raw.License,
		ModrinthSlug: strOrSentinel(raw.ModrinthSlug),
		URLSource:    strOrSentinel(raw.URLSource),
		URLModrinth:  strOrSentinel(raw.URLModrinth),
		URLWiki:      strOrSentinel(raw.URLWiki),
		URLIssues:    strOrSentinel(raw.URLIssues),
		URLDiscord:   strOrSentinel(raw.URLDiscord),
		Translations: raw.Translations,
	}

	// Items
	for _, r := range raw.Items {
		if isSentinel(r.ID) {
			continue
		}
		maxStack := 64
		if r.MaxStack != nil {
			maxStack = *r.MaxStack
		}
		def.Items = append(def.Items, model.ItemDef{
			ModID:    raw.ModID,
			ItemID:   r.ID,
			MaxStack: maxStack,
			LangKey:  strOrSentinel(r.LangKey),
		})
	}

	// Fluids.
	for _, r := range raw.Fluids {
		if isSentinel(r.ID) {
			continue
		}
		fluidModID, fluidID, err := splitRef(r.ID, raw.ModID)
		if err != nil {
			return nil, fmt.Errorf("modfile: fluid id %q: %w", r.ID, err)
		}
		def.Fluids = append(def.Fluids, model.FluidDef{
			ModID:   fluidModID,
			FluidID: fluidID,
			LangKey: strOrSentinel(r.LangKey),
		})
	}

	// Tags
	for _, r := range raw.Tags {
		if isSentinel(r.Name) {
			continue
		}
		kind := r.Kind
		if kind == "" {
			kind = model.TagKindItem
		}
		if kind != model.TagKindItem && kind != model.TagKindFluid {
			return nil, fmt.Errorf("modfile: mod.yml line %d: tag %s: kind %q is neither item nor fluid", r.line, r.Name, r.Kind)
		}
		td := model.TagDef{Name: r.Name, Kind: kind}
		for _, m := range r.Members {
			if !isSentinel(m) {
				td.Members = append(td.Members, m)
			}
		}
		def.Tags = append(def.Tags, td)
	}

	// Machines
	for _, r := range raw.Machines {
		if isSentinel(r.ID) {
			continue
		}
		m := model.MachineTypeDef{
			ModID:      raw.ModID,
			MachineID:  r.ID,
			LangKey:    r.LangKey,
			Implements: r.Implements,
			Ecosystem:  r.Ecosystem,
			ModData:    r.ModData,
		}
		for _, s := range r.Slots {
			m.Slots = append(m.Slots, model.MachineSlotDef{
				ModID:     raw.ModID,
				MachineID: r.ID,
				Index:     s.Index,
				SlotType:  s.Type,
				X:         s.X,
				Y:         s.Y,
				Label:     s.Label,
			})
		}
		def.Machines = append(def.Machines, m)
	}

	// Recipes
	for _, r := range raw.Recipes {
		if isSentinel(r.Machine) {
			continue
		}
		machineModID, machineID, err := splitRef(r.Machine, raw.ModID)
		if err != nil {
			return nil, fmt.Errorf("modfile: recipe machine ref %q: %w", r.Machine, err)
		}
		if r.DurationTicks <= 0 {
			return nil, fmt.Errorf("modfile: mod.yml line %d: recipe on %s: duration_ticks must be positive", r.line, r.Machine)
		}
		rec := model.ModRecipeDef{
			MachineModID:  machineModID,
			MachineID:     machineID,
			DurationTicks: r.DurationTicks,
			Shape:         r.Shape,
			ModData:       r.ModData,
		}
		ioErr := func(line int, side, ref string, err error) error {
			return fmt.Errorf("modfile: mod.yml line %d: recipe on %s, %s %s: %w", line, r.Machine, side, ref, err)
		}
		for _, io := range r.Inputs.Items {
			d, err := parseItemIO(io, false, raw.ModID)
			if err != nil {
				return nil, ioErr(io.line, "input", itemName(io), err)
			}
			if d != nil {
				rec.ItemInputs = append(rec.ItemInputs, *d)
			}
		}
		for _, io := range r.Outputs.Items {
			d, err := parseItemIO(io, true, raw.ModID)
			if err != nil {
				return nil, ioErr(io.line, "output", itemName(io), err)
			}
			if d != nil {
				rec.ItemOutputs = append(rec.ItemOutputs, *d)
			}
		}
		for _, io := range r.Inputs.Fluids {
			d, err := parseFluidIO(io, false, raw.ModID)
			if err != nil {
				return nil, ioErr(io.line, "fluid input", fluidName(io), err)
			}
			if d != nil {
				rec.FluidInputs = append(rec.FluidInputs, *d)
			}
		}
		for _, io := range r.Outputs.Fluids {
			d, err := parseFluidIO(io, true, raw.ModID)
			if err != nil {
				return nil, ioErr(io.line, "fluid output", fluidName(io), err)
			}
			if d != nil {
				rec.FluidOutputs = append(rec.FluidOutputs, *d)
			}
		}
		def.Recipes = append(def.Recipes, rec)
	}

	// Block drops
	for _, r := range raw.BlockDrops {
		if isSentinel(r.Block) {
			continue
		}
		blockModID, blockItemID, err := splitRef(r.Block, raw.ModID)
		if err != nil {
			return nil, fmt.Errorf("modfile: block drop block ref: %w", err)
		}
		for _, d := range r.Drops {
			if isSentinel(d.Item) {
				continue
			}
			dropModID, dropItemID, err := splitRef(d.Item, raw.ModID)
			if err != nil {
				return nil, fmt.Errorf("modfile: block drop item ref: %w", err)
			}
			cond := d.Condition
			if cond == "" {
				cond = "normal"
			}
			min := d.MinCount
			if min == 0 {
				min = 1
			}
			max := d.MaxCount
			if max == 0 {
				max = 1
			}
			def.BlockDrops = append(def.BlockDrops, model.BlockDrop{
				BlockModID:  blockModID,
				BlockItemID: blockItemID,
				DropModID:   dropModID,
				DropItemID:  dropItemID,
				MinCount:    min,
				MaxCount:    max,
				Condition:   cond,
			})
		}
	}

	// Villager trades
	for _, r := range raw.VillagerTrades {
		if isSentinel(r.Profession) {
			continue
		}
		costModID, costItemID, err := splitRef(r.Cost.Item, "minecraft")
		if err != nil {
			return nil, fmt.Errorf("modfile: trade cost ref: %w", err)
		}
		resultModID, resultItemID, err := splitRef(r.Result.Item, raw.ModID)
		if err != nil {
			return nil, fmt.Errorf("modfile: trade result ref: %w", err)
		}
		// The second cost slot is optional; only resolve a ref when one is set.
		var cost2ModID, cost2ItemID string
		if r.Cost2.Item != "" {
			cost2ModID, cost2ItemID, err = splitRef(r.Cost2.Item, "minecraft")
			if err != nil {
				return nil, fmt.Errorf("modfile: trade cost2 ref: %w", err)
			}
		}
		costCount := r.Cost.Count
		if costCount == 0 {
			costCount = 1
		}
		cost2Count := r.Cost2.Count
		if cost2ItemID != "" && cost2Count == 0 {
			cost2Count = 1
		}
		resultCount := r.Result.Count
		if resultCount == 0 {
			resultCount = 1
		}
		key := r.Key
		if key == "" {
			key = costItemID + ">" + resultItemID
		}
		def.VillagerTrades = append(def.VillagerTrades, model.VillagerTrade{
			SourceModID:    raw.ModID,
			TradeKey:       key,
			Profession:     r.Profession,
			Tier:           r.Tier,
			CostModID:      costModID,
			CostItemID:     costItemID,
			CostCount:      costCount,
			Cost2ModID:     cost2ModID,
			Cost2ItemID:    cost2ItemID,
			Cost2Count:     cost2Count,
			ResultModID:    resultModID,
			ResultItemID:   resultItemID,
			ResultCount:    resultCount,
			ResultModified: r.ResultModified,
			CostVariable:   r.CostVariable,
			MaxUses:        r.MaxUses,
			XP:             r.XP,
		})
	}

	return def, nil
}

// splitRef parses "mod_id:item_id" or plain "item_id" (uses defaultMod).
func splitRef(ref, defaultMod string) (modID, id string, err error) {
	if ref == "" {
		return "", "", fmt.Errorf("empty reference")
	}
	if i := strings.IndexByte(ref, ':'); i > 0 {
		return ref[:i], ref[i+1:], nil
	}
	return defaultMod, ref, nil
}

func itemName(io rawItemIO) string {
	if !isSentinel(io.Tag) {
		return "#" + io.Tag
	}
	return io.Item
}

func fluidName(io rawFluidIO) string {
	if !isSentinel(io.Tag) {
		return "#" + io.Tag
	}
	return io.Fluid
}

func parseItemIO(io rawItemIO, output bool, defaultMod string) (*model.ModIODef, error) {
	if isSentinel(io.Item) && isSentinel(io.Tag) {
		return nil, nil
	}
	d := &model.ModIODef{AmountNum: 1, AmountDen: 1, ProbNum: 1, ProbDen: 1}
	if !isSentinel(io.Tag) {
		d.TagName = ptr(io.Tag)
	} else {
		modID, id, err := splitRef(io.Item, defaultMod)
		if err != nil {
			return nil, err
		}
		d.ItemModID = ptr(modID)
		d.ItemID = ptr(id)
	}
	switch {
	case io.Amount != nil && (io.AmountNum != nil || io.AmountDen != nil):
		return nil, fmt.Errorf("give either amount or amount_num/amount_den")
	case (io.AmountNum == nil) != (io.AmountDen == nil):
		return nil, fmt.Errorf("amount_num and amount_den must be given together")
	case io.AmountNum != nil:
		if !fitsInt32(int64(*io.AmountNum), true) || !fitsInt32(int64(*io.AmountDen), true) {
			return nil, fmt.Errorf("amount %d/%d is out of range", *io.AmountNum, *io.AmountDen)
		}
		d.AmountNum, d.AmountDen = *io.AmountNum, *io.AmountDen
	case io.Amount != nil:
		d.AmountNum, d.AmountDen = io.Amount.num, io.Amount.den
	}
	if err := checkAmount(int64(d.AmountNum), int64(d.AmountDen)); err != nil {
		return nil, err
	}
	if io.Probability != nil {
		d.ProbNum, d.ProbDen = io.Probability.num, io.Probability.den
	}
	if err := checkProbability(int64(d.ProbNum), int64(d.ProbDen), !output); err != nil {
		return nil, err
	}
	return d, nil
}

func parseFluidIO(io rawFluidIO, output bool, defaultMod string) (*model.ModFluidIODef, error) {
	if isSentinel(io.Fluid) && isSentinel(io.Tag) {
		return nil, nil
	}
	d := &model.ModFluidIODef{ProbNum: 1, ProbDen: 1}
	if !isSentinel(io.Tag) {
		d.TagName = ptr(io.Tag)
	} else {
		modID, id, err := splitRef(io.Fluid, defaultMod)
		if err != nil {
			return nil, err
		}
		d.FluidModID = ptr(modID)
		d.FluidID = ptr(id)
	}
	if io.AmountMB == nil {
		return nil, fmt.Errorf("amount_mb is required")
	}
	if err := checkAmount(*io.AmountMB, 1); err != nil {
		return nil, err
	}
	d.AmountMB = *io.AmountMB
	if io.Probability != nil {
		d.ProbNum, d.ProbDen = io.Probability.num, io.Probability.den
	}
	if err := checkProbability(int64(d.ProbNum), int64(d.ProbDen), !output); err != nil {
		return nil, err
	}
	return d, nil
}

func isSentinel(s string) bool {
	return s == "" || s == exampleSentinel
}

func strOrSentinel(s string) string {
	if isSentinel(s) {
		return ""
	}
	return s
}

func strOrFallback(s, fallback string) string {
	if isSentinel(s) {
		return fallback
	}
	return s
}

func ptr[T any](v T) *T { return &v }
