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

func createOrUpdateVLStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLStorage == nil {
		return nil
	}
	if cr.Spec.VLStorage.PodDisruptionBudget != nil {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
		pdb := build.PodDisruptionBudget(b, cr.Spec.VLStorage.PodDisruptionBudget)
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.VLStorage.PodDisruptionBudget != nil {
			b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
			prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.VLStorage.PodDisruptionBudget)
		}
		owner := cr.AsOwner()
		err := reconcile.PDB(ctx, rclient, pdb, prevPDB, &owner)
		if err != nil {
			return err
		}
	}
	if err := createOrUpdateVLStorageHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVLStorageVPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVLStorageSTS(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVLStorageService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return nil
}

func buildVLStorageScrape(cr *vmv1.VLCluster, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.VLStorage == nil || ptr.Deref(cr.Spec.VLStorage.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr.Spec.VLStorage)
}

func createOrUpdateVLStorageService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
	svc := build.Service(b, cr.Spec.VLStorage.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.PublishNotReadyAddresses = true
	})
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil && prevCR.Spec.VLStorage != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
		prevSvc = build.Service(b, prevCR.Spec.VLStorage.Port, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			svc.Spec.PublishNotReadyAddresses = true
		})
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.VLStorage.ServiceSpec)

	}
	owner := cr.AsOwner()
	if err := cr.Spec.VLStorage.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("VLStorage additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile storage additional service: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile storage service: %w", err)
	}
	if !ptr.Deref(cr.Spec.VLStorage.DisableSelfServiceScrape, false) {
		svs := buildVLStorageScrape(cr, svc)
		prevSvs := buildVLStorageScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs, prevSvs, &owner); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VLStorage: %w", err)
		}
	}
	return nil
}

func createOrUpdateVLStorageHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	hpa := cr.Spec.VLStorage.HPA
	if hpa == nil {
		return nil
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       b.PrefixedName(),
		Kind:       "StatefulSet",
		APIVersion: "apps/v1",
	}
	defaultHPA := build.HPA(b, targetRef, hpa)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VLStorage.HPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.VLStorage.HPA)
	}

	owner := cr.AsOwner()
	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA, &owner)
}

func createOrUpdateVLStorageVPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	vpa := cr.Spec.VLStorage.VPA
	if vpa == nil {
		return nil
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
	targetRef := autoscalingv1.CrossVersionObjectReference{
		Name:       b.PrefixedName(),
		Kind:       "StatefulSet",
		APIVersion: "apps/v1",
	}
	newVPA := build.VPA(b, targetRef, vpa)
	var prevVPA *vpav1.VerticalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VLStorage.VPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
		prevVPA = build.VPA(b, targetRef, prevCR.Spec.VLStorage.VPA)
	}
	owner := cr.AsOwner()
	return reconcile.VPA(ctx, rclient, newVPA, prevVPA, &owner)
}

func createOrUpdateVLStorageSTS(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	var prevSts *appsv1.StatefulSet

	if prevCR != nil && prevCR.Spec.VLStorage != nil {
		var err error
		prevSts, err = buildVLStorageSTSSpec(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev storage spec: %w", err)
		}
	}
	newSts, err := buildVLStorageSTSSpec(cr)
	if err != nil {
		return err
	}

	stsOpts := reconcile.STSOptions{
		HasClaim: len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: func() map[string]string {
			return cr.SelectorLabels(vmv1beta1.ClusterComponentStorage)
		},
		UpdateBehavior: cr.Spec.VLStorage.RollingUpdateStrategyBehavior,
	}
	owner := cr.AsOwner()
	return reconcile.StatefulSet(ctx, rclient, stsOpts, newSts, prevSts, &owner)
}

