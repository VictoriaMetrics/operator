package vmdistributedcluster

import (
	"fmt"

	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMAuthConfig is the top-level structure for vmauth configuration.
type VMAuthConfig struct {
	UnauthorizedUser *VMAuthUser `yaml:"unauthorized_user,omitempty"`
}

// VMAuthUser defines the user settings for vmauth.
type VMAuthUser struct {
	URLMap []URLMap `yaml:"url_map"`
}

// URLMap defines a single URL mapping rule.
type URLMap struct {
	SrcPaths           []string `yaml:"src_paths"`
	URLPrefix          string   `yaml:"url_prefix"`
	DiscoverBackendIPs bool     `yaml:"discover_backend_ips"`
}

// buildVMAuthVMSelectRefs builds the URLMap entries for each vmselect in the vmclusters.
func buildVMAuthVMSelectURLMaps(vmClusters []*vmv1beta1.VMCluster) []URLMap {
	maps := make([]URLMap, 0, len(vmClusters))
	for _, vmCluster := range vmClusters {
		targetHostSuffix := fmt.Sprintf("%s.svc", vmCluster.Namespace)
		if vmCluster.Spec.ClusterDomainName != "" {
			targetHostSuffix += fmt.Sprintf(".%s", vmCluster.Spec.ClusterDomainName)
		}
		selectPort := "8481"
		if vmCluster.Spec.VMSelect != nil && vmCluster.Spec.VMSelect.Port != "" {
			selectPort = vmCluster.Spec.VMSelect.Port
		}
		maps = append(maps, URLMap{
			SrcPaths:           []string{"/.*"},
			URLPrefix:          fmt.Sprintf("http://srv+%s.%s:%s", vmCluster.PrefixedName(vmv1beta1.ClusterComponentSelect), targetHostSuffix, selectPort),
			DiscoverBackendIPs: true,
		})
	}
	return maps
}

// buildVMAuthLBSecret builds a secret containing the vmauth configuration.
func buildVMAuthLBSecret(cr *vmv1alpha1.VMDistributedCluster, vmClusters []*vmv1beta1.VMCluster) (*corev1.Secret, error) {
	config := VMAuthConfig{
		UnauthorizedUser: &VMAuthUser{
			URLMap: buildVMAuthVMSelectURLMaps(vmClusters),
		},
	}

	configData, err := yaml.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("cannot marshal vmauth config: %w", err)
	}

	lbScrt := &corev1.Secret{
		ObjectMeta: buildLBConfigMeta(cr),
		StringData: map[string]string{"config.yaml": string(configData)},
	}
	return lbScrt, nil
}
