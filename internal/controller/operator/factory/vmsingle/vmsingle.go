package vmsingle

import (
	"context"
	"fmt"
	"path"
	"sort"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/logger"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	"gopkg.in/yaml.v2"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmSingleDataDir  = "/victoria-metrics-data"
	vmDataVolumeName = "data"
)

// CreateVMSingleStorage creates persistent volume for vmsingle
func CreateVMSingleStorage(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) error {
	l := logger.WithContext(ctx).WithValues("vm.single.pvc.create", cr.Name)
	ctx = logger.AddToContext(ctx, l)
	newPvc := makeVMSinglePvc(cr)
	existPvc := &corev1.PersistentVolumeClaim{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}, existPvc)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new pvc for vmsingle")
			if err := rclient.Create(ctx, newPvc); err != nil {
				return fmt.Errorf("cannot create new pvc for vmsingle: %w", err)
			}
			return nil
		}
		return fmt.Errorf("cannot get existing pvc for vmsingle: %w", err)
	}
	if !existPvc.DeletionTimestamp.IsZero() {
		l.Info("pvc has non zero DeletionTimestamp, skip update. To fix this, make backup for this pvc, delete VMSingle object and restore from backup.", "vmsingle", cr.Name, "namespace", cr.Namespace, "pvc", existPvc.Name)
		return nil
	}
	if existPvc.Spec.Resources.String() != newPvc.Spec.Resources.String() {
		l.Info("volume requests isn't same, update required")
	}
	newResources := newPvc.Spec.Resources.DeepCopy()
	newPvc.Spec = existPvc.Spec
	newPvc.Spec.Resources = *newResources
	newPvc.Annotations = labels.Merge(existPvc.Annotations, newPvc.Annotations)
	vmv1beta1.AddFinalizer(newPvc, existPvc)

	if err := rclient.Update(ctx, newPvc); err != nil {
		return err
	}

	return nil
}

func makeVMSinglePvc(cr *vmv1beta1.VMSingle) *corev1.PersistentVolumeClaim {
	pvcObject := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:        cr.PrefixedName(),
			Namespace:   cr.Namespace,
			Labels:      labels.Merge(cr.Spec.StorageMetadata.Labels, cr.SelectorLabels()),
			Annotations: cr.Spec.StorageMetadata.Annotations,
			Finalizers:  []string{vmv1beta1.FinalizerName},
		},
		Spec: *cr.Spec.Storage,
	}
	if cr.Spec.RemovePvcAfterDelete {
		pvcObject.OwnerReferences = cr.AsOwner()
	}
	return pvcObject
}

// CreateOrUpdateVMSingle performs an update for single node resource
func CreateOrUpdateVMSingle(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client, c *config.BaseOperatorConf) error {
	cr = cr.DeepCopy()
	if cr.Spec.Image.Repository == "" {
		cr.Spec.Image.Repository = c.VMSingleDefault.Image
	}
	if cr.Spec.Image.Tag == "" {
		cr.Spec.Image.Tag = c.VMSingleDefault.Version
	}
	if cr.Spec.Port == "" {
		cr.Spec.Port = c.VMSingleDefault.Port
	}
	if cr.Spec.Image.PullPolicy == "" {
		cr.Spec.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if cr.IsOwnsServiceAccount() {
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr)); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	svc, err := CreateOrUpdateVMSingleService(ctx, cr, rclient, c)
	if err != nil {
		return err
	}

	if !c.DisableSelfServiceScrapeCreation {
		err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(svc, cr.Spec.ServiceScrapeSpec, cr.MetricPath()))
		if err != nil {
			return fmt.Errorf("cannot create serviceScrape for vmsingle: %w", err)
		}
	}
	newDeploy, err := newDeployForVMSingle(ctx, cr, c)
	if err != nil {
		return fmt.Errorf("cannot generate new deploy for vmsingle: %w", err)
	}

	return reconcile.Deployment(ctx, rclient, newDeploy, c.PodWaitReadyTimeout, false)
}

