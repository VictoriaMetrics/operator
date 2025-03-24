package reconcile

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
)

type fieldDiffRecorder struct {
	path              cmp.Path
	diffs             []string
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
	if !rs.Equal() {
		a1, a2 := r.path.Last().Values()
		if r.useDerivativeDiff {
			switch a1.Kind() {
			case reflect.String:
				if a1.Len() == 0 {
					return
				}
			case reflect.Slice:
				if a1.IsNil() || a1.Len() == 0 {
					return
				}
			case reflect.Pointer:
				if a1.IsNil() {
					return
				}
			case reflect.Map:
				if a1.IsNil() || a1.Len() == 0 {
					return
				}
			}
		}
		r.diffs = append(r.diffs, fmt.Sprintf("%#v:-%q +%q", r.path, formatDiffValue(a2), formatDiffValue(a1)))
	}
}

// String implements Stringer interface
func (r *fieldDiffRecorder) String() string {
	return strings.Join(r.diffs, ",")
}

func diffDeep(a1, a2 interface{}) string {
	return diffDeepInternal(a1, a2, false)

}

func diffDeepDerivative(a1, a2 interface{}) string {
	return diffDeepInternal(a1, a2, true)
}

// diffDeepInternal is similar to diffDeep except that unset fields in a1 are
// ignored (not compared). This allows us to focus on the fields that matter to
// the semantic comparison.
//
// The unset fields include a nil pointer and an empty string.
//
// Helper function for equality.Semantic.DeepDerivative
func diffDeepInternal(a1, a2 interface{}, useDerivative bool) string {
	r := fieldDiffRecorder{
		useDerivativeDiff: useDerivative,
	}
	cmp.Diff(a1, a2, cmp.Reporter(&r))
	return r.String()

}

func formatDiffValue(v reflect.Value) string {
	if !v.IsValid() {
		return "nil"
	}
	switch v.Kind() {
	case reflect.Map:
		if v.IsNil() {
			return "nil"
		}
	case reflect.Slice:
		if v.IsNil() {
			return "nil"
		}
	}
	return fmt.Sprintf("%v", v)
}
