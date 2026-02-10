package vmsingle

import (
	"context"
	"fmt"
	"path"
	"sort"

	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	dataDataDir         = "/victoria-metrics-data"
	streamAggrSecretKey = "config.yaml"
)

func createStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle) error {
	newPvc := makePvc(cr)
	var prevPVC *corev1.PersistentVolumeClaim
	if prevCR != nil && prevCR.Spec.Storage != nil {
		prevPVC = makePvc(prevCR)
	}

	return reconcile.PersistentVolumeClaim(ctx, rclient, newPvc, prevPVC)
}

func makePvc(cr *vmv1beta1.VMSingle) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          labels.Merge(cr.Spec.StorageMetadata.Labels, cr.SelectorLabels()),
			Annotations:     cr.Spec.StorageMetadata.Annotations,
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: *cr.Spec.Storage,
	}
	if len(pvcObject.Spec.AccessModes) == 0 {
		pvcObject.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{
			corev1.ReadWriteOnce,
		}
	}
	return pvcObject
}

// CreateOrUpdate performs an update for single node resource
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) error {

	var prevCR *vmv1beta1.VMSingle
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
		if err := deleteOrphaned(ctx, rclient, cr); err != nil {
			return fmt.Errorf("cannot delete objects from prev state: %w", err)
		}
	}
	ac := getAssetsCache(ctx, rclient)
	if err := createOrUpdateStreamAggrConfig(ctx, rclient, cr, prevCR, ac); err != nil {
		return fmt.Errorf("cannot update stream aggregation config for vmsingle: %w", err)
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

	if cr.Spec.Storage != nil {
		if err := createStorage(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot create storage: %w", err)
		}
	}
	svc, err := createOrUpdateService(ctx, rclient, cr, prevCR)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrape(svc, cr, "vmbackupmanager")); err != nil {
			return fmt.Errorf("cannot create serviceScrape for vmsingle: %w", err)
		}
	}
	var prevDeploy *appsv1.Deployment
	if prevCR != nil {
		prevDeploy, err = newDeploy(ctx, prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeploy, err := newDeploy(ctx, cr)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vmsingle: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false)
}

func newDeploy(ctx context.Context, cr *vmv1beta1.VMSingle) (*appsv1.Deployment, error) {

	podSpec, err := makeSpec(ctx, cr)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(),
			Annotations:     cr.FinalAnnotations(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(),
			},
			Strategy: appsv1.DeploymentStrategy{
				// we use recreate, coz of volume claim
				Type: appsv1.RecreateDeploymentStrategyType,
			},
			Template: *podSpec,
		},
	}
	build.DeploymentAddCommonParams(depSpec, ptr.Deref(cr.Spec.UseStrictSecurity, false), &cr.Spec.CommonApplicationDeploymentParams)
	return depSpec, nil
}

func makeSpec(ctx context.Context, cr *vmv1beta1.VMSingle) (*corev1.PodTemplateSpec, error) {
	var args []string

	if cr.Spec.RetentionPeriod != "" {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", cr.Spec.RetentionPeriod))
	}

	storagePath := dataDataDir
	if cr.Spec.StorageDataPath != "" {
		storagePath = cr.Spec.StorageDataPath
	}
	args = append(args, fmt.Sprintf("-storageDataPath=%s", storagePath))
	if cr.Spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.LogLevel))
	}
	if cr.Spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.LogFormat))
	}

	cfg := config.MustGetBaseConfig()
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if len(cr.Spec.ExtraEnvs) > 0 || len(cr.Spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	args = build.AppendArgsForInsertPorts(args, cr.Spec.InsertPorts)

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = build.AppendInsertPorts(ports, cr.Spec.InsertPorts)

	var pvcSrc *corev1.PersistentVolumeClaimVolumeSource
	if cr.Spec.Storage != nil {
		pvcSrc = &corev1.PersistentVolumeClaimVolumeSource{
			ClaimName: cr.PrefixedName(),
		}
	}

	volumes, vmMounts, err := build.StorageVolumeMountsTo(cr.Spec.Volumes, cr.Spec.VolumeMounts, pvcSrc, storagePath, build.DataVolumeName)
	if err != nil {
		return nil, err
	}
	commonMounts := vmMounts

	if cr.Spec.VMBackup != nil && cr.Spec.VMBackup.CredentialsSecret != nil {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + cr.Spec.VMBackup.CredentialsSecret.Name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.Spec.VMBackup.CredentialsSecret.Name,
				},
			},
		})
	}

	for _, s := range cr.Spec.Secrets {
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

	for _, c := range cr.Spec.ConfigMaps {
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

	volumes, vmMounts = build.StreamAggrVolumeTo(volumes, vmMounts, cr)
	streamAggrKeys := []string{streamAggrSecretKey}
	streamAggrConfigs := []*vmv1beta1.StreamAggrConfig{cr.Spec.StreamAggrConfig}
	args = build.StreamAggrArgsTo(args, "streamAggr", streamAggrKeys, streamAggrConfigs...)

	// deduplication can work without stream aggregation rules
	if cr.Spec.StreamAggrConfig != nil && cr.Spec.StreamAggrConfig.DedupInterval != "" {
		args = append(args, fmt.Sprintf("--streamAggr.dedupInterval=%s", cr.Spec.StreamAggrConfig.DedupInterval))
	}

	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	vmsingleContainer := corev1.Container{
		Name:                     "vmsingle",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.Image.Repository, cr.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.Resources,
		Env:                      envs,
		EnvFrom:                  cr.Spec.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}

	vmsingleContainer = build.Probe(vmsingleContainer, cr)

	operatorContainers := []corev1.Container{vmsingleContainer}
	var initContainers []corev1.Container

	if cr.Spec.VMBackup != nil {
		vmBackupManagerContainer, err := build.VMBackupManager(ctx, cr.Spec.VMBackup, cr.Spec.Port, storagePath, commonMounts, cr.Spec.ExtraArgs, false, cr.Spec.License)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			operatorContainers = append(operatorContainers, *vmBackupManagerContainer)
		}
		if cr.Spec.VMBackup.Restore != nil &&
			cr.Spec.VMBackup.Restore.OnStart != nil &&
			cr.Spec.VMBackup.Restore.OnStart.Enabled {
			vmRestore, err := build.VMRestore(cr.Spec.VMBackup, storagePath, commonMounts)
			if err != nil {
				return nil, err
			}
			if vmRestore != nil {
				initContainers = append(initContainers, *vmRestore)
			}
		}
	}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, initContainers, ptr.Deref(cr.Spec.UseStrictSecurity, false))
	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot apply initContainer patch: %w", err)
	}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.UseStrictSecurity, false))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	vmSingleSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(),
			Annotations: cr.PodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     ic,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}

	return vmSingleSpec, nil
}