func newDeployForVMSingle(ctx context.Context, cr *vmv1beta1.VMSingle, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {
	podSpec, err := makeSpecForVMSingle(ctx, cr, c)
	if err != nil {
		return nil, err
	}

	depSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.PrefixedName(),
			Namespace:       cr.Namespace,
			Labels:          c.Labels.Merge(cr.AllLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:             cr.Spec.ReplicaCount,
			RevisionHistoryLimit: cr.Spec.RevisionHistoryLimitCount,
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
	return depSpec, nil
}

func makeSpecForVMSingle(ctx context.Context, cr *vmv1beta1.VMSingle, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
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

	if cr.HasStreamAggrConfig() {
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

		args = append(args, fmt.Sprintf("--streamAggr.config=%s", path.Join(vmv1beta1.StreamAggrConfigDir, "config.yaml")))
		if cr.Spec.StreamAggrConfig.KeepInput {
			args = append(args, "--streamAggr.keepInput=true")
		}
		if cr.Spec.StreamAggrConfig.DedupInterval != "" {
			args = append(args, fmt.Sprintf("--streamAggr.dedupInterval=%s", cr.Spec.StreamAggrConfig.DedupInterval))
		}
	}
	volumes, vmMounts = cr.Spec.License.MaybeAddToVolumes(volumes, vmMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.ExtraArgs, "-")
	sort.Strings(args)
	vmsingleContainer := corev1.Container{
		Name:                     "vmsingle",
		Image:                    fmt.Sprintf("%s:%s", build.FormatContainerImage(c.ContainerRegistry, cr.Spec.Image.Repository), cr.Spec.Image.Tag),
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                build.Resources(cr.Spec.Resources, config.Resource(c.VMSingleDefault.Resource), c.VMSingleDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		ImagePullPolicy:          cr.Spec.Image.PullPolicy,
	}

	vmsingleContainer = build.Probe(vmsingleContainer, cr)

	operatorContainers := []corev1.Container{vmsingleContainer}
	initContainers := cr.Spec.InitContainers

	if cr.Spec.VMBackup != nil {
		vmBackupManagerContainer, err := build.VMBackupManager(ctx, cr.Spec.VMBackup, c, cr.Spec.Port, storagePath, vmDataVolumeName, cr.Spec.ExtraArgs, false, cr.Spec.License)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			operatorContainers = append(operatorContainers, *vmBackupManagerContainer)
		}
		if cr.Spec.VMBackup.Restore != nil &&
			cr.Spec.VMBackup.Restore.OnStart != nil &&
			cr.Spec.VMBackup.Restore.OnStart.Enabled {
			vmRestore, err := build.VMRestore(cr.Spec.VMBackup, c, storagePath, vmDataVolumeName)
			if err != nil {
				return nil, err
			}
			if vmRestore != nil {
				initContainers = append(initContainers, *vmRestore)
			}
		}
	}

	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.Containers)
	if err != nil {
		return nil, err
	}

	useStrictSecurity := c.EnableStrictSecurity
	if cr.Spec.UseStrictSecurity != nil {
		useStrictSecurity = *cr.Spec.UseStrictSecurity
	}
	vmSingleSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(),
			Annotations: cr.PodAnnotations(),
		},
		Spec: corev1.PodSpec{
			NodeSelector:                  cr.Spec.NodeSelector,
			Volumes:                       volumes,
			InitContainers:                build.AddStrictSecuritySettingsToContainers(initContainers, useStrictSecurity),
			Containers:                    build.AddStrictSecuritySettingsToContainers(containers, useStrictSecurity),
			ServiceAccountName:            cr.GetServiceAccountName(),
			SecurityContext:               build.AddStrictSecuritySettingsToPod(cr.Spec.SecurityContext, useStrictSecurity),
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.Affinity,
			RuntimeClassName:              cr.Spec.RuntimeClassName,
			SchedulerName:                 cr.Spec.SchedulerName,
			Tolerations:                   cr.Spec.Tolerations,
			PriorityClassName:             cr.Spec.PriorityClassName,
			HostNetwork:                   cr.Spec.HostNetwork,
			DNSPolicy:                     cr.Spec.DNSPolicy,
			DNSConfig:                     cr.Spec.DNSConfig,
			TopologySpreadConstraints:     cr.Spec.TopologySpreadConstraints,
			HostAliases:                   cr.Spec.HostAliases,
			TerminationGracePeriodSeconds: cr.Spec.TerminationGracePeriodSeconds,
			ReadinessGates:                cr.Spec.ReadinessGates,
		},
	}

	return vmSingleSpec, nil
}

