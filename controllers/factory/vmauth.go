package factory

import (
	"bytes"
	"context"
	"fmt"
	"path"
	"sort"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/psp"
	"github.com/VictoriaMetrics/operator/controllers/factory/vmauth"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v12 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmauthPort          = "8427"
	vmAuthConfigMountGz = "/opt/vmauth-config-gz"
	vmAuthConfigFolder  = "/opt/vmauth"
	vmAuthConfigName    = "config.yaml"
	vmAuthConfigNameGz  = "config.yaml.gz"
	vmAuthVolumeName    = "config"
)

// CreateOrUpdateVMAuthService creates service for VMAuth
func CreateOrUpdateVMAuthService(ctx context.Context, cr *victoriametricsv1beta1.VMAuth, rclient client.Client) (*corev1.Service, error) {
	cr = cr.DeepCopy()
	if cr.Spec.Port == "" {
		cr.Spec.Port = vmauthPort
	}
	additionalService := buildDefaultService(cr, cr.Spec.Port, nil)
	mergeServiceSpec(additionalService, cr.Spec.ServiceSpec)

	newService := buildDefaultService(cr, cr.Spec.Port, nil)

	if cr.Spec.ServiceSpec != nil {
		if additionalService.Name == newService.Name {
			log.Error(fmt.Errorf("vmauth additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if _, err := reconcileServiceForCRD(ctx, rclient, additionalService); err != nil {
			return nil, err
		}
	}

	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.SelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: cr.PrefixedName}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.ServiceSpec); err != nil {
		return nil, err
	}

	return reconcileServiceForCRD(ctx, rclient, newService)
}

// CreateOrUpdateVMAuth - handles VMAuth deployment reconciliation.
func CreateOrUpdateVMAuth(ctx context.Context, cr *victoriametricsv1beta1.VMAuth, rclient client.Client, c *config.BaseOperatorConf) error {
	l := log.WithValues("controller", "vmauth.crud")

	if err := psp.CreateServiceAccountForCRD(ctx, cr, rclient); err != nil {
		return fmt.Errorf("failed create service account: %w", err)
	}
	if c.PSPAutoCreateEnabled {
		if err := psp.CreateOrUpdateServiceAccountWithPSP(ctx, cr, rclient); err != nil {
			l.Error(err, "cannot create podsecuritypolicy")
			return fmt.Errorf("cannot create podsecurity policy for vmauth, err: %w", err)
		}
	}
	if c.UseCustomConfigReloader {
		if err := vmauth.CreateVMAuthSecretAccess(ctx, cr, rclient); err != nil {
			return err
		}
	}

	// we have to create empty or full cm first
	err := createOrUpdateVMAuthConfig(ctx, rclient, cr)
	if err != nil {
		l.Error(err, "cannot create configmap")
		return err
	}

	if cr.Spec.PodDisruptionBudget != nil {
		err = CreateOrUpdatePodDisruptionBudget(ctx, rclient, cr, cr.Kind, cr.Spec.PodDisruptionBudget)
		if err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vmauth: %w", err)
		}
	}
	newDeploy, err := newDeployForVMAuth(cr, c)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vmagent: %w", err)
	}

	return k8stools.HandleDeployUpdate(ctx, rclient, newDeploy)
}

