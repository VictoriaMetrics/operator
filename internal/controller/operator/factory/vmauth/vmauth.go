package vmauth

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
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
	"github.com/hashicorp/go-version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmauthPort            = "8427"
	vmAuthConfigMountGz   = "/opt/vmauth-config-gz"
	vmAuthConfigFolder    = "/opt/vmauth"
	vmAuthConfigRawFolder = "/opt/vmauth/config"
	vmAuthConfigName      = "config.yaml"
	vmAuthConfigNameGz    = "config.yaml.gz"
	vmAuthVolumeName      = "config"
)

// CreateOrUpdateVMAuthService creates service for VMAuth
func CreateOrUpdateVMAuthService(ctx context.Context, cr *vmv1beta1.VMAuth, rclient client.Client) (*corev1.Service, error) {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = vmauthPort
	}
	newService := build.Service(cr, cr.Spec.Port, nil)
	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			logger.WithContext(ctx).Error(fmt.Errorf("vmauth additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if err := reconcile.ServiceForCRD(ctx, rclient, additionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmauth: %w", err)
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
		return nil, fmt.Errorf("cannot reconcile service for vmauth: %w", err)
	}
	return newService, nil
}

// CreateOrUpdateVMAuth - handles VMAuth deployment reconciliation.
func CreateOrUpdateVMAuth(ctx context.Context, cr *vmv1beta1.VMAuth, rclient client.Client, c *config.BaseOperatorConf) error {
	l := logger.WithContext(ctx).WithValues("controller", "vmauth.crud")
	ctx = logger.AddToContext(ctx, l)
	if cr.IsOwnsServiceAccount() {
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr)); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
		if c.UseCustomConfigReloader && cr.Spec.ConfigSecret == "" {
			if err := createVMAuthSecretAccess(ctx, cr, rclient); err != nil {
				return err
			}
		}
	}

	// we have to create empty or full cm first
	err := CreateOrUpdateVMAuthConfig(ctx, rclient, cr)
	if err != nil {
		l.Error(err, "cannot create configmap")
		return err
	}

	if cr.Spec.PodDisruptionBudget != nil {
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget)); err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vmauth: %w", err)
		}
	}
	newDeploy, err := newDeployForVMAuth(cr, c)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vmauth: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, c.PodWaitReadyTimeout, false)
}

func newDeployForVMAuth(cr *vmv1beta1.VMAuth, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {
	cr = cr.DeepCopy()

	if cr.Spec.Image.Repository == "" {
		cr.Spec.Image.Repository = c.VMAuthDefault.Image
	}
	if cr.Spec.Image.Tag == "" {
		cr.Spec.Image.Tag = c.VMAuthDefault.Version
	}
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMAuthDefault.Port
	}
	if cr.Spec.Image.PullPolicy == "" {
		cr.Spec.Image.PullPolicy = corev1.PullIfNotPresent
	}
	podSpec, err := makeSpecForVMAuth(cr, c)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          c.Labels.Merge(cr.AllLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
		},
		Spec: appsv1.DeploymentSpec{
			MinReadySeconds:      cr.Spec.MinReadySeconds,
			Replicas:             cr.Spec.ReplicaCount,
			RevisionHistoryLimit: cr.Spec.RevisionHistoryLimitCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: *podSpec,
		},
	}

	return depSpec, nil
}

