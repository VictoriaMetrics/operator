package config

import (
	"fmt"
)

type writer struct {
	Class         string              `yaml:"class"`
	DatasourceURL string              `yaml:"datasource_url"`
	MetricFormat  *writerMetricFormat `yaml:"metric_format,omitempty"`
	ClientConfig  clientConfig        `yaml:",inline"`
}

func (w *writer) validate() error {
	if w.Class != "writer.vm.VmReader" && w.Class != "vm" {
		return fmt.Errorf("anomaly writer class=%q is not supported", w.Class)
	}
	return nil
}

type writerMetricFormat struct {
	Name   string            `yaml:"__name__"`
	For    string            `yaml:"for"`
	Labels map[string]string `yaml:",omitempty"`
}