func newDeployForVMAuth(cr *victoriametricsv1beta1.VMAuth, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {
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
			Replicas: cr.Spec.ReplicaCount,
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

func makeSpecForVMAuth(cr *victoriametricsv1beta1.VMAuth, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-auth.config=%s", path.Join(vmAuthConfigFolder, vmAuthConfigName)),
	}

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
	volumes := []corev1.Volume{
		{
			Name: vmAuthVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.ConfigSecretName(),
				},
			},
		},
		{
			Name: "config-out",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
	}

	volumes = append(volumes, cr.Spec.Volumes...)
	vmMounts := []corev1.VolumeMount{
		{
			Name:      "config-out",
			MountPath: vmAuthConfigFolder,
		},
	}

	vmMounts = append(vmMounts, cr.Spec.VolumeMounts...)

	for _, s := range cr.Spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(SecretsDir, s),
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
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}

	args = addExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)

	vmauthContainer := corev1.Container{
		Name:                     "vmauth",
		Image:                    fmt.Sprintf("%s:%s", formatContainerImage(c.ContainerRegistry, cr.Spec.Image.Repository), cr.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                buildResources(cr.Spec.Resources, config.Resource(c.VMAuthDefault.Resource), c.VMAuthDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}

	configReloader := buildVMAuthConfigReloaderContainer(cr, c)
	cr.Spec.InitContainers = maybeAddInitConfigContainer(cr.Spec.InitContainers, c, vmAuthConfigMountGz, vmAuthConfigNameGz, vmAuthConfigFolder, vmAuthConfigName)

	vmauthContainer = buildProbe(vmauthContainer, cr)
	operatorContainers := []corev1.Container{configReloader, vmauthContainer}
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	vmAuthSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(),
			Annotations: cr.PodAnnotations(),
		},
		Spec: corev1.PodSpec{
			NodeSelector:                  cr.Spec.NodeSelector,
			Volumes:                       volumes,
			InitContainers:                cr.Spec.InitContainers,
			Containers:                    containers,
			ServiceAccountName:            cr.GetServiceAccountName(),
			SecurityContext:               cr.Spec.SecurityContext,
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

// creates configuration secret for vmauth.
func createOrUpdateVMAuthConfig(ctx context.Context, rclient client.Client, cr *victoriametricsv1beta1.VMAuth) error {

	s := makeVMAuthConfigSecret(cr)

	generatedConfig, err := buildVMAuthConfig(ctx, rclient, cr)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	if err := gzipConfig(&buf, generatedConfig); err != nil {
		return fmt.Errorf("cannot gzip config for vmagent: %w", err)
	}
	s.Data[vmAuthConfigNameGz] = buf.Bytes()

	var curSecret corev1.Secret

	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: s.Name}, &curSecret); err != nil {
		if errors.IsNotFound(err) {
			log.Info("creating new configuration secret for vmauth")
			return rclient.Create(ctx, s)
		}
		return err
	}
	var (
		generatedConf             = s.Data[vmAuthConfigNameGz]
		curConfig, curConfigFound = curSecret.Data[vmAuthConfigNameGz]
	)
	if curConfigFound {
		if bytes.Equal(curConfig, generatedConf) {
			log.Info("updating VMAuth configuration secret skipped, no configuration change")
			return nil
		}
		log.Info("current VMAuth configuration has changed")
	} else {
		log.Info("no current VMAuth configuration secret found", "currentConfigFound", curConfigFound)
	}
	s.Annotations = labels.Merge(curSecret.Annotations, s.Annotations)
	victoriametricsv1beta1.MergeFinalizers(&curSecret, victoriametricsv1beta1.FinalizerName)

	log.Info("updating VMAuth configuration secret")
	return rclient.Update(ctx, s)
}

func makeVMAuthConfigSecret(cr *victoriametricsv1beta1.VMAuth) *corev1.Secret {
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
				victoriametricsv1beta1.FinalizerName,
			},
		},
		Data: map[string][]byte{
			vmAuthConfigNameGz: {},
		},
	}
}

// CreateOrUpdateVMAuthIngress handles ingress for vmauth.
func CreateOrUpdateVMAuthIngress(ctx context.Context, rclient client.Client, cr *victoriametricsv1beta1.VMAuth) error {
	if cr.Spec.Ingress == nil {
		// handle delete case
		if err := finalize.VMAuthIngressDelete(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete ingress for vmauth: %s, err :%w", cr.Name, err)
		}
		return nil
	}
	newIngress := buildIngressConfig(cr)
	var existIngress v12.Ingress
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: newIngress.Namespace, Name: newIngress.Name}, &existIngress); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, newIngress)
		}
		return err
	}
	newIngress.Annotations = labels.Merge(existIngress.Annotations, newIngress.Annotations)
	newIngress.Finalizers = victoriametricsv1beta1.MergeFinalizers(&existIngress, victoriametricsv1beta1.FinalizerName)
	return rclient.Update(ctx, newIngress)
}

var defaultPt = v12.PathTypePrefix

