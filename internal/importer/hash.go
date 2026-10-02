package importer

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Wirezat/production-optimizer/internal/model"
	"github.com/Wirezat/production-optimizer/internal/resource"
)

// ContentHash computes a stable SHA-256 fingerprint of a normalized recipe.
func ContentHash(n model.NormalizedRecipe) string {
	type itemEntry struct {
		Ref     string `json:"r"` // "mod:id" or "#tag"
		Amount  int    `json:"a"`
		ProbNum int    `json:"pn"`
		ProbDen int    `json:"pd"`
		Tool    bool   `json:"nc,omitempty"`
	}
	type fluidEntry struct {
		Fluid   string `json:"f"`
		Amount  int64  `json:"a"`
		ProbNum int    `json:"pn"`
		ProbDen int    `json:"pd"`
		Tool    bool   `json:"nc,omitempty"`
	}
	type canonical struct {
		Mod      string       `json:"m"`
		Machine  string       `json:"mc"`
		Duration int          `json:"d"`
		II       []itemEntry  `json:"ii"`
		IO       []itemEntry  `json:"io"`
		FI       []fluidEntry `json:"fi"`
		FO       []fluidEntry `json:"fo"`
	}

	c := canonical{
		Mod:      n.ModID,
		Machine:  n.MachineID,
		Duration: n.Duration,
	}
	addIO := func(ios []resource.IO, items *[]itemEntry, fluids *[]fluidEntry) {
		for _, io := range ios {
			switch io.Ref.Kind.Or() {
			case resource.KindFluid:
				*fluids = append(*fluids, fluidEntry{hashRef(io.Ref), io.Amount.Num, int(io.Prob.Num), int(io.Prob.Den), !io.Consumed})
			default:
				*items = append(*items, itemEntry{hashRef(io.Ref), int(io.Amount.Num), int(io.Prob.Num), int(io.Prob.Den), !io.Consumed})
			}
		}
	}
	addIO(n.Inputs, &c.II, &c.FI)
	addIO(n.Outputs, &c.IO, &c.FO)

	sort.Slice(c.II, func(i, j int) bool { return c.II[i].Ref < c.II[j].Ref })
	sort.Slice(c.IO, func(i, j int) bool { return c.IO[i].Ref < c.IO[j].Ref })
	sort.Slice(c.FI, func(i, j int) bool { return c.FI[i].Fluid < c.FI[j].Fluid })
	sort.Slice(c.FO, func(i, j int) bool { return c.FO[i].Fluid < c.FO[j].Fluid })

	data, _ := json.Marshal(c)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func hashRef(r resource.Ref) string {
	if r.TagRef != "" {
		return "#" + r.TagRef
	}
	return r.ModID + ":" + r.ID
}
