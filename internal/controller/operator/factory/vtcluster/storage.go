package vtcluster

import (
	"context"
	"fmt"
	"path"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func createOrUpdateVTStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) error {
	if cr.Spec.Storage == nil {
		return nil
	}
	b := newOptsBuilder(cr, cr.GetVTStorageName(), cr.VTStorageSelectorLabels())

	if cr.Spec.Storage.PodDisruptionBudget != nil {
		pdb := build.PodDisruptionBudget(b, cr.Spec.Storage.PodDisruptionBudget)
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.Storage.PodDisruptionBudget != nil {
			prevB := newOptsBuilder(prevCR, prevCR.GetVTStorageName(), prevCR.VTStorageSelectorLabels())
			prevPDB = build.PodDisruptionBudget(prevB, prevCR.Spec.Storage.PodDisruptionBudget)
		}
		err := reconcile.PDB(ctx, rclient, pdb, prevPDB)
		if err != nil {
			return err
		}
	}
	if err := createOrUpdateVTStorageSTS(ctx, rclient, cr, prevCR); err != nil {
		return err
	}

	storageSvc, err := createOrUpdateVTStorageService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	if !ptr.Deref(cr.Spec.Storage.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(storageSvc, cr.Spec.Storage))
		if err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VTStorage: %w", err)
		}
	}

	return nil
}

func createOrUpdateVTStorageService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VTCluster) (*corev1.Service, error) {
	t := &optsBuilder{
		cr,
		cr.GetVTStorageName(),
		cr.FinalLabels(cr.VTStorageSelectorLabels()),
		cr.VTStorageSelectorLabels(),
		cr.Spec.Storage.ServiceSpec,
	}
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.Storage != nil {
		prevT := &optsBuilder{
			prevCR,
			prevCR.GetVTStorageName(),
			prevCR.FinalLabels(prevCR.VTStorageSelectorLabels()),
			prevCR.VTStorageSelectorLabels(),
			prevCR.Spec.Storage.ServiceSpec,
		}

		prevService = build.Service(prevT, prevCR.Spec.Storage.Port, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			svc.Spec.PublishNotReadyAddresses = true
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.Storage.ServiceSpec)

	}
	newHeadless := build.Service(t, cr.Spec.Storage.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.PublishNotReadyAddresses = true
	})

	if err := cr.Spec.Storage.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newHeadless, s)
		if additionalService.Name == newHeadless.Name {
			return fmt.Errorf("VTStorage additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newHeadless.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile storage additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newHeadless, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile storage service: %w", err)
	}
	return newHeadless, nil
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
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.VTStorageSelectorLabels,
		UpdateBehavior: cr.Spec.Storage.RollingUpdateStrategyBehavior,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, prevSts)
}

func buildVTStorageSTSSpec(cr *vmv1.VTCluster) (*appsv1.StatefulSet, error) {

	podSpec, err := buildVTStoragePodSpec(cr)
	if err != nil {
		return nil, err
	}

	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetVTStorageName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VTStorageSelectorLabels()),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VTStorageSelectorLabels(),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.Storage.RollingUpdateStrategy,
			},
			Template:    *podSpec,
			ServiceName: cr.GetVTStorageName(),
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
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Storage.Port),
		fmt.Sprintf("-storageDataPath=%s", cr.Spec.Storage.StorageDataPath),
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
				MatchLabels: cr.VTStorageSelectorLabels(),
			}
		}
	}

	vmStoragePodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VTStoragePodLabels(),
			Annotations: cr.VTStoragePodAnnotations(),
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
