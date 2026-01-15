package config

type monitoring struct {
	Pull *endpoint       `yaml:"pull,omitempty"`
	Push *pushMonitoring `yaml:"push,omitempty"`
}

func (m *monitoring) validate() error {
	return nil
}

type endpoint struct {
	Addr string `yaml:"addr,omitempty"`
	Port string `yaml:"port,omitempty"`
}

type pushMonitoring struct {
	URL           string            `yaml:"url"`
	ClientConfig  clientConfig      `yaml:",inline"`
	PushFrequency *duration         `yaml:"push_frequency,omitempty"`
	ExtraLabels   map[string]string `yaml:"extra_labels,omitempty"`
}
