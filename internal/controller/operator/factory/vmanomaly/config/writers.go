package config

import (
	"fmt"
	"slices"
	"strings"
)

// Ref: https://docs.victoriametrics.com/anomaly-detection/components/writer/#vm-writer
type writer struct {
	Class         string              `yaml:"class"`
	DatasourceURL string              `yaml:"datasource_url"`
	MetricFormat  *writerMetricFormat `yaml:"metric_format,omitempty"`
	ClientConfig  clientConfig        `yaml:",inline"`
}

func (w *writer) validate() error {
	if strings.ToLower(w.Class) == "noop" {
		return nil
	}
	if !slices.Contains([]string{"writer.vm.VmWriter", "vm"}, w.Class) {
		return fmt.Errorf("anomaly writer class=%q is not supported", w.Class)
	}
	if w.MetricFormat != nil {
		if w.MetricFormat.Name == "" {
			return fmt.Errorf("anomaly writer `metricFormat.name` is required")
		}

		if w.MetricFormat.Name != "" && !strings.Contains(w.MetricFormat.Name, "$VAR") {
			return fmt.Errorf("anomaly writer `metricFormat.name` must contain `$VAR` placeholder, got %q", w.MetricFormat.Name)
		}

		if w.MetricFormat.For != "" && !strings.Contains(w.MetricFormat.For, "$QUERY_KEY") {
			return fmt.Errorf("anomaly writer `metricFormat.for` must contain `$QUERY_KEY` placeholder, got %q", w.MetricFormat.For)
		}
	}
	return nil
}

type writerMetricFormat struct {
	Name        string            `yaml:"__name__"`
	For         string            `yaml:"for"`
	Labels      map[string]string `yaml:",inline,omitempty"`
	ExtraLabels map[string]string `yaml:"extra_labels,omitempty"`
}
