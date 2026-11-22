package vmestimator

import (
	"context"
	"fmt"
	"path"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	configDir        = "/etc/vmestimator/config"
	configVolumeName = "config"
)

// createOrUpdateStorage reconciles storage nodes
func createOrUpdateStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	if cr.Spec.Storage == nil {
		return nil
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
	owner := cr.AsOwner()
	var prev componentObjects
	var prevB *build.ChildBuilder
	if prevCR != nil {
		ps := prevCR.Spec.Storage
		prev = componentObjects{pdb: ps.PodDisruptionBudget, np: ps.NetworkPolicy, vpa: ps.VPA}
		prevB = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
	}
	s := cr.Spec.Storage
	cur := componentObjects{pdb: s.PodDisruptionBudget, np: s.NetworkPolicy, vpa: s.VPA}
	if err := createOrUpdateComponentObjects(ctx, rclient, b, prevB, &owner, "StatefulSet", cur, prev); err != nil {
		return err
	}
	if err := createOrUpdateStorageService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	return createOrUpdateStorageSTS(ctx, rclient, cr, prevCR)
}

func buildStorageScrape(cr *vmv1.VMEstimator, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || cr.Spec.Storage == nil || ptr.Deref(cr.Spec.Storage.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr.Spec.Storage)
}

// buildStorageService builds headless service, which provides stable network identities for storage pods
func buildStorageService(cr *vmv1.VMEstimator) *corev1.Service {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
	return build.Service(b, cr.Spec.Storage.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.PublishNotReadyAddresses = true
	})
}

func createOrUpdateStorageService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil {
		prevSvc = buildStorageService(prevCR)
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.Storage.ServiceSpec)
	}
	svc := buildStorageService(cr)
	owner := cr.AsOwner()
	if err := cr.Spec.Storage.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("storage additional service name: %q cannot be the same as %q ", additionalSvc.Name, svc.Name)
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
		svs := buildStorageScrape(cr, svc)
		prevSvs := buildStorageScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrape(ctx, rclient, svs, prevSvs, &owner, false); err != nil {
			return fmt.Errorf("cannot create VMServiceScrape for storage: %w", err)
		}
	}
	return nil
}

func createOrUpdateStorageSTS(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VMEstimator) error {
	var prevSts *appsv1.StatefulSet
	if prevCR != nil {
		var err error
		prevSts, err = buildStorageSTS(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev storage statefulset: %w", err)
		}
	}
	newSts, err := buildStorageSTS(cr)
	if err != nil {
		return err
	}
	o := reconcile.StatefulSetOpts{
		SelectorLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentStorage),
		UpdateBehavior: cr.Spec.Storage.RollingUpdateStrategyBehavior,
	}
	owner := cr.AsOwner()
	return reconcile.StatefulSet(ctx, rclient, newSts, prevSts, &owner, &o)
}

func buildStorageSTS(cr *vmv1.VMEstimator) (*appsv1.StatefulSet, error) {
	storage := cr.Spec.Storage
	podSpec, err := newStorageSpec(cr)
	if err != nil {
		return nil, err
	}
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentStorage),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentStorage),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentStorage),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: storage.RollingUpdateStrategy,
			},
			Template:    *podSpec,
			ServiceName: cr.PrefixedName(vmv1beta1.ClusterComponentStorage),
		},
	}
	build.StatefulSetAddCommonParams(sts, &storage.CommonAppsParams)
	return sts, nil
}

