package alertmanager

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/go-logr/logr"
	version "github.com/hashicorp/go-version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultRetention            = "120h"
	alertmanagerSecretConfigKey = "alertmanager.yaml"
	alertmanagerConfDir         = "/etc/alertmanager/config"
	alertmanagerConfFile        = alertmanagerConfDir + "/alertmanager.yaml"
	alertmanagerStorageDir      = "/alertmanager"
	defaultPortName             = "web"
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

var (
	minReplicas                  int32 = 1
	minimalConfigReloaderVersion       = version.Must(version.NewVersion("v0.43.0"))
)

func newStsForAlertManager(cr *vmv1beta1.VMAlertmanager, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
	if cr.Spec.Image.Repository == "" {
		cr.Spec.Image.Repository = c.VMAlertManager.AlertmanagerDefaultBaseImage
	}
	if cr.Spec.PortName == "" {
		cr.Spec.PortName = defaultPortName
	}

	if cr.Spec.ReplicaCount == nil {
		cr.Spec.ReplicaCount = &minReplicas
	}
	intZero := int32(0)
	if cr.Spec.ReplicaCount != nil && *cr.Spec.ReplicaCount < 0 {
		cr.Spec.ReplicaCount = &intZero
	}
	if cr.Spec.Retention == "" {
		cr.Spec.Retention = defaultRetention
	}

	spec, err := makeStatefulSetSpec(cr, c)
	if err != nil {
		return nil, err
	}

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Labels:          c.Labels.Merge(cr.AllLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			Namespace:       cr.Namespace,
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: *spec,
	}

	if cr.Spec.ImagePullSecrets != nil && len(cr.Spec.ImagePullSecrets) > 0 {
		statefulset.Spec.Template.Spec.ImagePullSecrets = cr.Spec.ImagePullSecrets
	}

	cr.Spec.Storage.IntoSTSVolume(cr.GetVolumeName(), &statefulset.Spec)
	statefulset.Spec.Template.Spec.Volumes = append(statefulset.Spec.Template.Spec.Volumes, cr.Spec.Volumes...)

	return statefulset, nil
}

