package vmsingle

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmSingleDataDir     = "/victoria-metrics-data"
	vmDataVolumeName    = "data"
	streamAggrSecretKey = "config.yaml"
)

func createVMSingleStorage(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) error {
	l := logger.WithContext(ctx).WithValues("pvc_for", "vmsingle")
	ctx = logger.AddToContext(ctx, l)
	newPvc := makeVMSinglePvc(cr)

	return reconcile.PersistentVolumeClaim(ctx, rclient, newPvc)
}

func makeVMSinglePvc(cr *vmv1beta1.VMSingle) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          labels.Merge(cr.Spec.StorageMetadata.Labels, cr.SelectorLabels()),
			Annotations:     cr.Spec.StorageMetadata.Annotations,
			Finalizers:      []string{vmv1beta1.FinalizerName},
			OwnerReferences: cr.AsOwner(),
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

// CreateOrUpdateVMSingle performs an update for single node resource
func CreateOrUpdateVMSingle(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) error {

	if err := deletePrevStateResources(ctx, cr, rclient); err != nil {
		return fmt.Errorf("cannot delete objects from prev state: %w", err)
	}
	if cr.IsOwnsServiceAccount() {
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr)); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	if cr.Spec.Storage != nil && cr.Spec.StorageDataPath == "" {
		if err := createVMSingleStorage(ctx, cr, rclient); err != nil {
			return fmt.Errorf("cannot create storage: %w", err)
		}
	}
	svc, err := createOrUpdateVMSingleService(ctx, cr, rclient)
	if err != nil {
		return err
	}

	if !ptr.Deref(cr.Spec.DisableSelfServiceScrape, false) {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr))
		if err != nil {
			return fmt.Errorf("cannot create serviceScrape for vmsingle: %w", err)
		}
	}
	var prevDeploy *appsv1.Deployment
	if cr.ParsedLastAppliedSpec != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
		prevDeploy, err = newDeployForVMSingle(ctx, prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeploy, err := newDeployForVMSingle(ctx, cr)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vmsingle: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, false)
}

