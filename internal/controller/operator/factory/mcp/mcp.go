package mcp

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

// mcpCR is implemented by CRDs of MCP servers
type mcpCR interface {
	client.Object
	PrefixedName() string
	SelectorLabels() map[string]string
	PodLabels() map[string]string
	PodAnnotations() map[string]string
	FinalLabels() map[string]string
	FinalAnnotations() map[string]string
	AsOwner() metav1.OwnerReference
	GetServiceAccountName() string
	IsOwnsServiceAccount() bool
	GetAdditionalService() *vmv1beta1.AdditionalServiceSpec
	GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec
	GetExtraArgs() map[string]string
	GetMetricsPath() string
	UseTLS() bool
	ProbePath() string
	ProbeScheme() string
	ProbePort() string
	ProbeNeedLiveness() bool
	UseProxyProtocol() bool
}

// server defines MCP server with settings specific to its CRD
type server struct {
	cr   mcpCR
	spec *vmv1.CommonMCPSpec
	// kind is used as container name and in error messages
	kind string
	// envs contains env vars, which configure access to the queried instance and MCP features specific to the server kind
	envs []corev1.EnvVar
}

func createOrUpdate(ctx context.Context, rclient client.Client, s, prevS *server) error {
	cfg := config.MustGetBaseConfig()
	if s.spec.VPA != nil && !cfg.VPAAPIEnabled {
		return fmt.Errorf("spec.vpa is set but VM_VPA_API_ENABLED=true env var was not provided")
	}
	if prevS != nil {
		if err := deleteOrphaned(ctx, rclient, s); err != nil {
			return err
		}
	}
	owner := s.cr.AsOwner()
	if s.cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevS != nil {
			prevSA = build.ServiceAccount(prevS.cr)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(s.cr), prevSA, &owner); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}
	if err := createOrUpdateService(ctx, rclient, s, prevS); err != nil {
		return err
	}
	if s.spec.PodDisruptionBudget != nil {
		var prevPDB *policyv1.PodDisruptionBudget
		if prevS != nil && prevS.spec.PodDisruptionBudget != nil {
			prevPDB = build.PodDisruptionBudget(prevS.cr, prevS.spec.PodDisruptionBudget)
		}
		if err := reconcile.PDB(ctx, rclient, build.PodDisruptionBudget(s.cr, s.spec.PodDisruptionBudget), prevPDB, &owner); err != nil {
			return fmt.Errorf("cannot update pod disruption budget for %s: %w", s.kind, err)
		}
	}
	if err := createOrUpdateVPA(ctx, rclient, s, prevS); err != nil {
		return fmt.Errorf("cannot create or update vpa for %s: %w", s.kind, err)
	}
	if s.spec.NetworkPolicy != nil {
		var prevNP *networkingv1.NetworkPolicy
		if prevS != nil && prevS.spec.NetworkPolicy != nil {
			prevNP = build.NetworkPolicy(prevS.cr, prevS.spec.NetworkPolicy)
		}
		if err := reconcile.NetworkPolicy(ctx, rclient, build.NetworkPolicy(s.cr, s.spec.NetworkPolicy), prevNP, &owner); err != nil {
			return fmt.Errorf("cannot update network policy for %s: %w", s.kind, err)
		}
	}

	var prevDeploy *appsv1.Deployment
	if prevS != nil {
		var err error
		prevDeploy, err = newDeployment(prevS)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeploy, err := newDeployment(s)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for %s: %w", s.kind, err)
	}
	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, &owner, nil)
}

func createOrUpdateVPA(ctx context.Context, rclient client.Client, s, prevS *server) error {
	if s.spec.VPA == nil {
		return nil
	}
	targetRef := autoscalingv1.CrossVersionObjectReference{
		Name:       s.cr.PrefixedName(),
		Kind:       string(vmv1beta1.WorkloadKindDeployment),
		APIVersion: "apps/v1",
	}
	newVPA := build.VPA(s.cr, targetRef, s.spec.VPA)
	var prevVPA *vpav1.VerticalPodAutoscaler
	if prevS != nil && prevS.spec.VPA != nil {
		prevVPA = build.VPA(prevS.cr, targetRef, prevS.spec.VPA)
	}
	owner := s.cr.AsOwner()
	return reconcile.VPA(ctx, rclient, newVPA, prevVPA, &owner)
}