// CreateOrUpdateAlertManagerService creates service for alertmanager
func CreateOrUpdateAlertManagerService(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) (*corev1.Service, error) {
	cr = cr.DeepCopy()
	if cr.Spec.PortName == "" {
		cr.Spec.PortName = defaultPortName
	}

	newService := build.Service(cr, cr.Spec.PortName, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.Ports[0].Port = 9093
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

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			logger.WithContext(ctx).Error(fmt.Errorf("vmalertmanager additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if err := reconcile.ServiceForCRD(ctx, rclient, additionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmalertmanager: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.SelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: cr.PrefixedName}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.ServiceSpec); err != nil {
		return nil, err
	}
	if err := reconcile.ServiceForCRD(ctx, rclient, newService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmalertmanager: %w", err)
	}
	return newService, nil
}

func makeStatefulSetSpec(cr *vmv1beta1.VMAlertmanager, c *config.BaseOperatorConf) (*appsv1.StatefulSetSpec, error) {
	cr = cr.DeepCopy()

	image := fmt.Sprintf("%s:%s", build.FormatContainerImage(c.ContainerRegistry, cr.Spec.Image.Repository), cr.Spec.Image.Tag)

	amArgs := []string{
		fmt.Sprintf("--config.file=%s", alertmanagerConfFile),
		fmt.Sprintf("--storage.path=%s", alertmanagerStorageDir),
		fmt.Sprintf("--data.retention=%s", cr.Spec.Retention),
	}

	if *cr.Spec.ReplicaCount == 1 {
		amArgs = append(amArgs, "--cluster.listen-address=")
	} else {
		amArgs = append(amArgs, "--cluster.listen-address=[$(POD_IP)]:9094")
	}

	if cr.Spec.ListenLocal {
		amArgs = append(amArgs, "--web.listen-address=127.0.0.1:9093")
	} else {
		amArgs = append(amArgs, "--web.listen-address=:9093")
	}

	if cr.Spec.ExternalURL != "" {
		amArgs = append(amArgs, "--web.external-url="+cr.Spec.ExternalURL)
	}

	webRoutePrefix := "/"
	if cr.Spec.RoutePrefix != "" {
		webRoutePrefix = cr.Spec.RoutePrefix
	}
	amArgs = append(amArgs, fmt.Sprintf("--web.route-prefix=%s", webRoutePrefix))

	if cr.Spec.LogLevel != "" && cr.Spec.LogLevel != "info" {
		amArgs = append(amArgs, fmt.Sprintf("--log.level=%s", cr.Spec.LogLevel))
	}

	if cr.Spec.LogFormat != "" {
		amArgs = append(amArgs, fmt.Sprintf("--log.format=%s", cr.Spec.LogFormat))
	}

	if cr.Spec.ClusterAdvertiseAddress != "" {
		amArgs = append(amArgs, fmt.Sprintf("--cluster.advertise-address=%s", cr.Spec.ClusterAdvertiseAddress))
	}

	var clusterPeerDomain string
	if c.ClusterDomainName != "" {
		clusterPeerDomain = fmt.Sprintf("%s.%s.svc.%s.", cr.PrefixedName(), cr.Namespace, c.ClusterDomainName)
	} else {
		// The default DNS search path is .svc.<cluster domain>
		clusterPeerDomain = cr.PrefixedName()
	}
	for i := int32(0); i < *cr.Spec.ReplicaCount; i++ {
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
				ContainerPort: 9093,
				Protocol:      corev1.ProtocolTCP,
			},
		}, ports...)
	}

	volumes := []corev1.Volume{
		{
			Name: "config-volume",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.ConfigSecretName(),
				},
			},
		},
	}
	if c.UseCustomConfigReloader && c.CustomConfigReloaderImageVersion().GreaterThanOrEqual(minimalConfigReloaderVersion) {
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

	terminationGracePeriod := int64(120)
	if cr.Spec.TerminationGracePeriodSeconds != nil {
		terminationGracePeriod = *cr.Spec.TerminationGracePeriodSeconds
	}

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

	initContainers = append(initContainers, buildInitConfigContainer(cr, c)...)
	if len(cr.Spec.InitContainers) > 0 {
		var err error
		initContainers, err = k8stools.MergePatchContainers(initContainers, cr.Spec.InitContainers)
		if err != nil {
			return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
		}
	}
	vmaContainer := corev1.Container{
		Args:                     amArgs,
		Name:                     "alertmanager",
		Image:                    image,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             amVolumeMounts,
		Resources:                build.Resources(cr.Spec.Resources, config.Resource(c.VMAlertManager.Resource), c.VMAlertManager.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	vmaContainer = build.Probe(vmaContainer, cr)
	defaultContainers := []corev1.Container{vmaContainer}
	defaultContainers = append(defaultContainers, buildVMAlertmanagerConfigReloader(cr, c, crVolumeMounts))

	containers, err := k8stools.MergePatchContainers(defaultContainers, cr.Spec.Containers)
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
	useStrictSecurity := c.EnableStrictSecurity
	if cr.Spec.UseStrictSecurity != nil {
		useStrictSecurity = *cr.Spec.UseStrictSecurity
	}

	mp := appsv1.ParallelPodManagement
	if cr.Spec.MinReadySeconds > 0 {
		mp = appsv1.OrderedReadyPodManagement
	}

	return &appsv1.StatefulSetSpec{
		ServiceName:          cr.PrefixedName(),
		Replicas:             cr.Spec.ReplicaCount,
		RevisionHistoryLimit: cr.Spec.RevisionHistoryLimitCount,
		PodManagementPolicy:  mp,
		MinReadySeconds:      cr.Spec.MinReadySeconds,
		UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
			Type: cr.UpdateStrategy(),
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
				NodeSelector:                  cr.Spec.NodeSelector,
				PriorityClassName:             cr.Spec.PriorityClassName,
				TerminationGracePeriodSeconds: &terminationGracePeriod,
				InitContainers:                build.AddStrictSecuritySettingsToContainers(initContainers, useStrictSecurity),
				Containers:                    build.AddStrictSecuritySettingsToContainers(containers, useStrictSecurity),
				Volumes:                       volumes,
				RuntimeClassName:              cr.Spec.RuntimeClassName,
				SchedulerName:                 cr.Spec.SchedulerName,
				ServiceAccountName:            cr.GetServiceAccountName(),
				SecurityContext:               build.AddStrictSecuritySettingsToPod(cr.Spec.SecurityContext, useStrictSecurity),
				Tolerations:                   cr.Spec.Tolerations,
				Affinity:                      cr.Spec.Affinity,
				HostNetwork:                   cr.Spec.HostNetwork,
				DNSPolicy:                     cr.Spec.DNSPolicy,
				DNSConfig:                     cr.Spec.DNSConfig,
				TopologySpreadConstraints:     cr.Spec.TopologySpreadConstraints,
				ReadinessGates:                cr.Spec.ReadinessGates,
			},
		},
	}, nil
}

