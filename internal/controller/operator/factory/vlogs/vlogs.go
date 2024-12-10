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

func createVLogsStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VLogs) error {
	l := logger.WithContext(ctx).WithValues("pvc_for", "vlogs")
	ctx = logger.AddToContext(ctx, l)
	newPvc := makeVLogsPvc(cr)
	var prevPVC *corev1.PersistentVolumeClaim
	if prevCR != nil && prevCR.Spec.Storage != nil {
		prevPVC = makeVLogsPvc(prevCR)
	}
	return reconcile.PersistentVolumeClaim(ctx, rclient, newPvc, prevPVC)
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
	if len(pvcObject.Spec.AccessModes) == 0 {
		pvcObject.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}

	return pvcObject
}

// CreateOrUpdateVLogs performs an update for vlogs resource
func CreateOrUpdateVLogs(ctx context.Context, rclient client.Client, cr *vmv1beta1.VLogs) error {

	var prevCR *vmv1beta1.VLogs
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if err := deletePrevStateResources(ctx, cr, rclient); err != nil {
		return err
	}
	if cr.Spec.Storage != nil && cr.Spec.StorageDataPath == "" {
		if err := createVLogsStorage(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
	}

	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	svc, err := createOrUpdateVLogsService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr))
		if err != nil {
			return fmt.Errorf("cannot create serviceScrape for vlogs: %w", err)
		}
	}

	var prevDeploy *appsv1.Deployment
	if prevCR != nil {
		prevDeploy, err = newDeployForVLogs(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}

	newDeploy, err := newDeployForVLogs(cr)
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

// createOrUpdateVLogsService creates service for vlogs
func createOrUpdateVLogsService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VLogs) (*corev1.Service, error) {
	newService := build.Service(cr, cr.Spec.Port, nil)

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			logger.WithContext(ctx).Error(fmt.Errorf("vlogs additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if err := reconcile.Service(ctx, rclient, additionalService, nil); err != nil {
			// TODO: f41gh7 add prevCR
			return fmt.Errorf("cannot reconcile additional service for vlogs: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	var prevService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, nil)
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vlogs: %w", err)
	}
	return newService, nil
}

func deletePrevStateResources(ctx context.Context, cr *vmv1beta1.VLogs, rclient client.Client) error {
	if cr.ParsedLastAppliedSpec == nil {
		return nil
	}
	prevSvc, currSvc := cr.ParsedLastAppliedSpec.ServiceSpec, cr.Spec.ServiceSpec
	if err := reconcile.AdditionalServices(ctx, rclient, cr.PrefixedName(), cr.Namespace, prevSvc, currSvc); err != nil {
		return fmt.Errorf("cannot remove additional service: %w", err)
	}

	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	if ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) && !ptr.Deref(cr.ParsedLastAppliedSpec.DisableSelfServiceScrape, false) {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: objMeta}); err != nil {
			return fmt.Errorf("cannot remove serviceScrape: %w", err)
		}
	}

	return nil
}
