package reconcile

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type fieldDiff struct {
	Value   any `json:"~~,omitempty"`
	Current any `json:"--,omitempty"`
	Desired any `json:"++,omitempty"`
}

type fieldDiffRecorder struct {
	root              string
	path              cmp.Path
	diffs             map[string]fieldDiff
	useDerivativeDiff bool
}

// PushStep implements cmp.Reporter interface
func (r *fieldDiffRecorder) PushStep(ps cmp.PathStep) {
	r.path = append(r.path, ps)
}

// PopStep implements cmp.Reporter interface
func (r *fieldDiffRecorder) PopStep() {
	r.path = r.path[:len(r.path)-1]
}

// Report implements cmp.Reporter interface
func (r *fieldDiffRecorder) Report(rs cmp.Result) {
	if rs.Equal() {
		return
	}
	desired, current := r.path.Last().Values()
	if r.useDerivativeDiff {
		switch desired.Kind() {
		case reflect.String, reflect.Slice:
			if desired.Len() == 0 {
				return
			}
		case reflect.Pointer:
			if desired.IsNil() {
				return
			}
		}
	}
	key := r.root
	var moved bool
	for i, path := range r.path {
		switch v := path.(type) {
		case cmp.StructField:
			name := v.Name()
			if i > 0 {
				parentType := r.path[i-1].Type()
				if parentType.Kind() == reflect.Ptr {
					parentType = parentType.Elem()
				}
				if parentType.Kind() == reflect.Struct {
					field, found := parentType.FieldByName(name)
					if found {
						jsonTag := field.Tag.Get("json")
						if jsonTag != "" && jsonTag != "-" {
							name = strings.Split(jsonTag, ",")[0]
						}
					}
				}
			}
			key = key + "." + name
		case cmp.SliceIndex:
			ix, iy := v.SplitKeys()
			switch {
			case ix == -1 || ix == iy:
				key += fmt.Sprintf("[%d]", iy)
			case iy == -1:
				key += fmt.Sprintf("[%d]", ix)
			default:
				moved = true
				key += fmt.Sprintf("[%d\u2192%d]", ix, iy)
			}
		case cmp.MapIndex:
			key += strings.ReplaceAll(v.String(), `"`, `'`)
		}
	}
	if !moved {
		r.diffs[key] = fieldDiff{
			Desired: toInterface(desired),
			Current: toInterface(current),
		}
		return
	}
	r.diffs[key] = fieldDiff{
		Value: toInterface(desired),
	}
}

func toInterface(v reflect.Value) any {
	if v.IsValid() && v.CanInterface() {
		switch v.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice:
			if v.IsNil() {
				return nil
			}
		}
		return v.Interface()
	}
	return nil
}

func diffDeep(desired, current any, root string) map[string]fieldDiff {
	return diffDeepInternal(desired, current, false, root)
}

func diffDeepDerivative(desired, current any, root string) map[string]fieldDiff {
	return diffDeepInternal(desired, current, true, root)
}

// diffDeepInternal is similar to diffDeep except that unset fields in desired are
// ignored (not compared). This allows us to focus on the fields that matter to
// the semantic comparison.
//
// The unset fields include a nil pointer and an empty string.
//
// Helper function for equality.Semantic.DeepDerivative
func diffDeepInternal(desired, current any, useDerivative bool, root string) map[string]fieldDiff {
	r := fieldDiffRecorder{
		diffs:             make(map[string]fieldDiff),
		root:              root,
		useDerivativeDiff: useDerivative,
	}
	cmp.Diff(desired, current, cmp.Reporter(&r), cmpopts.EquateEmpty())
	return r.diffs
}