// CreateOrUpdateVMSingleService creates service for vmsingle
func CreateOrUpdateVMSingleService(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	addBackupPort := func(svc *corev1.Service) {
		if cr.Spec.VMBackup != nil {
			if cr.Spec.VMBackup.Port == "" {
				cr.Spec.VMBackup.Port = c.VMBackup.Port
			}
			parsedPort := intstr.Parse(cr.Spec.VMBackup.Port)
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "vmbackupmanager",
				Protocol:   corev1.ProtocolTCP,
				Port:       parsedPort.IntVal,
				TargetPort: parsedPort,
			})
		}
	}
	newService := build.Service(cr, cr.Spec.Port, func(svc *corev1.Service) {
		addBackupPort(svc)
		build.AppendInsertPortsToService(cr.Spec.InsertPorts, svc)
	})

	if err := cr.Spec.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			logger.WithContext(ctx).Error(fmt.Errorf("vmsingle additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if err := reconcile.ServiceForCRD(ctx, rclient, additionalService); err != nil {
			return fmt.Errorf("cannot reconcile additional service for vmsingle: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.SelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: cr.PrefixedName}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.ServiceSpec); err != nil {
		return nil, err
	}

	if err := reconcile.ServiceForCRD(ctx, rclient, newService); err != nil {
		return nil, fmt.Errorf("cannot reconcile service for vmsingle: %w", err)
	}
	return newService, nil
}

// buildVMSingleStreamAggrConfig build configmap with stream aggregation config for vmsingle.
func buildVMSingleStreamAggrConfig(cr *vmv1beta1.VMSingle) (*corev1.ConfigMap, error) {
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
	data, err := yaml.Marshal(cr.Spec.StreamAggrConfig.Rules)
	if err != nil {
		return nil, fmt.Errorf("cannot serialize StreamAggrConfig rules as yaml: %w", err)
	}
	if len(data) > 0 {
		cfgCM.Data["config.yaml"] = string(data)
	}

	return cfgCM, nil
}

// CreateOrUpdateVMSingleStreamAggrConfig builds stream aggregation configs for vmsingle at separate configmap, serialized as yaml
func CreateOrUpdateVMSingleStreamAggrConfig(ctx context.Context, cr *vmv1beta1.VMSingle, rclient client.Client) error {
	if !cr.HasStreamAggrConfig() {
		return nil
	}
	streamAggrCM, err := buildVMSingleStreamAggrConfig(cr)
	if err != nil {
		return err
	}
	var existCM corev1.ConfigMap
	if err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.StreamAggrConfigName()}, &existCM); err != nil {
		if errors.IsNotFound(err) {
			return rclient.Create(ctx, streamAggrCM)
		}
		return fmt.Errorf("cannot fetch exist configmap for vmsingle streamAggr: %w", err)
	}
	if err := finalize.FreeIfNeeded(ctx, rclient, &existCM); err != nil {
		return err
	}
	streamAggrCM.Annotations = labels.Merge(existCM.Annotations, streamAggrCM.Annotations)
	vmv1beta1.AddFinalizer(streamAggrCM, &existCM)
	return rclient.Update(ctx, streamAggrCM)
}
