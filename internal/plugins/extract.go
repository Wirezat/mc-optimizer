package plugins

import (
	"fmt"
	"math"
	"reflect"
	"strconv"

	"github.com/dop251/goja"
)

// reflectTypeBool and reflectTypeInt64 are the Go types goja.Value.ExportType() reports for
// a JS boolean and for goja's internal integer representation of a JS number.
var (
	reflectTypeBool  = reflect.TypeOf(false)
	reflectTypeInt64 = reflect.TypeOf(int64(0))
)

// maxNestedItems bounds costs, outputs and items within a single variant.
const maxNestedItems = 64

// maxStringLen bounds a single extracted string, counted in UTF-16 code units.
const maxStringLen = 256

const maxStringBytes = 4 << 20

// maxInt64Float is 2^63 as a float64: the smallest float64 strictly greater than
// math.MaxInt64, which is itself not representable as a float64.
const maxInt64Float = float64(1 << 63)

// jsClassArray is the ECMAScript [[Class]] tag goja reports via (*Object).ClassName() for a
// genuine Array.
const jsClassArray = "Array"

// extractor holds the per-call state of one extraction: the mod being extracted, for error
// messages, and the string byte budget left for the rest of this evaluate call (see
// maxStringBytes).
type extractor struct {
	modID       string
	stringBytes int
}

