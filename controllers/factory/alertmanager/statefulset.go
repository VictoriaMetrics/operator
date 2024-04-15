package alertmanager

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/build"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
	"github.com/VictoriaMetrics/operator/controllers/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/go-logr/logr"
	version "github.com/hashicorp/go-version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
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

var minReplicas int32 = 1

func newStsForAlertManager(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, c *config.BaseOperatorConf, amVersion *version.Version) (*appsv1.StatefulSet, error) {
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

	spec, err := makeStatefulSetSpec(ctx, cr, c, amVersion)
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
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
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
func CreateOrUpdateAlertManagerService(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client) (*v1.Service, error) {
	cr = cr.DeepCopy()
	if cr.Spec.PortName == "" {
		cr.Spec.PortName = defaultPortName
	}

	newService := build.Service(cr, cr.Spec.PortName, func(svc *v1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.Ports[0].Port = 9093
		svc.Spec.Ports = append(svc.Spec.Ports,
			v1.ServicePort{
				Name:       "tcp-mesh",
				Port:       9094,
				TargetPort: intstr.FromInt(9094),
				Protocol:   v1.ProtocolTCP,
			},
			v1.ServicePort{
				Name:       "udp-mesh",
				Port:       9094,
				TargetPort: intstr.FromInt(9094),
				Protocol:   v1.ProtocolUDP,
			},
		)
	})

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *victoriametricsv1beta1.AdditionalServiceSpec) error {
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

func makeStatefulSetSpec(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, c *config.BaseOperatorConf, amVersion *version.Version) (*appsv1.StatefulSetSpec, error) {
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

	ports := []v1.ContainerPort{
		{
			Name:          "mesh-tcp",
			ContainerPort: 9094,
			Protocol:      v1.ProtocolTCP,
		},
		{
			Name:          "mesh-udp",
			ContainerPort: 9094,
			Protocol:      v1.ProtocolUDP,
		},
	}
	if !cr.Spec.ListenLocal {
		ports = append([]v1.ContainerPort{
			{
				Name:          cr.Spec.PortName,
				ContainerPort: 9093,
				Protocol:      v1.ProtocolTCP,
			},
		}, ports...)
	}

	if amVersion != nil {
		// Adjust VMAlertmanager command line args to specified AM version
		if amVersion.LessThan(version.Must(version.NewVersion("v0.15.0"))) {
			for i := range amArgs {
				// below VMAlertmanager v0.15.0 peer address port specification is not necessary
				if strings.Contains(amArgs[i], "--cluster.peer") {
					amArgs[i] = strings.TrimSuffix(amArgs[i], ":9094")
				}

				// below VMAlertmanager v0.15.0 high availability flags are prefixed with 'mesh' instead of 'cluster'
				amArgs[i] = strings.Replace(amArgs[i], "--cluster.", "--mesh.", 1)
			}
		}
		if amVersion.LessThan(version.Must(version.NewVersion("v0.13.0"))) {
			for i := range amArgs {
				// below VMAlertmanager v0.13.0 all flags are with single dash.
				amArgs[i] = strings.Replace(amArgs[i], "--", "-", 1)
			}
		}
		if amVersion.LessThan(version.Must(version.NewVersion("v0.7.0"))) {
			// below VMAlertmanager v0.7.0 the flag 'web.route-prefix' does not exist
			amArgs = filter(amArgs, func(s string) bool {
				return !strings.Contains(s, "web.route-prefix")
			})
		}
	}

	volumes := []v1.Volume{
		{
			Name: "config-volume",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: cr.ConfigSecretName(),
				},
			},
		},
	}
	if c.UseCustomConfigReloader {
		volumes[0] = v1.Volume{
			Name: configVolumeName,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		}
	}

	amVolumeMounts := []v1.VolumeMount{
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
		volumes = append(volumes, v1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		amVolumeMounts = append(amVolumeMounts, v1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(victoriametricsv1beta1.SecretsDir, s),
		})
	}

	crVolumeMounts := []v1.VolumeMount{
		{
			Name:      configVolumeName,
			MountPath: alertmanagerConfDir,
			ReadOnly:  false,
		},
	}

	for _, c := range cr.Spec.ConfigMaps {
		volumes = append(volumes, v1.Volume{
			Name: k8stools.SanitizeVolumeName("configmap-" + c),
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: v1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		cmVolumeMount := v1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(victoriametricsv1beta1.ConfigMapsDir, c),
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
		volumes = append(volumes, v1.Volume{
			Name: k8stools.SanitizeVolumeName("templates-" + t.Name),
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: t.LocalObjectReference,
				},
			},
		})
		tmplVolumeMount := v1.VolumeMount{
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

	envs := []v1.EnvVar{
		{
			// Necessary for '--cluster.listen-address' flag
			Name: "POD_IP",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{
					FieldPath: "status.podIP",
				},
			},
		},
	}
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var initContainers []corev1.Container

	initContainers = append(initContainers, buildInitConfigContainer(ctx, cr, c)...)
	if len(cr.Spec.InitContainers) > 0 {
		var err error
		initContainers, err = k8stools.MergePatchContainers(initContainers, cr.Spec.InitContainers)
		if err != nil {
			return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
		}
	}
	vmaContainer := v1.Container{
		Args:                     amArgs,
		Name:                     "alertmanager",
		Image:                    image,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             amVolumeMounts,
		Resources:                build.Resources(cr.Spec.Resources, config.Resource(c.VMAlertManager.Resource), c.VMAlertManager.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
	}
	vmaContainer = build.Probe(vmaContainer, cr)
	defaultContainers := []v1.Container{vmaContainer}
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
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      cr.PodLabels(),
				Annotations: cr.PodAnnotations(),
			},
			Spec: v1.PodSpec{
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

var alertmanagerConfigMinimumVersion = version.Must(version.NewVersion("v0.22.0"))

// createDefaultAMConfig - check if secret with config exist,
// if not create with predefined or user value.
func createDefaultAMConfig(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, rclient client.Client, amVersion *version.Version) error {
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
		// retrieve content
		secretContent, err := getSecretContentForAlertmanager(ctx, rclient, cr.Spec.ConfigSecret, cr.Namespace)
		if err != nil {
			return fmt.Errorf("cannot fetch secret content for alertmanager config secret, err: %w", err)
		}
		alertmananagerConfig = secretContent
	// use in-line config
	case cr.Spec.ConfigRawYaml != "":
		alertmananagerConfig = []byte(cr.Spec.ConfigRawYaml)
	}

	// it makes sense only for alertmanager version above v22.0
	if amVersion != nil && !amVersion.LessThan(alertmanagerConfigMinimumVersion) {
		mergedCfg, err := buildAlertmanagerConfigWithCRDs(ctx, rclient, cr, alertmananagerConfig, l, tlsAssets)
		if err != nil {
			return fmt.Errorf("cannot build alertmanager config with configSelector, err: %w", err)
		}
		alertmananagerConfig = mergedCfg
	} else {
		l.Info("alertmanager version doesnt supports VMAlertmanagerConfig CRD", "version", amVersion)
	}

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

	newAMSecretConfig := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.ConfigSecretName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Data: map[string][]byte{alertmanagerSecretConfigKey: alertmananagerConfig},
	}

	for assetKey, assetValue := range tlsAssets {
		newAMSecretConfig.Data[assetKey] = []byte(assetValue)
	}
	var existAMSecretConfig v1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.ConfigSecretName()}, &existAMSecretConfig); err != nil {
		if errors.IsNotFound(err) {
			logger.WithContext(ctx).Info("creating default alertmanager config with secret", "secret_name", newAMSecretConfig.Name)
			return rclient.Create(ctx, newAMSecretConfig)
		}
		return err
	}

	newAMSecretConfig.Annotations = labels.Merge(existAMSecretConfig.Annotations, newAMSecretConfig.Annotations)
	newAMSecretConfig.Finalizers = victoriametricsv1beta1.MergeFinalizers(&existAMSecretConfig, victoriametricsv1beta1.FinalizerName)
	return rclient.Update(ctx, newAMSecretConfig)
}