func newDeployForVMSingle(ctx context.Context, cr *vmv1beta1.VMSingle) (*appsv1.Deployment, error) {

	podSpec, err := makeSpecForVMSingle(ctx, cr)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
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

func makeSpecForVMSingle(ctx context.Context, cr *vmv1beta1.VMSingle) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-retentionPeriod=%s", cr.Spec.RetentionPeriod),
	}

	// if customStorageDataPath is not empty, do not add pvc.
	shouldAddPVC := cr.Spec.StorageDataPath == ""

	storagePath := vmSingleDataDir
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

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.Port))
	if len(cr.Spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	args = build.AppendArgsForInsertPorts(args, cr.Spec.InsertPorts)

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.Port).IntVal})
	ports = build.AppendInsertPorts(ports, cr.Spec.InsertPorts)
	volumes := []corev1.Volume{}

	storageSpec := cr.Spec.Storage

	if storageSpec == nil {
		volumes = append(volumes, corev1.Volume{
			Name: vmDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else if shouldAddPVC {
		volumes = append(volumes, corev1.Volume{
			Name: vmDataVolumeName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: cr.PrefixedName(),
				},
			},
		})
	}
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
	volumes = append(volumes, cr.Spec.Volumes...)
	vmMounts := []corev1.VolumeMount{
		{
			Name:      vmDataVolumeName,
			MountPath: storagePath,
		},
	}

	vmMounts = append(vmMounts, cr.Spec.VolumeMounts...)

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

	if cr.HasAnyStreamAggrRule() {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("stream-aggr-conf"),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: cr.StreamAggrConfigName(),
					},
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      k8stools.SanitizeVolumeName("stream-aggr-conf"),
			ReadOnly:  true,
			MountPath: vmv1beta1.StreamAggrConfigDir,
		})

		args = append(args, fmt.Sprintf("--streamAggr.config=%s", path.Join(vmv1beta1.StreamAggrConfigDir, streamAggrSecretKey)))
		if cr.Spec.StreamAggrConfig.KeepInput {
			args = append(args, "--streamAggr.keepInput=true")
		}
		if cr.Spec.StreamAggrConfig.DropInput {
			args = append(args, "--streamAggr.dropInput=true")
		}
		if len(cr.Spec.StreamAggrConfig.DropInputLabels) > 0 {
			args = append(args, fmt.Sprintf("--streamAggr.dropInputLabels=%s", strings.Join(cr.Spec.StreamAggrConfig.DropInputLabels, ",")))
		}
		if cr.Spec.StreamAggrConfig.IgnoreFirstIntervals > 0 {
			args = append(args, fmt.Sprintf("--streamAggr.ignoreFirstIntervals=%d", cr.Spec.StreamAggrConfig.IgnoreFirstIntervals))
		}
		if cr.Spec.StreamAggrConfig.IgnoreOldSamples {
			args = append(args, "--streamAggr.ignoreOldSamples=true")
		}
	}

	// deduplication can work without stream aggregation rules
	if cr.Spec.StreamAggrConfig != nil && cr.Spec.StreamAggrConfig.DedupInterval != "" {
		args = append(args, fmt.Sprintf("--streamAggr.dedupInterval=%s", cr.Spec.StreamAggrConfig.DedupInterval))
	}

	volumes, vmMounts = cr.Spec.License.MaybeAddToVolumes(volumes, vmMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

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
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}

	vmsingleContainer = build.Probe(vmsingleContainer, cr)

	operatorContainers := []corev1.Container{vmsingleContainer}
	var initContainers []corev1.Container

	if cr.Spec.VMBackup != nil {
		vmBackupManagerContainer, err := build.VMBackupManager(ctx, cr.Spec.VMBackup, cr.Spec.Port, storagePath, vmDataVolumeName, cr.Spec.ExtraArgs, false, cr.Spec.License)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			operatorContainers = append(operatorContainers, *vmBackupManagerContainer)
		}
		if cr.Spec.VMBackup.Restore != nil &&
			cr.Spec.VMBackup.Restore.OnStart != nil &&
			cr.Spec.VMBackup.Restore.OnStart.Enabled {
			vmRestore, err := build.VMRestore(cr.Spec.VMBackup, storagePath, vmDataVolumeName)
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

func createOrUpdateVMSingleService(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) (*corev1.Service, error) {

	addBackupPort := func(svc *corev1.Service, vmb *vmv1beta1.VMBackup) {
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
		addBackupPort(svc, cr.Spec.VMBackup)
		build.AppendInsertPortsToService(cr.Spec.InsertPorts, svc)
	})

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			logger.WithContext(ctx).Error(fmt.Errorf("vmsingle additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if err := reconcile.Service(ctx, rclient, additionalService, nil); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmsingle: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var prevService *corev1.Service
	if cr.ParsedLastAppliedSpec != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
		prevService = build.Service(prevCR, prevCR.Spec.Port, func(svc *corev1.Service) {
			addBackupPort(svc, prevCR.Spec.VMBackup)
			build.AppendInsertPortsToService(prevCR.Spec.InsertPorts, svc)

		})
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmsingle: %w", err)
	}
	return newService, nil
}

// buildVMSingleStreamAggrConfig build configmap with stream aggregation config for vmsingle.
func buildVMSingleStreamAggrConfig(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) (*corev1.ConfigMap, error) {
	cfgCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       cr.Namespace,
			Name:            cr.StreamAggrConfigName(),
			Labels:          cr.AllLabels(),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
		},
		Data: make(map[string]string),
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
		data, err := k8stools.FetchConfigMapContentByKey(ctx, rclient,
			&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: cr.Spec.StreamAggrConfig.RuleConfigMap.Name, Namespace: cr.Namespace}},
			cr.Spec.StreamAggrConfig.RuleConfigMap.Key)
		if err != nil {
			return nil, fmt.Errorf("cannot fetch configmap: %s, err: %w", cr.Spec.StreamAggrConfig.RuleConfigMap.Name, err)
		}
		if len(data) > 0 {
			cfgCM.Data[streamAggrSecretKey] += data
		}
	}
	return cfgCM, nil
}

// CreateOrUpdateVMSingleStreamAggrConfig builds stream aggregation configs for vmsingle at separate configmap, serialized as yaml
func CreateOrUpdateVMSingleStreamAggrConfig(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) error {
	if !cr.HasAnyStreamAggrRule() {
		return nil
	}
	streamAggrCM, err := buildVMSingleStreamAggrConfig(ctx, cr, rclient)
	if err != nil {
		return err
	}
	return reconcile.ConfigMap(ctx, rclient, streamAggrCM)
}

func deletePrevStateResources(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) error {
	if cr.ParsedLastAppliedSpec == nil {
		return nil
	}
	// TODO check storage for nil
	// TODO check for stream aggr removed

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
