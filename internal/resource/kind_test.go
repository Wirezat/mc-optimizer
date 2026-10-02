package resource

import (
	"strings"
	"testing"
)

func validKinds() []KindInfo {
	return []KindInfo{
		{Kind: KindItem, KeyPrefix: "", BaseUnit: "one", Expand: ExpandAlways},
		{Kind: KindFluid, KeyPrefix: "fluid:", BaseUnit: "mb", UOMSystem: "volume", Expand: ExpandOnChoice},
	}
}

func TestLoad_SetsPrefixAndExpand(t *testing.T) {
	if err := Load(validKinds()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if Prefix(KindFluid) != "fluid:" || Prefix(KindItem) != "" {
		t.Errorf("prefixes = %q, %q", Prefix(KindItem), Prefix(KindFluid))
	}
	if Expand(KindItem) != ExpandAlways || Expand(KindFluid) != ExpandOnChoice {
		t.Errorf("expand = %q, %q", Expand(KindItem), Expand(KindFluid))
	}
}

func TestLoad_RejectsBadTables(t *testing.T) {
	cases := map[string]func([]KindInfo) []KindInfo{
		"fluid missing":        func(k []KindInfo) []KindInfo { return k[:1] },
		"bad expand":           func(k []KindInfo) []KindInfo { k[1].Expand = "sometimes"; return k },
		"duplicate prefix":     func(k []KindInfo) []KindInfo { k[1].KeyPrefix = ""; return k },
		"duplicate kind":       func(k []KindInfo) []KindInfo { return append(k, k[0]) },
		"prefix without colon": func(k []KindInfo) []KindInfo { k[1].KeyPrefix = "fluid"; return k },
	}
	for name, mutate := range cases {
		err := Load(mutate(validKinds()))
		if err == nil {
			t.Errorf("%s: Load succeeded, want an error", name)
			continue
		}
		if !strings.Contains(err.Error(), "resource kinds") {
			t.Errorf("%s: error %q does not say resource kinds", name, err)
		}
	}
}

func TestLoad_FailureKeepsThePreviousRegistry(t *testing.T) {
	if err := Load(validKinds()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	_ = Load(validKinds()[:1])
	if Prefix(KindFluid) != "fluid:" {
		t.Errorf("fluid prefix after a failed load = %q, want fluid:", Prefix(KindFluid))
	}
}

func TestKindByPrefix(t *testing.T) {
	if err := Load(validKinds()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	k, rest := SplitKey("fluid:mi:steam")
	if k != KindFluid || rest != "mi:steam" {
		t.Errorf("SplitKey fluid = %q, %q", k, rest)
	}
	k, rest = SplitKey("minecraft:stone")
	if k != KindItem || rest != "minecraft:stone" {
		t.Errorf("SplitKey item = %q, %q", k, rest)
	}
}

func TestEmptyKindIsItem(t *testing.T) {
	if err := Load(validKinds()); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if Kind("").Or() != KindItem || Prefix("") != "" || Expand("") != ExpandAlways {
		t.Errorf("empty kind = %q, prefix %q, expand %q", Kind("").Or(), Prefix(""), Expand(""))
	}
}
