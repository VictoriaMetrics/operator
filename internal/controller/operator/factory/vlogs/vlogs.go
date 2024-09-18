package vlogs

import (
	"context"
	"fmt"
	"path"
	"sort"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vlogsDataDir        = "/victoria-metrics-data"
	vlogsDataVolumeName = "data"
)

// CreateVLogsStorage creates persistent volume for vlogs
func CreateVLogsStorage(ctx context.Context, r *vmv1beta1.VLogs, rclient client.Client) error {
	l := logger.WithContext(ctx).WithValues("vlogs.pvc.create", r.Name)
	ctx = logger.AddToContext(ctx, l)
	newPvc := makeVLogsPvc(r)
	return reconcile.PersistentVolumeClaim(ctx, rclient, newPvc)
}

func makeVLogsPvc(r *vmv1beta1.VLogs) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            r.PrefixedName(),
			Namespace:       r.Namespace,
			Labels:          labels.Merge(r.Spec.StorageMetadata.Labels, r.SelectorLabels()),
			Annotations:     r.Spec.StorageMetadata.Annotations,
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: r.AsOwner(),
		},
		Spec: *r.Spec.Storage,
	}
	return pvcObject
}

// CreateOrUpdateVLogs performs an update for vlogs resource
func CreateOrUpdateVLogs(ctx context.Context, r *vmv1beta1.VLogs, rclient client.Client) error {

	if r.IsOwnsServiceAccount() {
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(r)); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	svc, err := CreateOrUpdateVLogsService(ctx, r, rclient)
	if err != nil {
		return err
	}

	if !ptr.Deref(r.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, r))
		if err != nil {
			return fmt.Errorf("cannot create serviceScrape for vlogs: %w", err)
		}
	}

	var prevDeploy *appsv1.Deployment

	if r.Spec.ParsedLastAppliedSpec != nil {
		prevCR := r.DeepCopy()
		prevCR.Spec = *r.Spec.ParsedLastAppliedSpec
		prevDeploy, err = newDeployForVLogs(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}

	newDeploy, err := newDeployForVLogs(r)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vlogs: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false)
}

func newDeployForVLogs(r *vmv1beta1.VLogs) (*appsv1.Deployment, error) {
	podSpec, err := makeSpecForVLogs(r)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            r.PrefixedName(),
			Namespace:       r.Namespace,
			Labels:          r.AllLabels(),
			Annotations:     r.AnnotationsFiltered(),
			OwnerReferences: r.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: r.Spec.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: r.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				// we use recreate, coz of volume claim
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, ptr.Deref(r.Spec.UseStrictSecurity, false), &r.Spec.CommonApplicationDeploymentParams)
	return depSpec, nil
}

func makeSpecForVLogs(r *vmv1beta1.VLogs) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-retentionPeriod=%s", r.Spec.RetentionPeriod),
	}

	// if customStorageDataPath is not empty, do not add pvc.
	shouldAddPVC := r.Spec.StorageDataPath == ""

	storagePath := vlogsDataDir
	if r.Spec.StorageDataPath != "" {
		storagePath = r.Spec.StorageDataPath
	}
	args = append(args, fmt.Sprintf("-storageDataPath=%s", storagePath))
	if r.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", r.Spec.LogLevel))
	}
	if r.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", r.Spec.LogFormat))
	}
	if len(r.Spec.FutureRetention) > 0 {
		args = append(args, fmt.Sprintf("-futureRetention=%s", r.Spec.FutureRetention))
	}
	if r.Spec.LogNewStreams {
		args = append(args, "-logNewStreams")
	}
	if r.Spec.LogIngestedRows {
		args = append(args, "-logIngestedRows")
	}
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", r.Spec.Port))
	if len(r.Spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, r.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(r.Spec.Port).IntVal})
	volumes := []corev1.Volume{}

	storageSpec := r.Spec.Storage

	if storageSpec == nil {
		volumes = append(volumes, corev1.Volume{
			Name: vlogsDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else if shouldAddPVC {
		volumes = append(volumes, corev1.Volume{
			Name: vlogsDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: r.PrefixedName(),
				},
			},
		})
	}
	volumes = append(volumes, r.Spec.Volumes...)
	vmMounts := []corev1.VolumeMount{
		{
			Name:      vlogsDataVolumeName,
			MountPath: storagePath,
		},
	}

	vmMounts = append(vmMounts, r.Spec.VolumeMounts...)

	for _, s := range r.Spec.Secrets {
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

	for _, c := range r.Spec.ConfigMaps {
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

	args = build.AddExtraArgsOverrideDefaults(args, r.Spec.ExtraArgs, "-")
	sort.Strings(args)
	vlogsContainer := corev1.Container{
		Name:                     "vlogs",
		Image:                    fmt.Sprintf("%s:%s", r.Spec.Image.Repository, r.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                r.Spec.Resources,
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          r.Spec.Image.PullPolicy,
	}

	vlogsContainer = build.Probe(vlogsContainer, r)

	operatorContainers := []corev1.Container{vlogsContainer}

	build.AddStrictSecuritySettingsToContainers(r.Spec.SecurityContext, operatorContainers, ptr.Deref(r.Spec.UseStrictSecurity, false))

	containers, err := k8stools.MergePatchContainers(operatorContainers, r.Spec.Containers)
	if err != nil {
		return nil, err
	}

	vlogsSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      r.PodLabels(),
			Annotations: r.PodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     r.Spec.InitContainers,
			Containers:         containers,
			ServiceAccountName: r.GetServiceAccountName(),
		},
	}

	return vlogsSpec, nil
}

// CreateOrUpdateVLogsService creates service for vlogs
func CreateOrUpdateVLogsService(ctx context.Context, r *vmv1beta1.VLogs, rclient client.Client) (*corev1.Service, error) {
	newService := build.Service(r, r.Spec.Port, nil)

	if err := r.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			logger.WithContext(ctx).Error(fmt.Errorf("vlogs additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if err := reconcile.Service(ctx, rclient, additionalService, nil); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vlogs: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	var prevService *corev1.Service
	if r.Spec.ParsedLastAppliedSpec != nil {
		prevCR := r.DeepCopy()
		prevCR.Spec = *r.Spec.ParsedLastAppliedSpec
		prevService = build.Service(prevCR, prevCR.Spec.Port, nil)
	}

	if r.Spec.ServiceSpec == nil &&
		r.Spec.ParsedLastAppliedSpec != nil &&
		r.Spec.ParsedLastAppliedSpec.ServiceSpec != nil {
		rca := finalize.RemoveSvcArgs{SelectorLabels: r.SelectorLabels, GetNameSpace: r.GetNamespace, PrefixedName: r.PrefixedName}
		if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, r.Spec.ServiceSpec); err != nil {
			return nil, err
		}
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vlogs: %w", err)
	}
	return newService, nil
}
