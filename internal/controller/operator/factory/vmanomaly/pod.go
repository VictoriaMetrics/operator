package vmanomaly

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	secretConfigKey     = "vmanomaly.yaml"
	confDir             = "/etc/vmanomaly"
	confFile            = confDir + "/vmanomaly.yaml"
	tlsAssetsDir        = "/etc/anomaly/tls"
	tlsAssetsVolumeName = "tls-assets"
	storageDir          = "/storage"
	configVolumeName    = "config-volume"
)

var configSchema *jsonschema.Schema

func init() {
	var err error
	c := jsonschema.NewCompiler()
	configSchema, err = c.Compile("schema.json")
	if err != nil {
		panic(fmt.Errorf("Failed to compile vmanomaly config schema: %v", err))
	}
}

func newPodSpec(cr *vmv1.VMAnomaly) (*corev1.PodSpec, error) {
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	image := fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag)

	var args []string
	port, err := strconv.ParseInt(cr.Port(), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot reconcile vmanomaly: failed to parse port: %w", err)
	}

	if cr.Spec.LogLevel != "" && cr.Spec.LogLevel != "info" {
		args = append(args, fmt.Sprintf("--loggerLevel=%s", strings.ToUpper(cr.Spec.LogLevel)))
	}

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			ContainerPort: int32(port),
			Protocol:      corev1.ProtocolTCP,
		},
	}

	volumes := []corev1.Volume{
		{
			Name: configVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.ConfigSecretName(),
				},
			},
		},
		// use a different volume mount for the case of vm config-reloader
		// it overrides actual mounts with empty dir
		{
			Name: tlsAssetsVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.ConfigSecretName(),
				},
			},
		},
	}
	if !cr.Spec.StatefulMode {
		volumes = append(volumes, corev1.Volume{
			Name: cr.GetVolumeName(),
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	}
	if useVMConfigReloader {
		volumes[0] = corev1.Volume{
			Name: configVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		}
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      configVolumeName,
			MountPath: confDir,
			ReadOnly:  true,
		},
		{
			Name:      cr.GetVolumeName(),
			MountPath: storageDir,
			SubPath:   subPathForStorage(cr.Spec.StatefulStorage),
		},
		{
			Name:      tlsAssetsVolumeName,
			MountPath: tlsAssetsDir,
			ReadOnly:  true,
		},
	}

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		cmVolumeMount := corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		}
		volumeMounts = append(volumeMounts, cmVolumeMount)
	}

	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "--")
	sort.Strings(args)

	envs := []corev1.EnvVar{
		{
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
		{
			Name:  "VMANOMALY_MODEL_DUMPS_DIR",
			Value: path.Join(storageDir, "model"),
		},
		{
			Name:  "VMANOMALY_DATA_DUMPS_DIR",
			Value: path.Join(storageDir, "data"),
		},
	}

	envs = append(envs, cr.Spec.ExtraEnvs...)

	var initContainers []corev1.Container

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)

	initContainers = append(initContainers, buildInitConfigContainer(cr)...)
	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, initContainers, useStrictSecurity)

	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
	}

	volumes, volumeMounts = cr.Spec.License.MaybeAddToVolumes(volumes, volumeMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)
	args = append(args, confFile)

	container := corev1.Container{
		Args:                     args,
		Name:                     "vmanomaly",
		Image:                    image,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             volumeMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	container = build.Probe(container, cr)
	operatorContainers := []corev1.Container{container}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, operatorContainers, useStrictSecurity)
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("failed to merge containers spec: %w", err)
	}

	if useVMConfigReloader {
		volumes = build.AddServiceAccountTokenVolume(volumes, &cr.Spec.CommonApplicationDeploymentParams)
	}

	for i := range cr.Spec.TopologySpreadConstraints {
		if cr.Spec.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			}
		}
	}
	if useVMConfigReloader {
		volumes = build.AddServiceAccountTokenVolume(volumes, &cr.Spec.CommonApplicationDeploymentParams)
	}
	return &corev1.PodSpec{
		InitContainers:     ic,
		Containers:         containers,
		Volumes:            volumes,
		ServiceAccountName: cr.GetServiceAccountName(),
	}, nil
}

