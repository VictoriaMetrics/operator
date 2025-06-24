package config

import (
	"fmt"
	"strings"
)

// Ref: https://docs.victoriametrics.com/anomaly-detection/components/writer/#vm-writer
type writer struct {
	Class         string            `yaml:"class"`
	DatasourceURL string            `yaml:"datasource_url"`
	MetricFormat  map[string]string `yaml:"metric_format,omitempty"`
	ClientConfig  clientConfig      `yaml:",inline"`
}

func (w *writer) validate() error {
	if w.Class != "writer.vm.VmReader" && w.Class != "vm" {
		return fmt.Errorf("anomaly writer class=%q is not supported", w.Class)
	}
	if len(w.MetricFormat) > 0 { // Migrate `name` to `__name__` for backwards compatibility
		if name, ok := w.MetricFormat["name"]; ok && name != "" {
			w.MetricFormat["__name__"] = name
			delete(w.MetricFormat, "name")
		}

		name, ok := w.MetricFormat["__name__"]
		if !ok || name == "" {
			return fmt.Errorf("anomaly writer `metric_format.__name__` is required")
		}
		if !strings.Contains(name, "$VAR") {
			return fmt.Errorf("anomaly writer `metric_format.__name__` must contain `$VAR` placeholder, got %q", name)
		}

		forValue, ok := w.MetricFormat["for"]
		if ok && forValue != "" && !strings.Contains(forValue, "$QUERY_KEY") {
			return fmt.Errorf("anomaly writer `metric_format.for` must contain `$QUERY_KEY` placeholder, got %q", forValue)
		}
	}
	return nil
}