// createDefaultAMConfig - check if secret with config exist,
// if not create with predefined or user value.
func createDefaultAMConfig(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	cr = cr.DeepCopy()
	l := logger.WithContext(ctx).WithValues("alertmanager", cr.Name)
	ctx = logger.AddToContext(ctx, l)

	// name of tls object and it's value
	// e.g. namespace_secret_name_secret_key
	tlsAssets := make(map[string]string)

	var alertmananagerConfig []byte
	switch {
	// fetch content from user defined secret
	case cr.Spec.ConfigSecret != "":
		if cr.Spec.ConfigSecret == cr.ConfigSecretName() {
			l.Info("ignoring content of ConfigSecret, since it has the same name as secreted created by operator for config", "secretName", cr.Spec.ConfigSecret)
		} else {
			// retrieve content
			secretContent, err := getSecretContentForAlertmanager(ctx, rclient, cr.Spec.ConfigSecret, cr.Namespace)
			if err != nil {
				return fmt.Errorf("cannot fetch secret content for alertmanager config secret, err: %w", err)
			}
			alertmananagerConfig = secretContent

		}
		// use in-line config
	case cr.Spec.ConfigRawYaml != "":
		alertmananagerConfig = []byte(cr.Spec.ConfigRawYaml)
	}
	mergedCfg, err := buildAlertmanagerConfigWithCRDs(ctx, rclient, cr, alertmananagerConfig, l, tlsAssets)
	if err != nil {
		return fmt.Errorf("cannot build alertmanager config with configSelector, err: %w", err)
	}
	alertmananagerConfig = mergedCfg

	// apply default config to be able just start alertmanager
	if len(alertmananagerConfig) == 0 {
		alertmananagerConfig = []byte(defaultAMConfig)
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

	newAMSecretConfig := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.ConfigSecretName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Data: map[string][]byte{alertmanagerSecretConfigKey: alertmananagerConfig},
	}

	for assetKey, assetValue := range tlsAssets {
		newAMSecretConfig.Data[assetKey] = []byte(assetValue)
	}
	var existAMSecretConfig corev1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.ConfigSecretName()}, &existAMSecretConfig); err != nil {
		if errors.IsNotFound(err) {
			logger.WithContext(ctx).Info("creating default alertmanager config with secret", "secret_name", newAMSecretConfig.Name)
			return rclient.Create(ctx, newAMSecretConfig)
		}
		return err
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &existAMSecretConfig); err != nil {
		return err
	}

	newAMSecretConfig.Annotations = labels.Merge(existAMSecretConfig.Annotations, newAMSecretConfig.Annotations)
	newAMSecretConfig.Finalizers = vmv1beta1.MergeFinalizers(&existAMSecretConfig, vmv1beta1.FinalizerName)
	return rclient.Update(ctx, newAMSecretConfig)
}

func buildInitConfigContainer(cr *vmv1beta1.VMAlertmanager, c *config.BaseOperatorConf) []corev1.Container {
	if !c.UseCustomConfigReloader || c.CustomConfigReloaderImageVersion().LessThan(minimalConfigReloaderVersion) {
		return nil
	}
	var initReloader corev1.Container
	resources := corev1.ResourceRequirements{Limits: corev1.ResourceList{}, Requests: corev1.ResourceList{}}
	if c.VMAlertManager.ConfigReloaderCPU != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMAlertManager.ConfigReloaderCPU)
		resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMAlertManager.ConfigReloaderCPU)
	}
	if c.VMAlertManager.ConfigReloaderMemory != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMAlertManager.ConfigReloaderMemory)
		resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMAlertManager.ConfigReloaderMemory)
	}
	initReloader = corev1.Container{
		Image: build.FormatContainerImage(c.ContainerRegistry, c.CustomConfigReloaderImage),
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
		Resources: resources,
	}
	return []corev1.Container{initReloader}
}

