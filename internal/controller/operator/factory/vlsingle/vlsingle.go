package vlsingle

import (
	"context"
	"fmt"
	"path"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	dataDataDir              = "/victoria-logs-data"
	tlsServerConfigMountPath = "/etc/vm/tls-server-secrets"
)

func createOrUpdatePVC(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLSingle) error {
	newPvc := newPVC(cr)
	var prevPVC *corev1.PersistentVolumeClaim
	if prevCR != nil && prevCR.Spec.Storage != nil {
		prevPVC = newPVC(prevCR)
	}
	owner := cr.AsOwner()
	return reconcile.PersistentVolumeClaim(ctx, rclient, newPvc, prevPVC, &owner)
}

func newPVC(r *vmv1.VLSingle) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            r.PrefixedName(),
			Namespace:       r.Namespace,
			Labels:          labels.Merge(r.Spec.StorageMetadata.Labels, r.SelectorLabels()),
			Annotations:     r.Spec.StorageMetadata.Annotations,
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: []metav1.OwnerReference{r.AsOwner()},
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

// CreateOrUpdate performs an update for vlsingle resource
func CreateOrUpdate(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle) error {
	var prevCR *vmv1.VLSingle
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return err
		}
	}
	if cr.Spec.Storage != nil {
		if err := createOrUpdatePVC(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
	}

	owner := cr.AsOwner()
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			prevSA = build.ServiceAccount(prevCR)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr), prevSA, &owner); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	if err := createOrUpdateService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}

	var prevDeploy *appsv1.Deployment
	if prevCR != nil {
		var err error
		prevDeploy, err = newDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}

	newDeploy, err := newDeployment(cr)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vlsingle: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false, &owner)
}

func newDeployment(r *vmv1.VLSingle) (*appsv1.Deployment, error) {
	podSpec, err := makePodSpec(r)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            r.PrefixedName(),
			Namespace:       r.Namespace,
			Labels:          r.FinalLabels(),
			Annotations:     r.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{r.AsOwner()},
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

func makePodSpec(r *vmv1.VLSingle) (*corev1.PodTemplateSpec, error) {
	var args []string

	if len(r.Spec.RetentionPeriod) > 0 {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", r.Spec.RetentionPeriod))
	}
	if len(r.Spec.RetentionMaxDiskSpaceUsageBytes) > 0 {
		args = append(args, fmt.Sprintf("-retention.maxDiskSpaceUsageBytes=%s", r.Spec.RetentionMaxDiskSpaceUsageBytes))
	}

	storagePath := dataDataDir
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
	cfg := config.MustGetBaseConfig()
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", r.Spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if len(r.Spec.ExtraEnvs) > 0 || len(r.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, r.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(r.Spec.Port).IntVal})

	var pvcSrc *corev1.PersistentVolumeClaimVolumeSource
	if r.Spec.Storage != nil {
		pvcSrc = &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: r.PrefixedName(),
		}
	}
	volumes, vmMounts, err := build.StorageVolumeMountsTo(r.Spec.Volumes, r.Spec.VolumeMounts, pvcSrc, storagePath, build.DataVolumeName)
	if err != nil {
		return nil, err
	}

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
	if r.Spec.SyslogSpec != nil {
		args = build.AddSyslogArgsTo(args, r.Spec.SyslogSpec, tlsServerConfigMountPath)
		volumes, vmMounts = build.AddSyslogTLSConfigToVolumes(volumes, vmMounts, r.Spec.SyslogSpec, tlsServerConfigMountPath)
		ports = build.AddSyslogPortsTo(ports, r.Spec.SyslogSpec)
	}

	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, r.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, r.Spec.License, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, r.Spec.ExtraArgs, "-")
	sort.Strings(args)
	vlsingleContainer := corev1.Container{
		Name:                     "vlsingle",
		Image:                    fmt.Sprintf("%s:%s", r.Spec.Image.Repository, r.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                r.Spec.Resources,
		Env:                      envs,
		EnvFrom:                  r.Spec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          r.Spec.Image.PullPolicy,
	}

	vlsingleContainer = build.Probe(vlsingleContainer, r)

	operatorContainers := []corev1.Container{vlsingleContainer}

	build.AddStrictSecuritySettingsToContainers(r.Spec.SecurityContext, operatorContainers, ptr.Deref(r.Spec.UseStrictSecurity, false))

	containers, err := k8stools.MergePatchContainers(operatorContainers, r.Spec.Containers)
	if err != nil {
		return nil, err
	}

	vlsingleSpec := &corev1.PodTemplateSpec{
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

	return vlsingleSpec, nil
}

func buildScrape(cr *vmv1.VLSingle, svc *corev1.Service) *vmv1beta1.VMServiceScrape {
	if cr == nil || svc == nil || ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		return nil
	}
	return build.VMServiceScrape(svc, cr)
}

// createOrUpdateService creates service for vlsingle
func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1.VLSingle) error {
	var prevSvc, prevAdditionalSvc *corev1.Service
	if prevCR != nil {
		prevSvc = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			build.AddSyslogPortsToService(svc, prevCR.Spec.SyslogSpec)
		})
		prevAdditionalSvc = build.AdditionalServiceFromDefault(prevSvc, prevCR.Spec.ServiceSpec)
	}

	svc := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		build.AddSyslogPortsToService(svc, cr.Spec.SyslogSpec)
	})
	owner := cr.AsOwner()
	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalSvc := build.AdditionalServiceFromDefault(svc, s)
		if additionalSvc.Name == svc.Name {
			return fmt.Errorf("vlsingle additional service name: %q cannot be the same as crd.prefixedname: %q", additionalSvc.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalSvc, prevAdditionalSvc, &owner); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vlsingle: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := reconcile.Service(ctx, rclient, svc, prevSvc, &owner); err != nil {
		return fmt.Errorf("cannot reconcile service for vlsingle: %w", err)
	}
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		svs := buildScrape(cr, svc)
		prevSvs := buildScrape(prevCR, prevSvc)
		if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs, prevSvs, &owner); err != nil {
			return fmt.Errorf("cannot create serviceScrape for vlsingle: %w", err)
		}
	}
	return nil
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1.VLSingle) error {
	owner := cr.AsOwner()
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	svcName := cr.PrefixedName()
	keepServices := map[string]struct{}{
		svcName: {},
	}
	keepServicesScrapes := make(map[string]struct{})
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		keepServicesScrapes[svcName] = struct{}{}
	}
	if cr.Spec.ServiceSpec != nil && !cr.Spec.ServiceSpec.UseAsDefault {
		extraSvcName := cr.Spec.ServiceSpec.NameOrDefault(svcName)
		keepServices[extraSvcName] = struct{}{}
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, cr, keepServices); err != nil {
		return fmt.Errorf("cannot remove services: %w", err)
	}
	if err := finalize.RemoveOrphanedVMServiceScrapes(ctx, rclient, cr, keepServicesScrapes); err != nil {
		return fmt.Errorf("cannot remove serviceScrapes: %w", err)
	}
	if !cr.IsOwnsServiceAccount() {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.ServiceAccount{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove serviceaccount: %w", err)
		}
	}
	return nil
}
