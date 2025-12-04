package config

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/timeutil"
	"gopkg.in/yaml.v2"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
)

type header struct {
	Class  string         `yaml:"class"`
	Fields map[string]any `yaml:",inline"`
}

type validatable interface {
	validate() error
}

type config struct {
	Schedulers map[string]*scheduler `yaml:"schedulers,omitempty"`
	Models     map[string]*model     `yaml:"models,omitempty"`
	Reader     *reader               `yaml:"reader,omitempty"`
	Writer     *writer               `yaml:"writer,omitempty"`
	Monitoring *monitoring           `yaml:"monitoring,omitempty"`
	Preset     string                `yaml:"preset,omitempty"`
	Settings   *settings             `yaml:"settings,omitempty"`
	Server     *server               `yaml:"server,omitempty"`
}

type settings struct {
	Workers           int     `yaml:"n_workers,omitempty"`
	ScoreOutsideRange float64 `yaml:"anomaly_score_outside_data_range,omitempty"`
	RestoreState      bool    `yaml:"restore_state,omitempty"`
}

func (c *config) override(cr *vmv1.VMAnomaly, ac *build.AssetsCache) error {
	c.Preset = strings.ToLower(c.Preset)
	if strings.HasPrefix(c.Preset, "ui:") {
		c.Reader = &reader{
			Class: "noop",
		}
		c.Writer = &writer{
			Class: "noop",
		}
		c.Schedulers = map[string]*scheduler{
			"noop": {
				validatable: &noopScheduler{
					Class: "noop",
				},
			},
		}
		c.Models = map[string]*model{
			"placeholder": {
				anomalyModel: &zScoreModel{
					commonModelParams: commonModelParams{
						Class:      "zscore",
						Schedulers: []string{"noop"},
					},
				},
			},
		}
		c.Server = &server{
			Addr: "0.0.0.0",
			Port: cr.Port(),
		}
		c.Monitoring = &monitoring{
			Pull: &server{
				Addr: "0.0.0.0",
				Port: cr.Spec.Monitoring.Pull.Port,
			},
		}
		return nil
	}
	crCanonicalName := strings.Join([]string{cr.Namespace, cr.Name}, "/")
	if cr.Spec.Reader == nil {
		return fmt.Errorf("reader is required for anomaly name=%q", crCanonicalName)
	}
	if c.Reader == nil || len(c.Reader.Queries) == 0 {
		return fmt.Errorf("reader.queries must be provided via configRawYaml or configSecret, name=%q", crCanonicalName)
	}
	if cr.Spec.Writer == nil {
		return fmt.Errorf("writer is required for anomaly name=%q", crCanonicalName)
	}
	// override reader
	data, err := yaml.Marshal(cr.Spec.Reader)
	if err != nil {
		return fmt.Errorf("failed to marshal anomaly CR reader config, name=%q: %w", crCanonicalName, err)
	}
	var r reader
	if err := yaml.UnmarshalStrict(data, &r); err != nil {
		return fmt.Errorf("failed to unmarshal anomaly CR reader config, name=%q: %w", crCanonicalName, err)
	}
	if err = r.ClientConfig.override(cr, &cr.Spec.Reader.VMAnomalyHTTPClientSpec, ac); err != nil {
		return fmt.Errorf("failed to update HTTP client for anomaly reader, name=%q: %w", crCanonicalName, err)
	}
	r.Class = "vm"

	r.Queries = c.Reader.Queries
	c.Reader = &r

	// override writer
	data, err = yaml.Marshal(cr.Spec.Writer)
	if err != nil {
		return fmt.Errorf("failed to marshal anomaly CR writer config, name=%q: %w", crCanonicalName, err)
	}
	var w writer
	if err = yaml.UnmarshalStrict(data, &w); err != nil {
		return fmt.Errorf("failed to unmarshal anomaly CR writer config, name=%q: %w", crCanonicalName, err)
	}
	if err = w.ClientConfig.override(cr, &cr.Spec.Writer.VMAnomalyHTTPClientSpec, ac); err != nil {
		return fmt.Errorf("failed to update HTTP client for anomaly writer, name=%q: %w", crCanonicalName, err)
	}
	if w.MetricFormat != nil && len(w.MetricFormat.ExtraLabels) > 0 {
		w.MetricFormat.Labels = w.MetricFormat.ExtraLabels
		w.MetricFormat.ExtraLabels = nil
	}
	w.Class = "vm"
	c.Writer = &w

	// override monitoring
	if cr.Spec.Monitoring != nil {
		mon := cr.Spec.Monitoring
		data, err := yaml.Marshal(mon)
		if err != nil {
			return fmt.Errorf("failed to marshal anomaly CR monitoring config, name=%q: %w", crCanonicalName, err)
		}
		var m monitoring
		if err = yaml.UnmarshalStrict(data, &m); err != nil {
			return fmt.Errorf("failed to unmarshal anomaly CR monitoring config, name=%q: %w", crCanonicalName, err)
		}
		if mon.Push != nil {
			if err = m.Push.ClientConfig.override(cr, &mon.Push.VMAnomalyHTTPClientSpec, ac); err != nil {
				return fmt.Errorf("failed to update HTTP client for anomaly monitoring, name=%q: %w", crCanonicalName, err)
			}
		}
		c.Monitoring = &m
	}
	return nil
}