func newDeployment(s *server) (*appsv1.Deployment, error) {
	podSpec, err := newPodSpec(s)
	if err != nil {
		return nil, err
	}
	strategy := appsv1.DeploymentStrategy{
		Type: appsv1.RollingUpdateDeploymentStrategyType,
	}
	if s.spec.UpdateStrategy != nil {
		strategy.Type = *s.spec.UpdateStrategy
	}
	if strategy.Type == appsv1.RollingUpdateDeploymentStrategyType {
		strategy.RollingUpdate = s.spec.RollingUpdate
	}
	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            s.cr.PrefixedName(),
			Namespace:       s.cr.GetNamespace(),
			Labels:          s.cr.FinalLabels(),
			Annotations:     s.cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{s.cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: s.cr.SelectorLabels(),
			},
			Strategy: strategy,
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, &s.spec.CommonAppsParams)
	return depSpec, nil
}

func newPodSpec(s *server) (*corev1.PodTemplateSpec, error) {
	spec := s.spec
	var volumes []corev1.Volume
	var vmMounts []corev1.VolumeMount
	volumes = append(volumes, spec.Volumes...)
	vmMounts = append(vmMounts, spec.VolumeMounts...)

	for _, sn := range spec.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + sn),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: sn,
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("secret-" + sn),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.SecretsDir, sn),
		})
	}

	for _, c := range spec.ConfigMaps {
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
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}

	container := corev1.Container{
		Name:  s.kind,
		Image: spec.Image.Reference(),
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				Protocol:      corev1.ProtocolTCP,
				ContainerPort: intstr.Parse(spec.Port).IntVal,
			},
		},
		VolumeMounts:             vmMounts,
		Resources:                spec.Resources,
		Env:                      buildEnvs(s),
		EnvFrom:                  spec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          spec.Image.PullPolicy,
	}
	build.Probe(&container, s.cr, &spec.CommonAppsParams)
	build.Lifecycle(&container, &spec.CommonAppsParams)

	operatorContainers := []corev1.Container{container}
	build.AddStrictSecuritySettingsToContainers(operatorContainers, &spec.CommonAppsParams)
	containers, err := k8stools.MergePatchContainers(operatorContainers, spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch containers: %w", err)
	}

	for i := range spec.TopologySpreadConstraints {
		if spec.TopologySpreadConstraints[i].LabelSelector == nil {
			spec.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: s.cr.SelectorLabels(),
			}
		}
	}

	return &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      s.cr.PodLabels(),
			Annotations: s.cr.PodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     spec.InitContainers,
			Containers:         containers,
			ServiceAccountName: s.cr.GetServiceAccountName(),
		},
	}, nil
}

// buildEnvs returns env vars of the MCP server container.
// Env vars defined at spec.extraEnvs override env vars with the same names set by operator.
func buildEnvs(s *server) []corev1.EnvVar {
	spec := s.spec
	serverMode := spec.ServerMode
	if serverMode == "" {
		serverMode = vmv1.MCPServerModeHTTP
	}
	envs := []corev1.EnvVar{
		{Name: "MCP_LISTEN_ADDR", Value: fmt.Sprintf(":%s", spec.Port)},
		{Name: "MCP_SERVER_MODE", Value: string(serverMode)},
	}
	if len(spec.DisabledTools) > 0 {
		envs = append(envs, corev1.EnvVar{Name: "MCP_DISABLED_TOOLS", Value: strings.Join(spec.DisabledTools, ",")})
	}
	if spec.HeartbeatInterval != "" {
		envs = append(envs, corev1.EnvVar{Name: "MCP_HEARTBEAT_INTERVAL", Value: spec.HeartbeatInterval})
	}
	if spec.LogLevel != "" {
		envs = append(envs, corev1.EnvVar{Name: "MCP_LOG_LEVEL", Value: spec.LogLevel})
	}
	if spec.LogFormat != "" {
		envs = append(envs, corev1.EnvVar{Name: "MCP_LOG_FORMAT", Value: spec.LogFormat})
	}
	envs = append(envs, s.envs...)

	overridden := sets.New[string]()
	for _, e := range spec.ExtraEnvs {
		overridden.Insert(e.Name)
	}
	envs = filterEnvs(envs, func(e corev1.EnvVar) bool {
		return !overridden.Has(e.Name)
	})
	sort.Slice(envs, func(i, j int) bool {
		return envs[i].Name < envs[j].Name
	})
	return append(envs, spec.ExtraEnvs...)
}

func filterEnvs(envs []corev1.EnvVar, keep func(corev1.EnvVar) bool) []corev1.EnvVar {
	dst := envs[:0]
	for _, e := range envs {
		if keep(e) {
			dst = append(dst, e)
		}
	}
	return dst
}

