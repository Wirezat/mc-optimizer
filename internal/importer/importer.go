package importer

import (
	"context"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ImporterDB is the subset of the database the modfile importer needs.
type ImporterDB interface {
	ImportRecipe(ctx context.Context, rec model.NormalizedRecipe) (bool, error)
	UpsertTranslations(ctx context.Context, lang string, entries map[string]string) error
	BulkUpsertItems(ctx context.Context, modID string, items []model.ItemDef) error
	UpsertBlockDrops(ctx context.Context, drops []model.BlockDrop) error
	UpsertVillagerTrades(ctx context.Context, trades []model.VillagerTrade) error

	UpsertMod(ctx context.Context, m model.ModDef) error
	UpsertFluids(ctx context.Context, modID string, fluidIDs []string) error
	UpsertMachineType(ctx context.Context, m model.MachineTypeDef) error
	UpsertMachineSlots(ctx context.Context, slots []model.MachineSlotDef) error
	AddMachineInterface(ctx context.Context, modID, machineID, baseModID, baseMachineID string) error
	UpsertDirectTagMembers(ctx context.Context, tagName string, members []string) error
	UpsertModPlugin(ctx context.Context, p db.ModPlugin) error
}

// Importer writes mod data to the database and the assets directory.
type Importer struct {
	db        ImporterDB
	assetsDir string
	// UploadedBy is attributed to any plugin recorded during import.
	UploadedBy string
}

// New creates an Importer.
func New(db ImporterDB, assetsDir string) *Importer {
	return &Importer{db: db, assetsDir: assetsDir}
}