func (c *config) validate() error {
	if len(c.Schedulers) < 1 {
		return fmt.Errorf("at least one scheduler is required")
	}
	if len(c.Models) < 1 {
		return fmt.Errorf("at least on model is required")
	}
	if c.Reader == nil {
		return fmt.Errorf("reader is required")
	}
	for modelName, m := range c.Models {
		for _, q := range m.queries() {
			if _, ok := c.Reader.Queries[q]; !ok {
				return fmt.Errorf(`models.%s.queries contains %q, which is not listed in reader.queries`, modelName, q)
			}
		}
		for _, s := range m.schedulers() {
			if _, ok := c.Schedulers[s]; !ok {
				return fmt.Errorf(`models.%s.schedulers contains %q, which is not listed in schedulers`, modelName, s)
			}
		}
	}
	for name, s := range c.Schedulers {
		if err := s.validate(); err != nil {
			return fmt.Errorf("failed to validate scheduler=%q: %w", name, err)
		}
	}
	for name, m := range c.Models {
		if err := m.validate(); err != nil {
			return fmt.Errorf("failed to validate model=%q: %w", name, err)
		}
	}
	if err := c.Reader.validate(); err != nil {
		return fmt.Errorf("failed to validate reader section: %w", err)
	}
	if err := c.Writer.validate(); err != nil {
		return fmt.Errorf("failed to validate writer section: %w", err)
	}
	if err := c.Monitoring.validate(); err != nil {
		return fmt.Errorf("failed to validate monitoring section: %w", err)
	}
	return nil
}

func marshalValues[T any](vs map[string]T) yaml.MapSlice {
	var keys []string
	var output yaml.MapSlice
	for name, v := range vs {
		keys = append(keys, name)
		output = append(output, yaml.MapItem{Key: name, Value: v})
	}
	build.OrderByKeys(output, keys)
	return output
}

func (c *config) marshal() yaml.MapSlice {
	output := yaml.MapSlice{
		yaml.MapItem{Key: "models", Value: marshalValues(c.Models)},
		yaml.MapItem{Key: "schedulers", Value: marshalValues(c.Schedulers)},
	}
	if c.Reader != nil {
		output = append(output, yaml.MapItem{Key: "reader", Value: c.Reader})
	}
	if c.Writer != nil {
		output = append(output, yaml.MapItem{Key: "writer", Value: c.Writer})
	}
	if c.Monitoring != nil {
		output = append(output, yaml.MapItem{Key: "monitoring", Value: c.Monitoring})
	}
	if c.Settings != nil {
		output = append(output, yaml.MapItem{Key: "settings", Value: c.Settings})
	}
	if c.Server != nil {
		output = append(output, yaml.MapItem{Key: "server", Value: c.Server})
	}
	if c.Preset != "" {
		output = append(output, yaml.MapItem{Key: "preset", Value: c.Preset})
	}
	return output
}

type duration string

