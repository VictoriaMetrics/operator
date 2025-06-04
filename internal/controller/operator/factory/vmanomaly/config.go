package vmanomaly

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/timeutil"
	"github.com/goccy/go-yaml"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

type validatable interface {
	validate() error
}

type config struct {
	Schedulers map[string]*scheduler `yaml:"schedulers,omitempty"`
	Models     map[string]*model     `yaml:"models,omitempty"`
	Reader     *reader               `yaml:"reader,omitempty"`
	Writer     *writer               `yaml:"writer,omitempty"`
	Monitoring *monitoring           `yaml:"monitoring,omitempty"`
}

func (c *config) override(cr *vmv1.VMAnomaly, ac *k8stools.AssetsCache) error {
	if cr.Spec.Reader != nil {
		data, err := yaml.Marshal(cr.Spec.Reader)
		if err != nil {
			return fmt.Errorf("failed to marshal anomaly reader CR, name=%q: %w", cr.Name, err)
		}
		var r reader
		if err := yaml.Unmarshal(data, &r); err != nil {
			return fmt.Errorf("failed to unmarshal anomaly reader CR config, name=%q: %w", cr.Name, err)
		}
		if err = r.ClientConfig.override(cr, &cr.Spec.Reader.VMAnomalyHTTPClientSpec, ac); err != nil {
			return fmt.Errorf("failed to update HTTP client for anomaly reader, name=%q: %w", cr.Name, err)
		}
		r.Class = "vm"
		c.Reader = &r
	}
	if cr.Spec.Writer != nil {
		data, err := yaml.Marshal(cr.Spec.Writer)
		if err != nil {
			return fmt.Errorf("failed to marshal anomaly writer CR, name=%q: %w", cr.Name, err)
		}
		var w vmWriter
		if err = yaml.Unmarshal(data, &w); err != nil {
			return fmt.Errorf("failed to unmarshal anomaly writer CR config, name=%q: %w", cr.Name, err)
		}
		if err = w.ClientConfig.override(cr, &cr.Spec.Writer.VMAnomalyHTTPClientSpec, ac); err != nil {
			return fmt.Errorf("failed to update HTTP client for anomaly writer, name=%q: %w", cr.Name, err)
		}
		w.Class = "vm"
		c.Writer.Data = &w
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
		for _, q := range m.Common.Queries {
			if _, ok := c.Reader.Queries[q]; !ok {
				return fmt.Errorf(`models.%s.queries contains %q, which is not listed in reader.queries`, modelName, q)
			}
		}
		for _, s := range m.Common.Schedulers {
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

func marshalValues[T validatable](vs map[string]T) yaml.MapSlice {
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
	return output
}

type duration string

func (d *duration) UnmarshalYAML(data []byte) (err error) {
	v := strings.TrimSpace(string(data))
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

func (c *clientConfig) override(cr *vmv1.VMAnomaly, cfg *vmv1.VMAnomalyHTTPClientSpec, ac *k8stools.AssetsCache) error {
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

func Validate(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) error {
	_, err := loadConfig(ctx, rclient, cr)
	if err != nil {
		return err
	}
	return nil
}

func loadConfig(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) ([]byte, error) {
	assetsCache := k8stools.NewAssetsCache(ctx, rclient, tlsAssetsDir)
	var data []byte
	switch {
	case cr.Spec.ConfigSecret != nil:
		secret, err := assetsCache.LoadKeyFromSecret(cr.Namespace, cr.Spec.ConfigSecret)
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
	err := yaml.Unmarshal(data, c)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal anomaly configuration, name=%q: %w", cr.Name, err)
	}
	if err = c.override(cr, assetsCache); err != nil {
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

func buildConfgSecretMeta(cr *vmv1.VMAnomaly) *metav1.ObjectMeta {
	return &metav1.ObjectMeta{
		Name:            cr.ConfigSecretName(),
		Namespace:       cr.Namespace,
		Labels:          cr.AllLabels(),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: cr.AsOwner(),
		Finalizers:      []string{vmv1beta1.FinalizerName},
	}

}

func createOrUpdateConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMAnomaly) (string, error) {
	data, err := loadConfig(ctx, rclient, cr)
	if err != nil {
		return "", err
	}
	newSecretConfig := &corev1.Secret{
		ObjectMeta: *buildConfgSecretMeta(cr),
		Data: map[string][]byte{
			secretConfigKey: data,
		},
	}

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = buildConfgSecretMeta(prevCR)
	}

	if err := reconcile.Secret(ctx, rclient, newSecretConfig, prevSecretMeta); err != nil {
		return "", err
	}

	hash := sha256.New()
	hash.Write(data)
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes), nil
}
