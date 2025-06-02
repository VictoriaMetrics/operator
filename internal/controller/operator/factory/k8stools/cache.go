package k8stools

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

type BasicAuthCreds struct {
	Username string
	Password string
}

type TLSCreds struct {
	CAFile   string
	CertFile string
	KeyFile  string
	Key      string
}

func (c *TLSCreds) BuildAssetPath(prefix string, name string, key string) string {
	if name == "" || key == "" {
		return ""
	}
	return fmt.Sprintf("%s_%s_%s", prefix, name, key)
}

type OAuth2Creds struct {
	ClientSecret string
	ClientID     string
}

type HTTPClientCreds struct {
	BasicAuth   *BasicAuthCreds
	BearerToken string
	OAuth2      *OAuth2Creds
}

// AssetsCache is a shared cache for all CR assets
type AssetsCache struct {
	ctx          context.Context
	client       client.Client
	secrets      map[string]*corev1.Secret
	configMaps   map[string]*corev1.ConfigMap
	tlsAssetsDir string
	TLSAssets    map[string]*TLSAsset
}

type TLSAsset struct {
	Namespace string
	Selector  *vmv1beta1.SecretOrConfigMap
}

func NewAssetsCache(ctx context.Context, client client.Client, tlsAssetsDir string) *AssetsCache {
	return &AssetsCache{
		tlsAssetsDir: tlsAssetsDir,
		ctx:          ctx,
		client:       client,
		secrets:      map[string]*corev1.Secret{},
		configMaps:   map[string]*corev1.ConfigMap{},
		TLSAssets:    map[string]*TLSAsset{},
	}
}

// BuildHTTPClientCreds build HTTPClientCreds from vmv1beta1.HTTPAuth
func (ac *AssetsCache) BuildHTTPClientCreds(ns string, cfg *vmv1beta1.HTTPAuth) (*HTTPClientCreds, error) {
	if cfg == nil {
		return nil, nil
	}
	creds := &HTTPClientCreds{}
	if basicAuth, err := ac.BuildBasicAuthCreds(ns, cfg.BasicAuth); err != nil {
		return nil, err
	} else if basicAuth != nil && basicAuth.Password != "" {
		creds.BasicAuth = basicAuth
	}
	if cfg.BearerAuth != nil {
		if bearerToken, err := ac.LoadKeyFromSecret(ns, cfg.TokenSecret); err != nil {
			return nil, err
		} else if len(bearerToken) > 0 {
			creds.BearerToken = bearerToken
		}
	}
	if oauth2, err := ac.BuildOAuth2Creds(ns, cfg.OAuth2); err != nil {
		return nil, err
	} else if oauth2 != nil && oauth2.ClientSecret != "" {
		creds.OAuth2 = oauth2
	}
	return creds, nil
}

func (ac *AssetsCache) TLSToYAML(ns, prefix string, cfg *vmv1beta1.TLSConfig) (yaml.MapSlice, error) {
	if cfg == nil {
		return nil, nil
	}
	c, err := ac.BuildTLSCreds(ns, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to load tls secret: %w", err)
	}
	var r yaml.MapSlice
	if cfg.InsecureSkipVerify {
		r = append(r, yaml.MapItem{Key: prefix + "insecure_skip_verify", Value: cfg.InsecureSkipVerify})
	}
	if c.CAFile != "" {
		r = append(r, yaml.MapItem{Key: prefix + "ca_file", Value: c.CAFile})
	}
	if c.CertFile != "" {
		r = append(r, yaml.MapItem{Key: prefix + "cert_file", Value: c.CertFile})
	}
	if c.KeyFile != "" {
		r = append(r, yaml.MapItem{Key: prefix + "key_file", Value: c.KeyFile})
	}
	if cfg.ServerName != "" {
		r = append(r, yaml.MapItem{Key: prefix + "server_name", Value: cfg.ServerName})
	}
	return r, nil
}

