package vmalertmanager

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/Masterminds/semver/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	defaultRetention            = "120h"
	alertmanagerSecretConfigKey = "alertmanager.yaml"
	webserverConfigKey          = "webserver_config.yaml"
	gossipConfigKey             = "gossip_config.yaml"
	alertmanagerConfDir         = "/etc/alertmanager/config"
	alertmanagerConfFile        = alertmanagerConfDir + "/alertmanager.yaml"
	tlsAssetsDir                = "/etc/alertmanager/tls_assets"
	tlsAssetsVolumeName         = "tls-assets"
	alertmanagerStorageDir      = "/alertmanager"
	configVolumeName            = "config-volume"
	defaultAMConfig             = `
global:
  resolve_timeout: 5m
route:
  receiver: 'blackhole'
receivers:
- name: blackhole
`
)

func newStsForAlertManager(cr *vmv1beta1.VMAlertmanager) (*appsv1.StatefulSet, error) {
	if cr.Spec.Retention == "" {
		cr.Spec.Retention = defaultRetention
	}

	spec, err := makeStatefulSetSpec(cr)
	if err != nil {
		return nil, err
	}

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			Namespace:       cr.Namespace,
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: *spec,
	}
	if cr.Spec.PersistentVolumeClaimRetentionPolicy != nil {

		statefulset.Spec.PersistentVolumeClaimRetentionPolicy = cr.Spec.PersistentVolumeClaimRetentionPolicy
	}
	build.StatefulSetAddCommonParams(statefulset, ptr.Deref(cr.Spec.UseStrictSecurity, false), &cr.Spec.CommonApplicationDeploymentParams)
	cr.Spec.Storage.IntoSTSVolume(cr.GetVolumeName(), &statefulset.Spec)
	statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, cr.Spec.Volumes...)

	return statefulset, nil
}

func createOrUpdateAlertManagerService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAlertmanager) (*corev1.Service, error) {
	port, err := strconv.ParseInt(cr.Port(), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot reconcile additional service for vmalertmanager: failed to parse port: %w", err)
	}
	newService := build.Service(cr, cr.Spec.PortName, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.Ports[0].Port = int32(port)
		svc.Spec.PublishNotReadyAddresses = true
		svc.Spec.Ports = append(svc.Spec.Ports,
			corev1.ServicePort{
				Name:       "tcp-mesh",
				Port:       9094,
				TargetPort: intstr.FromInt(9094),
				Protocol:   corev1.ProtocolTCP,
			},
			corev1.ServicePort{
				Name:       "udp-mesh",
				Port:       9094,
				TargetPort: intstr.FromInt(9094),
				Protocol:   corev1.ProtocolUDP,
			},
		)
	})
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevPort, err := strconv.ParseInt(prevCR.Port(), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("cannot reconcile additional service for vmalertmanager: failed to parse port: %w", err)
		}
		prevService = build.Service(prevCR, prevCR.Spec.PortName, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			svc.Spec.Ports[0].Port = int32(prevPort)
			svc.Spec.PublishNotReadyAddresses = true
			svc.Spec.Ports = append(svc.Spec.Ports,
				corev1.ServicePort{
					Name:       "tcp-mesh",
					Port:       9094,
					TargetPort: intstr.FromInt(9094),
					Protocol:   corev1.ProtocolTCP,
				},
				corev1.ServicePort{
					Name:       "udp-mesh",
					Port:       9094,
					TargetPort: intstr.FromInt(9094),
					Protocol:   corev1.ProtocolUDP,
				},
			)
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, cr.Spec.ServiceSpec)
	}

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vmalertmanager additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmalertmanager: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmalertmanager: %w", err)
	}
	return newService, nil
}

