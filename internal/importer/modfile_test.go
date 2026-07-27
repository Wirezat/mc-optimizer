package importer

import "testing"

// A modfile predating the key field must still import, distinguished only as
// far as the item pair allows — this is the fallback, not the fix.
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

// An explicit key is used as-is — it is what tells apart offers that would
// otherwise share the same profession, tier and item pair (the cartographer's
// explorer maps).
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

// The second cost slot is optional. Omitting it must leave both the mod and
// item empty, since that pair — not the count — is what UpsertVillagerTrades
// checks to decide whether a second slot exists at all.
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

// A present second slot resolves its ref and defaults its count the same way
// the first slot does.
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

// SourceModID is the mod declaring the offer, always — it is what tells a
// datapack's redefinition of a vanilla profession apart from vanilla's own.
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