func (ac *AssetsCache) ProxyAuthToYAML(ns string, cfg *vmv1beta1.ProxyAuth) (yaml.MapSlice, error) {
	if cfg == nil {
		return nil, nil
	}
	var r yaml.MapSlice
	if cfg.BasicAuth != nil {
		authYaml, err := ac.BasicAuthToYAML(ns, cfg.BasicAuth)
		if err != nil {
			return nil, err
		}
		if len(authYaml) > 0 {
			r = append(r, yaml.MapItem{Key: "proxy_basic_auth", Value: authYaml})
		}
	}
	if cfg.TLSConfig != nil {
		authYaml, err := ac.TLSToYAML(ns, "", cfg.TLSConfig)
		if err != nil {
			return nil, err
		}
		if len(authYaml) > 0 {
			r = append(r, yaml.MapItem{Key: "proxy_tls_config", Value: authYaml})
		}
	}
	if cfg.BearerToken != nil {
		if bearerToken, err := ac.LoadKeyFromSecret(ns, cfg.BearerToken); err != nil {
			return nil, err
		} else if len(bearerToken) > 0 {
			r = append(r, yaml.MapItem{Key: "proxy_bearer_token", Value: bearerToken})
		}
	} else if len(cfg.BearerTokenFile) > 0 {
		r = append(r, yaml.MapItem{Key: "proxy_bearer_token_file", Value: cfg.BearerTokenFile})
	}
	return r, nil
}

func (ac *AssetsCache) AuthorizationToYAML(ns string, cfg *vmv1beta1.Authorization) (yaml.MapSlice, error) {
	if cfg == nil || (cfg.Credentials == nil && len(cfg.CredentialsFile) == 0) {
		return nil, nil
	}
	var r yaml.MapSlice
	if cfg.Credentials != nil {
		if creds, err := ac.LoadKeyFromSecret(ns, cfg.Credentials); err != nil {
			return nil, err
		} else if len(creds) > 0 {
			r = append(r, yaml.MapItem{Key: "credentials", Value: creds})
		}
	}
	authType := cfg.Type
	if len(authType) == 0 {
		authType = "Bearer"
	}
	r = append(r, yaml.MapItem{Key: "type", Value: authType})
	if len(cfg.CredentialsFile) > 0 {
		r = append(r, yaml.MapItem{Key: "credentials_file", Value: cfg.CredentialsFile})
	}
	return yaml.MapSlice{
		yaml.MapItem{Key: "authorization", Value: r},
	}, nil
}

func (ac *AssetsCache) BasicAuthToYAML(ns string, cfg *vmv1beta1.BasicAuth) (yaml.MapSlice, error) {
	if cfg == nil {
		return nil, nil
	}
	c, err := ac.BuildBasicAuthCreds(ns, cfg)
	if err != nil {
		return nil, err
	}
	var r yaml.MapSlice
	if len(c.Username) > 0 {
		r = append(r, yaml.MapItem{Key: "username", Value: c.Username})
	}
	if len(c.Password) > 0 {
		r = append(r, yaml.MapItem{Key: "password", Value: c.Password})
	}
	if len(cfg.PasswordFile) > 0 {
		r = append(r, yaml.MapItem{Key: "password_file", Value: cfg.PasswordFile})
	}
	return yaml.MapSlice{
		{Key: "basic_auth", Value: r},
	}, nil
}

func (ac *AssetsCache) OAuth2ToYAML(ns string, cfg *vmv1beta1.OAuth2) (yaml.MapSlice, error) {
	if cfg == nil {
		return nil, nil
	}
	c, err := ac.BuildOAuth2Creds(ns, cfg)
	if err != nil {
		return nil, err
	}

	var r yaml.MapSlice
	if len(c.ClientID) > 0 {
		r = append(r, yaml.MapItem{Key: "client_id", Value: c.ClientID})
	}
	if cfg.ClientSecret != nil {
		r = append(r, yaml.MapItem{Key: "client_secret", Value: c.ClientSecret})
	} else if cfg.ClientSecretFile != "" {
		r = append(r, yaml.MapItem{Key: "client_secret_file", Value: cfg.ClientSecretFile})
	}
	if len(cfg.Scopes) > 0 {
		r = append(r, yaml.MapItem{Key: "scopes", Value: cfg.Scopes})
	}
	if len(cfg.EndpointParams) > 0 {
		r = append(r, yaml.MapItem{Key: "endpoint_params", Value: cfg.EndpointParams})
	}
	if len(cfg.TokenURL) > 0 {
		r = append(r, yaml.MapItem{Key: "token_url", Value: cfg.TokenURL})
	}

	if len(cfg.ProxyURL) > 0 {
		r = append(r, yaml.MapItem{Key: "proxy_url", Value: cfg.ProxyURL})
	}
	if cfg.TLSConfig != nil {
		tlsYaml, err := ac.TLSToYAML(ns, "", cfg.TLSConfig)
		if err != nil {
			return nil, fmt.Errorf("failed to load tls secret: %w", err)
		}
		if len(tlsYaml) > 0 {
			r = append(r, yaml.MapItem{Key: "tls_config", Value: tlsYaml})
		}
	}
	if len(r) == 0 {
		return nil, nil
	}
	return yaml.MapSlice{
		{Key: "oauth2", Value: r},
	}, nil
}

