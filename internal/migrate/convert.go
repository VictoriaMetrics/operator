package migrate

import (
	"fmt"
	"os"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/converter"
)

// ConvertSpec reads valuesFile, merges it over the chart's published defaults, and converts
// it to the operator CR for the given chart and target name/namespace.
func ConvertSpec(chart Chart, valuesFile, targetName, targetNamespace string) (any, error) {
	valuesData, err := os.ReadFile(valuesFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read values file %q: %w", valuesFile, err)
	}

	defaults, err := converter.FetchChartDefaults(string(chart))
	if err != nil {
		return nil, fmt.Errorf("cannot fetch chart defaults for %q: %w", chart, err)
	}
	merged, err := converter.MergeValues(defaults, valuesData)
	if err != nil {
		return nil, fmt.Errorf("cannot merge values: %w", err)
	}
	values, err := converter.UnmarshalValues(merged, string(chart))
	if err != nil {
		return nil, fmt.Errorf("cannot unmarshal values for chart %q: %w", chart, err)
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
