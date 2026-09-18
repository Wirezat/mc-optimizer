package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// evalTimeout bounds a single evaluate execution, and also bounds a VM's top-level
// bootstrap code (see newInstance).
const evalTimeout = 2 * time.Second

// maxVariants bounds how many variants a plugin may return per call.
const maxVariants = 256

// Program is a compiled plugin plus its VM pool.
type Program struct {
	modID string
	prog  *goja.Program
	pool  sync.Pool
	// hasWizard is resolved once in Compile, on the eager instance, before the Program is
	// published — never from a pooled VM, which several goroutines build concurrently.
	hasWizard bool
}

// HasWizard reports whether the plugin binds a wizard with a mount function.
func (p *Program) HasWizard() bool { return p.hasWizard }

// instance is a warmed-up VM with its evaluate function already resolved.
type instance struct {
	vm *goja.Runtime
	fn goja.Callable
}

// Compile parses the plugin source and checks that it binds an object `plugin` with a
// function `evaluate`.
func Compile(modID, source string) (*Program, error) {
	prog, err := goja.Compile(modID+"/plugin.js", source, true)
	if err != nil {
		return nil, fmt.Errorf("plugins: compile %s: %w", modID, err)
	}
	p := &Program{modID: modID, prog: prog}
	p.pool.New = func() any {
		inst, err := p.newInstance()
		if err != nil {
			return err
		}
		return inst
	}
	// Build one instance right away: a plugin missing `evaluate` fails here.
	inst, err := p.newInstance()
	if err != nil {
		return nil, err
	}
	p.hasWizard = bindsWizardMount(inst.vm)
	p.pool.Put(inst)
	return p, nil
}

// bindsWizardMount reports whether plugin.wizard.mount is a function.
func bindsWizardMount(vm *goja.Runtime) bool {
	obj := vm.Get("plugin")
	if obj == nil || goja.IsUndefined(obj) || goja.IsNull(obj) {
		return false
	}
	wizard, ok := obj.ToObject(vm).Get("wizard").(*goja.Object)
	if !ok {
		return false
	}
	_, ok = goja.AssertFunction(wizard.Get("mount"))
	return ok
}

// newInstance builds a fresh, isolated VM bound to the compiled program and resolves its
// evaluate function.
func (p *Program) newInstance() (*instance, error) {
	vm := goja.New()
	vm.SetFieldNameMapper(goja.TagFieldNameMapper("json", true))
	hardenGlobals(vm)

	if err := guard(context.Background(), evalTimeout, vm, func() error {
		_, runErr := vm.RunProgram(p.prog)
		return runErr
	}); err != nil {
		return nil, fmt.Errorf("plugins: run %s: %w", p.modID, err)
	}
	obj := vm.Get("plugin")
	if obj == nil || goja.IsUndefined(obj) || goja.IsNull(obj) {
		return nil, fmt.Errorf("plugins: %s defines no `plugin` object", p.modID)
	}
	fn, ok := goja.AssertFunction(obj.ToObject(vm).Get("evaluate"))
	if !ok {
		return nil, fmt.Errorf("plugins: %s has no evaluate function", p.modID)
	}
	return &instance{vm: vm, fn: fn}, nil
}

// hardenGlobals removes non-deterministic global sources so evaluate() is a pure function
// of its input.
func hardenGlobals(vm *goja.Runtime) {
	global := vm.GlobalObject()
	_ = global.Delete("Date")
	if math, ok := global.Get("Math").(*goja.Object); ok {
		_ = math.Delete("random")
	}
}

// Evaluate calls evaluate(ctx) and returns the plugin's variants.
func (p *Program) Evaluate(ctx context.Context, ec EvalContext) ([]Variant, error) {
	pooled := p.pool.Get()
	if err, isErr := pooled.(error); isErr {
		return nil, err
	}
	inst := pooled.(*instance)

	// The instance returns to the pool only after a clean run.
	keep := false
	defer func() {
		if keep {
			p.pool.Put(inst)
		}
	}()

	arg, err := toJSValue(inst.vm, ec)
	if err != nil {
		return nil, err
	}

	var out []Variant
	err = guard(ctx, evalTimeout, inst.vm, func() error {
		res, ferr := inst.fn(goja.Undefined(), arg)
		if ferr != nil {
			return fmt.Errorf("plugins: %s evaluate: %w", p.modID, ferr)
		}
		variants, eerr := extractVariants(p.modID, res)
		if eerr != nil {
			return eerr
		}
		if verr := Validate(ec, variants); verr != nil {
			return verr
		}
		out = variants
		return nil
	})
	if err != nil {
		return nil, err
	}
	keep = true
	return out, nil
}

// guard runs fn against vm under a deadline derived from ctx and timeout, and turns any
// panic raised inside fn into a returned error.
func guard(ctx context.Context, timeout time.Duration, vm *goja.Runtime, fn func() error) (err error) {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	done := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-deadline.Done():
			vm.Interrupt(deadline.Err())
		case <-done:
		}
	}()
	defer func() {
		close(done)
		<-watcherDone
		vm.ClearInterrupt()
	}()
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("plugins: panic: %v", r)
		}
	}()

	return fn()
}

// toJSValue passes the context through JSON so json.RawMessage fields arrive as real JS
// objects instead of strings.
func toJSValue(vm *goja.Runtime, ec EvalContext) (goja.Value, error) {
	raw, err := json.Marshal(ec)
	if err != nil {
		return nil, fmt.Errorf("plugins: encode context: %w", err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return nil, fmt.Errorf("plugins: decode context: %w", err)
	}
	return vm.ToValue(generic), nil
}
