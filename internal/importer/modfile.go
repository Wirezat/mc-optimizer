package importer

import (
	"fmt"
	"math"
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

	Tags []struct {
		Name    string   `yaml:"name"`
		Members []string `yaml:"members"`
	} `yaml:"tags"`

	Machines []struct {
		ID         string   `yaml:"id"`
		LangKey    string   `yaml:"lang_key"`
		Implements []string `yaml:"implements"`
		// Ecosystem names which plugin evaluates this machine; empty means
		// the machine's own mod_id.
		Ecosystem string `yaml:"ecosystem"`
		Slots     []struct {
			Index int     `yaml:"index"`
			Type  string  `yaml:"type"`
			X     *int16  `yaml:"x"`
			Y     *int16  `yaml:"y"`
			Label *string `yaml:"label"`
		} `yaml:"slots"`
		// ModData collects every key not claimed by a field above, so a
		// plugin can define arbitrary mod-specific machine fields.
		ModData map[string]any `yaml:",inline"`
	} `yaml:"machines"`

	Recipes []struct {
		Machine       string   `yaml:"machine"`
		DurationTicks int      `yaml:"duration_ticks"`
		Inputs        rawIO    `yaml:"inputs"`
		Outputs       rawIO    `yaml:"outputs"`
		Shape         []string `yaml:"shape"`
		// ModData collects every key not claimed by a field above, so a
		// plugin can define arbitrary mod-specific recipe fields.
		ModData map[string]any `yaml:",inline"`
	} `yaml:"recipes"`

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
		// key identifies the offer within this mod. Profession + tier + item
		// pair does not: several offers can share all three.
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
		// The data does not fix the price — an enchanted book's cost is derived
		// at runtime. What cost carries is then a floor, not the price.
		CostVariable bool `yaml:"cost_variable"`
		MaxUses      *int `yaml:"max_uses"`
		XP           *int `yaml:"xp"`
	} `yaml:"villager_trades"`
}

type rawIO struct {
	Items []struct {
		Item        string   `yaml:"item"`
		Tag         string   `yaml:"tag"`
		Amount      *float64 `yaml:"amount"`
		AmountNum   *int     `yaml:"amount_num"`
		AmountDen   *int     `yaml:"amount_den"`
		Probability *float64 `yaml:"probability"`
	} `yaml:"items"`
	Fluids []struct {
		Fluid       string   `yaml:"fluid"`
		Tag         string   `yaml:"tag"`
		AmountMB    int64    `yaml:"amount_mb"`
		Probability *float64 `yaml:"probability"`
	} `yaml:"fluids"`
}

// ParseModFile parses YAML bytes into a ModDef. Returns an error if mod_id is
// the sentinel or otherwise invalid. Sentinel entries within lists are silently dropped.
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

	// Fluids. id can be "mod:fluid" — a modfile can catalog another mod's fluid
	// (e.g. MI listing vanilla lava/water) to attach texture/name info to it.
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
		td := model.TagDef{Name: r.Name}
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
		rec := model.ModRecipeDef{
			MachineModID:  machineModID,
			MachineID:     machineID,
			DurationTicks: r.DurationTicks,
			Shape:         r.Shape,
			ModData:       r.ModData,
		}
		for _, io := range r.Inputs.Items {
			d, err := parseItemIO(io.Item, io.Tag, io.Amount, io.AmountNum, io.AmountDen, io.Probability, raw.ModID)
			if err != nil {
				return nil, fmt.Errorf("modfile: recipe input: %w", err)
			}
			if d != nil {
				rec.ItemInputs = append(rec.ItemInputs, *d)
			}
		}
		for _, io := range r.Outputs.Items {
			d, err := parseItemIO(io.Item, io.Tag, io.Amount, io.AmountNum, io.AmountDen, io.Probability, raw.ModID)
			if err != nil {
				return nil, fmt.Errorf("modfile: recipe output: %w", err)
			}
			if d != nil {
				rec.ItemOutputs = append(rec.ItemOutputs, *d)
			}
		}
		for _, io := range r.Inputs.Fluids {
			d, err := parseFluidIO(io.Fluid, io.Tag, io.AmountMB, io.Probability, raw.ModID)
			if err != nil {
				return nil, fmt.Errorf("modfile: recipe fluid input: %w", err)
			}
			if d != nil {
				rec.FluidInputs = append(rec.FluidInputs, *d)
			}
		}
		for _, io := range r.Outputs.Fluids {
			d, err := parseFluidIO(io.Fluid, io.Tag, io.AmountMB, io.Probability, raw.ModID)
			if err != nil {
				return nil, fmt.Errorf("modfile: recipe fluid output: %w", err)
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
		// Older modfiles predate the key; fall back to the item pair so they
		// still import, just without distinguishing same-pair offers.
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

func parseItemIO(item, tag string, amount *float64, amountNum, amountDen *int, prob *float64, defaultMod string) (*model.ModIODef, error) {
	if isSentinel(item) && isSentinel(tag) {
		return nil, nil
	}
	d := &model.ModIODef{
		AmountNum: 1,
		AmountDen: 1,
		ProbNum:   1,
		ProbDen:   1,
	}
	if !isSentinel(tag) && tag != "" {
		d.TagName = ptr(tag)
	} else {
		modID, id, err := splitRef(item, defaultMod)
		if err != nil {
			return nil, err
		}
		d.ItemModID = ptr(modID)
		d.ItemID = ptr(id)
	}
	if amountNum != nil && amountDen != nil {
		d.AmountNum = *amountNum
		d.AmountDen = *amountDen
	} else if amount != nil {
		d.AmountNum, d.AmountDen = floatToRational(*amount)
	}
	if prob != nil {
		d.ProbNum, d.ProbDen = floatToRational(*prob)
	}
	return d, nil
}

func parseFluidIO(fluid, tag string, amountMB int64, prob *float64, defaultMod string) (*model.ModFluidIODef, error) {
	if isSentinel(fluid) && isSentinel(tag) {
		return nil, nil
	}
	d := &model.ModFluidIODef{
		AmountMB: amountMB,
		ProbNum:  1,
		ProbDen:  1,
	}
	if !isSentinel(tag) && tag != "" {
		d.TagName = ptr(tag)
	} else {
		modID, id, err := splitRef(fluid, defaultMod)
		if err != nil {
			return nil, err
		}
		d.FluidModID = ptr(modID)
		d.FluidID = ptr(id)
	}
	if prob != nil {
		d.ProbNum, d.ProbDen = floatToRational(*prob)
	}
	return d, nil
}

func floatToRational(f float64) (num, den int) {
	if f <= 0 {
		return 0, 1
	}
	if f >= 1 {
		n := int(math.Round(f))
		return n, 1
	}
	return ProbToRational(f)
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
