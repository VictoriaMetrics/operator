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

func createOrUpdateVTSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Select == nil {
		return nil
	}
	if cr.Spec.Select.PodDisruptionBudget != nil {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
		pdb := build.PodDisruptionBudget(b, cr.Spec.Select.PodDisruptionBudget)
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.Select.PodDisruptionBudget != nil {
			b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)
			prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.Select.PodDisruptionBudget)
		}
		owner := cr.AsOwner()
		err := reconcile.PDB(ctx, rclient, pdb, prevPDB, &owner)
		if err != nil {
			return err
		}
	}
	if err := createOrUpdateVTSelectHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVTSelectVPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVTSelectService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVTSelectDeployment(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return nil
}

func createOrUpdateVTSelectHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Select.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.PrefixedName(vmv1beta1.ClusterComponentSelect),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	defaultHPA := build.HPA(b, targetRef, cr.Spec.Select.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.Select.HPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.Select.HPA)
	}
	owner := cr.AsOwner()
	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA, &owner)
}

func createOrUpdateVTSelectVPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Select.VPA == nil {
		return nil
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	targetRef := autoscalingv1.CrossVersionObjectReference{
		Name:       b.PrefixedName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	newVPA := build.VPA(b, targetRef, cr.Spec.Select.VPA)
	var prevVPA *vpav1.VerticalPodAutoscaler
	if prevCR != nil && prevCR.Spec.Select != nil && prevCR.Spec.Select.VPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)
		prevVPA = build.VPA(b, targetRef, prevCR.Spec.Select.VPA)
	}
	owner := cr.AsOwner()
	return reconcile.VPA(ctx, rclient, newVPA, prevVPA, &owner)
}

func buildVTSelectScrape(cr *vmv1.VTCluster, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.Select == nil || ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, false) {
		return nil
	}
	svs := build.VMServiceScrape(svc, cr.Spec.Select)
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		svs.Spec.JobLabel = vmv1beta1.VMAuthLBServiceProxyJobNameLabel
	}
	return svs
}

func createOrUpdateVTSelectService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil && prevCR.Spec.Select != nil {
		prevSvc = buildVTSelectService(prevCR)
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.Select.ServiceSpec)
	}
	svc := buildVTSelectService(cr)
	owner := cr.AsOwner()
	if err := cr.Spec.Select.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("VTSelect additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile service for select: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile select service: %w", err)
	}
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		var prevPort string
		if prevCR != nil && prevCR.Spec.Select != nil {
			prevPort = prevCR.Spec.Select.Port
		}
		kind := vmv1beta1.ClusterComponentSelect
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, kind, cr.Spec.Select.Port, prevPort); err != nil {
			return fmt.Errorf("cannot create lb svc for select: %w", err)
		}
	}
	if !ptr.Deref(cr.Spec.Select.DisableSelfServiceScrape, false) {
		svs := buildVTSelectScrape(cr, svc)
		prevSvs := buildVTSelectScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner, false); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VTSelect: %w", err)
		}
	}
	return nil
}

func buildVTSelectService(cr *vmv1.VTCluster) *corev1.Service {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	svc := build.Service(b, cr.Spec.Select.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.PublishNotReadyAddresses = true
	})
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		svc.Name = cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect)
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmv1beta1.VMAuthLBServiceProxyJobNameLabel] = cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
	}
	return svc
}

func createOrUpdateVTSelectDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	var prevDep *appsv1.Deployment
	if prevCR != nil && prevCR.Spec.Select != nil {
		var err error
		prevDep, err = buildVTSelectDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev select spec: %w", err)
		}
	}
	newDep, err := buildVTSelectDeployment(cr)
	if err != nil {
		return err
	}
	owner := cr.AsOwner()
	return reconcile.Deployment(ctx, rclient, newDep, prevDep, cr.Spec.Select.HPA != nil, &owner)
}

func buildVTSelectDeployment(cr *vmv1.VTCluster) (*appsv1.Deployment, error) {
	podSpec, err := buildVTSelectPodSpec(cr)
	if err != nil {
		return nil, err
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.Select.UpdateStrategy != nil {
		strategyType = *cr.Spec.Select.UpdateStrategy
	}
	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentSelect),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentSelect),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.Select.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentSelect),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, ptr.Deref(cr.Spec.Select.UseStrictSecurity, false), &cr.Spec.Select.CommonApplicationDeploymentParams)
	return depSpec, nil
}

func buildVTSelectPodSpec(cr *vmv1.VTCluster) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Select.Port),
		"-internalinsert.disable=true",
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if cr.Spec.Select.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.Select.LogLevel))
	}
	if cr.Spec.Select.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.Select.LogFormat))
	}

	if cr.Spec.Storage != nil && cr.Spec.Storage.ReplicaCount != nil {
		// TODO: check TLS
		storageNodeFlag := build.NewFlag("-storageNode", "")
		storageNodeIds := cr.AvailableStorageNodeIDs("select")
		for idx, i := range storageNodeIds {
			storageNodeFlag.Add(build.PodDNSAddress(cr.PrefixedName(vmv1beta1.ClusterComponentStorage), i, cr.Namespace, cr.Spec.Storage.Port, cr.Spec.ClusterDomainName), idx)
		}
		if len(cr.Spec.Select.ExtraStorageNodes) > 0 {
			for i, node := range cr.Spec.Select.ExtraStorageNodes {
				idx := i + len(storageNodeIds)
				storageNodeFlag.Add(node.Addr, idx)
			}
		}
		totalNodes := len(cr.Spec.Select.ExtraStorageNodes) + len(storageNodeIds)
		args = build.AppendFlagsToArgs(args, totalNodes, storageNodeFlag)
	}

	if len(cr.Spec.Select.ExtraEnvs) > 0 || len(cr.Spec.Select.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.Select.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{
		Name:          "http",
		Protocol:      "TCP",
		ContainerPort: intstr.Parse(cr.Spec.Select.Port).IntVal,
	})

	volumes := make([]corev1.Volume, 0)
	volumes = append(volumes, cr.Spec.Select.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)
	vmMounts = append(vmMounts, cr.Spec.Select.VolumeMounts...)

	for _, s := range cr.Spec.Select.Secrets {
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

	for _, c := range cr.Spec.Select.ConfigMaps {
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

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.Select.ExtraArgs, "-")
	sort.Strings(args)
	selectContainers := corev1.Container{
		Name:                     "vtselect",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Select.Image.Repository, cr.Spec.Select.Image.Tag),
		ImagePullPolicy:          cr.Spec.Select.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.Select.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.Select.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	selectContainers = build.Probe(selectContainers, cr.Spec.Select)
	operatorContainers := []corev1.Container{selectContainers}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.Select.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.Select.UseStrictSecurity, false))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Select.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.Select.TopologySpreadConstraints {
		if cr.Spec.Select.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.Select.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentSelect),
			}
		}
	}

	podSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(vmv1beta1.ClusterComponentSelect),
			Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentSelect),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.Select.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
			RestartPolicy:      "Always",
		},
	}

	return podSpec, nil
}
