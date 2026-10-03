package resource

// LoadForTest loads the rows migrations/001_catalog.up.sql seeds, for tests that run without a database.
func LoadForTest() {
	if err := Load([]KindInfo{
		{Kind: KindItem, KeyPrefix: "", BaseUnit: "one", Expand: ExpandAlways},
		{Kind: KindFluid, KeyPrefix: "fluid:", BaseUnit: "mb", UOMSystem: "volume", Expand: ExpandOnChoice},
		{Kind: KindEnergy, KeyPrefix: "energy:", BaseUnit: "fe", UOMSystem: "energy", Expand: ExpandNever},
	}); err != nil {
		panic(err)
	}
}
