package importer

import "testing"

// Any machine or recipe field not claimed by a core column (id, lang_key, ecosystem,
// implements, slots on a machine; machine, duration_ticks, inputs, outputs, shape on a
// recipe) must land in ModData verbatim, and a core field must never also leak into it — a
// plugin reads ModData as the mod's own opaque config and a leaked core field would shadow
// whatever key the mod author picked.
func TestParseModFile_MachineAndRecipeModData(t *testing.T) {
	data := []byte(`
mod_id: test_mod
machines:
  - id: iron_furnace
    ecosystem: modern_industrialization
    energy_per_tick: 32
    tier: electric
recipes:
  - machine: iron_furnace
    duration_ticks: 100
    energy_per_tick: 16
    inputs:
      items:
        - item: minecraft:iron_ore
          amount: 1
    outputs:
      items:
        - item: iron_ingot
          amount: 1
`)
	def, err := ParseModFile(data)
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if len(def.Machines) != 1 {
		t.Fatalf("got %d machines, want 1", len(def.Machines))
	}
	m := def.Machines[0]
	if m.Ecosystem != "modern_industrialization" {
		t.Errorf("Ecosystem = %q, want modern_industrialization", m.Ecosystem)
	}
	if got, ok := m.ModData["energy_per_tick"]; !ok || got != 32 {
		t.Errorf("machine ModData[energy_per_tick] = %#v (ok=%v), want 32", got, ok)
	}
	if got, ok := m.ModData["tier"]; !ok || got != "electric" {
		t.Errorf("machine ModData[tier] = %#v (ok=%v), want \"electric\"", got, ok)
	}
	if _, ok := m.ModData["ecosystem"]; ok {
		t.Error("ecosystem must not also leak into ModData")
	}
	if _, ok := m.ModData["id"]; ok {
		t.Error("id must not also leak into ModData")
	}

	if len(def.Recipes) != 1 {
		t.Fatalf("got %d recipes, want 1", len(def.Recipes))
	}
	r := def.Recipes[0]
	if got, ok := r.ModData["energy_per_tick"]; !ok || got != 16 {
		t.Errorf("recipe ModData[energy_per_tick] = %#v (ok=%v), want 16", got, ok)
	}
	if _, ok := r.ModData["duration_ticks"]; ok {
		t.Error("duration_ticks must not also leak into ModData")
	}
	if _, ok := r.ModData["machine"]; ok {
		t.Error("machine must not also leak into ModData")
	}
}

// A modfile predating the key field must still import, distinguished only as far as the
// item pair allows — this is the fallback, not the fix.
func TestParseModFile_VillagerTradeKeyFallsBackToItemPair(t *testing.T) {
	data := []byte(`
mod_id: test_mod
villager_trades:
  - profession: farmer
    tier: 1
    cost:
      item: minecraft:emerald
      count: 4
    result:
      item: minecraft:wheat
`)
	def, err := ParseModFile(data)
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if len(def.VillagerTrades) != 1 {
		t.Fatalf("got %d trades, want 1", len(def.VillagerTrades))
	}
	if want := "emerald>wheat"; def.VillagerTrades[0].TradeKey != want {
		t.Errorf("TradeKey = %q, want %q", def.VillagerTrades[0].TradeKey, want)
	}
}

func TestParseModFile_VillagerTradeExplicitKey(t *testing.T) {
	data := []byte(`
mod_id: test_mod
villager_trades:
  - key: buried_treasure_map
    profession: cartographer
    tier: 1
    cost:
      item: minecraft:emerald
      count: 13
    result:
      item: minecraft:map
`)
	def, err := ParseModFile(data)
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if got := def.VillagerTrades[0].TradeKey; got != "buried_treasure_map" {
		t.Errorf("TradeKey = %q, want the explicit key", got)
	}
}

// The second cost slot is optional.
func TestParseModFile_VillagerTradeNoCost2(t *testing.T) {
	data := []byte(`
mod_id: test_mod
villager_trades:
  - profession: farmer
    tier: 1
    cost:
      item: minecraft:emerald
      count: 4
    result:
      item: minecraft:wheat
`)
	def, err := ParseModFile(data)
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	tr := def.VillagerTrades[0]
	if tr.Cost2ModID != "" || tr.Cost2ItemID != "" {
		t.Errorf("Cost2ModID=%q Cost2ItemID=%q, want both empty", tr.Cost2ModID, tr.Cost2ItemID)
	}
	if tr.Cost2Count != 0 {
		t.Errorf("Cost2Count = %d, want 0 when there is no second slot", tr.Cost2Count)
	}
}

// A present second slot resolves its ref and defaults its count the same way the first slot
// does.
func TestParseModFile_VillagerTradeWithCost2(t *testing.T) {
	data := []byte(`
mod_id: test_mod
villager_trades:
  - key: explorer_map_swamp
    profession: cartographer
    tier: 1
    cost:
      item: minecraft:emerald
      count: 13
    cost2:
      item: minecraft:compass
    result:
      item: minecraft:map
`)
	def, err := ParseModFile(data)
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	tr := def.VillagerTrades[0]
	if tr.Cost2ModID != "minecraft" || tr.Cost2ItemID != "compass" {
		t.Errorf("Cost2ModID=%q Cost2ItemID=%q, want minecraft/compass", tr.Cost2ModID, tr.Cost2ItemID)
	}
	if tr.Cost2Count != 1 {
		t.Errorf("Cost2Count = %d, want 1 (default when omitted)", tr.Cost2Count)
	}
}

// SourceModID is the mod declaring the offer, always — it is what tells a datapack's
// redefinition of a vanilla profession apart from vanilla's own.
func TestParseModFile_VillagerTradeSourceModID(t *testing.T) {
	data := []byte(`
mod_id: trade_rebalance
villager_trades:
  - profession: armorer
    tier: 1
    cost:
      item: minecraft:emerald
      count: 5
    result:
      item: minecraft:iron_helmet
`)
	def, err := ParseModFile(data)
	if err != nil {
		t.Fatalf("ParseModFile: %v", err)
	}
	if got := def.VillagerTrades[0].SourceModID; got != "trade_rebalance" {
		t.Errorf("SourceModID = %q, want %q", got, "trade_rebalance")
	}
}