func makeStatefulSetSpec(cr *vmv1beta1.VMAlertmanager) (*appsv1.StatefulSetSpec, error) {

	useVMConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	image := fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag)

	amArgs := []string{
		fmt.Sprintf("--config.file=%s", alertmanagerConfFile),
		fmt.Sprintf("--storage.path=%s", alertmanagerStorageDir),
		fmt.Sprintf("--data.retention=%s", cr.Spec.Retention),
	}

	amArgs = addFeatureFlags(amArgs, cr)
	if cr.Spec.WebConfig != nil {
		amArgs = append(amArgs, fmt.Sprintf("--web.config.file=%s/%s", tlsAssetsDir, webserverConfigKey))
	}
	if cr.Spec.GossipConfig != nil {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.tls-config=%s/%s", tlsAssetsDir, gossipConfigKey))
	}

	if ptr.Deref(cr.Spec.ReplicaCount, 0) == 1 {
		amArgs = append(amArgs, "--cluster.listen-address=")
	} else {
		amArgs = append(amArgs, "--cluster.listen-address=[$(POD_IP)]:9094")
	}

	port, err := strconv.ParseInt(cr.Port(), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("cannot reconcile additional service for vmalertmanager: failed to parse port: %w", err)
	}

	listenHost := ""
	if cr.Spec.ListenLocal {
		listenHost = "127.0.0.1"
	}
	amArgs = append(amArgs, fmt.Sprintf("--web.listen-address=%s:%d", listenHost, port))

	if cr.Spec.ExternalURL != "" {
		amArgs = append(amArgs, "--web.external-url="+cr.Spec.ExternalURL)
	}

	webRoutePrefix := "/"
	if cr.Spec.RoutePrefix != "" {
		webRoutePrefix = cr.Spec.RoutePrefix
	}
	amArgs = append(amArgs, fmt.Sprintf("--web.route-prefix=%s", webRoutePrefix))

	if cr.Spec.LogLevel != "" && cr.Spec.LogLevel != "info" {
		amArgs = append(amArgs, fmt.Sprintf("--log.level=%s", strings.ToLower(cr.Spec.LogLevel)))
	}

	if cr.Spec.LogFormat != "" {
		amArgs = append(amArgs, fmt.Sprintf("--log.format=%s", cr.Spec.LogFormat))
	}

	if cr.Spec.ClusterAdvertiseAddress != "" {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.advertise-address=%s", cr.Spec.ClusterAdvertiseAddress))
	}

	var clusterPeerDomain string
	if cr.Spec.ClusterDomainName != "" {
		clusterPeerDomain = fmt.Sprintf("%s.%s.svc.%s.", cr.PrefixedName(), cr.Namespace, cr.Spec.ClusterDomainName)
	} else {
		// The default DNS search path is .svc.<cluster domain>
		clusterPeerDomain = cr.PrefixedName()
	}

	for i := int32(0); i < ptr.Deref(cr.Spec.ReplicaCount, 0); i++ {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.peer=%s-%d.%s:9094", cr.PrefixedName(), i, clusterPeerDomain))
	}

	for _, peer := range cr.Spec.AdditionalPeers {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.peer=%s", peer))
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

	amVolumeMounts := []corev1.VolumeMount{
		{
			Name:      configVolumeName,
			MountPath: alertmanagerConfDir,
			ReadOnly:  true,
		},
		{
			Name:      cr.GetVolumeName(),
			MountPath: alertmanagerStorageDir,
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
		amVolumeMounts = append(amVolumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
		})
	}

	crVolumeMounts := []corev1.VolumeMount{
		{
			Name:      configVolumeName,
			MountPath: alertmanagerConfDir,
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
		amVolumeMounts = append(amVolumeMounts, cmVolumeMount)
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
		amVolumeMounts = append(amVolumeMounts, tmplVolumeMount)
		crVolumeMounts = append(crVolumeMounts, tmplVolumeMount)
	}

	amVolumeMounts = append(amVolumeMounts, cr.Spec.VolumeMounts...)

	amArgs = build.AddExtraArgsOverrideDefaults(amArgs, cr.Spec.ExtraArgs, "--")
	sort.Strings(amArgs)

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

	vmaContainer := corev1.Container{
		Args:                     amArgs,
		Name:                     "alertmanager",
		Image:                    image,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             amVolumeMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	vmaContainer = build.Probe(vmaContainer, cr)
	operatorContainers := []corev1.Container{vmaContainer}
	operatorContainers = append(operatorContainers, buildVMAlertmanagerConfigReloader(cr, crVolumeMounts))

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, operatorContainers, useStrictSecurity)
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
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
	return &appsv1.StatefulSetSpec{
		ServiceName: cr.PrefixedName(),
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			Type: cr.Spec.RollingUpdateStrategy,
		},
		Selector: &metav1.LabelSelector{
			MatchLabels: cr.SelectorLabels(),
		},
		VolumeClaimTemplates: cr.Spec.ClaimTemplates,
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      cr.PodLabels(),
				Annotations: cr.PodAnnotations(),
			},
			Spec: corev1.PodSpec{
				InitContainers:     ic,
				Containers:         containers,
				Volumes:            volumes,
				ServiceAccountName: cr.GetServiceAccountName(),
			},
		},
	}, nil
}

