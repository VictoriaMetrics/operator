package vmauth

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"path"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	vmAuthConfigMountGz   = "/opt/vmauth-config-gz"
	vmAuthConfigFolder    = "/opt/vmauth"
	vmAuthConfigRawFolder = "/opt/vmauth/config"
	vmAuthConfigName      = "config.yaml"
	vmAuthConfigNameGz    = "config.yaml.gz"
	vmAuthVolumeName      = "config"
	internalPortName      = "internal"
)

// CreateOrUpdate - handles VMAuth deployment reconciliation.
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMAuth, rclient client.Client) error {

	var prevCR *vmv1beta1.VMAuth
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	cfg := config.MustGetBaseConfig()
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
		if err := createVMAuthSecretAccess(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
	}
	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return fmt.Errorf("cannot create or update vmauth service :%w", err)
	}
	if err := createOrUpdateIngress(ctx, rclient, cr); err != nil {
		return fmt.Errorf("cannot create or update ingress for vmauth: %w", err)
	}

	if cr.Spec.HTTPRoute != nil && !cfg.GatewayAPIEnabled {
		return fmt.Errorf("spec.httpRoute is set but VM_GATEWAY_API_ENABLED=true env var was not provided")
	}
	if err := createOrUpdateHTTPRoute(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot create or update httpRoute for vmauth: %w", err)
	}

	if err := createOrUpdateHPA(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("cannot create or update hpa for vmauth: %w", err)
	}
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, cfg.DisableSelfServiceScrapeCreation) {
		// it's not possible to scrape metrics from vmauth if proxyProtocol is configured
		if !cr.UseProxyProtocol() {
			if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, buildServiceScrape(svc, cr)); err != nil {
				return err
			}
		}
	}

	if err := CreateOrUpdateConfig(ctx, rclient, cr, nil); err != nil {
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
	if err := reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, cr.Spec.HPA != nil); err != nil {
		return fmt.Errorf("cannot reconcile vmauth deployment: %w", err)
	}
	if prevCR != nil {
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return err
		}
	}
	return nil
}

func createOrUpdateHTTPRoute(ctx context.Context, rclient client.Client, cr, prevCr *vmv1beta1.VMAuth) error {
	if cr.Spec.HTTPRoute == nil {
		return nil
	}

	newHTTPRoute, err := build.HTTPRoute(cr, cr.Spec.Port, cr.Spec.HTTPRoute)
	if err != nil {
		return err
	}

	var prevHTTPRoute *gwapiv1.HTTPRoute
	if prevCr != nil && prevCr.Spec.HTTPRoute != nil {
		prevHTTPRoute, err = build.HTTPRoute(cr, cr.Spec.Port, prevCr.Spec.HTTPRoute)
		if err != nil {
			return err
		}
	}

	return reconcile.HTTPRoute(ctx, rclient, newHTTPRoute, prevHTTPRoute)
}

func newDeployForVMAuth(cr *vmv1beta1.VMAuth) (*appsv1.Deployment, error) {

	cfg := config.MustGetBaseConfig()
	podSpec, err := makeSpecForVMAuth(cr)
	if err != nil {
		return nil, err
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.UpdateStrategy != nil {
		strategyType = *cr.Spec.UpdateStrategy
	}
	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.RollingUpdate,
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, ptr.Deref(cr.Spec.UseStrictSecurity, cfg.EnableStrictSecurity), &cr.Spec.CommonApplicationDeploymentParams)

	return depSpec, nil
}

