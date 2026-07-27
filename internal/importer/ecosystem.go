package importer

import (
	"context"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// EcosystemHandler applies ecosystem-specific post-processing after a mod's
// machines have been written to the DB. Add new ecosystems by registering a
// handler in the ecosystems map below.
type EcosystemHandler interface {
	// Name returns the ecosystem identifier as it appears in the YAML.
	Name() string
	// PostProcess is called once per machine after UpsertMachineType.
	PostProcess(ctx context.Context, db ModFileDB, m model.MachineTypeDef) error
}

// ModFileDB is the DB subset used by ecosystem handlers.
// It is a subset of ImporterDB so handlers only see what they need.
type ModFileDB interface {
	SetMIEnergyType(ctx context.Context, modIDs []string) error
	SetMachinesUpgradable(ctx context.Context, modIDs []string) error
	SetMISteamMachines(ctx context.Context, modIDs []string, machineIDs []string) error
}

// ecosystemRegistry maps ecosystem name → handler.
// Add a new entry here to support a new ecosystem.
var ecosystemRegistry = map[string]EcosystemHandler{
	"vanilla":                  &vanillaEcosystem{},
	"modern_industrialization": &miEcosystem{},
	"mekanism":                 &mekanismEcosystem{},
}

// lookupEcosystem returns the handler for the given name, or nil if unknown.
func lookupEcosystem(name string) EcosystemHandler {
	return ecosystemRegistry[name]
}

// vanillaEcosystem — no energy, no upgrades. Nothing to post-process.
type vanillaEcosystem struct{}

func (e *vanillaEcosystem) Name() string { return "vanilla" }
func (e *vanillaEcosystem) PostProcess(_ context.Context, _ ModFileDB, _ model.MachineTypeDef) error {
	return nil
}

// miEcosystem — EU energy, upgrade/overclock system.
// SetMIEnergyType and SetMachinesUpgradable are called once per mod in RunModFile,
// not per machine, so PostProcess is a no-op here.
type miEcosystem struct{}

func (e *miEcosystem) Name() string { return "modern_industrialization" }
func (e *miEcosystem) PostProcess(_ context.Context, _ ModFileDB, _ model.MachineTypeDef) error {
	return nil
}

// mekanismEcosystem — FE energy, no MI-style upgrades.
type mekanismEcosystem struct{}

func (e *mekanismEcosystem) Name() string { return "mekanism" }
func (e *mekanismEcosystem) PostProcess(_ context.Context, _ ModFileDB, _ model.MachineTypeDef) error {
	return nil
}