func buildInitConfigContainer(ctx context.Context, cr *victoriametricsv1beta1.VMAlertmanager, c *config.BaseOperatorConf) []v1.Container {
	var initReloader v1.Container
	if c.UseCustomConfigReloader {
		resources := v1.ResourceRequirements{Limits: v1.ResourceList{}, Requests: v1.ResourceList{}}
		if c.VMAlertManager.ConfigReloaderCPU != "0" && c.VMAgentDefault.UseDefaultResources {
			resources.Limits[v1.ResourceCPU] = resource.MustParse(c.VMAlertManager.ConfigReloaderCPU)
			resources.Requests[v1.ResourceCPU] = resource.MustParse(c.VMAlertManager.ConfigReloaderCPU)
		}
		if c.VMAlertManager.ConfigReloaderMemory != "0" && c.VMAgentDefault.UseDefaultResources {
			resources.Limits[v1.ResourceMemory] = resource.MustParse(c.VMAlertManager.ConfigReloaderMemory)
			resources.Requests[v1.ResourceMemory] = resource.MustParse(c.VMAlertManager.ConfigReloaderMemory)
		}
		// add custom config reloader as initContainer since v0.35.0
		reloaderImage := c.CustomConfigReloaderImage
		idx := strings.LastIndex(reloaderImage, "-")
		if idx > 0 {
			imageTag := reloaderImage[idx+1:]
			ver, err := version.NewVersion(imageTag)
			if err != nil {
				logger.WithContext(ctx).Error(err, "cannot parse custom config reloader version", "reloader-image", reloaderImage)
				return nil
			} else if ver.LessThan(version.Must(version.NewVersion("0.43.0"))) {
				return nil
			}
		}
		initReloader = v1.Container{
			Image: build.FormatContainerImage(c.ContainerRegistry, c.CustomConfigReloaderImage),
			Name:  "config-init",
			Command: []string{
				"/usr/local/bin/config-reloader",
			},
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
		return []v1.Container{initReloader}
	}
	return nil
}

func buildVMAlertmanagerConfigReloader(cr *victoriametricsv1beta1.VMAlertmanager, c *config.BaseOperatorConf, crVolumeMounts []v1.VolumeMount) v1.Container {
	localReloadURL := &url.URL{
		Scheme: "http",
		Host:   c.VMAlertManager.LocalHost + ":9093",
		Path:   path.Clean(cr.Spec.RoutePrefix + "/-/reload"),
	}
	resources := v1.ResourceRequirements{Limits: v1.ResourceList{}, Requests: v1.ResourceList{}}
	if c.VMAlertManager.ConfigReloaderCPU != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[v1.ResourceCPU] = resource.MustParse(c.VMAlertManager.ConfigReloaderCPU)
		resources.Requests[v1.ResourceCPU] = resource.MustParse(c.VMAlertManager.ConfigReloaderCPU)
	}
	if c.VMAlertManager.ConfigReloaderMemory != "0" && c.VMAgentDefault.UseDefaultResources {
		resources.Limits[v1.ResourceMemory] = resource.MustParse(c.VMAlertManager.ConfigReloaderMemory)
		resources.Requests[v1.ResourceMemory] = resource.MustParse(c.VMAlertManager.ConfigReloaderMemory)
	}

	var configReloaderArgs []string
	if c.UseCustomConfigReloader {
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

	configReloaderContainer := v1.Container{
		Name:                     "config-reloader",
		Image:                    build.FormatContainerImage(c.ContainerRegistry, c.VMAlertManager.ConfigReloaderImage),
		Args:                     configReloaderArgs,
		VolumeMounts:             crVolumeMounts,
		Resources:                resources,
		TerminationMessagePolicy: v1.TerminationMessageFallbackToLogsOnError,
	}
	if c.UseCustomConfigReloader {
		configReloaderContainer.Image = build.FormatContainerImage(c.ContainerRegistry, c.CustomConfigReloaderImage)
		configReloaderContainer.Command = []string{"/usr/local/bin/config-reloader"}
	}

	return configReloaderContainer
}

func getSecretContentForAlertmanager(ctx context.Context, rclient client.Client, secretName, ns string) ([]byte, error) {
	var s v1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, &s); err != nil {
		// return nil for backward compatability
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

func buildAlertmanagerConfigWithCRDs(ctx context.Context, rclient client.Client, cr *victoriametricsv1beta1.VMAlertmanager, originConfig []byte, l logr.Logger, tlsAssets map[string]string) ([]byte, error) {
	amConfigs := make(map[string]*victoriametricsv1beta1.VMAlertmanagerConfig)
	var badCfgCount int
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ConfigNamespaceSelector, cr.Spec.ConfigSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(ams *victoriametricsv1beta1.VMAlertmanagerConfigList) {
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

func subPathForStorage(s *victoriametricsv1beta1.StorageSpec) string {
	if s == nil {
		return ""
	}

	return "alertmanager-db"
}

func filter(strings []string, f func(string) bool) []string {
	filteredStrings := make([]string, 0)
	for _, s := range strings {
		if f(s) {
			filteredStrings = append(filteredStrings, s)
		}
	}
	return filteredStrings
}