func newStorageSpec(cr *vmv1.VMEstimator) (*corev1.PodTemplateSpec, error) {
	storage := cr.Spec.Storage
	cfg := config.MustGetBaseConfig()

	args := []string{
		fmt.Sprintf("-config=%s/%s", configDir, configFileName),
		fmt.Sprintf("-httpListenAddr=:%s", storage.Port),
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}

	if storage.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", storage.LogLevel))
	}
	if storage.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", storage.LogFormat))
	}
	if len(storage.ExtraEnvs) > 0 || len(storage.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	// select nodes expose merged cardinality estimations at the default path,
	// so storage nodes must expose local estimations at a different path
	if cr.Spec.Select != nil && ptr.Deref(cr.Spec.Select.ReplicaCount, 1) > 0 {
		args = append(args, "-cardinalityMetrics.exposeAt=/cardinality/metrics")
	}
	var envs []corev1.EnvVar

	envs = append(envs, storage.ExtraEnvs...)

	var volumes []corev1.Volume
	volumes = append(volumes, corev1.Volume{
		Name: configVolumeName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cr.GetConfigMapName(),
				},
			},
		},
	})

	volumes = append(volumes, storage.Volumes...)

	var volumeMounts []corev1.VolumeMount
	var crMounts []corev1.VolumeMount

	volumeMounts = append(volumeMounts, corev1.VolumeMount{
		Name:      configVolumeName,
		MountPath: configDir,
		ReadOnly:  true,
	})
	volumeMounts = append(volumeMounts, storage.VolumeMounts...)

	crMounts = append(crMounts, corev1.VolumeMount{
		Name:      configVolumeName,
		MountPath: configDir,
		ReadOnly:  true,
	})

	for _, s := range storage.Secrets {
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

	for _, c := range storage.ConfigMaps {
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
		vm := corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		}
		volumeMounts = append(volumeMounts, vm)
		crMounts = append(crMounts, vm)
	}
	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(storage.Port).IntVal})

	args = build.AddExtraArgsOverrideDefaults(args, storage.ExtraArgs, "-")
	// sort for consistency
	build.SortArgs(args)
	sort.Slice(volumes, func(i, j int) bool {
		return volumes[i].Name < volumes[j].Name
	})
	sort.Slice(volumeMounts, func(i, j int) bool {
		return volumeMounts[i].Name < volumeMounts[j].Name
	})

	var storageContainers []corev1.Container

	storageContainer := corev1.Container{
		Args:                     args,
		Name:                     "storage",
		Image:                    storage.Image.Reference(),
		ImagePullPolicy:          storage.Image.PullPolicy,
		Ports:                    ports,
		VolumeMounts:             volumeMounts,
		Resources:                storage.Resources,
		Env:                      envs,
		EnvFrom:                  storage.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}
	build.Probe(&storageContainer, storage, &storage.CommonAppsParams)
	build.Lifecycle(&storageContainer, &storage.CommonAppsParams)
	build.AddConfigReloadAuthKeyToApp(&storageContainer, storage.ExtraArgs, &storage.CommonConfigReloaderParams)
	storageContainers = append(storageContainers, storageContainer)

	crb := &configReloaderBuilder{VMEstimatorStorage: storage, ns: cr.Namespace}
	crc := build.ConfigReloaderContainer(build.ConfigReloaderOpts{
		CR:     crb,
		Mounts: crMounts,
	})

	storageContainers = append(storageContainers, crc)
	build.AddStrictSecuritySettingsToContainers(storageContainers, &storage.CommonAppsParams)
	containers, err := k8stools.MergePatchContainers(storageContainers, storage.Containers)
	if err != nil {
		return nil, err
	}

	for i := range storage.TopologySpreadConstraints {
		if storage.TopologySpreadConstraints[i].LabelSelector == nil {
			storage.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentStorage),
			}
		}
	}
	volumes = build.AddConfigReloadAuthKeyVolume(volumes, &storage.CommonConfigReloaderParams)

	spec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(vmv1beta1.ClusterComponentStorage),
			Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentStorage),
		},
		Spec: corev1.PodSpec{
			ServiceAccountName: cr.GetServiceAccountName(),
			InitContainers:     storage.InitContainers,
			Containers:         containers,
			Volumes:            volumes,
		},
	}
	return spec, nil
}

type configReloaderBuilder struct {
	*vmv1.VMEstimatorStorage
	ns string
}

func (crb *configReloaderBuilder) GetNamespace() string { return crb.ns }
