package vlcluster

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

func createOrUpdateVLSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLSelect == nil {
		return nil
	}
	if cr.Spec.VLSelect.PodDisruptionBudget != nil {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
		pdb := build.PodDisruptionBudget(b, cr.Spec.VLSelect.PodDisruptionBudget)
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.VLSelect.PodDisruptionBudget != nil {
			b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)
			prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.VLSelect.PodDisruptionBudget)
		}
		owner := cr.AsOwner()
		err := reconcile.PDB(ctx, rclient, pdb, prevPDB, &owner)
		if err != nil {
			return err
		}
	}
	if err := createOrUpdateVLSelectHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVLSelectVPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVLSelectService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVLSelectDeployment(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return nil
}

func createOrUpdateVLSelectHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLSelect.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.PrefixedName(vmv1beta1.ClusterComponentSelect),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	defaultHPA := build.HPA(b, targetRef, cr.Spec.VLSelect.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VLSelect.HPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.VLSelect.HPA)
	}
	owner := cr.AsOwner()
	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA, &owner)
}

func createOrUpdateVLSelectVPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLSelect.VPA == nil {
		return nil
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	targetRef := autoscalingv1.CrossVersionObjectReference{
		Name:       b.PrefixedName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	newVPA := build.VPA(b, targetRef, cr.Spec.VLSelect.VPA)
	var prevVPA *vpav1.VerticalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VLSelect != nil && prevCR.Spec.VLSelect.VPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)
		prevVPA = build.VPA(b, targetRef, prevCR.Spec.VLSelect.VPA)
	}
	owner := cr.AsOwner()
	return reconcile.VPA(ctx, rclient, newVPA, prevVPA, &owner)
}

func buildVLSelectScrape(cr *vmv1.VLCluster, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.VLSelect == nil || ptr.Deref(cr.Spec.VLSelect.DisableSelfServiceScrape, false) {
		return nil
	}
	svs := build.VMServiceScrape(svc, cr.Spec.VLSelect)
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		svs.Spec.JobLabel = vmv1beta1.VMAuthLBServiceProxyJobNameLabel
	}
	return svs
}

func createOrUpdateVLSelectService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil && prevCR.Spec.VLSelect != nil {
		prevSvc = buildVLSelectService(prevCR)
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.VLSelect.ServiceSpec)
	}
	svc := buildVLSelectService(cr)
	owner := cr.AsOwner()
	if err := cr.Spec.VLSelect.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("VLSelect additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
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
		if prevCR != nil && prevCR.Spec.VLSelect != nil {
			prevPort = prevCR.Spec.VLSelect.Port
		}
		kind := vmv1beta1.ClusterComponentSelect
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, kind, cr.Spec.VLSelect.Port, prevPort); err != nil {
			return fmt.Errorf("cannot create lb svc for select: %w", err)
		}
	}
	if !ptr.Deref(cr.Spec.VLSelect.DisableSelfServiceScrape, false) {
		svs := buildVLSelectScrape(cr, svc)
		prevSvs := buildVLSelectScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VLSelect: %w", err)
		}
	}
	return nil
}

func buildVLSelectService(cr *vmv1.VLCluster) *corev1.Service {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	svc := build.Service(b, cr.Spec.VLSelect.Port, func(svc *corev1.Service) {
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

func createOrUpdateVLSelectDeployment(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	var prevDep *appsv1.Deployment
	if prevCR != nil && prevCR.Spec.VLSelect != nil {
		var err error
		prevDep, err = buildVLSelectDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev select spec: %w", err)
		}
	}
	newDep, err := buildVLSelectDeployment(cr)
	if err != nil {
		return err
	}
	owner := cr.AsOwner()
	return reconcile.Deployment(ctx, rclient, newDep, prevDep, cr.Spec.VLSelect.HPA != nil, &owner)
}

func buildVLSelectDeployment(cr *vmv1.VLCluster) (*appsv1.Deployment, error) {
	podSpec, err := buildVLSelectPodSpec(cr)
	if err != nil {
		return nil, err
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.VLSelect.UpdateStrategy != nil {
		strategyType = *cr.Spec.VLSelect.UpdateStrategy
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
				RollingUpdate: cr.Spec.VLSelect.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentSelect),
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, ptr.Deref(cr.Spec.VLSelect.UseStrictSecurity, false), &cr.Spec.VLSelect.CommonApplicationDeploymentParams)
	return depSpec, nil
}

func buildVLSelectPodSpec(cr *vmv1.VLCluster) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VLSelect.Port),
		"-internalinsert.disable=true",
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if cr.Spec.VLSelect.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VLSelect.LogLevel))
	}
	if cr.Spec.VLSelect.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VLSelect.LogFormat))
	}

	if cr.Spec.VLStorage != nil && cr.Spec.VLStorage.ReplicaCount != nil {
		// TODO: check TLS
		storageNodeFlag := build.NewFlag("-storageNode", "")
		storageNodeIds := cr.AvailableStorageNodeIDs("select")
		for idx, i := range storageNodeIds {
			storageNodeFlag.Add(build.PodDNSAddress(cr.PrefixedName(vmv1beta1.ClusterComponentStorage), i, cr.Namespace, cr.Spec.VLStorage.Port, cr.Spec.ClusterDomainName), idx)
		}
		if len(cr.Spec.VLSelect.ExtraStorageNodes) > 0 {
			for i, node := range cr.Spec.VLSelect.ExtraStorageNodes {
				idx := i + len(storageNodeIds)
				storageNodeFlag.Add(node.Addr, idx)
			}
		}
		totalNodes := len(cr.Spec.VLSelect.ExtraStorageNodes) + len(storageNodeIds)
		args = build.AppendFlagsToArgs(args, totalNodes, storageNodeFlag)
	}

	if len(cr.Spec.VLSelect.ExtraEnvs) > 0 || len(cr.Spec.VLSelect.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.VLSelect.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{
		Name:          "http",
		Protocol:      "TCP",
		ContainerPort: intstr.Parse(cr.Spec.VLSelect.Port).IntVal,
	})

	volumes := make([]corev1.Volume, 0)
	volumes = append(volumes, cr.Spec.VLSelect.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)
	vmMounts = append(vmMounts, cr.Spec.VLSelect.VolumeMounts...)

	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

	for _, s := range cr.Spec.VLSelect.Secrets {
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

	for _, c := range cr.Spec.VLSelect.ConfigMaps {
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

	args = build.AddHTTPShutdownDelayArg(args, cr.Spec.VLSelect.ExtraArgs, cr.Spec.VLSelect.EmbeddedProbes)
	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VLSelect.ExtraArgs, "-")
	sort.Strings(args)
	selectContainers := corev1.Container{
		Name:                     "vlselect",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VLSelect.Image.Repository, cr.Spec.VLSelect.Image.Tag),
		ImagePullPolicy:          cr.Spec.VLSelect.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.VLSelect.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.VLSelect.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	selectContainers = build.Probe(selectContainers, cr.Spec.VLSelect)
	operatorContainers := []corev1.Container{selectContainers}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VLSelect.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.VLSelect.UseStrictSecurity, false))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VLSelect.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.VLSelect.TopologySpreadConstraints {
		if cr.Spec.VLSelect.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VLSelect.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
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
			InitContainers:     cr.Spec.VLSelect.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
			RestartPolicy:      "Always",
		},
	}

	return podSpec, nil
}