func makeSpecForVMAuth(cr *vmv1beta1.VMAuth) (*corev1.PodTemplateSpec, error) {
	var args []string
	configPath := path.Join(vmAuthConfigFolder, vmAuthConfigName)
	if cr.Spec.LocalPath != "" {
		configPath = cr.Spec.LocalPath
	}
	args = append(args, fmt.Sprintf("-auth.config=%s", configPath))

	cfg := config.MustGetBaseConfig()
	if cr.Spec.UseProxyProtocol {
		args = append(args, "-httpListenAddr.useProxyProtocol=true")
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if len(cr.Spec.InternalListenPort) > 0 {
		args = append(args, fmt.Sprintf("-httpInternalListenAddr=:%s", cr.Spec.InternalListenPort))
	}
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort

	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})

	if len(cr.Spec.InternalListenPort) > 0 {
		ports = append(ports, corev1.ContainerPort{
			Name:          internalPortName,
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.InternalListenPort).IntVal,
		})
	}

	useStrictSecurity := ptr.Deref(cr.Spec.UseStrictSecurity, cfg.EnableStrictSecurity)

	var volumes []corev1.Volume
	var volumeMounts []corev1.VolumeMount
	var crMounts []corev1.VolumeMount

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
	volumes, volumeMounts = build.LicenseVolumeTo(volumes, volumeMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

	var initContainers []corev1.Container
	var operatorContainers []corev1.Container
	// config mount options
	switch {
	case cr.Spec.SecretRef != nil:
		var keyToPath []corev1.KeyToPath
		if cr.Spec.SecretRef.Key != "" {
			keyToPath = append(keyToPath, corev1.KeyToPath{
				Key:  cr.Spec.SecretRef.Key,
				Path: vmAuthConfigName,
			})
		}
		volumes = append(volumes, corev1.Volume{
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.Spec.SecretRef.Name,
					Items:      keyToPath,
				},
			},
			Name: vmAuthVolumeName,
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      vmAuthVolumeName,
			MountPath: vmAuthConfigFolder,
		})

	case cr.Spec.LocalPath != "":
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
		m := corev1.VolumeMount{
			Name:      "config-out",
			MountPath: vmAuthConfigFolder,
		}
		volumeMounts = append(volumeMounts, m)
		crMounts = append(crMounts, m)
		ss := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: cr.ConfigSecretName(),
			},
			Key: vmAuthConfigName,
		}
		configReloader := build.ConfigReloaderContainer(false, cr, crMounts, ss)
		operatorContainers = append(operatorContainers, configReloader)
		initContainers = append(initContainers, build.ConfigReloaderContainer(true, cr, crMounts, ss))
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
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}
	vmauthContainer = addVMAuthProbes(cr, vmauthContainer)
	build.AddConfigReloadAuthKeyToApp(&vmauthContainer, &cr.Spec.CommonConfigReloaderParams)

	// move vmauth container to the 0 index
	operatorContainers = append([]corev1.Container{vmauthContainer}, operatorContainers...)

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, operatorContainers, useStrictSecurity)
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	volumes = build.AddServiceAccountTokenVolume(volumes, &cr.Spec.CommonApplicationDeploymentParams)
	volumes = build.AddConfigReloadAuthKeyVolume(volumes, &cr.Spec.CommonConfigReloaderParams)

	return &corev1.PodTemplateSpec{
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
	}, nil
}

func getAssetsCache(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{
		build.TLSAssetsResourceKind: {
			MountDir:   vmAuthConfigRawFolder,
			SecretName: build.ResourceName(build.TLSAssetsResourceKind, cr),
		},
	}
	return build.NewAssetsCache(ctx, rclient, cfg)
}

// CreateOrUpdateConfig configuration secret for vmauth.
func CreateOrUpdateConfig(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth, childObject *vmv1beta1.VMUser) error {
	// fast path
	if cr.Spec.SecretRef != nil || cr.Spec.LocalPath != "" {
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
	pos, err := selectUsers(ctx, rclient, cr)
	if err != nil {
		return err
	}
	ac := getAssetsCache(ctx, rclient, cr)
	generatedConfig, err := pos.buildConfig(ctx, rclient, cr, ac)
	if err != nil {
		return err
	}
	creds := ac.GetOutput()
	if secret, ok := creds[build.TLSAssetsResourceKind]; ok {
		for name, value := range secret.Data {
			s.Data[name] = value
		}
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
	pos.users.UpdateMetrics(ctx)

	parentObject := fmt.Sprintf("%s.%s.vmauth", cr.GetName(), cr.GetNamespace())
	if childObject != nil {
		if u := pos.users.Get(childObject); u != nil {
			return reconcile.StatusForChildObjects(ctx, rclient, parentObject, []*vmv1beta1.VMUser{u})
		}
	}
	if err := reconcile.StatusForChildObjects(ctx, rclient, parentObject, pos.users.All()); err != nil {
		return fmt.Errorf("cannot update statuses for vmusers: %w", err)
	}
	return nil
}

func buildConfigSecretMeta(cr *vmv1beta1.VMAuth) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:   cr.ConfigSecretName(),
		Labels: cr.FinalLabels(),
		Annotations: map[string]string{
			"generated": "true",
		},
		Namespace:       cr.Namespace,
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		Finalizers: []string{
			vmv1beta1.FinalizerName,
		},
	}
}

