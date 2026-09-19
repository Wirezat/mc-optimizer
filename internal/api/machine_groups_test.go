package api

import (
	"errors"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/plugins"
)

// A switch target the solver would never have picked itself must not become
// reachable through the API: solver.PickVariant skips any variant with
// Valid: false, and this check is what holds SetGroupVariantHandler to the
// same rule.
func TestCheckSwitchTargetRejectsInvalidVariant(t *testing.T) {
	to := plugins.Variant{ID: "eco", Valid: false}
	err := checkSwitchTarget(to, true)
	if !errors.Is(err, errInvalidVariant) {
		t.Fatalf("err = %v, want errInvalidVariant", err)
	}
}

func TestCheckSwitchTargetRejectsUnknownVariant(t *testing.T) {
	err := checkSwitchTarget(plugins.Variant{}, false)
	if !errors.Is(err, errUnknownVariant) {
		t.Fatalf("err = %v, want errUnknownVariant", err)
	}
}

func TestCheckSwitchTargetAcceptsValidResolvedVariant(t *testing.T) {
	to := plugins.Variant{ID: "eco", Valid: true}
	if err := checkSwitchTarget(to, true); err != nil {
		t.Fatalf("err = %v, want nil for a resolved, valid variant", err)
	}
}