func makeSpecForVMAuth(cr *vmv1beta1.VMAuth, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	var args []string
	args = append(args, fmt.Sprintf("-auth.config=%s", path.Join(vmAuthConfigFolder, vmAuthConfigName)))

	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if len(cr.Spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})

	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount

	volumes = append(volumes, cr.Spec.Volumes...)
	volumeMounts = append(volumeMounts, cr.Spec.VolumeMounts...)

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
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}
	volumes, volumeMounts = cr.Spec.License.MaybeAddToVolumes(volumes, volumeMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)

	vmauthContainer := corev1.Container{
		Name:                     "vmauth",
		Image:                    fmt.Sprintf("%s:%s", build.FormatContainerImage(c.ContainerRegistry, cr.Spec.Image.Repository), cr.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             volumeMounts,
		Resources:                build.Resources(cr.Spec.Resources, config.Resource(c.VMAuthDefault.Resource), c.VMAuthDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}
	vmauthContainer = build.Probe(vmauthContainer, cr)

	operatorContainers := []corev1.Container{vmauthContainer}

	var initContainers []corev1.Container
	if cr.Spec.ConfigSecret == "" {
		volumes = append(volumes, corev1.Volume{
			Name: "config-out",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		if !c.UseCustomConfigReloader {
			volumes = append(volumes, corev1.Volume{
				Name: vmAuthVolumeName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: cr.ConfigSecretName(),
					},
				},
			})
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      vmAuthVolumeName,
				MountPath: vmAuthConfigRawFolder,
			})
		}
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "config-out",
			MountPath: vmAuthConfigFolder,
		})
		operatorContainers[0].VolumeMounts = volumeMounts

		configReloader := buildVMAuthConfigReloaderContainer(cr, c)
		operatorContainers = append(operatorContainers, configReloader)
		initContainers = append(initContainers,
			buildInitConfigContainer(c.VMAuthDefault.ConfigReloadImage, buildConfigReloaderResourceReqsForVMAuth(c), c, configReloader.Args)...)
	} else {
		volumes = append(volumes, corev1.Volume{
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.Spec.ConfigSecret,
				},
			},
			Name: vmAuthVolumeName,
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      vmAuthVolumeName,
			MountPath: vmAuthConfigFolder,
		})
		operatorContainers[0].VolumeMounts = volumeMounts
	}

	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	if len(cr.Spec.InitContainers) > 0 {
		initContainers, err = k8stools.MergePatchContainers(initContainers, cr.Spec.InitContainers)
		if err != nil {
			return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
		}
	}
	useStrictSecurity := c.EnableStrictSecurity
	if cr.Spec.UseStrictSecurity != nil {
		useStrictSecurity = *cr.Spec.UseStrictSecurity
	}
	vmAuthSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(),
			Annotations: cr.PodAnnotations(),
		},
		Spec: corev1.PodSpec{
			NodeSelector:                  cr.Spec.NodeSelector,
			Volumes:                       volumes,
			InitContainers:                build.AddStrictSecuritySettingsToContainers(initContainers, useStrictSecurity),
			Containers:                    build.AddStrictSecuritySettingsToContainers(containers, useStrictSecurity),
			ServiceAccountName:            cr.GetServiceAccountName(),
			SecurityContext:               build.AddStrictSecuritySettingsToPod(cr.Spec.SecurityContext, useStrictSecurity),
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.Affinity,
			RuntimeClassName:              cr.Spec.RuntimeClassName,
			SchedulerName:                 cr.Spec.SchedulerName,
			Tolerations:                   cr.Spec.Tolerations,
			PriorityClassName:             cr.Spec.PriorityClassName,
			HostNetwork:                   cr.Spec.HostNetwork,
			DNSPolicy:                     cr.Spec.DNSPolicy,
			DNSConfig:                     cr.Spec.DNSConfig,
			TopologySpreadConstraints:     cr.Spec.TopologySpreadConstraints,
			HostAliases:                   cr.Spec.HostAliases,
			TerminationGracePeriodSeconds: cr.Spec.TerminationGracePeriodSeconds,
			ReadinessGates:                cr.Spec.ReadinessGates,
		},
	}
	return vmAuthSpec, nil
}

// CreateOrUpdateVMAuthConfig configuration secret for vmauth.
func CreateOrUpdateVMAuthConfig(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) error {
	// fast path
	if cr.Spec.ConfigSecret != "" {
		return nil
	}
	s := makeVMAuthConfigSecret(cr)

	// name of tls object and it's value
	// e.g. namespace_secret_name_secret_key
	tlsAssets := make(map[string]string)
	generatedConfig, err := buildVMAuthConfig(ctx, rclient, cr, tlsAssets)
	if err != nil {
		return err
	}
	for assetKey, assetValue := range tlsAssets {
		s.Data[assetKey] = []byte(assetValue)
	}

	var buf bytes.Buffer
	if err := gzipConfig(&buf, generatedConfig); err != nil {
		return fmt.Errorf("cannot gzip config for vmagent: %w", err)
	}
	s.Data[vmAuthConfigNameGz] = buf.Bytes()

	var curSecret corev1.Secret

	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: s.Name}, &curSecret); err != nil {
		if errors.IsNotFound(err) {
			logger.WithContext(ctx).Info("creating new configuration secret for vmauth")
			return rclient.Create(ctx, s)
		}
		return err
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &curSecret); err != nil {
		return err
	}
	var (
		generatedConf             = s.Data[vmAuthConfigNameGz]
		curConfig, curConfigFound = curSecret.Data[vmAuthConfigNameGz]
	)
	if curConfigFound {
		if bytes.Equal(curConfig, generatedConf) {
			logger.WithContext(ctx).Info("updating VMAuth configuration secret skipped, no configuration change")
			return nil
		}
		logger.WithContext(ctx).Info("current VMAuth configuration has changed")
	} else {
		logger.WithContext(ctx).Info("no current VMAuth configuration secret found", "currentConfigFound", curConfigFound)
	}
	s.Annotations = labels.Merge(curSecret.Annotations, s.Annotations)
	vmv1beta1.MergeFinalizers(&curSecret, vmv1beta1.FinalizerName)

	logger.WithContext(ctx).Info("updating VMAuth configuration secret")
	return rclient.Update(ctx, s)
}

