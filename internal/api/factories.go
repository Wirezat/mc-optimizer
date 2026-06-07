package api

import (
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"

	"github.com/Wirezat/production-optimizer/internal/db"
	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/google/uuid"
)

// balanceEntry is a single item/fluid rate in the factory balance response.
type balanceEntry struct {
	ModID       string `json:"mod_id"`
	ItemFluidID string `json:"item_fluid_id"`
	IOType      string `json:"io_type"`
	RateNum     int64  `json:"rate_num"`
	RateDen     int64  `json:"rate_den"`
	Unit        string `json:"unit"` // always "min"
}

type surplusEntry struct {
	balanceEntry
	IsBottleneck bool `json:"is_bottleneck"`
}

type factoryBalance struct {
	ExternalInputs []balanceEntry `json:"external_inputs"`
	Outputs        []balanceEntry `json:"outputs"`
	Surplus        []surplusEntry `json:"surplus"`
}

// computeBalance aggregates all active IO entries into a factory balance.
// Rates from different time units are normalized to per-minute.
func computeBalance(ios []db.PLIOWithUnit) factoryBalance {
	type key struct {
		modID, itemFluidID, ioType string
	}
	// Accumulate as rational num/den (int64), normalized to /min.
	type rat struct{ num, den int64 }
	inputs := map[key]rat{}
	outputs := map[key]rat{}

	addRate := func(m map[key]rat, k key, rateNum, rateDen int, timeUnit string) {
		// Convert to per-minute: multiply rate by (1200 / ticks_per_unit).
		// ticks/unit: t=1, s=20, min=1200, h=72000
		// per_min = rate * (1200 / tpu) = (rate_num * 1200) / (rate_den * tpu)
		var tpu int64
		switch timeUnit {
		case "t":
			tpu = 1
		case "s":
			tpu = 20
		case "h":
			tpu = 72000
		default: // "min"
			tpu = 1200
		}
		// rate_per_min = (rateNum * 1200) / (rateDen * tpu)  — already in /min when tpu==1200
		newNum := int64(rateNum) * 1200
		newDen := int64(rateDen) * tpu
		g := gcd64(absInt64(newNum), absInt64(newDen))
		newNum /= g
		newDen /= g

		cur := m[k]
		if cur.den == 0 {
			m[k] = rat{newNum, newDen}
			return
		}
		// Add fractions: a/b + c/d = (a*d + c*b) / (b*d)
		sumNum := cur.num*newDen + newNum*cur.den
		sumDen := cur.den * newDen
		g = gcd64(absInt64(sumNum), absInt64(sumDen))
		m[k] = rat{sumNum / g, sumDen / g}
	}

	for _, io := range ios {
		k := key{io.PLIO.ModID, io.PLIO.ItemFluidID, io.PLIO.IOType}
		if io.PLIO.Direction == "output" {
			addRate(outputs, k, io.PLIO.RateNum, io.PLIO.RateDen, io.TimeUnit)
		} else if io.PLIO.IsStopPoint {
			addRate(inputs, k, io.PLIO.RateNum, io.PLIO.RateDen, io.TimeUnit)
		}
	}

	toEntries := func(m map[key]rat) []balanceEntry {
		out := make([]balanceEntry, 0, len(m))
		for k, v := range m {
			out = append(out, balanceEntry{
				ModID: k.modID, ItemFluidID: k.itemFluidID,
				IOType: k.ioType, RateNum: v.num, RateDen: v.den, Unit: "min",
			})
		}
		return out
	}

	extInputs := toEntries(inputs)
	outs := toEntries(outputs)

	// Surplus = outputs - external_inputs per item.
	surplusMap := map[key]rat{}
	maps.Copy(surplusMap, outputs)
	for k, inp := range inputs {
		cur := surplusMap[k]
		if cur.den == 0 {
			// Input with no matching output → negative surplus.
			surplusMap[k] = rat{-inp.num, inp.den}
			continue
		}
		diffNum := cur.num*inp.den - inp.num*cur.den
		diffDen := cur.den * inp.den
		g := gcd64(absInt64(diffNum), absInt64(diffDen))
		surplusMap[k] = rat{diffNum / g, diffDen / g}
	}
	surplus := make([]surplusEntry, 0, len(surplusMap))
	for k, v := range surplusMap {
		surplus = append(surplus, surplusEntry{
			balanceEntry: balanceEntry{
				ModID: k.modID, ItemFluidID: k.itemFluidID,
				IOType: k.ioType, RateNum: v.num, RateDen: v.den, Unit: "min",
			},
			IsBottleneck: v.num < 0,
		})
	}

	return factoryBalance{ExternalInputs: extInputs, Outputs: outs, Surplus: surplus}
}

func gcd64(a, b int64) int64 {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// ListFactoriesHandler returns all factories belonging to a save.
func ListFactoriesHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		saveID, ok := parseUUIDParam(w, r, "save_id")
		if !ok {
			return
		}
		if err := requireSaveOwner(r, w, database, saveID, userID); err != nil {
			return
		}
		factories, err := database.ListFactoriesBySave(r.Context(), saveID)
		if err != nil {
			errInternal(w, err)
			return
		}
		if factories == nil {
			factories = []*model.Factory{}
		}
		writeJSON(w, http.StatusOK, factories)
	}
}

// CreateFactoryHandler creates a named factory under the given save.
func CreateFactoryHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		saveID, ok := parseUUIDParam(w, r, "save_id")
		if !ok {
			return
		}
		if err := requireSaveOwner(r, w, database, saveID, userID); err != nil {
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			errBadRequest(w, "invalid JSON")
			return
		}
		body.Name = strings.TrimSpace(body.Name)
		if body.Name == "" {
			errBadRequest(w, "name is required")
			return
		}
		f, err := database.CreateFactory(r.Context(), saveID, body.Name)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, f)
	}
}

// GetFactoryHandler fetches a single factory by ID with its computed balance.
func GetFactoryHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}
		f, err := database.GetFactory(r.Context(), factoryID)
		if err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		ios, err := database.ListActiveIOByFactory(r.Context(), factoryID)
		if err != nil {
			errInternal(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"factory": f,
			"balance": computeBalance(ios),
		})
	}
}

// DeleteFactoryHandler removes a factory by ID, enforcing ownership.
func DeleteFactoryHandler(database *db.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := userIDFromContext(r.Context())
		factoryID, ok := parseUUIDParam(w, r, "factory_id")
		if !ok {
			return
		}
		if err := requireFactoryOwner(r, w, database, factoryID, userID); err != nil {
			return
		}
		if err := database.DeleteFactory(r.Context(), factoryID); err != nil {
			if errors.Is(err, db.ErrNotFound) {
				errNotFound(w)
				return
			}
			errInternal(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// requireSaveOwner verifies the requesting user owns the given save.
func requireSaveOwner(r *http.Request, w http.ResponseWriter, database *db.DB, saveID, userID uuid.UUID) error {
	if _, err := database.GetSave(r.Context(), saveID, userID); err != nil {
		if errors.Is(err, db.ErrNotFound) {
			errNotFound(w)
		} else {
			errInternal(w, err)
		}
		return err
	}
	return nil
}

// requireFactoryOwner verifies the requesting user owns the given factory.
func requireFactoryOwner(r *http.Request, w http.ResponseWriter, database *db.DB, factoryID, userID uuid.UUID) error {
	ownerID, err := database.FactoryOwnerUserID(r.Context(), factoryID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			errNotFound(w)
		} else {
			errInternal(w, err)
		}
		return err
	}
	if ownerID != userID {
		errForbidden(w)
		return errors.New("forbidden")
	}
	return nil
}