func getAssetsCache(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.TLSAssetsResourceKind: {
			MountDir:   tlsAssetsDir,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	return build.NewAssetsCache(ctx, rclient, cfg)
}

// CreateOrUpdateConfig - check if secret with config exist,
// if not create with predefined or user value.
func CreateOrUpdateConfig(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager, childCR *vmv1beta1.VMAlertmanagerConfig) error {
	l := logger.WithContext(ctx)
	var prevCR *vmv1beta1.VMAlertmanager
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}

	ac := getAssetsCache(ctx, rclient, cr)
	var configSourceName string
	var alertmananagerConfig []byte
	switch {
	// fetch content from user defined secret
	case cr.Spec.ConfigSecret != "":
		if cr.Spec.ConfigSecret == cr.ConfigSecretName() {
			l.Info("ignoring content of ConfigSecret, "+
				"since it has the same name as secreted created by operator for config",
				"secretName", cr.Spec.ConfigSecret)
		} else {
			// retrieve content
			secretContent, err := getSecretContentForAlertmanager(ctx, rclient, cr.Spec.ConfigSecret, cr.Namespace)
			if err != nil {
				return fmt.Errorf("cannot fetch secret content for alertmanager config secret, err: %w", err)
			}
			alertmananagerConfig = secretContent
			configSourceName = "configSecret ref: " + cr.Spec.ConfigSecret
		}
		// use in-line config
	case cr.Spec.ConfigRawYaml != "":
		alertmananagerConfig = []byte(cr.Spec.ConfigRawYaml)
		configSourceName = "inline configuration at configRawYaml"
	}
	mergedCfg, err := buildAlertmanagerConfigWithCRDs(ctx, rclient, cr, alertmananagerConfig, ac)
	if err != nil {
		return fmt.Errorf("cannot build alertmanager config with configSelector, err: %w", err)
	}
	alertmananagerConfig = mergedCfg.data
	// apply default config to be able just start alertmanager
	if len(alertmananagerConfig) == 0 {
		alertmananagerConfig = []byte(defaultAMConfig)
		configSourceName = "default config"
	}

	webCfg, err := buildWebServerConfigYAML(cr, ac)
	if err != nil {
		return fmt.Errorf("cannot build webserver config: %w", err)
	}

	gossipCfg, err := buildGossipConfigYAML(cr, ac)
	if err != nil {
		return fmt.Errorf("cannot build gossip config: %w", err)
	}

	// add templates from CR to alermanager config
	if len(cr.Spec.Templates) > 0 {
		templatePaths := make([]string, 0, len(cr.Spec.Templates))
		for _, template := range cr.Spec.Templates {
			templatePaths = append(templatePaths, path.Join(templatesDir, template.Name, template.Key))
		}
		mergedCfg, err := addConfigTemplates(alertmananagerConfig, templatePaths)
		if err != nil {
			return fmt.Errorf("cannot build alertmanager config with templates, err: %w", err)
		}
		alertmananagerConfig = mergedCfg
	}

	if err := vmv1beta1.ValidateAlertmanagerConfigSpec(alertmananagerConfig); err != nil {
		return fmt.Errorf("incorrect result configuration, config source=%s,am cfgs count=%d: %w", configSourceName, len(mergedCfg.amcfgs), err)
	}

	newAMSecretConfig := &corev1.Secret{
		ObjectMeta: *buildConfigSecretMeta(cr),
		Data: map[string][]byte{
			alertmanagerSecretConfigKey: alertmananagerConfig,
		},
	}
	creds := ac.GetOutput()
	if secret, ok := creds[build.TLSAssetsResourceKind]; ok {
		for name, value := range secret.Data {
			newAMSecretConfig.Data[name] = value
		}
	}
	if cr.Spec.WebConfig != nil {
		newAMSecretConfig.Data[webserverConfigKey] = webCfg
	}
	if cr.Spec.GossipConfig != nil {
		newAMSecretConfig.Data[gossipConfigKey] = gossipCfg
	}

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = buildConfigSecretMeta(prevCR)
	}

	if err := reconcile.Secret(ctx, rclient, newAMSecretConfig, prevSecretMeta); err != nil {
		return err
	}

	parent := fmt.Sprintf("%s.%s.vmalertmanager", cr.Name, cr.Namespace)

	if childCR != nil {
		// fast path update only single object
		for _, amc := range mergedCfg.amcfgs {
			if amc.Name == childCR.Name && amc.Namespace == childCR.Namespace {
				return reconcile.StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMAlertmanagerConfig{amc})
			}
		}
		for _, amc := range mergedCfg.brokenAMCfgs {
			if amc.Name == childCR.Name && amc.Namespace == childCR.Namespace {
				return reconcile.StatusForChildObjects(ctx, rclient, parent, []*vmv1beta1.VMAlertmanagerConfig{amc})
			}
		}
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parent, mergedCfg.amcfgs); err != nil {
		return fmt.Errorf("failed to update vmalertmanagerConfigs statuses: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parent, mergedCfg.brokenAMCfgs); err != nil {
		return fmt.Errorf("failed to update broken vmalertmanagerConfigs statuses: %w", err)
	}
	return nil
}