var _ yaml.Unmarshaler = (*duration)(nil)

// UnmarshalYAML implements yaml.Unmarshaler interface
func (d *duration) UnmarshalYAML(unmarshal func(any) error) (err error) {
	var input any
	if err = unmarshal(&input); err != nil {
		return
	}
	v := strings.TrimSpace(input.(string))
	if len(v) > 1 && (v[0] == '"' || v[0] == '`') && v[0] == v[len(v)-1] {
		if v, err = strconv.Unquote(v); err != nil {
			return
		}
	}
	_, err = timeutil.ParseDuration(v)
	if err != nil {
		return fmt.Errorf("failed to parse duration %q: %w", v, err)
	}
	*d = duration(v)
	return nil
}

type clientConfig struct {
	TenantID        string    `yaml:"tenant_id,omitempty"`
	HealthPath      string    `yaml:"health_path,omitempty"`
	Timeout         *duration `yaml:"timeout,omitempty"`
	User            string    `yaml:"user,omitempty"`
	Password        string    `yaml:"password,omitempty"`
	BearerToken     string    `yaml:"bearer_token,omitempty"`
	BearerTokenFile string    `yaml:"bearer_token_file,omitempty"`
	VerifyTLS       bool      `yaml:"verify_tls,omitempty"`
	TLSCertFile     string    `yaml:"tls_cert_file,omitempty"`
	TLSKeyFile      string    `yaml:"tls_key_file,omitempty"`
}

func (c *clientConfig) override(cr *vmv1.VMAnomaly, cfg *vmv1.VMAnomalyHTTPClientSpec, ac *build.AssetsCache) error {
	if cfg.TLSConfig != nil {
		creds, err := ac.BuildTLSCreds(cr.Namespace, cfg.TLSConfig)
		if err != nil {
			return fmt.Errorf("failed to load TLS config: %w", err)
		}
		c.TLSCertFile = creds.CertFile
		c.TLSKeyFile = creds.KeyFile
		c.VerifyTLS = !cfg.TLSConfig.InsecureSkipVerify
	}
	if cfg.BasicAuth != nil {
		creds, err := ac.BuildBasicAuthCreds(cr.Namespace, cfg.BasicAuth)
		if err != nil {
			return fmt.Errorf("failed to load basic auth: %w", err)
		}
		c.User = creds.Username
		c.Password = creds.Password
	}
	if cfg.BearerAuth != nil {
		if bearerToken, err := ac.LoadKeyFromSecret(cr.Namespace, cfg.BearerAuth.TokenSecret); err != nil {
			return err
		} else if len(bearerToken) > 0 {
			c.BearerToken = bearerToken
		}
		c.BearerTokenFile = cfg.BearerAuth.TokenFilePath
	}
	return nil
}

// Load returns vmanomaly config merged with provided secrets
func Load(cr *vmv1.VMAnomaly, ac *build.AssetsCache) ([]byte, error) {
	var data []byte
	switch {
	case cr.Spec.ConfigSecret != nil:
		secret, err := ac.LoadKeyFromSecret(cr.Namespace, cr.Spec.ConfigSecret)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch secret content for anomaly config secret, name=%q: %w", cr.Name, err)
		}
		data = []byte(secret)
	case cr.Spec.ConfigRawYaml != "":
		data = []byte(cr.Spec.ConfigRawYaml)
	default:
		return nil, fmt.Errorf(`either "configRawYaml" or "configSecret" are required`)
	}
	c := &config{}
	err := yaml.UnmarshalStrict(data, c)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal anomaly configuration, name=%q: %w", cr.Name, err)
	}
	if err = c.override(cr, ac); err != nil {
		return nil, fmt.Errorf("failed to update secret values with values from anomaly instance, name=%q: %w", cr.Name, err)
	}
	if err = c.validate(); err != nil {
		return nil, fmt.Errorf("failed to validate anomaly configuration, name=%q: %w", cr.Name, err)
	}
	output := c.marshal()
	if data, err = yaml.Marshal(output); err != nil {
		return nil, fmt.Errorf("failed to marshal anomaly configuration, name=%q: %w", cr.Name, err)
	}
	return data, nil
}