// extractVariants converts a plugin's evaluate() return value into Variants by walking the
// expected shape by hand: every level gets its own class check, its own length cap, and
// reads its elements strictly by index.
func extractVariants(modID string, res goja.Value) ([]Variant, error) {
	ex := &extractor{modID: modID, stringBytes: maxStringBytes}
	elems, err := ex.readArray(res, maxVariants, "evaluate must return an array", "variants")
	if err != nil {
		return nil, err
	}
	out := make([]Variant, len(elems))
	for i, ev := range elems {
		v, err := ex.extractVariant(fmt.Sprintf("variants[%d]", i), ev)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// extractVariant reads the Variant fields off a single array element. A field that is
// absent (undefined) becomes its zero value; a field that is present but the wrong JS type
// is an error, never a silent default.
func (ex *extractor) extractVariant(what string, v goja.Value) (Variant, error) {
	obj, isObj := v.(*goja.Object)
	if !isObj {
		return Variant{}, fmt.Errorf("plugins: %s returned an unusable value: %s must be an object", ex.modID, what)
	}

	id, err := ex.readString(what, obj, "id")
	if err != nil {
		return Variant{}, err
	}
	label, err := ex.readString(what, obj, "label")
	if err != nil {
		return Variant{}, err
	}
	rate, err := ex.readRational(what, obj, "rate")
	if err != nil {
		return Variant{}, err
	}
	valid, err := ex.readBool(what, obj, "valid")
	if err != nil {
		return Variant{}, err
	}
	costs, err := extractList(ex, what+".costs", obj.Get("costs"), (*extractor).extractCost)
	if err != nil {
		return Variant{}, err
	}
	outputs, err := extractList(ex, what+".outputs", obj.Get("outputs"), (*extractor).extractOutput)
	if err != nil {
		return Variant{}, err
	}
	items, err := extractList(ex, what+".items", obj.Get("items"), (*extractor).extractItem)
	if err != nil {
		return Variant{}, err
	}
	rank, err := ex.readInt(what, obj, "rank")
	if err != nil {
		return Variant{}, err
	}

	return Variant{
		ID: id, Label: label, Rate: rate, Valid: valid,
		Costs: costs, Outputs: outputs, Items: items, Rank: int(rank),
	}, nil
}

// extractList reads a bounded, optional array field (costs, outputs or items) and converts
// each element with elemFn.
func extractList[T any](ex *extractor, what string, v goja.Value, elemFn func(*extractor, string, *goja.Object) (T, error)) ([]T, error) {
	if v == nil || goja.IsUndefined(v) {
		return nil, nil
	}
	elems, err := ex.readArray(v, maxNestedItems, what+" must be an array", what)
	if err != nil {
		return nil, err
	}
	out := make([]T, len(elems))
	for i, ev := range elems {
		eobj, isObj := ev.(*goja.Object)
		if !isObj {
			return nil, fmt.Errorf("plugins: %s returned an unusable value: %s[%d] must be an object", ex.modID, what, i)
		}
		v, err := elemFn(ex, fmt.Sprintf("%s[%d]", what, i), eobj)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func (ex *extractor) extractCost(what string, obj *goja.Object) (Cost, error) {
	resource, err := ex.readString(what, obj, "resource")
	if err != nil {
		return Cost{}, err
	}
	amount, err := ex.readRational(what, obj, "amount")
	if err != nil {
		return Cost{}, err
	}
	return Cost{Resource: resource, Amount: amount}, nil
}

func (ex *extractor) extractOutput(what string, obj *goja.Object) (Output, error) {
	ref, err := ex.readString(what, obj, "ref")
	if err != nil {
		return Output{}, err
	}
	amount, err := ex.readRational(what, obj, "amount")
	if err != nil {
		return Output{}, err
	}
	probability, err := ex.readRational(what, obj, "probability")
	if err != nil {
		return Output{}, err
	}
	return Output{Ref: ref, Amount: amount, Probability: probability}, nil
}

func (ex *extractor) extractItem(what string, obj *goja.Object) (Item, error) {
	ref, err := ex.readString(what, obj, "ref")
	if err != nil {
		return Item{}, err
	}
	count, err := ex.readInt(what, obj, "count")
	if err != nil {
		return Item{}, err
	}
	return Item{Ref: ref, Count: int(count)}, nil
}

// readArray validates that v is a genuine JS array (ClassName() == "Array") of at most max
// elements and returns its elements read one at a time by index.
func (ex *extractor) readArray(v goja.Value, max int, notArrayMsg, label string) ([]goja.Value, error) {
	obj, isObj := v.(*goja.Object)
	if !isObj || obj.ClassName() != jsClassArray {
		return nil, fmt.Errorf("plugins: %s returned an unusable value: %s", ex.modID, notArrayMsg)
	}
	lengthVal := obj.Get("length")
	if lengthVal == nil || goja.IsUndefined(lengthVal) {
		return nil, fmt.Errorf("plugins: %s returned an unusable value: %s", ex.modID, notArrayMsg)
	}
	n := int(lengthVal.ToInteger())
	if n < 0 {
		return nil, fmt.Errorf("plugins: %s returned an unusable value: %s", ex.modID, notArrayMsg)
	}
	if n > max {
		return nil, fmt.Errorf("plugins: %s returned %d %s, limit is %d", ex.modID, n, label, max)
	}
	elems := make([]goja.Value, n)
	for i := 0; i < n; i++ {
		elems[i] = obj.Get(strconv.Itoa(i))
	}
	return elems, nil
}

// readString reads a string field off obj.
func (ex *extractor) readString(what string, obj *goja.Object, field string) (string, error) {
	v := obj.Get(field)
	if v == nil || goja.IsUndefined(v) {
		return "", nil
	}
	s, isStr := v.(goja.String)
	if !isStr {
		return "", fmt.Errorf("plugins: %s returned an unusable value: %s.%s must be a string", ex.modID, what, field)
	}
	if n := s.Length(); n > maxStringLen {
		return "", fmt.Errorf("plugins: %s returned an unusable value: %s.%s is %d characters long, limit is %d", ex.modID, what, field, n, maxStringLen)
	}
	out := s.String()
	if len(out) > ex.stringBytes {
		return "", fmt.Errorf("plugins: %s returned more than %d bytes of strings in one evaluate call", ex.modID, maxStringBytes)
	}
	ex.stringBytes -= len(out)
	return out, nil
}

// readBool reads a boolean field off obj.
func (ex *extractor) readBool(what string, obj *goja.Object, field string) (bool, error) {
	v := obj.Get(field)
	if v == nil || goja.IsUndefined(v) {
		return false, nil
	}
	if v.ExportType() != reflectTypeBool {
		return false, fmt.Errorf("plugins: %s returned an unusable value: %s.%s must be a boolean", ex.modID, what, field)
	}
	return v.ToBoolean(), nil
}

// readInt reads a numeric field off obj as an exact integer.
func (ex *extractor) readInt(what string, obj *goja.Object, field string) (int64, error) {
	v := obj.Get(field)
	if v == nil || goja.IsUndefined(v) {
		return 0, nil
	}
	if !goja.IsNumber(v) {
		return 0, fmt.Errorf("plugins: %s returned an unusable value: %s.%s must be a number", ex.modID, what, field)
	}
	if v.ExportType() == reflectTypeInt64 {
		return v.ToInteger(), nil
	}
	f := v.ToFloat()
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f < math.MinInt64 || f >= maxInt64Float {
		return 0, fmt.Errorf("plugins: %s returned an unusable value: %s.%s must be a whole number within int64 range, got %s",
			ex.modID, what, field, strconv.FormatFloat(f, 'g', -1, 64))
	}
	return int64(f), nil
}

// readRational reads a {num, den} field off obj as a Rational.
func (ex *extractor) readRational(what string, obj *goja.Object, field string) (Rational, error) {
	v := obj.Get(field)
	if v == nil || goja.IsUndefined(v) {
		return Rational{}, nil
	}
	ro, isObj := v.(*goja.Object)
	if !isObj {
		return Rational{}, fmt.Errorf("plugins: %s returned an unusable value: %s.%s must be an object", ex.modID, what, field)
	}
	sub := what + "." + field
	num, err := ex.readInt(sub, ro, "num")
	if err != nil {
		return Rational{}, err
	}
	den, err := ex.readInt(sub, ro, "den")
	if err != nil {
		return Rational{}, err
	}
	if den == 0 {
		return Rational{}, fmt.Errorf("plugins: %s returned an unusable value: %s.den must not be zero", ex.modID, sub)
	}
	return Rational{Num: num, Den: den}, nil
}
