package vtcluster

import (
	"context"
	"fmt"
	"path"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func createOrUpdateVTInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Insert == nil {
		return nil
	}

	if err := createOrUpdatePodDisruptionBudgetForVTInsert(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVTInsertDeployment(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVTInsertService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVTInsertHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return createOrUpdateVTInsertVPA(ctx, rclient, cr, prevCR)
}

func createOrUpdatePodDisruptionBudgetForVTInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Insert.PodDisruptionBudget == nil {
		return nil
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	pdb := build.PodDisruptionBudget(b, cr.Spec.Insert.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.Insert.PodDisruptionBudget != nil {
		b := build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentInsert)
		prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.Insert.PodDisruptionBudget)
	}
	owner := cr.AsOwner()
	return reconcile.PDB(ctx, rclient, pdb, prevPDB, &owner)
}

func createOrUpdateVTInsertDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	var prevDeploy *appsv1.Deployment
	if prevCR != nil && prevCR.Spec.Insert != nil {
		var err error
		prevDeploy, err = buildVTInsertDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeployment, err := buildVTInsertDeployment(cr)
	if err != nil {
		return err
	}
	owner := cr.AsOwner()
	return reconcile.Deployment(ctx, rclient, newDeployment, prevDeploy, cr.Spec.Insert.HPA != nil, &owner)
}

func buildVTInsertDeployment(cr *vmv1.VTCluster) (*appsv1.Deployment, error) {
	podSpec, err := buildVTInsertPodSpec(cr)
	if err != nil {
		return nil, err
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.Insert.UpdateStrategy != nil {
		strategyType = *cr.Spec.Insert.UpdateStrategy
	}
	stsSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentInsert),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentInsert),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:             cr.Spec.Insert.ReplicaCount,
			RevisionHistoryLimit: cr.Spec.Insert.RevisionHistoryLimitCount,
			MinReadySeconds:      cr.Spec.Insert.MinReadySeconds,
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.Insert.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentInsert),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(stsSpec, ptr.Deref(cr.Spec.Insert.UseStrictSecurity, false), &cr.Spec.Insert.CommonApplicationDeploymentParams)
	return stsSpec, nil
}

func buildVTInsertPodSpec(cr *vmv1.VTCluster) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Insert.Port),
		"-internalselect.disable=true",
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if cr.Spec.Insert.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.Insert.LogLevel))
	}
	if cr.Spec.Insert.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.Insert.LogFormat))
	}

	if cr.Spec.Storage != nil && cr.Spec.Storage.ReplicaCount != nil {
		// TODO: check TLS
		storageNodeFlag := build.NewFlag("-storageNode", "")
		storageNodeIds := cr.AvailableStorageNodeIDs("insert")
		for idx, i := range storageNodeIds {
			storageNodeFlag.Add(build.PodDNSAddress(cr.PrefixedName(vmv1beta1.ClusterComponentStorage), i, cr.Namespace, cr.Spec.Storage.Port, cr.Spec.ClusterDomainName), idx)
		}
		totalNodes := len(storageNodeIds)
		args = build.AppendFlagsToArgs(args, totalNodes, storageNodeFlag)
	}

	if len(cr.Spec.Insert.ExtraEnvs) > 0 || len(cr.Spec.Insert.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.Insert.ExtraEnvs...)

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.Insert.Port).IntVal,
		},
	}

	volumes := make([]corev1.Volume, 0)
	volumes = append(volumes, cr.Spec.Insert.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)
	vmMounts = append(vmMounts, cr.Spec.Insert.VolumeMounts...)

	for _, s := range cr.Spec.Insert.Secrets {
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
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.Insert.ConfigMaps {
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

	args = build.AddHTTPShutdownDelayArg(args, cr.Spec.Insert.ExtraArgs, cr.Spec.Insert.EmbeddedProbes)
	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.Insert.ExtraArgs, "-")
	sort.Strings(args)

	insertContainers := corev1.Container{
		Name:                     "vtinsert",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Insert.Image.Repository, cr.Spec.Insert.Image.Tag),
		ImagePullPolicy:          cr.Spec.Insert.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.Insert.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.Insert.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	insertContainers = build.Probe(insertContainers, cr.Spec.Insert)
	operatorContainers := []corev1.Container{insertContainers}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.Insert.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.Insert.UseStrictSecurity, false))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Insert.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.Insert.TopologySpreadConstraints {
		if cr.Spec.Insert.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.Insert.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentInsert),
			}
		}
	}

	podSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(vmv1beta1.ClusterComponentInsert),
			Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentInsert),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.Insert.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}

	return podSpec, nil
}

func createOrUpdateVTInsertHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Insert.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.PrefixedName(vmv1beta1.ClusterComponentInsert),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	newHPA := build.HPA(b, targetRef, cr.Spec.Insert.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.Insert.HPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentInsert)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.Insert.HPA)
	}
	owner := cr.AsOwner()
	return reconcile.HPA(ctx, rclient, newHPA, prevHPA, &owner)
}

func createOrUpdateVTInsertVPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Insert.VPA == nil {
		return nil
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	targetRef := autoscalingv1.CrossVersionObjectReference{
		Name:       b.PrefixedName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	newVPA := build.VPA(b, targetRef, cr.Spec.Insert.VPA)
	var prevVPA *vpav1.VerticalPodAutoscaler
	if prevCR != nil && prevCR.Spec.Insert != nil && prevCR.Spec.Insert.VPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentInsert)
		prevVPA = build.VPA(b, targetRef, prevCR.Spec.Insert.VPA)
	}
	owner := cr.AsOwner()
	return reconcile.VPA(ctx, rclient, newVPA, prevVPA, &owner)
}

func buildVTInsertScrape(cr *vmv1.VTCluster, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.Insert == nil || ptr.Deref(cr.Spec.Insert.DisableSelfServiceScrape, false) {
		return nil
	}
	svs := build.VMServiceScrape(svc, cr.Spec.Insert)
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		svs.Spec.JobLabel = vmv1beta1.VMAuthLBServiceProxyJobNameLabel
	}
	return svs
}

func createOrUpdateVTInsertService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil && prevCR.Spec.Insert != nil {
		prevSvc = buildVTInsertService(prevCR)
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.Insert.ServiceSpec)
	}
	svc := buildVTInsertService(cr)
	owner := cr.AsOwner()
	if err := cr.Spec.Insert.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("VTInsert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile insert additional service: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile insert service: %w", err)
	}

	// create extra service for loadbalancing
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		var prevPort string
		if prevCR != nil && prevCR.Spec.Insert != nil {
			prevPort = prevCR.Spec.Insert.Port
		}
		kind := vmv1beta1.ClusterComponentInsert
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, kind, cr.Spec.Insert.Port, prevPort); err != nil {
			return fmt.Errorf("cannot create lb svc for insert: %w", err)
		}
	}
	if !ptr.Deref(cr.Spec.Insert.DisableSelfServiceScrape, false) {
		svs := buildVTInsertScrape(cr, svc)
		prevSvs := buildVTInsertScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VTInsert: %w", err)
		}
	}
	return nil
}

func buildVTInsertService(cr *vmv1.VTCluster) *corev1.Service {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	svc := build.Service(b, cr.Spec.Insert.Port, nil)
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		svc.Name = cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert)
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmv1beta1.VMAuthLBServiceProxyJobNameLabel] = cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
	}
	return svc
}
