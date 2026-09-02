package importer

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/Wirezat/production-optimizer/internal/model"
)

// ContentHash computes a stable SHA-256 fingerprint of a normalized recipe.
// Two recipes with identical machines, EU, duration, and IO (regardless of order)
// produce the same hash — this is used for idempotent imports.
func ContentHash(n model.NormalizedRecipe) string {
	type itemEntry struct {
		Ref     string `json:"r"` // "mod:id" or "#tag"
		Amount  int    `json:"a"`
		ProbNum int    `json:"pn"`
		ProbDen int    `json:"pd"`
	}
	type fluidEntry struct {
		Fluid   string `json:"f"`
		Amount  int64  `json:"a"`
		ProbNum int    `json:"pn"`
		ProbDen int    `json:"pd"`
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
	for _, io := range n.ItemInputs {
		c.II = append(c.II, itemEntry{ioRef(io), io.AmountNum, io.ProbNum, io.ProbDen})
	}
	for _, io := range n.ItemOutputs {
		c.IO = append(c.IO, itemEntry{ioRef(io), io.AmountNum, io.ProbNum, io.ProbDen})
	}
	for _, io := range n.FluidInputs {
		c.FI = append(c.FI, fluidEntry{ioRef(io), io.AmountMB, io.ProbNum, io.ProbDen})
	}
	for _, io := range n.FluidOutputs {
		c.FO = append(c.FO, fluidEntry{ioRef(io), io.AmountMB, io.ProbNum, io.ProbDen})
	}

	sort.Slice(c.II, func(i, j int) bool { return c.II[i].Ref < c.II[j].Ref })
	sort.Slice(c.IO, func(i, j int) bool { return c.IO[i].Ref < c.IO[j].Ref })
	sort.Slice(c.FI, func(i, j int) bool { return c.FI[i].Fluid < c.FI[j].Fluid })
	sort.Slice(c.FO, func(i, j int) bool { return c.FO[i].Fluid < c.FO[j].Fluid })

	data, _ := json.Marshal(c)
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func ioRef(io model.NormalizedIO) string {
	if io.TagName != nil {
		return "#" + *io.TagName
	}
	return *io.ModID + ":" + *io.ID
}
