package migrate

import (
	"fmt"
	"os"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"

	"github.com/VictoriaMetrics/operator/internal/converter"
)

// ConvertSpecAtVersion reads valuesFile and merges it over chartVersion's own defaults, since
// merging against a mismatched version's defaults could produce a target CR that doesn't match
// what's actually running.
func ConvertSpecAtVersion(chart Chart, chartVersion, valuesFile, targetName, targetNamespace string) (any, error) {
	defaults, err := converter.FetchChartDefaultsAtVersion(string(chart), chartVersion)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch chart defaults for %q version %q: %w", chart, chartVersion, err)
	}
	return convertSpec(chart, defaults, valuesFile, targetName, targetNamespace)
}

const maxTargetNameLength = 29

func convertSpec(chart Chart, defaults []byte, valuesFile, targetName, targetNamespace string) (any, error) {
	if errs := validation.IsDNS1123Label(targetName); len(errs) > 0 {
		return nil, fmt.Errorf("invalid target name %q: %s", targetName, strings.Join(errs, "; "))
	}
	if len(targetName) > maxTargetNameLength {
		return nil, fmt.Errorf("target name %q is %d characters, but the operator's derived resource names (e.g. cluster storage PVCs) must fit in Kubernetes's 63-character limit after prefixing — keep it at %d characters or fewer", targetName, len(targetName), maxTargetNameLength)
	}
	valuesData, err := os.ReadFile(valuesFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read values file %q: %w", valuesFile, err)
	}
	merged, err := converter.MergeValues(defaults, valuesData)
	if err != nil {
		return nil, fmt.Errorf("cannot merge values: %w", err)
	}
	values, err := converter.UnmarshalValues(merged, string(chart))
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal values for chart %q: %w", chart, err)
	}
	if chart == ChartVLSingle {
		// converter.Convert resolves this chart to the deprecated VLogs kind for the
		// standalone convert command's backwards compatibility; migrate needs VLSingle.
		return converter.ConvertVLSingle(targetName, targetNamespace, values)
	}
	cr, err := converter.Convert(targetName, targetNamespace, values)
	if err != nil {
		return nil, fmt.Errorf("cannot convert values to CR: %w", err)
	}
	return cr, nil
}

// AsCR type-asserts the converted CR, returning a clear error if chart/cr mismatch.
func AsCR[T any](cr any) (T, error) {
	v, ok := cr.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("expected %T, got %T", zero, cr)
	}
	return v, nil
}
