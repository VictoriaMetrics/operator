package migrate

import (
	"fmt"
	"os"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/converter"
)

// ConvertSpec reads valuesFile, merges it over the chart's latest published defaults, and
// converts it to the operator CR for the given chart and target name/namespace.
func ConvertSpec(chart Chart, valuesFile, targetName, targetNamespace string) (any, error) {
	defaults, err := converter.FetchChartDefaults(string(chart))
	if err != nil {
		return nil, fmt.Errorf("cannot fetch chart defaults for %q: %w", chart, err)
	}
	return convertSpec(chart, defaults, valuesFile, targetName, targetNamespace)
}

// ConvertSpecAtVersion behaves like ConvertSpec, but merges valuesFile over chartVersion's
// defaults rather than the latest published version's. migrate uses this instead of
// ConvertSpec: merging the release's values against a mismatched chart version's defaults
// (schema and defaults can both differ across versions) could silently produce a target CR
// that doesn't match what's actually running — unsafe for WithDowntime in particular, which
// deletes/rebinds the old workload based on that CR.
func ConvertSpecAtVersion(chart Chart, chartVersion, valuesFile, targetName, targetNamespace string) (any, error) {
	defaults, err := converter.FetchChartDefaultsAtVersion(string(chart), chartVersion)
	if err != nil {
		return nil, fmt.Errorf("cannot fetch chart defaults for %q version %q: %w", chart, chartVersion, err)
	}
	return convertSpec(chart, defaults, valuesFile, targetName, targetNamespace)
}

func convertSpec(chart Chart, defaults []byte, valuesFile, targetName, targetNamespace string) (any, error) {
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

// AsVMSingle type-asserts the converted CR, returning a clear error if chart/cr mismatch.
func AsVMSingle(cr any) (*vmv1beta1.VMSingle, error) {
	v, ok := cr.(*vmv1beta1.VMSingle)
	if !ok {
		return nil, fmt.Errorf("expected *vmv1beta1.VMSingle, got %T", cr)
	}
	return v, nil
}

// AsVLSingle type-asserts the converted CR, returning a clear error if chart/cr mismatch.
func AsVLSingle(cr any) (*vmv1.VLSingle, error) {
	v, ok := cr.(*vmv1.VLSingle)
	if !ok {
		return nil, fmt.Errorf("expected *vmv1.VLSingle, got %T", cr)
	}
	return v, nil
}

// AsVMCluster type-asserts the converted CR, returning a clear error if chart/cr mismatch.
func AsVMCluster(cr any) (*vmv1beta1.VMCluster, error) {
	v, ok := cr.(*vmv1beta1.VMCluster)
	if !ok {
		return nil, fmt.Errorf("expected *vmv1beta1.VMCluster, got %T", cr)
	}
	return v, nil
}

// AsVLCluster type-asserts the converted CR, returning a clear error if chart/cr mismatch.
func AsVLCluster(cr any) (*vmv1.VLCluster, error) {
	v, ok := cr.(*vmv1.VLCluster)
	if !ok {
		return nil, fmt.Errorf("expected *vmv1.VLCluster, got %T", cr)
	}
	return v, nil
}

// AsVTSingle type-asserts the converted CR, returning a clear error if chart/cr mismatch.
func AsVTSingle(cr any) (*vmv1.VTSingle, error) {
	v, ok := cr.(*vmv1.VTSingle)
	if !ok {
		return nil, fmt.Errorf("expected *vmv1.VTSingle, got %T", cr)
	}
	return v, nil
}

// AsVTCluster type-asserts the converted CR, returning a clear error if chart/cr mismatch.
func AsVTCluster(cr any) (*vmv1.VTCluster, error) {
	v, ok := cr.(*vmv1.VTCluster)
	if !ok {
		return nil, fmt.Errorf("expected *vmv1.VTCluster, got %T", cr)
	}
	return v, nil
}
