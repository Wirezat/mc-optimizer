// Package resource holds the resource kinds (item, fluid, energy) as loaded from resource_kinds.
package resource

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Kind names a resource kind, a row of resource_kinds; the empty Kind means KindItem.
type Kind string

// MarshalText writes the empty Kind as KindItem.
func (k Kind) MarshalText() ([]byte, error) {
	return []byte(k.Or()), nil
}

// Or returns k, or KindItem for the empty Kind.
func (k Kind) Or() Kind {
	if k == "" {
		return KindItem
	}
	return k
}

const (
	KindItem   Kind = "item"
	KindFluid  Kind = "fluid"
	KindEnergy Kind = "energy"
)

// ExpandRule says whether the solver resolves a node of a kind through recipes.
type ExpandRule string

const (
	ExpandAlways   ExpandRule = "always"
	ExpandOnChoice ExpandRule = "on_choice"
	ExpandNever    ExpandRule = "never"
)

// KindInfo is one row of resource_kinds.
type KindInfo struct {
	Kind      Kind
	KeyPrefix string
	BaseUnit  string
	UOMSystem string
	Expand    ExpandRule
}

var (
	mu       sync.RWMutex
	registry map[Kind]KindInfo
	prefixes []KindInfo
)

// Load validates kinds and makes them the registry; on error the previous registry stays.
func Load(kinds []KindInfo) error {
	byKind := make(map[Kind]KindInfo, len(kinds))
	byPrefix := make(map[string]Kind, len(kinds))
	for _, k := range kinds {
		if _, dup := byKind[k.Kind]; dup {
			return fmt.Errorf("resource kinds: %q listed twice", k.Kind)
		}
		switch k.Expand {
		case ExpandAlways, ExpandOnChoice, ExpandNever:
		default:
			return fmt.Errorf("resource kinds: %q has expand %q", k.Kind, k.Expand)
		}
		if k.KeyPrefix != "" && !strings.HasSuffix(k.KeyPrefix, ":") {
			return fmt.Errorf("resource kinds: %q has key prefix %q without a trailing colon", k.Kind, k.KeyPrefix)
		}
		if other, dup := byPrefix[k.KeyPrefix]; dup {
			return fmt.Errorf("resource kinds: %q and %q share key prefix %q", other, k.Kind, k.KeyPrefix)
		}
		byKind[k.Kind] = k
		byPrefix[k.KeyPrefix] = k.Kind
	}
	for _, required := range []Kind{KindItem, KindFluid, KindEnergy} {
		if _, ok := byKind[required]; !ok {
			return fmt.Errorf("resource kinds: %q missing", required)
		}
	}
	ordered := make([]KindInfo, 0, len(byKind))
	for _, k := range byKind {
		ordered = append(ordered, k)
	}
	sort.Slice(ordered, func(i, j int) bool { return len(ordered[i].KeyPrefix) > len(ordered[j].KeyPrefix) })

	mu.Lock()
	registry, prefixes = byKind, ordered
	mu.Unlock()
	return nil
}

// Info returns the registry row for k and whether k is known.
func Info(k Kind) (KindInfo, bool) {
	mu.RLock()
	defer mu.RUnlock()
	info, ok := registry[k.Or()]
	return info, ok
}

// Prefix returns k's key prefix; it panics for a kind the registry does not know.
func Prefix(k Kind) string {
	return mustInfo(k).KeyPrefix
}

// Expand returns k's expansion rule; it panics for a kind the registry does not know.
func Expand(k Kind) ExpandRule {
	return mustInfo(k).Expand
}

// SplitKey splits a resource key into its kind and the rest after the kind's prefix.
func SplitKey(key string) (Kind, string) {
	mu.RLock()
	defer mu.RUnlock()
	for _, k := range prefixes {
		if strings.HasPrefix(key, k.KeyPrefix) {
			return k.Kind, key[len(k.KeyPrefix):]
		}
	}
	return KindItem, key
}

func mustInfo(k Kind) KindInfo {
	info, ok := Info(k)
	if !ok {
		panic(fmt.Sprintf("resource: kind %q not in the registry", k))
	}
	return info
}
