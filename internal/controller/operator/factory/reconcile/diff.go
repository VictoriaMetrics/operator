package reconcile

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
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
	r.diffs = append(r.diffs, fmt.Sprintf("%#v:-%q +%q", r.path, formatDiffValue(current), formatDiffValue(desired)))
}

// String implements Stringer interface
func (r *fieldDiffRecorder) String() string {
	return strings.Join(r.diffs, ",")
}

func diffDeep(desired, current any) string {
	return diffDeepInternal(desired, current, false)
}

func diffDeepDerivative(desired, current any) string {
	return diffDeepInternal(desired, current, true)
}

// diffDeepInternal is similar to diffDeep except that unset fields in desired are
// ignored (not compared). This allows us to focus on the fields that matter to
// the semantic comparison.
//
// The unset fields include a nil pointer and an empty string.
//
// Helper function for equality.Semantic.DeepDerivative
func diffDeepInternal(desired, current any, useDerivative bool) string {
	r := fieldDiffRecorder{
		useDerivativeDiff: useDerivative,
	}
	cmp.Diff(desired, current, cmp.Reporter(&r), cmpopts.EquateEmpty())
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
