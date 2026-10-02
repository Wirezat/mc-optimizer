package api

import (
	"os"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

func TestMain(m *testing.M) {
	resource.LoadForTest()
	os.Exit(m.Run())
}
