package db

import (
	"context"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

func TestListResourceKinds_LoadsTheSeed(t *testing.T) {
	d := testDB(t)
	kinds, err := d.ListResourceKinds(context.Background())
	if err != nil {
		t.Fatalf("ListResourceKinds: %v", err)
	}
	if err := resource.Load(kinds); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if resource.Prefix(resource.KindFluid) != "fluid:" || resource.Expand(resource.KindFluid) != resource.ExpandOnChoice {
		t.Errorf("fluid = %q / %q", resource.Prefix(resource.KindFluid), resource.Expand(resource.KindFluid))
	}
	if info, _ := resource.Info(resource.KindFluid); info.BaseUnit != "mb" || info.UOMSystem != "volume" {
		t.Errorf("fluid info = %+v", info)
	}
	if info, _ := resource.Info(resource.KindEnergy); info.KeyPrefix != "energy:" || info.BaseUnit != "fe" || info.UOMSystem != "energy" || info.Expand != resource.ExpandNever {
		t.Errorf("energy info = %+v", info)
	}
}
