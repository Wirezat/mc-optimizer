package importer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Wirezat/production-optimizer/internal/resource"
)

func TestZZToolHashMap(t *testing.T) {
	out := os.Getenv("TOOL_MAP_OUT")
	if out == "" {
		t.Skip()
	}
	files, _ := filepath.Glob("../../mod-sources/modfiles/*.yml")
	var b strings.Builder
	for _, f := range files {
		data, _ := os.ReadFile(f)
		def, err := ParseModFile(data)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		for _, r := range def.Recipes {
			tool, tagTool := false, false
			legacy := r
			legacy.Inputs = append([]resource.IO(nil), r.Inputs...)
			for i, io := range legacy.Inputs {
				if io.Consumed {
					continue
				}
				tool = true
				legacy.Inputs[i].Consumed = true
				if io.Ref.TagRef != "" {
					tagTool = true
					legacy.Inputs[i].Prob = resource.NewRational(1, 1)
				} else {
					legacy.Inputs[i].Prob = resource.NewRational(0, 1)
				}
			}
			if !tool {
				continue
			}
			var tools []string
			for _, io := range r.Inputs {
				if !io.Consumed {
					tools = append(tools, io.Ref.Key())
				}
			}
			fmt.Fprintf(&b, "%s,%s,%t,%s,%s\n", ContentHash(ModRecipeToNormalized(legacy, def.ModID)), ContentHash(ModRecipeToNormalized(r, def.ModID)), tagTool, filepath.Base(f), strings.Join(tools, " "))
		}
	}
	os.WriteFile(out, []byte(b.String()), 0o644)
}
