package resource

import "strings"

// Ref names an item, fluid or tag of a resource kind.
type Ref struct {
	ModID  string `json:"mod_id"`
	ID     string `json:"id"`
	TagRef string `json:"tag_ref"` // if set, this is a tag requirement rather than a specific item
	Kind   Kind   `json:"kind"`
}

func RefFromKey(key string) Ref {
	kind, rest := SplitKey(key)
	if tag, ok := strings.CutPrefix(rest, "#"); ok {
		return Ref{TagRef: tag, Kind: kind}
	}
	mod, id, ok := strings.Cut(rest, ":")
	if !ok {
		return Ref{ID: rest, Kind: kind}
	}
	return Ref{ModID: mod, ID: id, Kind: kind}
}

// Key returns the canonical string key for use in maps.
func (i *Ref) Key() string {
	prefix := Prefix(i.Kind)
	if i.TagRef != "" {
		return prefix + "#" + i.TagRef
	}
	return prefix + i.ModID + ":" + i.ID
}

// IO is one input or output of a recipe; Prob is the chance it applies and Consumed is false for a tool.
type IO struct {
	Ref      Ref      `json:"ref"`
	Amount   Rational `json:"amount"`
	Prob     Rational `json:"probability"`
	Consumed bool     `json:"consumed"`
}
