package config

import (
	"fmt"
)

type writer struct {
	validatable
}

func (w *writer) UnmarshalYAML(unmarshal func(any) error) error {
	var h header
	if err := unmarshal(&h); err != nil {
		return err
	}
	var wrt validatable
	switch h.Class {
	case "writer.vm.VmWriter", "vm":
		wrt = new(vmWriter)
	case "writer.noop.NoOpWriter", "noop":
		wrt = new(noOpWriter)
	case "writer.in_memory.InMemoryWriter", "in_memory":
		wrt = new(inMemoryWriter)
	default:
		return fmt.Errorf("anomaly writer class=%q is not supported", h.Class)
	}
	if err := unmarshal(wrt); err != nil {
		return err
	}
	w.validatable = wrt
	return nil
}

func (w *writer) MarshalYAML() (any, error) {
	return w.validatable, nil
}

type vmWriter struct {
	Class         string              `yaml:"class"`
	DatasourceURL string              `yaml:"datasource_url"`
	MetricFormat  *writerMetricFormat `yaml:"metric_format,omitempty"`
	ClientConfig  clientConfig        `yaml:",inline"`
}

func (w *vmWriter) validate() error {
	return nil
}

type writerMetricFormat struct {
	Name   string            `yaml:"__name__"`
	For    string            `yaml:"for"`
	Labels map[string]string `yaml:",omitempty"`
}

type noOpWriter struct {
	Class string `yaml:"class"`
}

func (w *noOpWriter) validate() error {
	return nil
}

type inMemoryWriter struct {
	Class string `yaml:"class"`
}

func (w *inMemoryWriter) validate() error {
	return nil
}
