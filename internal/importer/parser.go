package importer

import "github.com/Wirezat/production-optimizer/internal/model"

// RecipeParser handles one mod's recipe format.
// Each mod registers a parser; the importer iterates them for each recipe entry.
type RecipeParser interface {
	// Skip returns true for recipe types that should be silently ignored.
	Skip(recipeType string) bool
	// Accepts returns true if this parser handles the given recipe type.
	Accepts(recipeType string) bool
	// Decode parses raw recipe JSON into a normalised recipe.
	// modID and machineID are resolved by the matcher before Decode is called.
	Decode(data []byte, modID, machineID, sourceFile string) (model.NormalizedRecipe, error)
}

// parsers is the ordered registry of recipe parsers.
var parsers = []RecipeParser{
	&MIParser{},
	&VanillaParser{},
}