func makeVMAuthConfigSecret(cr *vmv1beta1.VMAuth) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:   cr.ConfigSecretName(),
			Labels: cr.AllLabels(),
			Annotations: map[string]string{
				"generated": "true",
			},
			Namespace:       cr.Namespace,
			OwnerReferences: cr.AsOwner(),
			Finalizers: []string{
				vmv1beta1.FinalizerName,
			},
		},
		Data: map[string][]byte{
			vmAuthConfigNameGz: {},
		},
	}
}

// CreateOrUpdateVMAuthIngress handles ingress for vmauth.
func CreateOrUpdateVMAuthIngress(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) error {
	if cr.Spec.Ingress == nil {
		// handle delete case
		if err := finalize.VMAuthIngressDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete ingress for vmauth: %s, err :%w", cr.Name, err)
		}
		return nil
	}
	newIngress := buildIngressConfig(cr)
	var existIngress networkingv1.Ingress
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: newIngress.Namespace, Name: newIngress.Name}, &existIngress); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newIngress)
		}
		return err
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &existIngress); err != nil {
		return err
	}
	newIngress.Annotations = labels.Merge(existIngress.Annotations, newIngress.Annotations)
	newIngress.Finalizers = vmv1beta1.MergeFinalizers(&existIngress, vmv1beta1.FinalizerName)
	return rclient.Update(ctx, newIngress)
}

var defaultPt = networkingv1.PathTypePrefix

func buildIngressConfig(cr *vmv1beta1.VMAuth) *networkingv1.Ingress {
	defaultRule := networkingv1.IngressRule{
		Host: cr.Spec.Ingress.Host,
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: []networkingv1.HTTPIngressPath{
					{
						Path: "/",
						Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{
								Name: cr.PrefixedName(),
								Port: networkingv1.ServiceBackendPort{Name: "http"},
							},
						},
						PathType: &defaultPt,
					},
				},
			},
		},
	}
	spec := networkingv1.IngressSpec{
		Rules:            []networkingv1.IngressRule{},
		IngressClassName: cr.Spec.Ingress.ClassName,
	}
	if cr.Spec.Ingress.TlsSecretName != "" {
		spec.TLS = []networkingv1.IngressTLS{
			{
				SecretName: cr.Spec.Ingress.TlsSecretName,
				Hosts:      cr.Spec.Ingress.TlsHosts,
			},
		}
		for _, host := range cr.Spec.Ingress.TlsHosts {
			hostRule := defaultRule.DeepCopy()
			hostRule.Host = host
			spec.Rules = append(spec.Rules, *hostRule)
		}
	} else {
		spec.Rules = append(spec.Rules, defaultRule)
	}
	// add user defined routes.
	spec.Rules = append(spec.Rules, cr.Spec.Ingress.ExtraRules...)
	spec.TLS = append(spec.TLS, cr.Spec.Ingress.ExtraTLS...)
	lbls := labels.Merge(cr.Spec.Ingress.Labels, cr.SelectorLabels())
	return &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          lbls,
			Annotations:     cr.Spec.Ingress.Annotations,
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: spec,
	}
}