func buildIngressConfig(cr *victoriametricsv1beta1.VMAuth) *v12.Ingress {
	defaultRule := v12.IngressRule{
		Host: cr.Spec.Ingress.Host,
		IngressRuleValue: v12.IngressRuleValue{
			HTTP: &v12.HTTPIngressRuleValue{
				Paths: []v12.HTTPIngressPath{
					{
						Path: "/",
						Backend: v12.IngressBackend{
							Service: &v12.IngressServiceBackend{
								Name: cr.PrefixedName(),
								Port: v12.ServiceBackendPort{Name: "http"},
							},
						},
						PathType: &defaultPt,
					},
				},
			},
		},
	}
	spec := v12.IngressSpec{
		Rules:            []v12.IngressRule{},
		IngressClassName: cr.Spec.Ingress.ClassName,
	}
	if cr.Spec.Ingress.TlsSecretName != "" {
		spec.TLS = []v12.IngressTLS{
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
	return &v12.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          lbls,
			Annotations:     cr.Spec.Ingress.Annotations,
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: spec,
	}
}

func buildVMAuthConfigReloaderContainer(cr *victoriametricsv1beta1.VMAuth, c *config.BaseOperatorConf) corev1.Container {
	configReloaderArgs := []string{
		fmt.Sprintf("--reload-url=%s", victoriametricsv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)),
		fmt.Sprintf("--config-envsubst-file=%s", path.Join(vmAuthConfigFolder, vmAuthConfigName)),
	}
	if c.UseCustomConfigReloader {
		configReloaderArgs = append(configReloaderArgs, fmt.Sprintf("--config-secret-name=%s/%s", cr.Namespace, cr.ConfigSecretName()))
		configReloaderArgs = victoriametricsv1beta1.MaybeEnableProxyProtocol(configReloaderArgs, cr.Spec.ExtraArgs)
	} else {
		configReloaderArgs = append(configReloaderArgs, fmt.Sprintf("--config-file=%s", path.Join(vmAuthConfigMountGz, vmAuthConfigNameGz)))
	}

	reloaderMounts := []corev1.VolumeMount{
		{
			Name:      "config-out",
			MountPath: vmAuthConfigFolder,
		},
		{
			Name:      vmAuthVolumeName,
			MountPath: vmAuthConfigMountGz,
		},
	}
	configReloaderResources := corev1.ResourceRequirements{
		Limits: corev1.ResourceList{}, Requests: corev1.ResourceList{}}
	if c.VMAuthDefault.ConfigReloaderCPU != "0" && c.VMAuthDefault.UseDefaultResources {
		configReloaderResources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMAuthDefault.ConfigReloaderCPU)
	}
	if c.VMAgentDefault.ConfigReloaderMemory != "0" && c.VMAuthDefault.UseDefaultResources {
		configReloaderResources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMAuthDefault.ConfigReloaderMemory)
	}

	configReloader := corev1.Container{
		Name:                     "config-reloader",
		Image:                    formatContainerImage(c.ContainerRegistry, c.VMAuthDefault.ConfigReloadImage),
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
		Resources:    configReloaderResources,
	}

	if c.UseCustomConfigReloader {
		configReloader.Image = fmt.Sprintf("%s", formatContainerImage(c.ContainerRegistry, c.CustomConfigReloaderImage))
		configReloader.Command = []string{"/usr/local/bin/config-reloader"}
	}
	return configReloader
}

func maybeAddInitConfigContainer(src []corev1.Container, c *config.BaseOperatorConf, configDirName, configFileName, outConfigDir, outFileName string) []corev1.Container {
	// TODO add support for custom reloader
	if c.UseCustomConfigReloader {
		return src
	}
	initReloader := corev1.Container{
		Image: c.VMAgentDefault.ConfigReloadImage,
		Name:  "config-init",
		Command: []string{
			"/bin/sh",
		},
		Args: []string{
			"-c",
			fmt.Sprintf("gunzip -c %s > %s", path.Join(configDirName, configFileName), path.Join(outConfigDir, outFileName)),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "config",
				MountPath: configDirName,
			},
			{
				Name:      "config-out",
				MountPath: outConfigDir,
			},
		},
	}
	src = append(src, initReloader)
	return src
}
