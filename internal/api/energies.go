package api

import (
	"net/http"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
)

// ListEnergiesHandler handles GET /api/energies: every energy form with its FE factor, only
// those of the save's active mods when save_id is given.
func ListEnergiesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		energies, err := database.ListAllEnergies(r.Context(), optionalSaveID(r))
		if err != nil {
			errInternal(w, err)
			return
		}
		if energies == nil {
			energies = []*model.Energy{}
		}
		writeJSON(w, http.StatusOK, energies)
	}
}
