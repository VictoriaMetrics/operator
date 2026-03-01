package reconcile

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type fieldDiff struct {
	Value any `json:"~~,omitempty"`
	From  any `json:"--,omitempty"`
	To    any `json:"++,omitempty"`
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
	from, to := r.path.Last().Values()
	if r.useDerivativeDiff {
		switch to.Kind() {
		case reflect.String, reflect.Slice:
			if to.Len() == 0 {
				return
			}
		case reflect.Pointer:
			if to.IsNil() {
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
			case ix == -1:
				key += fmt.Sprintf("[%d]", iy)
			case iy == -1:
				key += fmt.Sprintf("[%d]", ix)
			default:
				moved = true
				key += fmt.Sprintf("[%d->%d]", ix, iy)
			}
		case cmp.MapIndex:
			key += "['" + v.String() + "']"
		}
	}
	if !moved {
		r.diffs[key] = fieldDiff{
			From: toInterface(from),
			To:   toInterface(to),
		}
	} else {
		r.diffs[key] = fieldDiff{
			Value: toInterface(from),
		}
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

func diffDeep(from, to any, root string) map[string]fieldDiff {
	return diffDeepInternal(from, to, false, root)
}

func diffDeepDerivative(from, to any, root string) map[string]fieldDiff {
	return diffDeepInternal(from, to, true, root)
}

// diffDeepInternal is similar to diffDeep except that unset fields in from are
// ignored (not compared). This allows us to focus on the fields that matter to
// the semantic comparison.
//
// The unset fields include a nil pointer and an empty string.
//
// Helper function for equality.Semantic.DeepDerivative
func diffDeepInternal(from, to any, useDerivative bool, root string) map[string]fieldDiff {
	r := fieldDiffRecorder{
		diffs:             make(map[string]fieldDiff),
		root:              root,
		useDerivativeDiff: useDerivative,
	}
	cmp.Diff(from, to, cmp.Reporter(&r), cmpopts.EquateEmpty())
	return r.diffs

}