func buildConfigSecretMeta(cr *vmv1beta1.VMAlertmanager) *metav1.ObjectMeta {
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
			fmt.Sprintf("--config-secret-key=%s", alertmanagerSecretConfigKey),
			fmt.Sprintf("--config-secret-name=%s/%s", cr.Namespace, cr.ConfigSecretName()),
			fmt.Sprintf("--config-envsubst-file=%s", alertmanagerConfFile),
			"--only-init-config",
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      configVolumeName,
				MountPath: alertmanagerConfDir,
			},
		},
		Resources: cr.Spec.ConfigReloaderResources,
	}
	if useVMConfigReloader {
		build.AddServiceAccountTokenVolumeMount(&initReloader, &cr.Spec.CommonApplicationDeploymentParams)
	}

	return []corev1.Container{initReloader}
}

func buildVMAlertmanagerConfigReloader(cr *vmv1beta1.VMAlertmanager, crVolumeMounts []corev1.VolumeMount) corev1.Container {
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
			fmt.Sprintf("--config-envsubst-file=%s", alertmanagerConfFile),
			fmt.Sprintf("--config-secret-key=%s", alertmanagerSecretConfigKey),
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

func getSecretContentForAlertmanager(ctx context.Context, rclient client.Client, secretName, ns string) ([]byte, error) {
	var s corev1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, &s); err != nil {
		// return nil for backward compatibility
		if k8serrors.IsNotFound(err) {
			logger.WithContext(ctx).Error(err, fmt.Sprintf("alertmanager config secret=%q doesn't exist at namespace=%q, default config is used", secretName, ns))
			return nil, nil
		}
		return nil, fmt.Errorf("cannot get secret: %s at ns: %s, err: %w", secretName, ns, err)
	}
	if d, ok := s.Data[alertmanagerSecretConfigKey]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("cannot find alertmanager config key: %q at secret: %q", alertmanagerSecretConfigKey, secretName)
}

func buildAlertmanagerConfigWithCRDs(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager, originConfig []byte, ac *build.AssetsCache) (*parsedConfig, error) {
	var amCfgs []*vmv1beta1.VMAlertmanagerConfig
	var badCfgs []*vmv1beta1.VMAlertmanagerConfig
	var namespacedNames []string
	opts := &k8stools.SelectorOpts{
		SelectAll:         cr.Spec.SelectAllByDefault,
		ObjectSelector:    cr.Spec.ConfigSelector,
		NamespaceSelector: cr.Spec.ConfigNamespaceSelector,
		DefaultNamespace:  cr.Namespace,
	}
	if err := k8stools.VisitSelected(ctx, rclient, opts, func(ams *vmv1beta1.VMAlertmanagerConfigList) {
		for i := range ams.Items {
			item := &ams.Items[i]
			if !item.DeletionTimestamp.IsZero() {
				continue
			}
			namespacedNames = append(namespacedNames, fmt.Sprintf("%s/%s", item.Namespace, item.Name))
			item.Status.ObservedGeneration = item.Generation
			if item.Spec.ParsingError != "" {
				item.Status.CurrentSyncError = item.Spec.ParsingError
				badCfgs = append(badCfgs, item)
				continue
			}
			if !build.MustSkipRuntimeValidation {
				if err := item.Validate(); err != nil {
					item.Status.CurrentSyncError = err.Error()
					badCfgs = append(badCfgs, item)
					continue
				}

			}
			amCfgs = append(amCfgs, item)
		}
	}); err != nil {
		return nil, fmt.Errorf("cannot select alertmanager configs: %w", err)
	}

	parsedCfg, err := buildConfig(cr, originConfig, amCfgs, ac)
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

var utf8MinVersion = semver.MustParse("v0.28.0")

// addFeatureFlags adds features based on alertmanager version
//
// https://github.com/prometheus/alertmanager/blob/main/featurecontrol/featurecontrol.go#L23
func addFeatureFlags(args []string, cr *vmv1beta1.VMAlertmanager) []string {
	amVersion, err := semver.NewVersion(cr.Spec.Image.Tag)
	if err != nil {
		return args
	}
	var features []string
	if amVersion.GreaterThanEqual(utf8MinVersion) {
		features = append(features, "utf8-strict-mode")
	}
	if len(features) > 0 {
		args = append(args, fmt.Sprintf("--enable-feature=%s", strings.Join(features, ",")))
	}
	return args
}