// createOrUpdateIngress handles ingress for vmauth.
func createOrUpdateIngress(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) error {
	if cr.Spec.Ingress == nil {
		return nil
	}
	newIngress := buildIngressConfig(cr)
	var existIngress networkingv1.Ingress
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: newIngress.Namespace, Name: newIngress.Name}, &existIngress); err != nil {
		if k8serrors.IsNotFound(err) {
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

func buildIngressConfig(cr *vmv1beta1.VMAuth) *networkingv1.Ingress {
	var paths []networkingv1.HTTPIngressPath
	defaultBackend := networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{
			Name: cr.PrefixedName(),
			Port: networkingv1.ServiceBackendPort{Name: "http"},
		},
	}
	if len(cr.Spec.Ingress.Paths) == 0 {
		paths = []networkingv1.HTTPIngressPath{
			{
				Path:     "/",
				PathType: ptr.To(networkingv1.PathTypePrefix),
				Backend:  defaultBackend,
			},
		}
	} else {
		for _, p := range cr.Spec.Ingress.Paths {
			paths = append(paths, networkingv1.HTTPIngressPath{
				Path:     p,
				PathType: ptr.To(networkingv1.PathTypePrefix),
				Backend:  defaultBackend,
			})
		}
	}
	defaultRule := networkingv1.IngressRule{
		Host: cr.Spec.Ingress.Host,
		IngressRuleValue: networkingv1.IngressRuleValue{
			HTTP: &networkingv1.HTTPIngressRuleValue{
				Paths: paths,
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
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: spec,
	}
}

func gzipConfig(buf *bytes.Buffer, conf []byte) error {
	w := gzip.NewWriter(buf)
	defer w.Close()
	if _, err := w.Write(conf); err != nil {
		return err
	}
	return nil
}

func setInternalSvcPort(cr *vmv1beta1.VMAuth) func(svc *corev1.Service) {
	return func(svc *corev1.Service) {
		if len(cr.Spec.InternalListenPort) > 0 {
			p := intstr.Parse(cr.Spec.InternalListenPort)
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       internalPortName,
				Port:       p.IntVal,
				TargetPort: p,
			})
		}
	}
}

// createOrUpdateService creates service for VMAuth
func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAuth) (*corev1.Service, error) {
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, setInternalSvcPort(prevCR))
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.ServiceSpec)
	}
	newService := build.Service(cr, cr.Spec.Port, setInternalSvcPort(cr))
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
func createOrUpdateHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMAuth) error {
	if cr.Spec.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.PrefixedName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	newHPA := build.HPA(cr, targetRef, cr.Spec.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.HPA != nil {
		prevHPA = build.HPA(prevCR, targetRef, prevCR.Spec.HPA)
	}
	return reconcile.HPA(ctx, rclient, newHPA, prevHPA)
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMAuth) error {
	owner := cr.AsOwner()
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if cr.Spec.PodDisruptionBudget == nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot delete PDB from prev state: %w", err)
		}
	}

	cfg := config.MustGetBaseConfig()
	if cfg.GatewayAPIEnabled && cr.Spec.HTTPRoute == nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &gwapiv1.HTTPRoute{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot delete httproute from prev state: %w", err)
		}
	}

	if cr.Spec.Ingress == nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &networkingv1.Ingress{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot delete ingress from prev state: %w", err)
		}
	}
	if cr.Spec.HPA == nil {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove HPA from prev state: %w", err)
		}
	}
	disableSelfScrape := cfg.DisableSelfServiceScrapeCreation
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, disableSelfScrape) {
		if err := finalize.SafeDeleteForSelectorsWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}, cr.SelectorLabels(), &owner); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, disableSelfScrape) {
		if cr.UseProxyProtocol() {
			if err := finalize.SafeDeleteForSelectorsWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}, cr.SelectorLabels(), &owner); err != nil {
				return fmt.Errorf("cannot remove serviceScrape: %w", err)
			}
		}
	}

	svcName := cr.PrefixedName()
	keepServices := map[string]struct{}{
		svcName: {},
	}
	if cr.Spec.ServiceSpec != nil && !cr.Spec.ServiceSpec.UseAsDefault {
		extraSvcName := cr.Spec.ServiceSpec.NameOrDefault(svcName)
		keepServices[extraSvcName] = struct{}{}
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, cr, keepServices); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}
	if !cr.IsOwnsServiceAccount() {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.ServiceAccount{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove serviceaccount: %w", err)
		}
	}
	return nil
}

func buildServiceScrape(svc *corev1.Service, cr *vmv1beta1.VMAuth) *vmv1beta1.VMServiceScrape {
	b := build.VMServiceScrapeForServiceWithSpec(svc, cr)
	if len(cr.Spec.InternalListenPort) == 0 {
		return b
	}
	for idx := range b.Spec.Endpoints {
		ep := &b.Spec.Endpoints[idx]
		if ep.Port == "http" {
			ep.Port = internalPortName
			break
		}
	}
	return b
}

func addVMAuthProbes(cr *vmv1beta1.VMAuth, vmauthContainer corev1.Container) corev1.Container {
	if cr.UseProxyProtocol() && cr.Spec.EmbeddedProbes == nil {
		probePort := intstr.Parse(cr.ProbePort())
		cr.Spec.EmbeddedProbes = &vmv1beta1.EmbeddedProbes{
			ReadinessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: probePort,
					},
				},
			},
			LivenessProbe: &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					TCPSocket: &corev1.TCPSocketAction{
						Port: probePort,
					},
				},
			},
		}
	}
	vmauthContainer = build.Probe(vmauthContainer, cr)
	return vmauthContainer
}