func buildVLStorageSTSSpec(cr *vmv1.VLCluster) (*appsv1.StatefulSet, error) {

	podSpec, err := buildVLStoragePodSpec(cr)
	if err != nil {
		return nil, err
	}

	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentStorage),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentStorage),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentStorage),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VLStorage.RollingUpdateStrategy,
			},
			Template:    *podSpec,
			ServiceName: cr.PrefixedName(vmv1beta1.ClusterComponentStorage),
		},
	}
	if cr.Spec.VLStorage.PersistentVolumeClaimRetentionPolicy != nil {
		stsSpec.Spec.PersistentVolumeClaimRetentionPolicy = cr.Spec.VLStorage.PersistentVolumeClaimRetentionPolicy
	}
	build.StatefulSetAddCommonParams(stsSpec, ptr.Deref(cr.Spec.VLStorage.UseStrictSecurity, false), &cr.Spec.VLStorage.CommonApplicationDeploymentParams)
	storageSpec := cr.Spec.VLStorage.Storage
	storageSpec.IntoSTSVolume(cr.Spec.VLStorage.GetStorageVolumeName(), &stsSpec.Spec)
	stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.VLStorage.ClaimTemplates...)

	return stsSpec, nil
}

func buildVLStoragePodSpec(cr *vmv1.VLCluster) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VLStorage.Port),
		fmt.Sprintf("-storageDataPath=%s", cr.Spec.VLStorage.StorageDataPath),
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if cr.Spec.VLStorage.RetentionPeriod != "" {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", cr.Spec.VLStorage.RetentionPeriod))
	}
	if cr.Spec.VLStorage.FutureRetention != "" {
		args = append(args, fmt.Sprintf("-futureRetention=%s", cr.Spec.VLStorage.FutureRetention))
	}
	if cr.Spec.VLStorage.RetentionMaxDiskSpaceUsageBytes != "" {
		args = append(args, fmt.Sprintf("-retention.maxDiskSpaceUsageBytes=%s", cr.Spec.VLStorage.RetentionMaxDiskSpaceUsageBytes))
	}
	if cr.Spec.VLStorage.LogNewStreams {
		args = append(args, "-logNewStreams")
	}
	if cr.Spec.VLStorage.LogIngestedRows {
		args = append(args, "-logIngestedRows")
	}

	if cr.Spec.VLStorage.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VLStorage.LogLevel))
	}
	if cr.Spec.VLStorage.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VLStorage.LogFormat))
	}

	if len(cr.Spec.VLStorage.ExtraEnvs) > 0 || len(cr.Spec.VLStorage.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.VLStorage.ExtraEnvs...)

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.VLStorage.Port).IntVal,
		},
	}
	volumes := make([]corev1.Volume, 0)
	vmMounts := make([]corev1.VolumeMount, 0)

	volumes = append(volumes, cr.Spec.VLStorage.Volumes...)
	vmMounts = append(vmMounts, corev1.VolumeMount{
		Name:      cr.Spec.VLStorage.GetStorageVolumeName(),
		MountPath: cr.Spec.VLStorage.StorageDataPath,
	})

	vmMounts = append(vmMounts, cr.Spec.VLStorage.VolumeMounts...)

	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

	for _, s := range cr.Spec.VLStorage.Secrets {
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

	for _, c := range cr.Spec.VLStorage.ConfigMaps {
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

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VLStorage.ExtraArgs, "-")
	sort.Strings(args)
	vmstorageContainer := corev1.Container{
		Name:                     "vlstorage",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VLStorage.Image.Repository, cr.Spec.VLStorage.Image.Tag),
		ImagePullPolicy:          cr.Spec.VLStorage.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.VLStorage.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.VLStorage.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	vmstorageContainer = build.Probe(vmstorageContainer, cr.Spec.VLStorage)

	storageContainers := []corev1.Container{vmstorageContainer}
	var initContainers []corev1.Container

	useStrictSecurity := ptr.Deref(cr.Spec.VLStorage.UseStrictSecurity, false)
	build.AddStrictSecuritySettingsToContainers(cr.Spec.VLStorage.SecurityContext, initContainers, useStrictSecurity)
	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.VLStorage.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch storage init containers: %w", err)
	}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VLStorage.SecurityContext, storageContainers, useStrictSecurity)
	containers, err := k8stools.MergePatchContainers(storageContainers, cr.Spec.VLStorage.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch storage containers: %w", err)
	}

	for i := range cr.Spec.VLStorage.TopologySpreadConstraints {
		if cr.Spec.VLStorage.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VLStorage.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentStorage),
			}
		}
	}

	vmStoragePodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(vmv1beta1.ClusterComponentStorage),
			Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentStorage),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     ic,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}

	return vmStoragePodSpec, nil
}