func buildVMAlertmanagerConfigReloader(cr *vmv1beta1.VMAlertmanager, c *config.BaseOperatorConf, crVolumeMounts []corev1.VolumeMount) corev1.Container {
	localReloadURL := &url.URL{
		Scheme: "http",
		Host:   c.VMAlertManager.LocalHost + ":9093",
		Path:   path.Clean(cr.Spec.RoutePrefix + "/-/reload"),
	}
	resources := corev1.ResourceRequirements{Limits: corev1.ResourceList{}, Requests: corev1.ResourceList{}}
	if c.VMAlertManager.ConfigReloaderCPU != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMAlertManager.ConfigReloaderCPU)
		resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMAlertManager.ConfigReloaderCPU)
	}
	if c.VMAlertManager.ConfigReloaderMemory != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMAlertManager.ConfigReloaderMemory)
		resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMAlertManager.ConfigReloaderMemory)
	}

	var configReloaderArgs []string
	if c.UseCustomConfigReloader && c.CustomConfigReloaderImageVersion().GreaterThanOrEqual(minimalConfigReloaderVersion) {
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
	}

	configReloaderContainer := corev1.Container{
		Name:                     "config-reloader",
		Image:                    build.FormatContainerImage(c.ContainerRegistry, c.VMAlertManager.ConfigReloaderImage),
		Args:                     configReloaderArgs,
		VolumeMounts:             crVolumeMounts,
		Resources:                resources,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	if c.UseCustomConfigReloader && c.CustomConfigReloaderImageVersion().GreaterThanOrEqual(minimalConfigReloaderVersion) {
		configReloaderContainer.Image = build.FormatContainerImage(c.ContainerRegistry, c.CustomConfigReloaderImage)
		configReloaderContainer.Command = nil
	}

	build.AddsPortProbesToConfigReloaderContainer(&configReloaderContainer, c)

	return configReloaderContainer
}

func getSecretContentForAlertmanager(ctx context.Context, rclient client.Client, secretName, ns string) ([]byte, error) {
	var s corev1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, &s); err != nil {
		// return nil for backward compatibility
		if errors.IsNotFound(err) {
			logger.WithContext(ctx).Error(err, "alertmanager config secret doens't exist, default config is used", "secret", secretName, "ns", ns)
			return nil, nil
		}
		return nil, fmt.Errorf("cannot get secret: %s at ns: %s, err: %w", secretName, ns, err)
	}
	if d, ok := s.Data[alertmanagerSecretConfigKey]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("cannot find alertmanager config key: %q at secret: %q", alertmanagerSecretConfigKey, secretName)
}

func buildAlertmanagerConfigWithCRDs(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAlertmanager, originConfig []byte, l logr.Logger, tlsAssets map[string]string) ([]byte, error) {
	amConfigs := make(map[string]*vmv1beta1.VMAlertmanagerConfig)
	var badCfgCount int
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ConfigNamespaceSelector, cr.Spec.ConfigSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(ams *vmv1beta1.VMAlertmanagerConfigList) {
			for i := range ams.Items {
				item := ams.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				if item.Spec.ParsingError != "" {
					badCfgCount++
					l.Error(fmt.Errorf(item.Spec.ParsingError), "parsing failed for alertmanager config", "objectName", item.Name)
					continue
				}
				if err := item.Validate(); err != nil {
					l.Error(err, "validation failed for alertmanager config", "objectName", item.Name)
					badCfgCount++
					continue
				}
				amConfigs[item.AsKey()] = &item
			}
		}); err != nil {
		return nil, fmt.Errorf("cannot select alertmanager configs: %w", err)
	}

	parsedCfg, err := buildConfig(ctx, rclient, !cr.Spec.DisableNamespaceMatcher, cr.Spec.DisableRouteContinueEnforce, originConfig, amConfigs, tlsAssets)
	if err != nil {
		return nil, err
	}
	l.Info("selected alertmanager configs", "len", len(amConfigs), "invalid configs", badCfgCount+parsedCfg.BadObjectsCount)
	if len(parsedCfg.ParseErrors) > 0 {
		l.Error(fmt.Errorf("errors: %s", strings.Join(parsedCfg.ParseErrors, ";")), "bad configs found during alertmanager config building")
	}
	badConfigsTotal.Add(float64(badCfgCount))
	return parsedCfg.Data, nil
}

func subPathForStorage(s *vmv1beta1.StorageSpec) string {
	if s == nil {
		return ""
	}

	return "alertmanager-db"
}
