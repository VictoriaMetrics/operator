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

func createOrUpdateVTStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Storage == nil {
		return nil
	}

	if cr.Spec.Storage.PodDisruptionBudget != nil {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
		pdb := build.PodDisruptionBudget(b, cr.Spec.Storage.PodDisruptionBudget)
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.Storage.PodDisruptionBudget != nil {
			b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
			prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.Storage.PodDisruptionBudget)
		}
		owner := cr.AsOwner()
		err := reconcile.PDB(ctx, rclient, pdb, prevPDB, &owner)
		if err != nil {
			return err
		}
	}
	if err := createOrUpdateVTStorageHPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVTStorageVPA(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if err := createOrUpdateVTStorageSTS(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return createOrUpdateVTStorageService(ctx, rclient, cr, prevCR)
}

func buildVTStorageScrape(cr *vmv1.VTCluster, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.Storage == nil || ptr.Deref(cr.Spec.Storage.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr.Spec.Storage)
}

func createOrUpdateVTStorageService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
	svc := build.Service(b, cr.Spec.Storage.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.PublishNotReadyAddresses = true
	})
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil && prevCR.Spec.Storage != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
		prevSvc = build.Service(b, prevCR.Spec.Storage.Port, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			svc.Spec.PublishNotReadyAddresses = true
		})
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.Storage.ServiceSpec)

	}
	owner := cr.AsOwner()
	if err := cr.Spec.Storage.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("VTStorage additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
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
	if !ptr.Deref(cr.Spec.Storage.DisableSelfServiceScrape, false) {
		svs := buildVTStorageScrape(cr, svc)
		prevSvs := buildVTStorageScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs, prevSvs, &owner); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VTStorage: %w", err)
		}
	}
	return nil
}

func createOrUpdateVTStorageHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	hpa := cr.Spec.Storage.HPA
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
	if prevCR != nil && prevCR.Spec.Storage.HPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.Storage.HPA)
	}
	owner := cr.AsOwner()
	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA, &owner)
}

func createOrUpdateVTStorageVPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	vpa := cr.Spec.Storage.VPA
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
	if prevCR != nil && prevCR.Spec.Storage != nil && prevCR.Spec.Storage.VPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
		prevVPA = build.VPA(b, targetRef, prevCR.Spec.Storage.VPA)
	}
	owner := cr.AsOwner()
	return reconcile.VPA(ctx, rclient, newVPA, prevVPA, &owner)
}

func createOrUpdateVTStorageSTS(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	var prevSts *appsv1.StatefulSet

	if prevCR != nil && prevCR.Spec.Storage != nil {
		var err error
		prevSts, err = buildVTStorageSTSSpec(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev storage spec: %w", err)
		}
	}
	newSts, err := buildVTStorageSTSSpec(cr)
	if err != nil {
		return err
	}

	stsOpts := reconcile.STSOptions{
		HasClaim: len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: func() map[string]string {
			return cr.SelectorLabels(vmv1beta1.ClusterComponentStorage)
		},
		UpdateBehavior: cr.Spec.Storage.RollingUpdateStrategyBehavior,
	}
	owner := cr.AsOwner()
	return reconcile.StatefulSet(ctx, rclient, stsOpts, newSts, prevSts, &owner)
}

func buildVTStorageSTSSpec(cr *vmv1.VTCluster) (*appsv1.StatefulSet, error) {

	podSpec, err := buildVTStoragePodSpec(cr)
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
				Type: cr.Spec.Storage.RollingUpdateStrategy,
			},
			Template:    *podSpec,
			ServiceName: cr.PrefixedName(vmv1beta1.ClusterComponentStorage),
		},
	}
	if cr.Spec.Storage.PersistentVolumeClaimRetentionPolicy != nil {
		stsSpec.Spec.PersistentVolumeClaimRetentionPolicy = cr.Spec.Storage.PersistentVolumeClaimRetentionPolicy
	}
	build.StatefulSetAddCommonParams(stsSpec, ptr.Deref(cr.Spec.Storage.UseStrictSecurity, false), &cr.Spec.Storage.CommonApplicationDeploymentParams)
	storageSpec := cr.Spec.Storage.Storage
	storageSpec.IntoSTSVolume(cr.Spec.Storage.GetStorageVolumeName(), &stsSpec.Spec)
	stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.Storage.ClaimTemplates...)

	return stsSpec, nil
}

func buildVTStoragePodSpec(cr *vmv1.VTCluster) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Storage.Port),
		fmt.Sprintf("-storageDataPath=%s", cr.Spec.Storage.StorageDataPath),
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if cr.Spec.Storage.RetentionPeriod != "" {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", cr.Spec.Storage.RetentionPeriod))
	}
	if cr.Spec.Storage.FutureRetention != "" {
		args = append(args, fmt.Sprintf("-futureRetention=%s", cr.Spec.Storage.FutureRetention))
	}
	if cr.Spec.Storage.RetentionMaxDiskSpaceUsageBytes != "" {
		args = append(args, fmt.Sprintf("-retention.maxDiskSpaceUsageBytes=%s", cr.Spec.Storage.RetentionMaxDiskSpaceUsageBytes))
	}
	if cr.Spec.Storage.LogNewStreams {
		args = append(args, "-logNewStreams")
	}
	if cr.Spec.Storage.LogIngestedRows {
		args = append(args, "-logIngestedRows")
	}

	if cr.Spec.Storage.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.Storage.LogLevel))
	}
	if cr.Spec.Storage.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.Storage.LogFormat))
	}

	if len(cr.Spec.Storage.ExtraEnvs) > 0 || len(cr.Spec.Storage.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.Storage.ExtraEnvs...)

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.Storage.Port).IntVal,
		},
	}
	volumes := make([]corev1.Volume, 0)
	vmMounts := make([]corev1.VolumeMount, 0)

	volumes = append(volumes, cr.Spec.Storage.Volumes...)
	vmMounts = append(vmMounts, corev1.VolumeMount{
		Name:      cr.Spec.Storage.GetStorageVolumeName(),
		MountPath: cr.Spec.Storage.StorageDataPath,
	})

	vmMounts = append(vmMounts, cr.Spec.Storage.VolumeMounts...)

	for _, s := range cr.Spec.Storage.Secrets {
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

	for _, c := range cr.Spec.Storage.ConfigMaps {
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

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.Storage.ExtraArgs, "-")
	sort.Strings(args)
	vmstorageContainer := corev1.Container{
		Name:                     "vtstorage",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Storage.Image.Repository, cr.Spec.Storage.Image.Tag),
		ImagePullPolicy:          cr.Spec.Storage.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.Storage.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.Storage.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	vmstorageContainer = build.Probe(vmstorageContainer, cr.Spec.Storage)

	storageContainers := []corev1.Container{vmstorageContainer}
	var initContainers []corev1.Container

	useStrictSecurity := ptr.Deref(cr.Spec.Storage.UseStrictSecurity, false)
	build.AddStrictSecuritySettingsToContainers(cr.Spec.Storage.SecurityContext, initContainers, useStrictSecurity)
	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.Storage.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch storage init containers: %w", err)
	}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.Storage.SecurityContext, storageContainers, useStrictSecurity)
	containers, err := k8stools.MergePatchContainers(storageContainers, cr.Spec.Storage.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch storage containers: %w", err)
	}

	for i := range cr.Spec.Storage.TopologySpreadConstraints {
		if cr.Spec.Storage.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.Storage.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
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