// BuildOAuth2Creds fetches content of OAuth2 secret and returns it plain text value
func (ac *AssetsCache) BuildOAuth2Creds(ns string, cfg *vmv1beta1.OAuth2) (*OAuth2Creds, error) {
	if cfg == nil {
		return nil, nil
	}
	creds := &OAuth2Creds{}
	if cfg.ClientSecret != nil {
		s, err := ac.LoadKeyFromSecret(ns, cfg.ClientSecret)
		if err != nil {
			return nil, fmt.Errorf("cannot load oauth2 secret, err: %w", err)
		}
		creds.ClientSecret = s
	}
	s, err := ac.LoadKeyFromSecretOrConfigMap(ns, &cfg.ClientID)
	if err != nil {
		return nil, fmt.Errorf("cannot load oauth2 secret, err: %w", err)
	}
	creds.ClientID = s
	return creds, nil
}

// BuildTLSCreds fetches content of TLS secret and returns struct with plain text fields
func (ac *AssetsCache) BuildTLSCreds(ns string, cfg *vmv1beta1.TLSConfig) (*TLSCreds, error) {
	if cfg == nil {
		return nil, nil
	}
	creds := &TLSCreds{}
	pathPrefix := path.Join(ac.tlsAssetsDir, ns)

	// if using SecretOrConfigMap, data will be fetched from secrets or configmap,
	// and be rewrote to new config files in cr's pods for service to use.
	if len(cfg.CAFile) > 0 {
		creds.CAFile = cfg.CAFile
	} else if len(cfg.CA.PrefixedName()) > 0 {
		if _, err := ac.LoadKeyFromSecretOrConfigMap(ns, &cfg.CA); err != nil {
			return nil, fmt.Errorf("cannot fetch ca: %w", err)
		}
		creds.CAFile = cfg.BuildAssetPath(pathPrefix, cfg.CA.PrefixedName(), cfg.CA.Key())
	}

	if len(cfg.CertFile) > 0 {
		creds.CertFile = cfg.CertFile
	} else if len(cfg.Cert.PrefixedName()) > 0 {
		_, err := ac.LoadKeyFromSecretOrConfigMap(ns, &cfg.Cert)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch cert: %w", err)
		}
		creds.CertFile = cfg.BuildAssetPath(pathPrefix, cfg.Cert.PrefixedName(), cfg.Cert.Key())
	}

	if len(cfg.KeyFile) > 0 {
		creds.KeyFile = cfg.KeyFile
	} else if cfg.KeySecret != nil {
		secret, err := ac.LoadKeyFromSecret(ns, cfg.KeySecret)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch keySecret: %w", err)
		}
		creds.KeyFile = cfg.BuildAssetPath(pathPrefix, cfg.KeySecret.Name, cfg.KeySecret.Key)
		creds.Key = secret
	}
	return creds, nil
}

// BuildBasicAuthCreds fetch content of kubernetes secrets and returns it within plain text
func (ac *AssetsCache) BuildBasicAuthCreds(ns string, cfg *vmv1beta1.BasicAuth) (*BasicAuthCreds, error) {
	if cfg == nil {
		return nil, nil
	}
	creds := &BasicAuthCreds{}
	username, err := ac.LoadKeyFromSecret(ns, &cfg.Username)
	if err != nil {
		return nil, err
	}
	creds.Username = username
	if len(cfg.Password.Name) == 0 {
		// fast path for empty password
		// it can be skipped or defined via password_file
		return creds, nil
	}
	password, err := ac.LoadKeyFromSecret(ns, &cfg.Password)
	if err != nil {
		return nil, err
	}
	creds.Password = password
	return creds, nil
}

func (ac *AssetsCache) AddSecret(secret *corev1.Secret) {
	key := buildCacheKey(secret.Namespace, secret.Name)
	ac.secrets[key] = secret
}

func (ac *AssetsCache) AddConfigMap(cm *corev1.ConfigMap) {
	key := buildCacheKey(cm.Namespace, cm.Name)
	ac.configMaps[key] = cm
}