// CreateOrUpdateConfig - check if secret with config exist,
// if not create with predefined or user value.
func CreateOrUpdateConfig(ctx context.Context, rclient client.Client, cr *vmv1.VMAnomaly) error {
	l := logger.WithContext(ctx)
	var prevCR *vmv1.VMAnomaly
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}

	// name of tls object and it's value
	// e.g. namespace_secret_name_secret_key
	tlsAssets := make(map[string]string)

	var configSourceName string
	var config []byte
	switch {
	// fetch content from user defined secret
	case cr.Spec.ConfigSecret != "":
		if cr.Spec.ConfigSecret == cr.ConfigSecretName() {
			l.Info("ignoring content of ConfigSecret, "+
				"since it has the same name as secreted created by operator for config",
				"secretName", cr.Spec.ConfigSecret)
		} else {
			// retrieve content
			secretContent, err := getSecretContentForApp(ctx, rclient, cr.Spec.ConfigSecret, cr.Namespace)
			if err != nil {
				return fmt.Errorf("cannot fetch secret content for anomaly config secret, err: %w", err)
			}
			config = secretContent
			configSourceName = "configSecret ref: " + cr.Spec.ConfigSecret
		}
		// use in-line config
	case cr.Spec.ConfigRawYaml != "":
		config = []byte(cr.Spec.ConfigRawYaml)
		configSourceName = "inline configuration at configRawYaml"
	}

	if err := validateConfig(config); err != nil {
		return fmt.Errorf("incorrect configuration, config source=%s: %w", configSourceName, err)
	}

	newSecretConfig := &corev1.Secret{
		ObjectMeta: *buildConfgSecretMeta(cr),
		Data: map[string][]byte{
			secretConfigKey: config,
		}}

	for assetKey, assetValue := range tlsAssets {
		newSecretConfig.Data[assetKey] = []byte(assetValue)
	}

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = buildConfgSecretMeta(prevCR)
	}

	if err := reconcile.Secret(ctx, rclient, newSecretConfig, prevSecretMeta); err != nil {
		return err
	}

	return nil
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

func buildInitConfigContainer(cr *vmv1.VMAnomaly) []corev1.Container {
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	if !useVMConfigReloader {
		return nil
	}
	initReloader := corev1.Container{
		Image: cr.Spec.ConfigReloaderImageTag,
		Name:  "config-init",
		Args: []string{
			fmt.Sprintf("--config-secret-key=%s", secretConfigKey),
			fmt.Sprintf("--config-secret-name=%s/%s", cr.Namespace, cr.ConfigSecretName()),
			fmt.Sprintf("--config-envsubst-file=%s", confFile),
			"--only-init-config",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      configVolumeName,
				MountPath: confDir,
			},
		},
		Resources: cr.Spec.ConfigReloaderResources,
	}
	if useVMConfigReloader {
		build.AddServiceAccountTokenVolumeMount(&initReloader, &cr.Spec.CommonApplicationDeploymentParams)
	}

	return []corev1.Container{initReloader}
}

func getSecretContentForApp(ctx context.Context, rclient client.Client, secretName, ns string) ([]byte, error) {
	var s corev1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, &s); err != nil {
		// return nil for backward compatibility
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Error(err, fmt.Sprintf("anomaly config secret=%q doesn't exist at namespace=%q, default config is used", secretName, ns))
			return nil, nil
		}
		return nil, fmt.Errorf("cannot get secret: %s at ns: %s, err: %w", secretName, ns, err)
	}
	if d, ok := s.Data[secretConfigKey]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("cannot find anomaly config key: %q at secret: %q", secretConfigKey, secretName)
}

func subPathForStorage(s *vmv1beta1.StorageSpec) string {
	if s == nil {
		return ""
	}

	return "anomaly-db"
}

func validateConfig(srcYAML []byte) error {
	var raw any
	err := yaml.Unmarshal(srcYAML, &raw)
	if err != nil {
		return fmt.Errorf("failed to unmarshal anomaly config: %w", err)
	}
	err = configSchema.Validate(raw)
	if err != nil {
		return fmt.Errorf("failed to validate anomaly config: %w", err)
	}
	return nil
}