func createOrUpdateService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle) (*corev1.Service, error) {

	addExtraPorts := func(svc *corev1.Service, vmb *vmv1beta1.VMBackup) {
		if cr.Spec.Port != "8428" {
			// conditionally add 8428 port to be compatible with binary port
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "http-alias",
				Protocol:   "TCP",
				Port:       8428,
				TargetPort: intstr.Parse(cr.Spec.Port),
			})
		}

		if vmb != nil {
			parsedPort := intstr.Parse(vmb.Port)
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "vmbackupmanager",
				Protocol:   corev1.ProtocolTCP,
				Port:       parsedPort.IntVal,
				TargetPort: parsedPort,
			})
		}
	}
	newService := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		addExtraPorts(svc, cr.Spec.VMBackup)
		build.AppendInsertPortsToService(cr.Spec.InsertPorts, svc)
	})

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil {
		prevService = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			addExtraPorts(svc, prevCR.Spec.VMBackup)
			build.AppendInsertPortsToService(prevCR.Spec.InsertPorts, svc)
		})
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.ServiceSpec)
	}
	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vmsingle additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmsingle: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmsingle: %w", err)
	}
	return newService, nil
}

// buildStreamAggrConfig build configmap with stream aggregation config for vmsingle.
func buildStreamAggrConfig(cr *vmv1beta1.VMSingle, ac *build.AssetsCache) (*corev1.ConfigMap, error) {
	cfgCM := &corev1.ConfigMap{
		ObjectMeta: build.ResourceMeta(build.StreamAggrConfigResourceKind, cr),
		Data:       make(map[string]string),
	}
	if len(cr.Spec.StreamAggrConfig.Rules) > 0 {
		data, err := yaml.Marshal(cr.Spec.StreamAggrConfig.Rules)
		if err != nil {
			return nil, fmt.Errorf("cannot serialize relabelConfig as yaml: %w", err)
		}
		if len(data) > 0 {
			cfgCM.Data[streamAggrSecretKey] = string(data)
		}
	}
	if cr.Spec.StreamAggrConfig.RuleConfigMap != nil {
		data, err := ac.LoadKeyFromConfigMap(cr.Namespace, cr.Spec.StreamAggrConfig.RuleConfigMap)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", cr.Spec.StreamAggrConfig.RuleConfigMap.Name, err)
		}
		if len(data) > 0 {
			cfgCM.Data[streamAggrSecretKey] += data
		}
	}
	return cfgCM, nil
}

// createOrUpdateStreamAggrConfig builds stream aggregation configs for vmsingle at separate configmap, serialized as yaml
func createOrUpdateStreamAggrConfig(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMSingle, ac *build.AssetsCache) error {
	if !cr.HasAnyStreamAggrRule() {
		return nil
	}
	streamAggrCM, err := buildStreamAggrConfig(cr, ac)
	if err != nil {
		return err
	}
	var prevCMMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevCMMeta = ptr.To(build.ResourceMeta(build.StreamAggrConfigResourceKind, prevCR))
	}
	_, err = reconcile.ConfigMap(ctx, rclient, streamAggrCM, prevCMMeta)
	return err
}

func deleteOrphaned(ctx context.Context, rclient client.Client, cr *vmv1beta1.VMSingle) error {
	// TODO check storage for nil
	// TODO check for stream aggr removed

	owner := cr.AsOwner()
	objMeta := metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}
	svcName := cr.PrefixedName()
	keepServices := map[string]struct{}{
		svcName: {},
	}
	keepServiceScrapes := make(map[string]struct{})
	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		keepServiceScrapes[svcName] = struct{}{}
	}
	if cr.Spec.ServiceSpec != nil && !cr.Spec.ServiceSpec.UseAsDefault {
		extraSvcName := cr.Spec.ServiceSpec.NameOrDefault(svcName)
		keepServices[extraSvcName] = struct{}{}
	}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, cr, keepServices); err != nil {
		return fmt.Errorf("cannot remove services: %w", err)
	}
	if err := finalize.RemoveOrphanedVMServiceScrapes(ctx, rclient, cr, keepServiceScrapes); err != nil {
		return fmt.Errorf("cannot remove serviceScrapes: %w", err)
	}
	if !cr.IsOwnsServiceAccount() {
		if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.ServiceAccount{ObjectMeta: objMeta}, &owner); err != nil {
			return fmt.Errorf("cannot remove serviceaccount: %w", err)
		}
	}
	return nil
}

func getAssetsCache(ctx context.Context, rclient client.Client) *build.AssetsCache {
	cfg := map[build.ResourceKind]*build.ResourceCfg{}
	return build.NewAssetsCache(ctx, rclient, cfg)
}