// instanceEnvs returns env vars, which configure access to the instance queried by the MCP server.
// The prefix depends on the queried instance, e.g. VM for VMMCP.
func instanceEnvs(prefix, entrypoint string, bearerToken *corev1.SecretKeySelector, headers []string, defaultTenantID string) []corev1.EnvVar {
	var envs []corev1.EnvVar
	if entrypoint != "" {
		envs = append(envs, corev1.EnvVar{Name: prefix + "_INSTANCE_ENTRYPOINT", Value: entrypoint})
	}
	if bearerToken != nil {
		envs = append(envs, corev1.EnvVar{
			Name: prefix + "_INSTANCE_BEARER_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: bearerToken,
			},
		})
	}
	if len(headers) > 0 {
		envs = append(envs, corev1.EnvVar{Name: prefix + "_INSTANCE_HEADERS", Value: strings.Join(headers, ",")})
	}
	if defaultTenantID != "" {
		envs = append(envs, corev1.EnvVar{Name: prefix + "_DEFAULT_TENANT_ID", Value: defaultTenantID})
	}
	return envs
}

func passthroughHeadersEnvs(headers []string) []corev1.EnvVar {
	if len(headers) == 0 {
		return nil
	}
	return []corev1.EnvVar{{Name: "MCP_PASSTHROUGH_HEADERS", Value: strings.Join(headers, ",")}}
}

func buildScrape(s *server, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if s == nil || svc == nil || ptr.Deref(s.spec.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, s.cr)
}

func createOrUpdateService(ctx context.Context, rclient client.Client, s, prevS *server) error {
	owner := s.cr.AsOwner()
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevS != nil {
		prevSvc = build.Service(prevS.cr, prevS.spec.Port, nil)
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevS.spec.ServiceSpec)
	}
	svc := build.Service(s.cr, s.spec.Port, nil)
	if err := s.spec.ServiceSpec.IsSomeAndThen(func(ss *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(svc, ss)
		if additionalService.Name == svc.Name {
			return fmt.Errorf("%s additional service name: %q cannot be the same as crd.prefixedname: %q", s.kind, additionalService.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile additional service for %s: %w", s.kind, err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile service for %s: %w", s.kind, err)
	}
	if !ptr.Deref(s.spec.DisableSelfServiceScrape, false) {
		svs := buildScrape(s, svc)
		prevSvs := buildScrape(prevS, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner, false); err != nil {
			return fmt.Errorf("cannot create serviceScrape for %s: %w", s.kind, err)
		}
	}
	return nil
}

func deleteOrphaned(ctx context.Context, rclient client.Client, s *server) error {
	svcName := s.cr.PrefixedName()
	keepServices := sets.New(svcName)
	keepServicesScrapes := sets.New[string]()
	if !ptr.Deref(s.spec.DisableSelfServiceScrape, false) {
		keepServicesScrapes.Insert(svcName)
	}
	if s.spec.ServiceSpec != nil && !s.spec.ServiceSpec.UseAsDefault {
		extraSvcName := s.spec.ServiceSpec.NameOrDefault(svcName)
		keepServices.Insert(extraSvcName)
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, s.cr, keepServices, true); err != nil {
		return fmt.Errorf("cannot remove services: %w", err)
	}
	if err := finalize.RemoveOrphanedVMServiceScrapes(ctx, rclient, s.cr, keepServicesScrapes, true); err != nil {
		return fmt.Errorf("cannot remove serviceScrapes: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: s.cr.PrefixedName(), Namespace: s.cr.GetNamespace()}
	var objsToRemove []client.Object
	if s.spec.PodDisruptionBudget == nil {
		objsToRemove = append(objsToRemove, &policyv1.PodDisruptionBudget{ObjectMeta: objMeta})
	}
	if config.MustGetBaseConfig().VPAAPIEnabled && s.spec.VPA == nil {
		objsToRemove = append(objsToRemove, &vpav1.VerticalPodAutoscaler{ObjectMeta: objMeta})
	}
	if s.spec.NetworkPolicy == nil {
		objsToRemove = append(objsToRemove, &networkingv1.NetworkPolicy{ObjectMeta: objMeta})
	}
	if !s.cr.IsOwnsServiceAccount() {
		objsToRemove = append(objsToRemove, &corev1.ServiceAccount{ObjectMeta: objMeta})
	}
	return finalize.SafeDeleteWithFinalizer(ctx, rclient, objsToRemove, s.cr)
}
