package vlcluster

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

func createOrUpdateVLStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	if cr.Spec.VLStorage == nil {
		return nil
	}
	b := newOptsBuilder(cr, cr.GetVLStorageName(), cr.VLStorageSelectorLabels())

	if cr.Spec.VLStorage.PodDisruptionBudget != nil {
		pdb := build.PodDisruptionBudget(b, cr.Spec.VLStorage.PodDisruptionBudget)
		var prevPDB *policyv1.PodDisruptionBudget
		if prevCR != nil && prevCR.Spec.VLStorage.PodDisruptionBudget != nil {
			prevB := newOptsBuilder(prevCR, prevCR.GetVLStorageName(), prevCR.VLStorageSelectorLabels())
			prevPDB = build.PodDisruptionBudget(prevB, prevCR.Spec.VLStorage.PodDisruptionBudget)
		}
		err := reconcile.PDB(ctx, rclient, pdb, prevPDB)
		if err != nil {
			return err
		}
	}
	if err := createOrUpdateVLStorageStatefulSet(ctx, rclient, cr, prevCR); err != nil {
		return err
	}

	storageSvc, err := createOrUpdateVLStorageService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}
	if !ptr.Deref(cr.Spec.VLStorage.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(storageSvc, cr.Spec.VLStorage))
		if err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for VLStorage: %w", err)
		}
	}

	return nil
}

func createOrUpdateVLStorageService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) (*corev1.Service, error) {
	t := &optsBuilder{
		cr,
		cr.GetVLStorageName(),
		cr.FinalLabels(cr.VLStorageSelectorLabels()),
		cr.VLStorageSelectorLabels(),
		cr.Spec.VLStorage.ServiceSpec,
	}
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.VLStorage != nil {
		prevT := &optsBuilder{
			prevCR,
			prevCR.GetVLStorageName(),
			prevCR.FinalLabels(prevCR.VLStorageSelectorLabels()),
			prevCR.VLStorageSelectorLabels(),
			prevCR.Spec.VLStorage.ServiceSpec,
		}

		prevService = build.Service(prevT, prevCR.Spec.VLStorage.Port, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			svc.Spec.PublishNotReadyAddresses = true
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.VLStorage.ServiceSpec)

	}
	newHeadless := build.Service(t, cr.Spec.VLStorage.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.PublishNotReadyAddresses = true
	})

	if err := cr.Spec.VLStorage.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newHeadless, s)
		if additionalService.Name == newHeadless.Name {
			return fmt.Errorf("VLStorage additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newHeadless.Name)
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

func createOrUpdateVLStorageStatefulSet(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLCluster) error {
	var prevSts *appsv1.StatefulSet

	if prevCR != nil && prevCR.Spec.VLStorage != nil {
		var err error
		prevSts, err = buildVLStorageStatefulSetSpec(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev storage spec: %w", err)
		}
	}
	newSts, err := buildVLStorageStatefulSetSpec(cr)
	if err != nil {
		return err
	}

	stsOpts := reconcile.StatefulSetOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.VLStorageSelectorLabels,
	}
	return reconcile.HandleStatefulSetUpdate(ctx, rclient, stsOpts, newSts, prevSts)
}

func buildVLStorageStatefulSetSpec(cr *vmv1.VLCluster) (*appsv1.StatefulSet, error) {

	podSpec, err := newPodSpecForVLStorage(cr)
	if err != nil {
		return nil, err
	}

	app := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetVLStorageName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VLStorageSelectorLabels()),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VLStorageSelectorLabels(),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VLStorage.RollingUpdateStrategy,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.VLStoragePodLabels(),
					Annotations: cr.VLStoragePodAnnotations(),
				},
				Spec: *podSpec,
			},
			ServiceName: cr.GetVLStorageName(),
		},
	}
	build.StatefulSetAddCommonParams(app, ptr.Deref(cr.Spec.VLStorage.UseStrictSecurity, false), &cr.Spec.VLStorage.CommonApplicationDeploymentParams)
	storageSpec := cr.Spec.VLStorage.Storage
	storageSpec.IntoStatefulSetVolume(cr.Spec.VLStorage.GetStorageVolumeName(), &app.Spec)
	app.Spec.VolumeClaimTemplates = append(app.Spec.VolumeClaimTemplates, cr.Spec.VLStorage.ClaimTemplates...)

	return app, nil
}

func newPodSpecForVLStorage(cr *vmv1.VLCluster) (*corev1.PodSpec, error) {
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VLStorage.Port),
		fmt.Sprintf("-storageDataPath=%s", cr.Spec.VLStorage.StorageDataPath),
	}

	if cr.Spec.VLStorage.RetentionPeriod != "" {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", cr.Spec.VLStorage.RetentionPeriod))
	}
	if cr.Spec.VLStorage.FutureRetention != "" {
		args = append(args, fmt.Sprintf("-futureRetention=%s", cr.Spec.VLStorage.RetentionPeriod))
	}
	if cr.Spec.VLStorage.RetentionMaxDiskSpaceUsageBytes != "" {
		args = append(args, fmt.Sprintf("-retention.maxDiskSpaceUsageBytes=%s", cr.Spec.VLStorage.RetentionPeriod))
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
	volumeMounts := make([]corev1.VolumeMount, 0)

	volumes = append(volumes, cr.Spec.VLStorage.Volumes...)
	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      cr.Spec.VLStorage.GetStorageVolumeName(),
		MountPath: cr.Spec.VLStorage.StorageDataPath,
	})

	volumeMounts = append(volumeMounts, cr.Spec.VLStorage.VolumeMounts...)

	for _, s := range cr.Spec.VLStorage.Secrets {
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
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VLStorage.ExtraArgs, "-")
	sort.Strings(args)
	container := corev1.Container{
		Name:                     "vlstorage",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VLStorage.Image.Repository, cr.Spec.VLStorage.Image.Tag),
		ImagePullPolicy:          cr.Spec.VLStorage.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             volumeMounts,
		Resources:                cr.Spec.VLStorage.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.VLStorage.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	container = build.Probe(container, cr.Spec.VLStorage)

	containers := []corev1.Container{container}
	var initContainers []corev1.Container

	useStrictSecurity := ptr.Deref(cr.Spec.VLStorage.UseStrictSecurity, false)
	build.AddStrictSecuritySettingsToContainers(cr.Spec.VLStorage.SecurityContext, initContainers, useStrictSecurity)
	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.VLStorage.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch storage init containers: %w", err)
	}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VLStorage.SecurityContext, containers, useStrictSecurity)
	containers, err = k8stools.MergePatchContainers(containers, cr.Spec.VLStorage.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch storage containers: %w", err)
	}

	for i := range cr.Spec.VLStorage.TopologySpreadConstraints {
		if cr.Spec.VLStorage.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VLStorage.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.VLStorageSelectorLabels(),
			}
		}
	}

	return &corev1.PodSpec{
		Volumes:            volumes,
		InitContainers:     ic,
		Containers:         containers,
		ServiceAccountName: cr.GetServiceAccountName(),
	}, nil
}
