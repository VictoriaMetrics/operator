package vmauth

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	vmAuthConfigMountGz   = "/opt/vmauth-config-gz"
	vmAuthConfigFolder    = "/opt/vmauth"
	vmAuthConfigRawFolder = "/opt/vmauth/config"
	vmAuthConfigName      = "config.yaml"
	vmAuthConfigNameGz    = "config.yaml.gz"
	vmAuthVolumeName      = "config"
)

// CreateOrUpdateVMAuth - handles VMAuth deployment reconciliation.
func CreateOrUpdateVMAuth(ctx context.Context, cr *vmv1beta1.VMAuth, rclient client.Client) error {

	var prevCR *vmv1beta1.VMAuth
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
		if ptr.Deref(cr.Spec.UseVMConfigReloader, false) {
			if err := createVMAuthSecretAccess(ctx, rclient, cr, prevCR); err != nil {
				return err
			}
		}
	}
	svc, err := createOrUpdateVMAuthService(ctx, rclient, cr, prevCR)
	if err != nil {
		return fmt.Errorf("cannot create or update vmauth service :%w", err)
	}
	if err := createOrUpdateVMAuthIngress(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot create or update ingress for vmauth: %w", err)
	}
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr)); err != nil {
			return err
		}
	}

	if err := CreateOrUpdateVMAuthConfig(ctx, rclient, cr, nil); err != nil {
		return err
	}

	if cr.Spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevCR, prevCR.Spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(cr, cr.Spec.PodDisruptionBudget), prevPDB); err != nil {
			return fmt.Errorf("cannot update pod disruption budget for vmauth: %w", err)
		}
	}
	var prevDeploy *appsv1.Deployment
	if prevCR != nil {
		prevDeploy, err = newDeployForVMAuth(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}

	newDeploy, err := newDeployForVMAuth(cr)
	if err != nil {
		return fmt.Errorf("cannot build new deploy for vmauth: %w", err)
	}
	if err := reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false); err != nil {
		return fmt.Errorf("cannot reconcile vmauth deployment: %w", err)
	}
	if err := deletePrevStateResources(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return nil
}

func newDeployForVMAuth(cr *vmv1beta1.VMAuth) (*appsv1.Deployment, error) {

	podSpec, err := makeSpecForVMAuth(cr)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, ptr.Deref(cr.Spec.UseStrictSecurity, false), &cr.Spec.CommonApplicationDeploymentParams)

	return depSpec, nil
}

func makeSpecForVMAuth(cr *vmv1beta1.VMAuth) (*corev1.PodTemplateSpec, error) {
	var args []string
	configPath := path.Join(vmAuthConfigFolder, vmAuthConfigName)
	if cr.Spec.ExternalConfig.LocalPath != "" {
		configPath = cr.Spec.ExternalConfig.LocalPath
	}
	args = append(args, fmt.Sprintf("-auth.config=%s", configPath))

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

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, false)
	useCustomConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)

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

	var initContainers []corev1.Container
	var operatorContainers []corev1.Container
	// config mount options
	switch {
	case cr.Spec.ExternalConfig.SecretRef != nil:
		var keyToPath []corev1.KeyToPath
		if cr.Spec.ExternalConfig.SecretRef.Key != "" {
			keyToPath = append(keyToPath, corev1.KeyToPath{
				Key:  cr.Spec.ExternalConfig.SecretRef.Key,
				Path: vmAuthConfigName,
			})
		}
		volumes = append(volumes, corev1.Volume{
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.Spec.ExternalConfig.SecretRef.Name,
					Items:      keyToPath,
				},
			},
			Name: vmAuthVolumeName,
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      vmAuthVolumeName,
			MountPath: vmAuthConfigFolder,
		})

	case cr.Spec.ExternalConfig.LocalPath != "":
		// no-op external managed configuration
		// add check interval
		args = append(args, "-configCheckInterval=1m")
	default:
		volumes = append(volumes, corev1.Volume{
			Name: "config-out",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
		if !useCustomConfigReloader {
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

		configReloader := buildVMAuthConfigReloaderContainer(cr)
		operatorContainers = append(operatorContainers, configReloader)
		initContainers = append(initContainers,
			buildInitConfigContainer(useCustomConfigReloader, cr.Spec.ConfigReloaderImageTag, cr.Spec.ConfigReloaderResources, configReloader.Args)...)
		build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, initContainers, useStrictSecurity)
	}
	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply patch for initContainers: %w", err)
	}

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)

	vmauthContainer := corev1.Container{
		Name:                     "vmauth",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             volumeMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}
	vmauthContainer = build.Probe(vmauthContainer, cr)

	// move vmauth container to the 0 index
	operatorContainers = append([]corev1.Container{vmauthContainer}, operatorContainers...)

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, operatorContainers, useStrictSecurity)
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
			Volumes:            volumes,
			InitContainers:     ic,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}
	return vmAuthSpec, nil
}