func buildVMAuthConfigReloaderContainer(cr *vmv1beta1.VMAuth, c *config.BaseOperatorConf) corev1.Container {
	configReloaderArgs := []string{
		fmt.Sprintf("--reload-url=%s", vmv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)),
		fmt.Sprintf("--config-envsubst-file=%s", path.Join(vmAuthConfigFolder, vmAuthConfigName)),
	}
	if c.UseCustomConfigReloader {
		configReloaderArgs = append(configReloaderArgs, fmt.Sprintf("--config-secret-name=%s/%s", cr.Namespace, cr.ConfigSecretName()))
		configReloaderArgs = vmv1beta1.MaybeEnableProxyProtocol(configReloaderArgs, cr.Spec.ExtraArgs)
	} else {
		configReloaderArgs = append(configReloaderArgs, fmt.Sprintf("--config-file=%s", path.Join(vmAuthConfigMountGz, vmAuthConfigNameGz)))
	}

	reloaderMounts := []corev1.VolumeMount{
		{
			Name:      "config-out",
			MountPath: vmAuthConfigFolder,
		},
	}
	if !c.UseCustomConfigReloader {
		reloaderMounts = append(reloaderMounts, corev1.VolumeMount{
			Name:      vmAuthVolumeName,
			MountPath: vmAuthConfigMountGz,
		})
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
	configReloader := corev1.Container{
		Name:  "config-reloader",
		Image: build.FormatContainerImage(c.ContainerRegistry, c.VMAuthDefault.ConfigReloadImage),

		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		Env: []corev1.EnvVar{
			{
				Name: "POD_NAME",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{FieldPath: "metadata.name"},
				},
			},
		},
		Command:      []string{"/bin/prometheus-config-reloader"},
		Args:         configReloaderArgs,
		VolumeMounts: reloaderMounts,
		Resources:    buildConfigReloaderResourceReqsForVMAuth(c),
	}

	if c.UseCustomConfigReloader {
		configReloader.Image = build.FormatContainerImage(c.ContainerRegistry, c.CustomConfigReloaderImage)
		configReloader.Command = nil
	}

	build.AddsPortProbesToConfigReloaderContainer(&configReloader, c)
	return configReloader
}

var minimalCustomConfigReloaderVersion = version.Must(version.NewVersion("v0.35.0"))

func buildInitConfigContainer(baseImage string, resources corev1.ResourceRequirements, c *config.BaseOperatorConf, configReloaderArgs []string) []corev1.Container {
	var initReloader corev1.Container
	if c.UseCustomConfigReloader && c.CustomConfigReloaderImageVersion().GreaterThanOrEqual(minimalCustomConfigReloaderVersion) {
		initReloader = corev1.Container{
			Image: build.FormatContainerImage(c.ContainerRegistry, c.CustomConfigReloaderImage),
			Name:  "config-init",
			Args:  append(configReloaderArgs, "--only-init-config"),
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "config-out",
					MountPath: vmAuthConfigFolder,
				},
			},
			Resources: resources,
		}
		return []corev1.Container{initReloader}
	}
	initReloader = corev1.Container{
		Image: build.FormatContainerImage(c.ContainerRegistry, baseImage),
		Name:  "config-init",
		Command: []string{
			"/bin/sh",
		},
		Args: []string{
			"-c",
			fmt.Sprintf("gunzip -c %s > %s", path.Join(vmAuthConfigMountGz, vmAuthConfigNameGz), path.Join(vmAuthConfigFolder, vmAuthConfigName)),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: vmAuthConfigMountGz,
			},
			{
				Name:      "config-out",
				MountPath: vmAuthConfigFolder,
			},
		},
		Resources: resources,
	}
	return []corev1.Container{initReloader}
}

func buildConfigReloaderResourceReqsForVMAuth(c *config.BaseOperatorConf) corev1.ResourceRequirements {
	configReloaderResources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{}, Requests: corev1.ResourceList{},
	}
	if c.VMAgentDefault.ConfigReloaderCPU != "0" && c.VMAuthDefault.UseDefaultResources {
		configReloaderResources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMAuthDefault.ConfigReloaderCPU)
		configReloaderResources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMAuthDefault.ConfigReloaderCPU)
	}
	if c.VMAgentDefault.ConfigReloaderMemory != "0" && c.VMAuthDefault.UseDefaultResources {
		configReloaderResources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMAuthDefault.ConfigReloaderMemory)
		configReloaderResources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMAuthDefault.ConfigReloaderMemory)
	}
	return configReloaderResources
}

func gzipConfig(buf *bytes.Buffer, conf []byte) error {
	w := gzip.NewWriter(buf)
	defer w.Close()
	if _, err := w.Write(conf); err != nil {
		return err
	}
	return nil
}
