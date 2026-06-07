package solver

import (
	"errors"
	"fmt"
)

var (
	ErrNoSolution      = errors.New("no solution: system is inconsistent")
	ErrUnderDetermined = errors.New("underdetermined system: infinite solutions")
)

// BuildStoichiometryMatrix creates the stoichiometry matrix S (items × recipes) from the recipe graph.
// Returns S, the list of items, and the list of recipe IDs.
func BuildStoichiometryMatrix(g *RecipeGraph) ([][]Rational, []ItemRef, []string) {
	itemIdx, recipeIdx := map[string]int{}, map[string]int{}
	var items []ItemRef
	var recipeIDs []string

	for key, node := range g.Nodes {
		if node.IsStopPoint || node.IsRawMaterial || node.IsFactoryProvided {
			continue
		}
		if _, ok := itemIdx[key]; !ok {
			itemIdx[key] = len(items)
			items = append(items, node.Item)
		}
	}
	for _, node := range g.Nodes {
		if node.RecipeID != "" {
			if _, ok := recipeIdx[node.RecipeID]; !ok {
				recipeIdx[node.RecipeID] = len(recipeIDs)
				recipeIDs = append(recipeIDs, node.RecipeID)
			}
		}
	}

	m, n := len(items), len(recipeIDs)
	S := make([][]Rational, m)
	for i := range S {
		S[i] = make([]Rational, n)
		for j := range S[i] {
			S[i][j] = NewRational(0, 1)
		}
	}

	for _, node := range g.Nodes {
		if node.RecipeID == "" {
			continue
		}
		j := recipeIdx[node.RecipeID]
		if i, ok := itemIdx[node.Item.Key()]; ok {
			S[i][j] = S[i][j].Add(node.OutputAmount)
		}
		for _, e := range node.Inputs {
			if i, ok := itemIdx[e.Item.Key()]; ok {
				S[i][j] = S[i][j].Sub(e.Amount.Mul(e.Probability))
			}
		}
	}
	return S, items, recipeIDs
}

// GaussJordanRational performs Gauss‑Jordan elimination on the augmented matrix [S|b] over rationals.
// Returns a solution vector x or an error (ErrNoSolution, ErrUnderDetermined).
func GaussJordanRational(S [][]Rational, b []Rational) ([]Rational, error) {
	m := len(S)
	if m == 0 {
		return nil, ErrNoSolution
	}
	n := len(S[0])

	aug := make([][]Rational, m)
	for i := range aug {
		aug[i] = make([]Rational, n+1)
		copy(aug[i], S[i])
		aug[i][n] = b[i]
	}

	pivotRow := 0
	pivotCols := make([]int, 0, n)

	for col := 0; col < n && pivotRow < m; col++ {
		pivot := -1
		for row := pivotRow; row < m; row++ {
			if !aug[row][col].IsZero() {
				pivot = row
				break
			}
		}
		if pivot == -1 {
			continue
		}
		aug[pivotRow], aug[pivot] = aug[pivot], aug[pivotRow]

		pv := aug[pivotRow][col]
		for j := col; j <= n; j++ {
			aug[pivotRow][j] = aug[pivotRow][j].Div(pv)
		}
		// Korrigierte Schleife: for row := 0; row < m; row++
		for row := 0; row < m; row++ {
			if row == pivotRow || aug[row][col].IsZero() {
				continue
			}
			f := aug[row][col]
			for j := col; j <= n; j++ {
				aug[row][j] = aug[row][j].Sub(f.Mul(aug[pivotRow][j]))
			}
		}
		pivotCols = append(pivotCols, col)
		pivotRow++
	}

	for i := pivotRow; i < m; i++ {
		if !aug[i][n].IsZero() {
			return nil, ErrNoSolution
		}
	}
	if len(pivotCols) < n {
		return nil, ErrUnderDetermined
	}

	r := make([]Rational, n)
	for i, col := range pivotCols {
		r[col] = aug[i][n]
	}
	return r, nil
}

// SolveLinearSystem solves the stoichiometry matrix for the recipe graph given a target rate per tick.
// Returns a RateVector mapping recipe IDs and item keys to rational rates.
func SolveLinearSystem(g *RecipeGraph, targetRatePerTick Rational) (RateVector, error) {
	S, items, recipeIDs := BuildStoichiometryMatrix(g)
	if len(recipeIDs) == 0 {
		return RateVector{}, fmt.Errorf("solver: no recipes in graph")
	}
	m, n := len(items), len(recipeIDs)
	rootKey := g.Root.Key()

	b := make([]Rational, m)
	for i, item := range items {
		if item.Key() == rootKey {
			b[i] = targetRatePerTick
		} else {
			b[i] = NewRational(0, 1)
		}
	}

	Sred := make([][]Rational, m)
	for i := range Sred {
		Sred[i] = make([]Rational, n)
		copy(Sred[i], S[i])
	}

	r, err := GaussJordanRational(Sred, b)
	if err != nil {
		return RateVector{}, fmt.Errorf("solver: linear system: %w", err)
	}
	for j, rate := range r {
		if rate.IsNegative() {
			return RateVector{}, fmt.Errorf("solver: recipe %s has negative rate — check recipe chain", recipeIDs[j])
		}
	}

	rv := newRateVector()
	for j, id := range recipeIDs {
		rv.RecipeRates[id] = r[j]
	}
	for i, item := range items {
		net := NewRational(0, 1)
		for j := range recipeIDs {
			net = net.Add(S[i][j].Mul(r[j]))
		}
		rv.ItemRates[item.Key()] = net
	}
	return rv, nil
}
