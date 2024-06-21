package build

import (
	"context"
	"fmt"
	"path"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TLSConfigBuilder help cache and build tls config
type TLSConfigBuilder struct {
	client.Client
	Ctx                context.Context
	CurrentCRName      string
	CurrentCRNamespace string
	SecretCache        map[string]*corev1.Secret
	ConfigmapCache     map[string]*corev1.ConfigMap
	TLSAssets          map[string]string
}

// BuildTLSConfig return map with tls config keys, let caller to use their own json tag
func (cb *TLSConfigBuilder) BuildTLSConfig(tlsCfg *vmv1beta1.TLSConfig, tlsAssetsDir string) (map[string]interface{}, error) {
	if tlsCfg == nil {
		return nil, nil
	}
	result := make(map[string]interface{})
	pathPrefix := path.Join(tlsAssetsDir, cb.CurrentCRNamespace)

	// if using SecretOrConfigMap, data will be fetched from secrets or configmap,
	// and be rewrote to new config files in cr's pods for service to use.
	if tlsCfg.CAFile != "" {
		result["ca_file"] = tlsCfg.CAFile
	} else if tlsCfg.CA.Name() != "" {
		assetKey := tlsCfg.BuildAssetPath(cb.CurrentCRNamespace, tlsCfg.CA.Name(), tlsCfg.CA.Key())
		if err := cb.fetchSecretWithAssets(tlsCfg.CA.Secret, tlsCfg.CA.ConfigMap, assetKey); err != nil {
			return nil, fmt.Errorf("cannot fetch ca: %w", err)
		}
		result["ca_file"] = tlsCfg.BuildAssetPath(pathPrefix, tlsCfg.CA.Name(), tlsCfg.CA.Key())
	}

	if tlsCfg.CertFile != "" {
		result["cert_file"] = tlsCfg.CertFile
	} else if tlsCfg.Cert.Name() != "" {
		assetKey := tlsCfg.BuildAssetPath(cb.CurrentCRNamespace, tlsCfg.Cert.Name(), tlsCfg.Cert.Key())
		if err := cb.fetchSecretWithAssets(tlsCfg.Cert.Secret, tlsCfg.Cert.ConfigMap, assetKey); err != nil {
			return nil, fmt.Errorf("cannot fetch cert: %w", err)
		}
		result["cert_file"] = tlsCfg.BuildAssetPath(pathPrefix, tlsCfg.Cert.Name(), tlsCfg.Cert.Key())
	}

	if tlsCfg.KeyFile != "" {
		result["key_file"] = tlsCfg.KeyFile
	} else if tlsCfg.KeySecret != nil {
		assetKey := tlsCfg.BuildAssetPath(cb.CurrentCRNamespace, tlsCfg.KeySecret.Name, tlsCfg.KeySecret.Key)
		if err := cb.fetchSecretWithAssets(tlsCfg.KeySecret, nil, assetKey); err != nil {
			return nil, fmt.Errorf("cannot fetch keySecret: %w", err)
		}
		result["key_file"] = tlsCfg.BuildAssetPath(pathPrefix, tlsCfg.KeySecret.Name, tlsCfg.KeySecret.Key)
	}
	if tlsCfg.ServerName != "" {
		result["server_name"] = tlsCfg.ServerName
	}
	if tlsCfg.InsecureSkipVerify {
		result["insecure_skip_verify"] = tlsCfg.InsecureSkipVerify
	}
	return result, nil
}

func (cb *TLSConfigBuilder) fetchSecretWithAssets(ss *corev1.SecretKeySelector, cs *corev1.ConfigMapKeySelector, assetKey string) error {
	var value string
	if ss != nil {
		var s corev1.Secret
		if v, ok := cb.SecretCache[ss.Name]; ok {
			s = *v
		} else {
			if err := cb.Client.Get(cb.Ctx, types.NamespacedName{Namespace: cb.CurrentCRNamespace, Name: ss.Name}, &s); err != nil {
				return fmt.Errorf("cannot fetch secret=%q for tlsAsset, err=%w", ss.Name, err)
			}
			cb.SecretCache[ss.Name] = &s
		}
		value = string(s.Data[ss.Key])
	}
	if cs != nil {
		var c corev1.ConfigMap
		if v, ok := cb.ConfigmapCache[cs.Name]; ok {
			c = *v
		} else {
			if err := cb.Client.Get(cb.Ctx, types.NamespacedName{Namespace: cb.CurrentCRNamespace, Name: cs.Name}, &c); err != nil {
				return fmt.Errorf("cannot fetch configmap=%q for tlsAssert, err=%w", cs.Name, err)
			}
		}
		value = c.Data[cs.Key]
	}
	if len(value) == 0 {
		return fmt.Errorf("cannot find tlsAsset secret or configmap for key=%q", assetKey)
	}
	cb.TLSAssets[assetKey] = value
	return nil
}
