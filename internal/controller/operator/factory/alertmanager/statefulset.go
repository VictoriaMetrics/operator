package alertmanager

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	var prevService *corev1.Service
	if prevCR != nil {
		prevPort, err := strconv.ParseInt(prevCR.Port(), 10, 32)
		if err != nil {
			return nil, fmt.Errorf("cannot reconcile additional service for vmalertmanager: failed to parse port: %w", err)
		}
		prevService = build.Service(prevCR, prevCR.Spec.PortName, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			svc.Spec.Ports[0].Port = int32(prevPort)
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
	}

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			logger.WithContext(ctx).Error(fmt.Errorf("vmalertmanager additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if err := reconcile.Service(ctx, rclient, additionalService, nil); err != nil {
			// TODO: @f41gh7 check prevCR
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

	image := fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag)

	amArgs := []string{
		fmt.Sprintf("--config.file=%s", alertmanagerConfFile),
		fmt.Sprintf("--storage.path=%s", alertmanagerStorageDir),
		fmt.Sprintf("--data.retention=%s", cr.Spec.Retention),
	}
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
	if ptr.Deref(cr.Spec.UseVMConfigReloader, false) {
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

// CreateAMConfig - check if secret with config exist,
// if not create with predefined or user value.
func CreateAMConfig(ctx context.Context, cr *vmv1beta1.VMAlertmanager, rclient client.Client) error {
	l := logger.WithContext(ctx).WithValues("secret_for", "vmalertmanager config")
	ctx = logger.AddToContext(ctx, l)
	var prevCR *vmv1beta1.VMAlertmanager
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}

	// name of tls object and it's value
	// e.g. namespace_secret_name_secret_key
	tlsAssets := make(map[string]string)

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
	webCfg, err := buildWebServerConfigYAML(ctx, rclient, cr, tlsAssets)
	if err != nil {
		return fmt.Errorf("cannot build webserver config: %w", err)
	}

	gossipCfg, err := buildGossipConfigYAML(ctx, rclient, cr, tlsAssets)
	if err != nil {
		return fmt.Errorf("cannot build gossip config: %w", err)
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
	if cr.Spec.WebConfig != nil {
		newAMSecretConfig.Data[webserverConfigKey] = webCfg
	}
	if cr.Spec.GossipConfig != nil {
		newAMSecretConfig.Data[gossipConfigKey] = gossipCfg
	}

	for assetKey, assetValue := range tlsAssets {
		newAMSecretConfig.Data[assetKey] = []byte(assetValue)
	}

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = &metav1.ObjectMeta{
			Name:            prevCR.ConfigSecretName(),
			Namespace:       prevCR.Namespace,
			Labels:          prevCR.AllLabels(),
			Annotations:     prevCR.AnnotationsFiltered(),
			OwnerReferences: prevCR.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		}
	}

	return reconcile.Secret(ctx, rclient, newAMSecretConfig, prevSecretMeta)
}

func buildInitConfigContainer(cr *vmv1beta1.VMAlertmanager) []corev1.Container {
	if !ptr.Deref(cr.Spec.UseVMConfigReloader, false) {
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
	useCustomConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)

	var configReloaderArgs []string
	if useCustomConfigReloader {
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

	build.AddsPortProbesToConfigReloaderContainer(useCustomConfigReloader, &configReloaderContainer)

	return configReloaderContainer
}

func getSecretContentForAlertmanager(ctx context.Context, rclient client.Client, secretName, ns string) ([]byte, error) {
	var s corev1.Secret
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: secretName}, &s); err != nil {
		// return nil for backward compatibility
		if errors.IsNotFound(err) {
			logger.WithContext(ctx).Error(err, "alertmanager config secret doens't exist, default config is used", "secret", secretName, "secret_namespace", ns)
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
	var amCfgs []*vmv1beta1.VMAlertmanagerConfig
	var badCfgs []*vmv1beta1.VMAlertmanagerConfig
	if err := k8stools.VisitObjectsForSelectorsAtNs(ctx, rclient, cr.Spec.ConfigNamespaceSelector, cr.Spec.ConfigSelector, cr.Namespace, cr.Spec.SelectAllByDefault,
		func(ams *vmv1beta1.VMAlertmanagerConfigList) {
			for i := range ams.Items {
				item := ams.Items[i]
				if !item.DeletionTimestamp.IsZero() {
					continue
				}
				if item.Spec.ParsingError != "" {
					item.Status.CurrentSyncError = item.Spec.ParsingError
					badCfgs = append(badCfgs, &item)
					continue
				}
				if err := item.Validate(); err != nil {
					item.Status.CurrentSyncError = err.Error()
					badCfgs = append(badCfgs, &item)
					continue
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
	l.Info("selected alertmanager configs",
		"len", len(amCfgs), "invalid configs", len(parsedCfg.brokenAMCfgs))
	if err := updateConfigsStatuses(ctx, rclient, cr, parsedCfg.amcfgs, parsedCfg.brokenAMCfgs); err != nil {
		return nil, fmt.Errorf("failed to update vmalertmanagerConfigs statuses: %w", err)
	}

	badConfigsTotal.Add(float64(len(badCfgs)))
	return parsedCfg.data, nil
}

func subPathForStorage(s *vmv1beta1.StorageSpec) string {
	if s == nil {
		return ""
	}

	return "alertmanager-db"
}

const (
	errorStatusUpdateTTL = 5 * time.Minute
	errorStatusExpireTTL = 15 * time.Minute
)

// performs status update for given alertmanager configs
func updateConfigsStatuses(ctx context.Context, rclient client.Client, amCR *vmv1beta1.VMAlertmanager, okConfigs, badconfig []*vmv1beta1.VMAlertmanagerConfig) error {
	var errors []string

	alertmanagerNamespacedName := fmt.Sprintf("%s/%s", amCR.Namespace, amCR.Name)
	for _, badCfg := range badconfig {

		// change status only at different error
		if badCfg.Status.CurrentSyncError != "" && badCfg.Status.CurrentSyncError != badCfg.Status.LastSyncError {
			// allow to change message only to single alertmanager
			if badCfg.Status.LastErrorParentAlertmanagerName == "" || badCfg.Status.LastErrorParentAlertmanagerName == alertmanagerNamespacedName {
				// patch update status
				pt := client.RawPatch(types.MergePatchType,
					[]byte(fmt.Sprintf(`{"status": {"lastSyncError":  %q , "status": %q, "lastErrorParentAlertmanagerName": %q, "lastSyncErrorTimestamp": %d} }`,
						badCfg.Status.CurrentSyncError, vmv1beta1.UpdateStatusFailed,
						alertmanagerNamespacedName, time.Now().Unix())))
				if err := rclient.Status().Patch(ctx, badCfg, pt); err != nil {
					return fmt.Errorf("failed to patch status of broken VMAlertmanagerConfig=%q: %w", badCfg.Name, err)
				}
			}
		}
		// need to update ttl and parent alertmanager name
		// race condition is possible, but it doesn't really matter.
		lastTs := time.Unix(badCfg.Status.LastSyncErrorTimestamp, 0)
		if time.Since(lastTs) > errorStatusUpdateTTL {
			// update ttl
			pt := client.RawPatch(types.MergePatchType,
				[]byte(fmt.Sprintf(`{"status": { "lastErrorParentAlertmanagerName": %q, "lastSyncErrorTimestamp": %d} }`,
					alertmanagerNamespacedName, time.Now().Unix())))
			if err := rclient.Status().Patch(ctx, badCfg, pt); err != nil {
				return fmt.Errorf("failed to patch status of broken VMAlertmanagerConfig=%q: %w", badCfg.Name, err)
			}
		}

		errors = append(errors, fmt.Sprintf("parent=%s config=namespace/name=%s/%s error text: %s", alertmanagerNamespacedName, badCfg.Namespace, badCfg.Name, badCfg.Status.CurrentSyncError))
	}
	if len(errors) > 0 {
		logger.WithContext(ctx).Error(fmt.Errorf("VMAlertmanagerConfigs have errors"), "skip it for config generation", "errors", strings.Join(errors, ","))
	}
	for _, amCfg := range okConfigs {
		if amCfg.Status.LastSyncError != "" || amCfg.Status.Status != vmv1beta1.UpdateStatusOperational {
			if amCfg.Status.LastErrorParentAlertmanagerName != alertmanagerNamespacedName {
				// transit to ok status only if it's the same alertmanager that set error
				// ot ttl passed
				lastTs := time.Unix(amCfg.Status.LastSyncErrorTimestamp, 0)
				if time.Since(lastTs) < errorStatusExpireTTL {
					continue
				}
			}
			amCfg.Status.LastSyncError = ""
			pt := client.RawPatch(types.MergePatchType,
				[]byte(fmt.Sprintf(`{"status": {"lastSyncError":  "" , "status": %q, "lastSyncErrorTimestamp": 0, "lastErrorParentAlertmanagerName": "" } }`, vmv1beta1.UpdateStatusOperational)))
			if err := rclient.Status().Patch(ctx, amCfg, pt); err != nil {
				return fmt.Errorf("failed to patch status of VMAlertmanagerConfig=%q: %w", amCfg.Name, err)
			}
		}
	}
	return nil
}
