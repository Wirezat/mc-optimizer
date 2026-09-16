package plugins

import (
	"os"
	"testing"

	"github.com/dop251/goja"
)

// miMachinesVM runs the MI plugin source in a bare VM and returns the VM
// together with plugin.machines. The machines hooks execute in the browser,
// not in this package's Program, so they are exercised directly here - it is
// the only automated check that the tier rules ported from the host's
// solve.html still say what they said.
func miMachinesVM(t *testing.T) (*goja.Runtime, *goja.Object) {
	t.Helper()
	src, err := os.ReadFile("../../mod-plugins/modern_industrialization/plugin/plugin.js")
	if err != nil {
		t.Fatalf("read MI plugin: %v", err)
	}
	vm := goja.New()
	if _, err := vm.RunString(string(src)); err != nil {
		t.Fatalf("run MI plugin: %v", err)
	}
	plugin, ok := vm.Get("plugin").(*goja.Object)
	if !ok {
		t.Fatal("plugin is not an object")
	}
	machines, ok := plugin.Get("machines").(*goja.Object)
	if !ok {
		t.Fatal("plugin.machines is not an object")
	}
	return vm, machines
}

func callCell(t *testing.T, vm *goja.Runtime, machines *goja.Object, machineID, columnID string) string {
	t.Helper()
	fn, ok := goja.AssertFunction(machines.Get("cell"))
	if !ok {
		t.Fatal("machines.cell is not a function")
	}
	machine := vm.ToValue(map[string]any{"mod_id": "modern_industrialization", "machine_id": machineID})
	res, err := fn(goja.Undefined(), machine, vm.ToValue(columnID))
	if err != nil {
		t.Fatalf("cell: %v", err)
	}
	return res.String()
}

func TestMIMachinesDeclaresTierColumn(t *testing.T) {
	vm, machines := miMachinesVM(t)

	if !goja.IsUndefined(machines.Get("axes")) && machines.Get("axes") != nil {
		t.Error("machines.axes still declared; the tier lives in the config now")
	}
	cols := machines.Get("columns").ToObject(vm)
	if n := cols.Get("length").ToInteger(); n != 1 {
		t.Fatalf("columns length = %d, want 1", n)
	}
	if id := cols.Get("0").ToObject(vm).Get("id").String(); id != "tier" {
		t.Errorf("column id = %q, want \"tier\"", id)
	}
}

// The column text: the blast-furnace family is the one place where two
// machines of the same tier differ, by coil material, and the names come
// from the catalog (Electric Blast Furnace (Kanthal Coils) /
// (Cupronickel Coils)).
func TestMIMachinesCellLabelsTier(t *testing.T) {
	vm, machines := miMachinesVM(t)
	for machineID, want := range map[string]string{
		"bronze_macerator":                   "Bronze",
		"steel_macerator":                    "Steel",
		"macerator":                          "Electric",
		"steam_blast_furnace":                "Steam",
		"electric_blast_furnace":             "Kanthal",
		"electric_blast_furnace_cupronickel": "Cupronickel",
	} {
		if got := callCell(t, vm, machines, machineID, "tier"); got != want {
			t.Errorf("cell(%q, tier) = %q, want %q", machineID, got, want)
		}
	}
	if got := callCell(t, vm, machines, "macerator", "not_a_column"); got != "" {
		t.Errorf("cell for an unknown column = %q, want \"\"", got)
	}
}

// A plugin cannot rank machines it never sees; the solver compares across mods.
func TestMIHasNoResolve(t *testing.T) {
	_, machines := miMachinesVM(t)
	if _, ok := goja.AssertFunction(machines.Get("resolve")); ok {
		t.Error("machines.resolve is still defined")
	}
}
