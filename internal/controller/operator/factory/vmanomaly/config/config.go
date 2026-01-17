package config

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/timeutil"
	"gopkg.in/yaml.v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func NewParsedObjects(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) (*ParsedObjects, error) {
	models, err := selectModels(ctx, rclient, cr)
	if err != nil {
		return nil, fmt.Errorf("selecting VMAnomalyModels failed: %w", err)
	}
	schedulers, err := selectSchedulers(ctx, rclient, cr)
	if err != nil {
		return nil, fmt.Errorf("selecting VMAnomalySchedulers failed: %w", err)
	}
	return &ParsedObjects{
		models:     models,
		schedulers: schedulers,
	}, nil
}

type ParsedObjects struct {
	models     *build.ChildObjects[*vmv1.VMAnomalyModel]
	schedulers *build.ChildObjects[*vmv1.VMAnomalyScheduler]
}

// Load returns vmanomaly config merged with provided secrets
func (pos *ParsedObjects) Load(cr *vmv1.VMAnomaly, ac *build.AssetsCache) ([]byte, error) {
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
	if err = c.override(cr, pos, ac); err != nil {
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

func (pos *ParsedObjects) UpdateStatusesForChildObjects(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly, childObject client.Object) error {
	parentObject := fmt.Sprintf("%s.%s.vmanomaly", cr.Name, cr.Namespace)
	if childObject != nil && !reflect.ValueOf(childObject).IsNil() {
		// fast path
		switch obj := childObject.(type) {
		case *vmv1.VMAnomalyModel:
			if o := pos.models.Get(obj); o != nil {
				return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1.VMAnomalyModel{o})
			}
		case *vmv1.VMAnomalyScheduler:
			if o := pos.schedulers.Get(obj); o != nil {
				return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1.VMAnomalyScheduler{o})
			}
		}
	}
	return nil
}

type header struct {
	Class  string         `yaml:"class"`
	Fields map[string]any `yaml:",inline"`
}

type validatable interface {
	validate() error
}

type config struct {
	Schedulers map[string]*Scheduler `yaml:"schedulers,omitempty"`
	Models     map[string]*Model     `yaml:"models,omitempty"`
	Reader     *reader               `yaml:"reader,omitempty"`
	Writer     *writer               `yaml:"writer,omitempty"`
	Monitoring *monitoring           `yaml:"monitoring,omitempty"`
	Preset     string                `yaml:"preset,omitempty"`
	Settings   *settings             `yaml:"settings,omitempty"`
	Server     *server               `yaml:"server,omitempty"`
}

type server struct {
	Addr               string `yaml:"addr,omitempty"`
	Port               string `yaml:"port,omitempty"`
	PathPrefix         string `yaml:"path_prefix,omitempty"`
	MaxConcurrentTasks int    `yaml:"max_concurrent_tasks,omitempty"`
}

func (s *server) validate() error {
	if s == nil {
		return nil
	}
	if s.MaxConcurrentTasks != 0 && (s.MaxConcurrentTasks < 1 || s.MaxConcurrentTasks > 20) {
		return fmt.Errorf("max_concurrent_tasks must be between 1 and 20, got %d", s.MaxConcurrentTasks)
	}
	return nil
}

type settings struct {
	Workers           int     `yaml:"n_workers,omitempty"`
	ScoreOutsideRange float64 `yaml:"anomaly_score_outside_data_range,omitempty"`
	RestoreState      bool    `yaml:"restore_state,omitempty"`
}

func (c *config) override(cr *vmv1.VMAnomaly, pos *ParsedObjects, ac *build.AssetsCache) error {
	crCanonicalName := strings.Join([]string{cr.Namespace, cr.Name}, "/")
	if cr.Spec.Server != nil {
		srv := cr.Spec.Server
		data, err := yaml.Marshal(srv)
		if err != nil {
			return fmt.Errorf("failed to marshal anomaly CR server config, name=%q: %w", crCanonicalName, err)
		}
		var s server
		if err = yaml.UnmarshalStrict(data, &s); err != nil {
			return fmt.Errorf("failed to unmarshal anomaly CR server config, name=%q: %w", crCanonicalName, err)
		}
		c.Server = &s
	}
	c.Preset = strings.ToLower(c.Preset)
	if strings.HasPrefix(c.Preset, "ui") {
		s := new(noopScheduler)
		s.setClass("noop")
		c.Reader = &reader{
			Class: "noop",
		}
		c.Writer = &writer{
			Class: "noop",
		}
		c.Schedulers = map[string]*Scheduler{
			"noop": {
				anomalyScheduler: s,
			},
		}
		c.Models = map[string]*Model{
			"placeholder": {
				anomalyModel: &zScoreModel{
					commonModelParams: commonModelParams{
						Class:      "zscore",
						Schedulers: []string{"noop"},
					},
				},
			},
		}
		c.Monitoring = &monitoring{
			Pull: &endpoint{
				Addr: "0.0.0.0",
				Port: cr.Spec.Monitoring.Pull.Port,
			},
		}
		return nil
	}
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

	// override models
	pos.models.ForEachCollectSkipInvalid(func(m *vmv1.VMAnomalyModel) error {
		name := fmt.Sprintf("%s-%s", m.Namespace, m.Name)
		nm, err := modelFromSpec(&m.Spec)
		if err != nil {
			return fmt.Errorf("failed to unmarshal model=%q: %w", name, err)
		}
		c.Models[name] = nm
		return nil
	})

	// override schedulers
	pos.schedulers.ForEachCollectSkipInvalid(func(s *vmv1.VMAnomalyScheduler) error {
		name := fmt.Sprintf("%s-%s", s.Namespace, s.Name)
		ns, err := schedulerFromSpec(&s.Spec)
		if err != nil {
			return fmt.Errorf("failed to unmarshal scheduler=%q: %w", name, err)
		}
		c.Schedulers[name] = ns
		return nil
	})
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
	if err := c.Server.validate(); err != nil {
		return fmt.Errorf("failed to validate server section: %w", err)
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
