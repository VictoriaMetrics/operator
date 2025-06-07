package vmanomaly

import (
	"fmt"

	"github.com/goccy/go-yaml"
)

type writer struct {
	Data validatable `yaml:",inline"`
}

func (w *writer) UnmarshalYAML(data []byte) error {
	h := struct {
		Class string `yaml:"class"`
	}{}
	if err := yaml.Unmarshal(data, &h); err != nil {
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
	if err := yaml.UnmarshalWithOptions(data, wrt, yaml.Strict()); err != nil {
		return err
	}
	w.Data = wrt
	return nil
}

func (w *writer) validate() error {
	return w.Data.validate()
}

type vmWriter struct {
	Class         string              `yaml:"class"`
	DatasourceURL string              `yaml:"datasource_url"`
	MetricFormat  *writerMetricFormat `yaml:"metric_format,omitempty"`
	ClientConfig  *clientConfig       `yaml:",inline"`
}

func (w *vmWriter) validate() error {
	return nil
}

type writerMetricFormat struct {
	Name   string            `yaml:"__name__"`
	For    string            `yaml:"for"`
	Labels map[string]string `yaml:",inline"`
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
