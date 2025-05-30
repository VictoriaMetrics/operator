package vmalertmanager

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
)

const (
	defaultRetention    = "120h"
	secretConfigKey     = "alertmanager.yaml"
	webserverConfigKey  = "webserver_config.yaml"
	gossipConfigKey     = "gossip_config.yaml"
	etcDir              = "/etc/alertmanager"
	confDir             = etcDir + "/config"
	confFile            = confDir + "/alertmanager.yaml"
	tlsAssetsDir        = etcDir + "/tls_assets"
	tlsAssetsVolumeName = "tls-assets"
	storageDir          = "/alertmanager"
	configVolumeName    = "config-volume"
	defaultAMConfig     = `
global:
  resolve_timeout: 5m
route:
  receiver: 'blackhole'
receivers:
- name: blackhole
`
)

func newPodSpec(cr *vmv1beta1.VMAlertmanager) (*corev1.PodSpec, error) {

	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	image := fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag)

	args := []string{
		fmt.Sprintf("--config.file=%s", confFile),
		fmt.Sprintf("--storage.path=%s", storageDir),
		fmt.Sprintf("--data.retention=%s", cr.Spec.Retention),
	}
	if cr.Spec.WebConfig != nil {
		args = append(args, fmt.Sprintf("--web.config.file=%s/%s", tlsAssetsDir, webserverConfigKey))
	}
	if cr.Spec.GossipConfig != nil {
		args = append(args, fmt.Sprintf("--cluster.tls-config=%s/%s", tlsAssetsDir, gossipConfigKey))
	}

	if ptr.Deref(cr.Spec.ReplicaCount, 0) == 1 {
		args = append(args, "--cluster.listen-address=")
	} else {
		args = append(args, "--cluster.listen-address=[$(POD_IP)]:9094")
	}

	port, err := strconv.ParseInt(cr.Port(), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot reconcile additional service for vmalertmanager: failed to parse port: %w", err)
	}

	listenHost := ""
	if cr.Spec.ListenLocal {
		listenHost = "127.0.0.1"
	}
	args = append(args, fmt.Sprintf("--web.listen-address=%s:%d", listenHost, port))

	if cr.Spec.ExternalURL != "" {
		args = append(args, "--web.external-url="+cr.Spec.ExternalURL)
	}

	webRoutePrefix := "/"
	if cr.Spec.RoutePrefix != "" {
		webRoutePrefix = cr.Spec.RoutePrefix
	}
	args = append(args, fmt.Sprintf("--web.route-prefix=%s", webRoutePrefix))

	if cr.Spec.LogLevel != "" && cr.Spec.LogLevel != "info" {
		args = append(args, fmt.Sprintf("--log.level=%s", strings.ToLower(cr.Spec.LogLevel)))
	}

	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("--log.format=%s", cr.Spec.LogFormat))
	}

	if cr.Spec.ClusterAdvertiseAddress != "" {
		args = append(args, fmt.Sprintf("--cluster.advertise-address=%s", cr.Spec.ClusterAdvertiseAddress))
	}

	var clusterPeerDomain string
	if cr.Spec.ClusterDomainName != "" {
		clusterPeerDomain = fmt.Sprintf("%s.%s.svc.%s.", cr.PrefixedName(), cr.Namespace, cr.Spec.ClusterDomainName)
	} else {
		// The default DNS search path is .svc.<cluster domain>
		clusterPeerDomain = cr.PrefixedName()
	}

	for i := int32(0); i < ptr.Deref(cr.Spec.ReplicaCount, 0); i++ {
		args = append(args, fmt.Sprintf("--cluster.peer=%s-%d.%s:9094", cr.PrefixedName(), i, clusterPeerDomain))
	}

	for _, peer := range cr.Spec.AdditionalPeers {
		args = append(args, fmt.Sprintf("--cluster.peer=%s", peer))
	}

	ports := []corev1.ContainerPort{
		{
			Name:          "mesh-tcp",
			ContainerPort: 9094,
			Protocol:      corev1.ProtocolTCP,
		},
		{
			Name:          "mesh-udp",
			ContainerPort: 9094,
			Protocol:      corev1.ProtocolUDP,
		},
	}
	if !cr.Spec.ListenLocal {
		ports = append([]corev1.ContainerPort{
			{
				Name:          cr.Spec.PortName,
				ContainerPort: int32(port),
				Protocol:      corev1.ProtocolTCP,
			},
		}, ports...)
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
			SubPath:   subPathForStorage(cr.Spec.Storage),
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

	crVolumeMounts := []corev1.VolumeMount{
		{
			Name:      configVolumeName,
			MountPath: confDir,
			ReadOnly:  false,
		},
		{
			Name:      tlsAssetsVolumeName,
			MountPath: tlsAssetsDir,
			ReadOnly:  true,
		},
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
		crVolumeMounts = append(crVolumeMounts, cmVolumeMount)
	}

	volumeByName := make(map[string]struct{})
	for _, t := range cr.Spec.Templates {
		// Deduplicate configmaps by name
		if _, ok := volumeByName[t.Name]; ok {
			continue
		}
		volumeByName[t.Name] = struct{}{}
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("templates-" + t.Name),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: t.LocalObjectReference,
				},
			},
		})
		tmplVolumeMount := corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("templates-" + t.Name),
			MountPath: path.Join(templatesDir, t.Name),
			ReadOnly:  true,
		}
		volumeMounts = append(volumeMounts, tmplVolumeMount)
		crVolumeMounts = append(crVolumeMounts, tmplVolumeMount)
	}

	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "--")
	sort.Strings(args)

	envs := []corev1.EnvVar{
		{
			// Necessary for '--cluster.listen-address' flag
			Name: "POD_IP",
			ValueFrom: &corev1.EnvVarSource{
				FieldRef: &corev1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
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

	container := corev1.Container{
		Args:                     args,
		Name:                     "alertmanager",
		Image:                    image,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             volumeMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	container = build.Probe(container, cr)
	containers := []corev1.Container{container}
	containers = append(containers, buildConfigReloader(cr, crVolumeMounts))

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, containers, useStrictSecurity)
	containers, err = k8stools.MergePatchContainers(containers, cr.Spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("failed to merge containers spec: %w", err)
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

func buildConfgSecretMeta(cr *vmv1beta1.VMAlertmanager) *metav1.ObjectMeta {
	return &metav1.ObjectMeta{
		Name:            cr.ConfigSecretName(),
		Namespace:       cr.Namespace,
		Labels:          cr.AllLabels(),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: cr.AsOwner(),
		Finalizers:      []string{vmv1beta1.FinalizerName},
	}

}

func buildInitConfigContainer(cr *vmv1beta1.VMAlertmanager) []corev1.Container {
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

func buildConfigReloader(cr *vmv1beta1.VMAlertmanager, crVolumeMounts []corev1.VolumeMount) corev1.Container {
	localReloadURL := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%s", "127.0.0.1", cr.Port()),
		Path:   path.Clean(cr.Spec.RoutePrefix + "/-/reload"),
	}
	if cr.Spec.WebConfig != nil && cr.Spec.WebConfig.TLSServerConfig != nil {
		localReloadURL.Scheme = "https"
	}
	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)

	var configReloaderArgs []string
	if useVMConfigReloader {
		configReloaderArgs = append(configReloaderArgs,
			fmt.Sprintf("--reload-url=%s", localReloadURL),
			fmt.Sprintf("--config-envsubst-file=%s", confFile),
			fmt.Sprintf("--config-secret-key=%s", secretConfigKey),
			fmt.Sprintf("--config-secret-name=%s/%s", cr.Namespace, cr.ConfigSecretName()),
			"--webhook-method=POST",
		)
		for _, vm := range crVolumeMounts {
			configReloaderArgs = append(configReloaderArgs, fmt.Sprintf("--watched-dir=%s", vm.MountPath))
		}
	} else {
		// Add watching for every volume mount in config-reloader
		configReloaderArgs = append(configReloaderArgs, fmt.Sprintf("-webhook-url=%s", localReloadURL))
		for _, vm := range crVolumeMounts {
			configReloaderArgs = append(configReloaderArgs, fmt.Sprintf("-volume-dir=%s", vm.MountPath))
		}
	}
	if len(cr.Spec.ConfigReloaderExtraArgs) > 0 {
		for idx, arg := range configReloaderArgs {
			cleanArg := strings.Split(strings.TrimLeft(arg, "-"), "=")[0]
			if replacement, ok := cr.Spec.ConfigReloaderExtraArgs[cleanArg]; ok {
				delete(cr.Spec.ConfigReloaderExtraArgs, cleanArg)
				configReloaderArgs[idx] = fmt.Sprintf(`--%s=%s`, cleanArg, replacement)
			}
		}
		for k, v := range cr.Spec.ConfigReloaderExtraArgs {
			configReloaderArgs = append(configReloaderArgs, fmt.Sprintf(`--%s=%s`, k, v))
		}
		sort.Strings(configReloaderArgs)
	}

	configReloaderContainer := corev1.Container{
		Name:                     "config-reloader",
		Image:                    cr.Spec.ConfigReloaderImageTag,
		Args:                     configReloaderArgs,
		VolumeMounts:             crVolumeMounts,
		Resources:                cr.Spec.ConfigReloaderResources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	build.AddsPortProbesToConfigReloaderContainer(useVMConfigReloader, &configReloaderContainer)
	if useVMConfigReloader {
		build.AddServiceAccountTokenVolumeMount(&configReloaderContainer, &cr.Spec.CommonApplicationDeploymentParams)
	}
	return configReloaderContainer
}

func getSecretContent(ctx context.Context, rclient client.Client, secretName, ns string) ([]byte, error) {
	var s corev1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, &s); err != nil {
		// return nil for backward compatibility
		if errors.IsNotFound(err) {
			logger.WithContext(ctx).Error(err, fmt.Sprintf("alertmanager config secret=%q doesn't exist at namespace=%q, default config is used", secretName, ns))
			return nil, nil
		}
		return nil, fmt.Errorf("cannot get secret: %s at ns: %s, err: %w", secretName, ns, err)
	}
	if d, ok := s.Data[secretConfigKey]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("cannot find alertmanager config key: %q at secret: %q", secretConfigKey, secretName)
}

func buildConfigWithCRDs(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager, originConfig []byte, tlsAssets map[string]string) (*parsedConfig, error) {
	var amCfgs []*vmv1beta1.VMAlertmanagerConfig
	var badCfgs []*vmv1beta1.VMAlertmanagerConfig
	var namespacedNames []string
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ConfigNamespaceSelector, cr.Spec.ConfigSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(ams *vmv1beta1.VMAlertmanagerConfigList) {
			for i := range ams.Items {
				item := ams.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
				item.Status.ObservedGeneration = item.Generation
				if item.Spec.ParsingError != "" {
					item.Status.CurrentSyncError = item.Spec.ParsingError
					badCfgs = append(badCfgs, &item)
					continue
				}
				if !build.MustSkipRuntimeValidation {
					if err := item.Validate(); err != nil {
						item.Status.CurrentSyncError = err.Error()
						badCfgs = append(badCfgs, &item)
						continue
					}

				}
				amCfgs = append(amCfgs, &item)
			}
		}); err != nil {
		return nil, fmt.Errorf("cannot select alertmanager configs: %w", err)
	}

	parsedCfg, err := buildConfig(ctx, rclient, cr, originConfig, amCfgs, tlsAssets)
	if err != nil {
		return nil, err
	}
	parsedCfg.brokenAMCfgs = append(parsedCfg.brokenAMCfgs, badCfgs...)
	logger.SelectedObjects(ctx, "VMAlertmanagerConfigs", len(parsedCfg.amcfgs), len(parsedCfg.brokenAMCfgs), namespacedNames)
	badConfigsTotal.Add(float64(len(badCfgs)))
	return parsedCfg, nil
}

func subPathForStorage(s *vmv1beta1.StorageSpec) string {
	if s == nil {
		return ""
	}

	return "alertmanager-db"
}