func (ac *AssetsCache) LoadSecret(ns, name string) (*corev1.Secret, error) {
	key := buildCacheKey(ns, name)
	s, ok := ac.secrets[key]
	if !ok {
		s = &corev1.Secret{}
		if err := ac.client.Get(ac.ctx, types.NamespacedName{Namespace: ns, Name: name}, s); err != nil {
			if k8serrors.IsNotFound(err) {
				ac.secrets[ns] = nil
			}
			return nil, fmt.Errorf("unable to fetch secret=%q, ns=%q: %w", name, ns, err)
		}
	}
	if s == nil {
		return nil, &KeyNotFoundError{"", path.Join(ns, name), "secret"}
	}
	ac.secrets[key] = s
	return s, nil
}

func (ac *AssetsCache) LoadKeyFromSecretOrConfigMap(ns string, soc *vmv1beta1.SecretOrConfigMap) (string, error) {
	var value string
	if soc.Secret != nil {
		return ac.LoadKeyFromSecret(ns, soc.Secret)
	}
	if soc.ConfigMap != nil {
		return ac.LoadKeyFromConfigMap(ns, soc.ConfigMap)
	}
	if len(value) == 0 {
		return "", fmt.Errorf("cannot find secret or configmap in ns=%q", ns)
	}
	return value, nil
}

// LoadKeyFromConfigMap fetches content of configmap by given key
func (ac *AssetsCache) LoadKeyFromConfigMap(ns string, cs *corev1.ConfigMapKeySelector) (string, error) {
	if cs == nil {
		return "", fmt.Errorf("BUG, configmap key selector must be non nil in ns=%q", ns)
	}
	key := buildCacheKey(ns, cs.Name)
	cm, ok := ac.configMaps[key]
	if !ok {
		cm = &corev1.ConfigMap{}
		if err := ac.client.Get(ac.ctx, types.NamespacedName{Namespace: ns, Name: cs.Name}, cm); err != nil {
			if k8serrors.IsNotFound(err) {
				ac.configMaps[ns] = nil
			}
			return "", fmt.Errorf("unable to fetch configmap=%q, ns=%q: %w", cs.Name, ns, err)
		}
	}
	if cm == nil {
		return "", &KeyNotFoundError{cs.Key, path.Join(ns, cs.Name, cs.Key), "configmap"}
	}
	if v, ok := cm.Data[cs.Key]; !ok {
		return "", &KeyNotFoundError{cs.Key, path.Join(ns, cs.Name, cs.Key), "configmap"}
	} else {
		ac.configMaps[key] = cm
		return maybeTrimSpace(v), nil
	}
}

// LoadKeyFromSecret fetch content of secret by given key
func (ac *AssetsCache) LoadKeyFromSecret(ns string, ss *corev1.SecretKeySelector) (string, error) {
	if ss == nil {
		return "", fmt.Errorf("BUG, secret key selector must be non nil in ns=%q", ns)
	}
	key := buildCacheKey(ns, ss.Name)
	s, ok := ac.secrets[key]
	if !ok {
		s = &corev1.Secret{}
		if err := ac.client.Get(ac.ctx, types.NamespacedName{Namespace: ns, Name: ss.Name}, s); err != nil {
			if k8serrors.IsNotFound(err) {
				ac.secrets[ns] = nil
			}
			return "", fmt.Errorf("unable to fetch secret=%q, ns=%q: %w", ss.Name, ns, err)
		}
	}
	if s == nil {
		return "", &KeyNotFoundError{ss.Key, path.Join(ns, ss.Name, ss.Key), "secret"}
	}
	if v, ok := s.Data[ss.Key]; !ok {
		return "", &KeyNotFoundError{ss.Key, path.Join(ns, ss.Name, ss.Key), "secret"}
	} else {
		ac.secrets[key] = s
		return maybeTrimSpace(string(v)), nil
	}
}

var disabledSpaceTrim bool

// SetSpaceTrim configures option to trim space
// at Secret/map keys
func SetSpaceTrim(disabled bool) {
	disabledSpaceTrim = disabled
}

func maybeTrimSpace(s string) string {
	if disabledSpaceTrim {
		return s
	}
	return strings.TrimRightFunc(s, unicode.IsSpace)
}

// KeyNotFoundError represents an error if expected key
// was not found at secret or configmap data
type KeyNotFoundError struct {
	key      string
	cacheKey string
	context  string
}

func IsNotFound(err error) bool {
	var e *KeyNotFoundError
	return k8serrors.IsNotFound(err) || errors.As(err, &e)
}

// Error implements interface
func (ke *KeyNotFoundError) Error() string {
	return fmt.Sprintf("expected key=%q was not found at=%q cache_key=%q", ke.key, ke.context, ke.cacheKey)
}

func buildCacheKey(ns, keyName string) string {
	return fmt.Sprintf("%s/%s", ns, keyName)
}