// CreateOrUpdateVMAuthConfig configuration secret for vmauth.
func CreateOrUpdateVMAuthConfig(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth, childObject *vmv1beta1.VMUser) error {
	// fast path
	if cr.Spec.ExternalConfig.SecretRef != nil || cr.Spec.ExternalConfig.LocalPath != "" {
		return nil
	}
	var prevCR *vmv1beta1.VMAuth
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	s := &corev1.Secret{
		ObjectMeta: buildConfigSecretMeta(cr),
		Data: map[string][]byte{
			vmAuthConfigNameGz: {},
		},
	}
	// fetch exist users for vmauth.
	sus, err := selectVMUsers(ctx, rclient, cr)
	if err != nil {
		return err
	}
	tlsAssets := make(map[string]string)
	generatedConfig, err := buildVMAuthConfig(ctx, rclient, cr, sus, tlsAssets)
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
	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = ptr.To(buildConfigSecretMeta(prevCR))
	}
	if err := reconcile.Secret(ctx, rclient, s, prevSecretMeta); err != nil {
		return err
	}
	logger.SelectedObjects(ctx, "VMUsers", len(sus.namespacedNames), len(sus.brokenVMUsers), sus.namespacedNames)

	parentObject := fmt.Sprintf("%s.%s.vmauth", cr.GetName(), cr.GetNamespace())
	if childObject != nil {
		// fast path
		for _, u := range sus.users {
			if u.Name == childObject.Name && u.Namespace == childObject.Namespace {
				return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMUser{u})
			}
		}
		for _, u := range sus.brokenVMUsers {
			if u.Name == childObject.Name && u.Namespace == childObject.Namespace {
				return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMUser{u})
			}
		}
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sus.users); err != nil {
		return fmt.Errorf("cannot update statuses for vmusers: %w", err)
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, sus.brokenVMUsers); err != nil {
		return fmt.Errorf("cannot update statuses for broken vmusers: %w", err)
	}

	return nil
}

func buildConfigSecretMeta(cr *vmv1beta1.VMAuth) metav1.ObjectMeta {
	return metav1.ObjectMeta{
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
	}
}

// createOrUpdateVMAuthIngress handles ingress for vmauth.
func createOrUpdateVMAuthIngress(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) error {
	if cr.Spec.Ingress == nil {
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
	// TODO compare
	newIngress.Annotations = labels.Merge(existIngress.Annotations, newIngress.Annotations)
	vmv1beta1.AddFinalizer(newIngress, &existIngress)
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

func buildVMAuthConfigReloaderContainer(cr *vmv1beta1.VMAuth) corev1.Container {
	configReloaderArgs := []string{
		fmt.Sprintf("--reload-url=%s", vmv1beta1.BuildReloadPathWithPort(cr.Spec.ExtraArgs, cr.Spec.Port)),
		fmt.Sprintf("--config-envsubst-file=%s", path.Join(vmAuthConfigFolder, vmAuthConfigName)),
	}
	useCustomConfigReloader := ptr.Deref(cr.Spec.UseVMConfigReloader, false)
	if useCustomConfigReloader {
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
	if !useCustomConfigReloader {
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
		sort.Strings(configReloaderArgs)
	}
	configReloader := corev1.Container{
		Name:  "config-reloader",
		Image: cr.Spec.ConfigReloaderImageTag,

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
		Resources:    cr.Spec.ConfigReloaderResources,
	}

	if useCustomConfigReloader {
		configReloader.Command = nil
	}

	build.AddsPortProbesToConfigReloaderContainer(useCustomConfigReloader, &configReloader)
	return configReloader
}

func buildInitConfigContainer(useCustomConfigReloader bool, baseImage string, resources corev1.ResourceRequirements, configReloaderArgs []string) []corev1.Container {
	var initReloader corev1.Container
	if useCustomConfigReloader {
		initReloader = corev1.Container{
			Image: baseImage,
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
		Image: baseImage,
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

func gzipConfig(buf *bytes.Buffer, conf []byte) error {
	w := gzip.NewWriter(buf)
	defer w.Close()
	if _, err := w.Write(conf); err != nil {
		return err
	}
	return nil
}

// createOrUpdateVMAuthService creates service for VMAuth
func createOrUpdateVMAuthService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAuth) (*corev1.Service, error) {
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, nil)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.ServiceSpec)
	}
	newService := build.Service(cr, cr.Spec.Port, nil)
	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vmauth additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmauth: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmauth: %w", err)
	}
	return newService, nil
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAuth) error {
	if prevCR == nil {
		return nil
	}

	prevSvc, currSvc := prevCR.Spec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil && prevCR.Spec.PodDisruptionBudget != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}

	if cr.Spec.Ingress == nil && prevCR.Spec.Ingress != nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &networkingv1.Ingress{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot delete ingress from prev state: %w", err)
		}
	}
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(prevCR.Spec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}
