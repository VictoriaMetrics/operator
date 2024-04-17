package vmcluster

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/build"
	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/logger"
	"github.com/VictoriaMetrics/operator/controllers/factory/reconcile"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/autoscaling/v2beta2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmStorageDefaultDBPath = "vmstorage-data"
)

var defaultTerminationGracePeriod = int64(30)

// CreateOrUpdateVMCluster reconciled cluster object with order
// first we check status of vmStorage and waiting for its readiness
// then vmSelect and wait for it readiness as well
// and last one is vmInsert
// we manually handle statefulsets rolling updates
// needed in update checked by revesion status
// its controlled by k8s controller-manager
func CreateOrUpdateVMCluster(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) error {
	if cr.IsOwnsServiceAccount() {
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr)); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	if cr.Spec.VMStorage != nil {
		if cr.Spec.VMStorage.PodDisruptionBudget != nil {
			err := createOrUpdatePodDisruptionBudgetForVMStorage(ctx, cr, rclient)
			if err != nil {
				return err
			}
		}
		if err := createOrUpdateVMStorage(ctx, cr, rclient, c); err != nil {
			return err
		}

		storageSvc, err := createOrUpdateVMStorageService(ctx, cr, rclient, c)
		if err != nil {
			return err
		}
		if !c.DisableSelfServiceScrapeCreation {
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(storageSvc, cr.Spec.VMStorage.ServiceScrapeSpec, cr.MetricPathStorage(), "http"))
			if err != nil {
				logger.WithContext(ctx).Error(err, "cannot create VMServiceScrape for vmStorage")
			}
		}
	}

	if cr.Spec.VMSelect != nil {
		if cr.Spec.VMSelect.PodDisruptionBudget != nil {
			if err := createOrUpdatePodDisruptionBudgetForVMSelect(ctx, cr, rclient); err != nil {
				return err
			}
		}
		if err := createOrUpdateVMSelect(ctx, cr, rclient, c); err != nil {
			return err
		}

		if err := createOrUpdateVMSelectHPA(ctx, rclient, cr); err != nil {
			return err
		}
		// create vmselect service
		selectSvc, err := createOrUpdateVMSelectService(ctx, cr, rclient, c)
		if err != nil {
			return err
		}
		if !c.DisableSelfServiceScrapeCreation {
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(selectSvc, cr.Spec.VMSelect.ServiceScrapeSpec, cr.MetricPathSelect(), "http"))
			if err != nil {
				logger.WithContext(ctx).Error(err, "cannot create VMServiceScrape for vmSelect")
			}
		}
	}

	if cr.Spec.VMInsert != nil {
		if cr.Spec.VMInsert.PodDisruptionBudget != nil {
			if err := createOrUpdatePodDisruptionBudgetForVMInsert(ctx, cr, rclient); err != nil {
				return err
			}
		}
		if err := createOrUpdateVMInsert(ctx, cr, rclient, c); err != nil {
			return err
		}
		insertSvc, err := createOrUpdateVMInsertService(ctx, cr, rclient, c)
		if err != nil {
			return err
		}
		if err := createOrUpdateVMInsertHPA(ctx, rclient, cr); err != nil {
			return err
		}
		if !c.DisableSelfServiceScrapeCreation {
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(insertSvc, cr.Spec.VMInsert.ServiceScrapeSpec, cr.MetricPathInsert(), "http"))
			if err != nil {
				logger.WithContext(ctx).Error(err, "cannot create VMServiceScrape for vmInsert")
			}
		}

	}
	return nil
}

func createOrUpdateVMSelect(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) error {
	// its tricky part.
	// we need replicas count from hpa to create proper args.
	// note, need to make copy of current crd. to able to change it without side effects.
	cr = cr.DeepCopy()

	newSts, err := genVMSelectSpec(cr, c)
	if err != nil {
		return err
	}

	stsOpts := reconcile.STSOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		VolumeName:     cr.Spec.VMSelect.GetCacheMountVolumeName,
		SelectorLabels: cr.VMSelectSelectorLabels,
		UpdateStrategy: cr.Spec.VMSelect.UpdateStrategy,
		HPA:            cr.Spec.VMSelect.HPA,
		UpdateReplicaCount: func(count *int32) {
			if cr.Spec.VMSelect.HPA != nil && count != nil {
				cr.Spec.VMSelect.ReplicaCount = count
			}
		},
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, c)
}

func createOrUpdateVMSelectService(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	cr = cr.DeepCopy()
	if cr.Spec.VMSelect.Port == "" {
		cr.Spec.VMSelect.Port = c.VMClusterDefault.VMSelectDefault.Port
	}

	t := &clusterSvcBuilder{
		cr,
		cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
		cr.FinalLabels(cr.VMSelectSelectorLabels()),
		cr.VMSelectSelectorLabels(),
		cr.Spec.VMSelect.ServiceSpec,
	}
	newHeadless := build.Service(t, cr.Spec.VMSelect.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		if cr.Spec.VMSelect.ClusterNativePort != "" {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "clusternative",
				Protocol:   "TCP",
				Port:       intstr.Parse(cr.Spec.VMSelect.ClusterNativePort).IntVal,
				TargetPort: intstr.Parse(cr.Spec.VMSelect.ClusterNativePort),
			})
		}
	})

	if err := cr.Spec.VMSelect.ServiceSpec.IsSomeAndThen(func(s *victoriametricsv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newHeadless, s)
		if additionalService.Name == newHeadless.Name {
			return fmt.Errorf("vmselect additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newHeadless.Name)
		} else if err := reconcile.ServiceForCRD(ctx, rclient, additionalService); err != nil {
			return fmt.Errorf("cannot reconcile service for vmselect: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.VMSelectSelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: func() string {
		return cr.Spec.VMSelect.GetNameWithPrefix(cr.Name)
	}}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.VMSelect.ServiceSpec); err != nil {
		return nil, err
	}

	if err := reconcile.ServiceForCRD(ctx, rclient, newHeadless); err != nil {
		return nil, fmt.Errorf("cannot reconcile vmselect service: %w", err)
	}
	return newHeadless, nil
}

func createOrUpdateVMInsert(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) error {
	newDeployment, err := genVMInsertSpec(cr, c)
	if err != nil {
		return err
	}

	currentDeployment := &appsv1.Deployment{}
	err = rclient.Get(ctx, types.NamespacedName{Name: newDeployment.Name, Namespace: newDeployment.Namespace}, currentDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := rclient.Create(ctx, newDeployment); err != nil {
				return fmt.Errorf("cannot create new vminsert deploy: %w", err)
			}
			return nil
		}
		return fmt.Errorf("cannot get vminsert deploy: %w", err)
	}

	// inherit replicas count if hpa enabled.
	if cr.Spec.VMInsert.HPA != nil {
		newDeployment.Spec.Replicas = currentDeployment.Spec.Replicas
	}

	newDeployment.Annotations = labels.Merge(currentDeployment.Annotations, newDeployment.Annotations)
	newDeployment.Finalizers = victoriametricsv1beta1.MergeFinalizers(newDeployment, victoriametricsv1beta1.FinalizerName)
	if err = rclient.Update(ctx, newDeployment); err != nil {
		return fmt.Errorf("cannot update vminsert deploy: %w", err)
	}

	return nil
}

func createOrUpdateVMInsertService(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	cr = cr.DeepCopy()
	if cr.Spec.VMInsert.Port == "" {
		cr.Spec.VMInsert.Port = c.VMClusterDefault.VMInsertDefault.Port
	}
	t := &clusterSvcBuilder{
		cr,
		cr.Spec.VMInsert.GetNameWithPrefix(cr.Name),
		cr.FinalLabels(cr.VMInsertSelectorLabels()),
		cr.VMInsertSelectorLabels(),
		cr.Spec.VMInsert.ServiceSpec,
	}

	newService := build.Service(t, cr.Spec.VMInsert.Port, func(svc *corev1.Service) {
		build.AppendInsertPortsToService(cr.Spec.VMInsert.InsertPorts, svc)
		if cr.Spec.VMInsert.ClusterNativePort != "" {
			svc.Spec.Ports = append(svc.Spec.Ports,
				corev1.ServicePort{
					Name:       "clusternative",
					Protocol:   "TCP",
					Port:       intstr.Parse(cr.Spec.VMInsert.ClusterNativePort).IntVal,
					TargetPort: intstr.Parse(cr.Spec.VMInsert.ClusterNativePort),
				})
		}
	})

	if err := cr.Spec.VMInsert.ServiceSpec.IsSomeAndThen(func(s *victoriametricsv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vminsert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		} else if err := reconcile.ServiceForCRD(ctx, rclient, additionalService); err != nil {
			return fmt.Errorf("cannot reconcile vminsert additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.VMInsertSelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: func() string {
		return cr.Spec.VMInsert.GetNameWithPrefix(cr.Name)
	}}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.VMInsert.ServiceSpec); err != nil {
		return nil, err
	}

	if err := reconcile.ServiceForCRD(ctx, rclient, newService); err != nil {
		return nil, fmt.Errorf("cannot reconcile vminsert service: %w", err)
	}
	return newService, nil
}

func createOrUpdateVMStorage(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) error {
	newSts, err := buildVMStorageSpec(ctx, cr, c)
	if err != nil {
		return err
	}

	stsOpts := reconcile.STSOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		VolumeName:     cr.Spec.VMStorage.GetStorageVolumeName,
		SelectorLabels: cr.VMStorageSelectorLabels,
		UpdateStrategy: cr.Spec.VMStorage.UpdateStrategy,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, c)
}

func createOrUpdateVMStorageService(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	if cr.Spec.VMStorage.Port == "" {
		cr.Spec.VMStorage.Port = c.VMClusterDefault.VMStorageDefault.Port
	}
	if cr.Spec.VMStorage.VMSelectPort == "" {
		cr.Spec.VMStorage.VMSelectPort = c.VMClusterDefault.VMStorageDefault.VMSelectPort
	}
	if cr.Spec.VMStorage.VMInsertPort == "" {
		cr.Spec.VMStorage.VMInsertPort = c.VMClusterDefault.VMStorageDefault.VMInsertPort
	}
	t := &clusterSvcBuilder{
		cr,
		cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
		cr.FinalLabels(cr.VMStorageSelectorLabels()),
		cr.VMStorageSelectorLabels(),
		cr.Spec.VMStorage.ServiceSpec,
	}
	newHeadless := build.Service(t, cr.Spec.VMStorage.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.Ports = append(svc.Spec.Ports, []corev1.ServicePort{
			{
				Name:       "vminsert",
				Protocol:   "TCP",
				Port:       intstr.Parse(cr.Spec.VMStorage.VMInsertPort).IntVal,
				TargetPort: intstr.Parse(cr.Spec.VMStorage.VMInsertPort),
			},
			{
				Name:       "vmselect",
				Protocol:   "TCP",
				Port:       intstr.Parse(cr.Spec.VMStorage.VMSelectPort).IntVal,
				TargetPort: intstr.Parse(cr.Spec.VMStorage.VMSelectPort),
			},
		}...)
		if cr.Spec.VMStorage.VMBackup != nil {
			backupPort := cr.Spec.VMStorage.VMBackup.Port
			if backupPort == "" {
				backupPort = c.VMBackup.Port
			}
			parsedPort := intstr.Parse(backupPort)
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "vmbackupmanager",
				Protocol:   corev1.ProtocolTCP,
				Port:       parsedPort.IntVal,
				TargetPort: parsedPort,
			})
		}
	})

	if err := cr.Spec.VMStorage.ServiceSpec.IsSomeAndThen(func(s *victoriametricsv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newHeadless, s)
		if additionalService.Name == newHeadless.Name {
			return fmt.Errorf("vmstorage additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newHeadless.Name)
		} else if err := reconcile.ServiceForCRD(ctx, rclient, additionalService); err != nil {
			return fmt.Errorf("cannot reconcile vmstorage additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.VMStorageSelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: func() string {
		return cr.Spec.VMStorage.GetNameWithPrefix(cr.Name)
	}}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.VMStorage.ServiceSpec); err != nil {
		return nil, err
	}
	if err := reconcile.ServiceForCRD(ctx, rclient, newHeadless); err != nil {
		return nil, fmt.Errorf("cannot reconcile vmstorage service: %w", err)
	}
	return newHeadless, nil
}

func genVMSelectSpec(cr *victoriametricsv1beta1.VMCluster, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
	cr = cr.DeepCopy()
	if cr.Spec.VMSelect.Image.Repository == "" {
		cr.Spec.VMSelect.Image.Repository = c.VMClusterDefault.VMSelectDefault.Image
	}
	if cr.Spec.VMSelect.Image.Tag == "" {
		if cr.Spec.ClusterVersion != "" {
			cr.Spec.VMSelect.Image.Tag = cr.Spec.ClusterVersion
		} else {
			cr.Spec.VMSelect.Image.Tag = c.VMClusterDefault.VMSelectDefault.Version
		}
	}
	if cr.Spec.VMSelect.Port == "" {
		cr.Spec.VMSelect.Port = c.VMClusterDefault.VMSelectDefault.Port
	}

	if cr.Spec.VMSelect.DNSPolicy == "" {
		cr.Spec.VMSelect.DNSPolicy = corev1.DNSClusterFirst
	}
	if cr.Spec.VMSelect.SchedulerName == "" {
		cr.Spec.VMSelect.SchedulerName = "default-scheduler"
	}
	if cr.Spec.VMSelect.Image.PullPolicy == "" {
		cr.Spec.VMSelect.Image.PullPolicy = corev1.PullIfNotPresent
	}
	// use "/cache" as default cache dir instead of "/tmp" if `CacheMountPath` not set
	if cr.Spec.VMSelect.CacheMountPath == "" {
		cr.Spec.VMSelect.CacheMountPath = "/cache"
	}
	podSpec, err := makePodSpecForVMSelect(cr, c)
	if err != nil {
		return nil, err
	}
	podMP := appsv1.ParallelPodManagement
	if cr.Spec.VMSelect.MinReadySeconds > 0 {
		podMP = appsv1.OrderedReadyPodManagement
	}
	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMSelectSelectorLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: cr.Spec.VMSelect.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMSelectSelectorLabels(),
			},
			PodManagementPolicy: podMP,
			MinReadySeconds:     cr.Spec.VMSelect.MinReadySeconds,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VMSelect.UpdateStrategy(),
			},
			Template:             *podSpec,
			ServiceName:          cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			RevisionHistoryLimit: cr.Spec.VMSelect.RevisionHistoryLimitCount,
		},
	}
	if cr.Spec.VMSelect.CacheMountPath != "" {
		storageSpec := cr.Spec.VMSelect.Storage
		// hack, storage is deprecated.
		if storageSpec == nil && cr.Spec.VMSelect.StorageSpec != nil {
			storageSpec = cr.Spec.VMSelect.StorageSpec
		}
		storageSpec.IntoSTSVolume(cr.Spec.VMSelect.GetCacheMountVolumeName(), &stsSpec.Spec)
	}
	stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.VMSelect.ClaimTemplates...)
	return stsSpec, nil
}

func makePodSpecForVMSelect(cr *victoriametricsv1beta1.VMCluster, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VMSelect.Port),
	}
	if cr.Spec.VMSelect.ClusterNativePort != "" {
		args = append(args, fmt.Sprintf("-clusternativeListenAddr=:%s", cr.Spec.VMSelect.ClusterNativePort))
	}
	if cr.Spec.VMSelect.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VMSelect.LogLevel))
	}
	if cr.Spec.VMSelect.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VMSelect.LogFormat))
	}
	if cr.Spec.ReplicationFactor != nil && *cr.Spec.ReplicationFactor > 1 {
		var replicationFactorIsSet bool
		var dedupIsSet bool
		for arg := range cr.Spec.VMSelect.ExtraArgs {
			if strings.Contains(arg, "dedup.minScrapeInterval") {
				dedupIsSet = true
			}
			if strings.Contains(arg, "replicationFactor") {
				replicationFactorIsSet = true
			}
		}
		if !dedupIsSet {
			args = append(args, "-dedup.minScrapeInterval=1ms")
		}
		if !replicationFactorIsSet {
			args = append(args, fmt.Sprintf("-replicationFactor=%d", *cr.Spec.ReplicationFactor))
		}
	}

	if cr.Spec.VMStorage != nil && cr.Spec.VMStorage.ReplicaCount != nil {
		if cr.Spec.VMStorage.VMSelectPort == "" {
			cr.Spec.VMStorage.VMSelectPort = c.VMClusterDefault.VMStorageDefault.VMSelectPort
		}
		storageArg := "-storageNode="
		for _, i := range cr.AvailableStorageNodeIDs("select") {
			storageArg += cr.Spec.VMStorage.BuildPodName(cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMStorage.VMSelectPort, c.ClusterDomainName)
		}
		storageArg = strings.TrimSuffix(storageArg, ",")
		args = append(args, storageArg)

	}
	// dd selectNode arg add for deployments without HPA
	// HPA leads to rolling restart for vmselect statefulset in case of replicas count changes
	if cr.Spec.VMSelect.HPA == nil {
		selectArg := "-selectNode="
		vmselectCount := *cr.Spec.VMSelect.ReplicaCount
		for i := int32(0); i < vmselectCount; i++ {
			selectArg += cr.Spec.VMSelect.BuildPodName(cr.Spec.VMSelect.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMSelect.Port, c.ClusterDomainName)
		}
		selectArg = strings.TrimSuffix(selectArg, ",")
		args = append(args, selectArg)
	}

	if len(cr.Spec.VMSelect.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.VMSelect.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.VMSelect.Port).IntVal})
	if cr.Spec.VMSelect.ClusterNativePort != "" {
		ports = append(ports, corev1.ContainerPort{Name: "clusternative", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.VMSelect.ClusterNativePort).IntVal})
	}

	volumes := make([]corev1.Volume, 0)
	volumes = append(volumes, cr.Spec.VMSelect.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)

	if cr.Spec.VMSelect.CacheMountPath != "" {
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      cr.Spec.VMSelect.GetCacheMountVolumeName(),
			MountPath: cr.Spec.VMSelect.CacheMountPath,
		})
		args = append(args, fmt.Sprintf("-cacheDataPath=%s", cr.Spec.VMSelect.CacheMountPath))
	}

	vmMounts = append(vmMounts, cr.Spec.VMSelect.VolumeMounts...)

	for _, s := range cr.Spec.VMSelect.Secrets {
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
			MountPath: path.Join(victoriametricsv1beta1.SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.VMSelect.ConfigMaps {
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
			MountPath: path.Join(victoriametricsv1beta1.ConfigMapsDir, c),
		})
	}

	volumes, vmMounts = cr.Spec.License.MaybeAddToVolumes(volumes, vmMounts, victoriametricsv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, victoriametricsv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VMSelect.ExtraArgs, "-")
	sort.Strings(args)
	vmselectContainer := corev1.Container{
		Name:                     "vmselect",
		Image:                    fmt.Sprintf("%s:%s", build.FormatContainerImage(c.ContainerRegistry, cr.Spec.VMSelect.Image.Repository), cr.Spec.VMSelect.Image.Tag),
		ImagePullPolicy:          cr.Spec.VMSelect.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                build.Resources(cr.Spec.VMSelect.Resources, config.Resource(c.VMClusterDefault.VMSelectDefault.Resource), c.VMClusterDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	vmselectContainer = build.Probe(vmselectContainer, cr.Spec.VMSelect)

	operatorContainers := []corev1.Container{vmselectContainer}

	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VMSelect.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.VMSelect.TopologySpreadConstraints {
		if cr.Spec.VMSelect.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VMSelect.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.VMSelectSelectorLabels(),
			}
		}
	}

	useStrictSecurity := c.EnableStrictSecurity
	if cr.Spec.UseStrictSecurity != nil {
		useStrictSecurity = *cr.Spec.UseStrictSecurity
	}
	vmSelectPodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMSelectPodLabels(),
			Annotations: cr.VMSelectPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			NodeSelector:                  cr.Spec.VMSelect.NodeSelector,
			Volumes:                       volumes,
			InitContainers:                build.AddStrictSecuritySettingsToContainers(cr.Spec.VMSelect.InitContainers, useStrictSecurity),
			Containers:                    build.AddStrictSecuritySettingsToContainers(containers, useStrictSecurity),
			ServiceAccountName:            cr.GetServiceAccountName(),
			SecurityContext:               build.AddStrictSecuritySettingsToPod(cr.Spec.VMSelect.SecurityContext, useStrictSecurity),
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.VMSelect.Affinity,
			SchedulerName:                 cr.Spec.VMSelect.SchedulerName,
			RuntimeClassName:              cr.Spec.VMSelect.RuntimeClassName,
			Tolerations:                   cr.Spec.VMSelect.Tolerations,
			PriorityClassName:             cr.Spec.VMSelect.PriorityClassName,
			HostNetwork:                   cr.Spec.VMSelect.HostNetwork,
			DNSPolicy:                     cr.Spec.VMSelect.DNSPolicy,
			DNSConfig:                     cr.Spec.VMSelect.DNSConfig,
			RestartPolicy:                 "Always",
			TerminationGracePeriodSeconds: cr.Spec.VMSelect.TerminationGracePeriodSeconds,
			TopologySpreadConstraints:     cr.Spec.VMSelect.TopologySpreadConstraints,
			ReadinessGates:                cr.Spec.VMSelect.ReadinessGates,
		},
	}

	return vmSelectPodSpec, nil
}

func createOrUpdatePodDisruptionBudgetForVMSelect(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client) error {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			Labels:          cr.FinalLabels(cr.VMSelectSelectorLabels()),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable:   cr.Spec.VMSelect.PodDisruptionBudget.MinAvailable,
			MaxUnavailable: cr.Spec.VMSelect.PodDisruptionBudget.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.Spec.VMSelect.PodDisruptionBudget.SelectorLabelsWithDefaults(cr.VMSelectSelectorLabels()),
			},
		},
	}
	return reconcile.PDB(ctx, rclient, pdb)
}

func genVMInsertSpec(cr *victoriametricsv1beta1.VMCluster, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {
	cr = cr.DeepCopy()

	if cr.Spec.VMInsert.Image.Repository == "" {
		cr.Spec.VMInsert.Image.Repository = c.VMClusterDefault.VMInsertDefault.Image
	}
	if cr.Spec.VMInsert.Image.Tag == "" {
		if cr.Spec.ClusterVersion != "" {
			cr.Spec.VMInsert.Image.Tag = cr.Spec.ClusterVersion
		} else {
			cr.Spec.VMInsert.Image.Tag = c.VMClusterDefault.VMInsertDefault.Version
		}
	}
	if cr.Spec.VMInsert.Port == "" {
		cr.Spec.VMInsert.Port = c.VMClusterDefault.VMInsertDefault.Port
	}

	podSpec, err := makePodSpecForVMInsert(cr, c)
	if err != nil {
		return nil, err
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.VMInsert.UpdateStrategy != nil {
		strategyType = *cr.Spec.VMInsert.UpdateStrategy
	}
	stsSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMInsert.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMInsertSelectorLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas:             cr.Spec.VMInsert.ReplicaCount,
			RevisionHistoryLimit: cr.Spec.VMInsert.RevisionHistoryLimitCount,
			MinReadySeconds:      cr.Spec.VMInsert.MinReadySeconds,
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.VMInsert.RollingUpdate,
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMInsertSelectorLabels(),
			},
			Template: *podSpec,
		},
	}
	return stsSpec, nil
}

func makePodSpecForVMInsert(cr *victoriametricsv1beta1.VMCluster, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VMInsert.Port),
	}
	if cr.Spec.VMInsert.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VMInsert.LogLevel))
	}
	if cr.Spec.VMInsert.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VMInsert.LogFormat))
	}

	args = build.AppendArgsForInsertPorts(args, cr.Spec.VMInsert.InsertPorts)
	if cr.Spec.VMInsert.ClusterNativePort != "" {
		args = append(args, fmt.Sprintf("--clusternativeListenAddr=:%s", cr.Spec.VMInsert.ClusterNativePort))
	}

	if cr.Spec.VMStorage != nil && cr.Spec.VMStorage.ReplicaCount != nil {
		if cr.Spec.VMStorage.VMInsertPort == "" {
			cr.Spec.VMStorage.VMInsertPort = c.VMClusterDefault.VMStorageDefault.VMInsertPort
		}
		storageArg := "-storageNode="
		for _, i := range cr.AvailableStorageNodeIDs("insert") {
			storageArg += cr.Spec.VMStorage.BuildPodName(cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMStorage.VMInsertPort, c.ClusterDomainName)
		}
		storageArg = strings.TrimSuffix(storageArg, ",")

		args = append(args, storageArg)

	}
	if cr.Spec.ReplicationFactor != nil {
		args = append(args, fmt.Sprintf("-replicationFactor=%d", *cr.Spec.ReplicationFactor))
	}
	if len(cr.Spec.VMInsert.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.VMInsert.ExtraEnvs...)

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.VMInsert.Port).IntVal,
		},
	}
	ports = build.AppendInsertPorts(ports, cr.Spec.VMInsert.InsertPorts)
	if cr.Spec.VMInsert.ClusterNativePort != "" {
		ports = append(ports,
			corev1.ContainerPort{
				Name:          "clusternative",
				Protocol:      "TCP",
				ContainerPort: intstr.Parse(cr.Spec.VMInsert.ClusterNativePort).IntVal,
			},
		)
	}

	volumes := make([]corev1.Volume, 0)

	volumes = append(volumes, cr.Spec.VMInsert.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)

	vmMounts = append(vmMounts, cr.Spec.VMInsert.VolumeMounts...)

	for _, s := range cr.Spec.VMInsert.Secrets {
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
			MountPath: path.Join(victoriametricsv1beta1.SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.VMInsert.ConfigMaps {
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
			MountPath: path.Join(victoriametricsv1beta1.ConfigMapsDir, c),
		})
	}
	volumes, vmMounts = cr.Spec.License.MaybeAddToVolumes(volumes, vmMounts, victoriametricsv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, victoriametricsv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VMInsert.ExtraArgs, "-")
	sort.Strings(args)

	vminsertContainer := corev1.Container{
		Name:                     "vminsert",
		Image:                    fmt.Sprintf("%s:%s", build.FormatContainerImage(c.ContainerRegistry, cr.Spec.VMInsert.Image.Repository), cr.Spec.VMInsert.Image.Tag),
		ImagePullPolicy:          cr.Spec.VMInsert.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                build.Resources(cr.Spec.VMInsert.Resources, config.Resource(c.VMClusterDefault.VMInsertDefault.Resource), c.VMClusterDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	vminsertContainer = build.Probe(vminsertContainer, cr.Spec.VMInsert)
	operatorContainers := []corev1.Container{vminsertContainer}

	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VMInsert.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.VMInsert.TopologySpreadConstraints {
		if cr.Spec.VMInsert.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VMInsert.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.VMInsertSelectorLabels(),
			}
		}
	}
	useStrictSecurity := c.EnableStrictSecurity
	if cr.Spec.UseStrictSecurity != nil {
		useStrictSecurity = *cr.Spec.UseStrictSecurity
	}

	vmInsertPodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMInsertPodLabels(),
			Annotations: cr.VMInsertPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			NodeSelector:                  cr.Spec.VMInsert.NodeSelector,
			Volumes:                       volumes,
			InitContainers:                build.AddStrictSecuritySettingsToContainers(cr.Spec.VMInsert.InitContainers, useStrictSecurity),
			Containers:                    build.AddStrictSecuritySettingsToContainers(containers, useStrictSecurity),
			ServiceAccountName:            cr.GetServiceAccountName(),
			SecurityContext:               build.AddStrictSecuritySettingsToPod(cr.Spec.VMInsert.SecurityContext, useStrictSecurity),
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.VMInsert.Affinity,
			SchedulerName:                 cr.Spec.VMInsert.SchedulerName,
			RuntimeClassName:              cr.Spec.VMInsert.RuntimeClassName,
			Tolerations:                   cr.Spec.VMInsert.Tolerations,
			PriorityClassName:             cr.Spec.VMInsert.PriorityClassName,
			HostNetwork:                   cr.Spec.VMInsert.HostNetwork,
			DNSPolicy:                     cr.Spec.VMInsert.DNSPolicy,
			DNSConfig:                     cr.Spec.VMInsert.DNSConfig,
			TopologySpreadConstraints:     cr.Spec.VMInsert.TopologySpreadConstraints,
			TerminationGracePeriodSeconds: cr.Spec.VMInsert.TerminationGracePeriodSeconds,
			ReadinessGates:                cr.Spec.VMInsert.ReadinessGates,
		},
	}

	return vmInsertPodSpec, nil
}

func createOrUpdatePodDisruptionBudgetForVMInsert(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client) error {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMInsert.GetNameWithPrefix(cr.Name),
			Labels:          cr.FinalLabels(cr.VMInsertSelectorLabels()),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable:   cr.Spec.VMInsert.PodDisruptionBudget.MinAvailable,
			MaxUnavailable: cr.Spec.VMInsert.PodDisruptionBudget.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.Spec.VMInsert.PodDisruptionBudget.SelectorLabelsWithDefaults(cr.VMInsertSelectorLabels()),
			},
		},
	}
	return reconcile.PDB(ctx, rclient, pdb)
}

func buildVMStorageSpec(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
	cr = cr.DeepCopy()
	if cr.Spec.VMStorage.Image.Repository == "" {
		cr.Spec.VMStorage.Image.Repository = c.VMClusterDefault.VMStorageDefault.Image
	}
	if cr.Spec.VMStorage.Image.Tag == "" {
		if cr.Spec.ClusterVersion != "" {
			cr.Spec.VMStorage.Image.Tag = cr.Spec.ClusterVersion
		} else {
			cr.Spec.VMStorage.Image.Tag = c.VMClusterDefault.VMStorageDefault.Version
		}
	}
	if cr.Spec.VMStorage.VMInsertPort == "" {
		cr.Spec.VMStorage.VMInsertPort = c.VMClusterDefault.VMStorageDefault.VMInsertPort
	}
	if cr.Spec.VMStorage.VMSelectPort == "" {
		cr.Spec.VMStorage.VMSelectPort = c.VMClusterDefault.VMStorageDefault.VMSelectPort
	}
	if cr.Spec.VMStorage.Port == "" {
		cr.Spec.VMStorage.Port = c.VMClusterDefault.VMStorageDefault.Port
	}

	if cr.Spec.VMStorage.DNSPolicy == "" {
		cr.Spec.VMStorage.DNSPolicy = corev1.DNSClusterFirst
	}
	if cr.Spec.VMStorage.SchedulerName == "" {
		cr.Spec.VMStorage.SchedulerName = "default-scheduler"
	}
	if cr.Spec.VMStorage.Image.PullPolicy == "" {
		cr.Spec.VMStorage.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if cr.Spec.VMStorage.StorageDataPath == "" {
		cr.Spec.VMStorage.StorageDataPath = vmStorageDefaultDBPath
	}
	podSpec, err := makePodSpecForVMStorage(ctx, cr, c)
	if err != nil {
		return nil, err
	}

	podMP := appsv1.ParallelPodManagement
	if cr.Spec.VMStorage.MinReadySeconds > 0 {
		podMP = appsv1.OrderedReadyPodManagement
	}
	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMStorageSelectorLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: cr.Spec.VMStorage.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMStorageSelectorLabels(),
			},
			MinReadySeconds:     cr.Spec.VMStorage.MinReadySeconds,
			PodManagementPolicy: podMP,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VMStorage.UpdateStrategy(),
			},
			Template:             *podSpec,
			ServiceName:          cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			RevisionHistoryLimit: cr.Spec.VMStorage.RevisionHistoryLimitCount,
		},
	}
	storageSpec := cr.Spec.VMStorage.Storage
	storageSpec.IntoSTSVolume(cr.Spec.VMStorage.GetStorageVolumeName(), &stsSpec.Spec)
	stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.VMStorage.ClaimTemplates...)

	return stsSpec, nil
}

func makePodSpecForVMStorage(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-vminsertAddr=:%s", cr.Spec.VMStorage.VMInsertPort),
		fmt.Sprintf("-vmselectAddr=:%s", cr.Spec.VMStorage.VMSelectPort),
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VMStorage.Port),
		fmt.Sprintf("-retentionPeriod=%s", cr.Spec.RetentionPeriod),
	}
	if cr.Spec.VMStorage.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VMStorage.LogLevel))
	}
	if cr.Spec.VMStorage.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VMStorage.LogFormat))
	}

	if len(cr.Spec.VMStorage.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	if cr.Spec.ReplicationFactor != nil && *cr.Spec.ReplicationFactor > 1 {
		var dedupIsSet bool
		for arg := range cr.Spec.VMStorage.ExtraArgs {
			if strings.Contains(arg, "dedup.minScrapeInterval") {
				dedupIsSet = true
			}
		}
		if !dedupIsSet {
			args = append(args, "-dedup.minScrapeInterval=1ms")
		}
	}

	var envs []corev1.EnvVar

	envs = append(envs, cr.Spec.VMStorage.ExtraEnvs...)

	ports := []corev1.ContainerPort{
		{
			Name:          "http",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.VMStorage.Port).IntVal,
		},
		{
			Name:          "vminsert",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.VMStorage.VMInsertPort).IntVal,
		},
		{
			Name:          "vmselect",
			Protocol:      "TCP",
			ContainerPort: intstr.Parse(cr.Spec.VMStorage.VMSelectPort).IntVal,
		},
	}
	volumes := make([]corev1.Volume, 0)

	volumes = append(volumes, cr.Spec.VMStorage.Volumes...)

	if cr.Spec.VMStorage.VMBackup != nil && cr.Spec.VMStorage.VMBackup.CredentialsSecret != nil {
		volumes = append(volumes, corev1.Volume{
			Name: k8stools.SanitizeVolumeName("secret-" + cr.Spec.VMStorage.VMBackup.CredentialsSecret.Name),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.Spec.VMStorage.VMBackup.CredentialsSecret.Name,
				},
			},
		})
	}

	vmMounts := make([]corev1.VolumeMount, 0)

	vmMounts = append(vmMounts, corev1.VolumeMount{
		Name:      cr.Spec.VMStorage.GetStorageVolumeName(),
		MountPath: cr.Spec.VMStorage.StorageDataPath,
	})
	args = append(args, fmt.Sprintf("-storageDataPath=%s", cr.Spec.VMStorage.StorageDataPath))

	vmMounts = append(vmMounts, cr.Spec.VMStorage.VolumeMounts...)

	for _, s := range cr.Spec.VMStorage.Secrets {
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
			MountPath: path.Join(victoriametricsv1beta1.SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.VMStorage.ConfigMaps {
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
			MountPath: path.Join(victoriametricsv1beta1.ConfigMapsDir, c),
		})
	}

	volumes, vmMounts = cr.Spec.License.MaybeAddToVolumes(volumes, vmMounts, victoriametricsv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, victoriametricsv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VMStorage.ExtraArgs, "-")
	sort.Strings(args)
	vmstorageContainer := corev1.Container{
		Name:                     "vmstorage",
		Image:                    fmt.Sprintf("%s:%s", build.FormatContainerImage(c.ContainerRegistry, cr.Spec.VMStorage.Image.Repository), cr.Spec.VMStorage.Image.Tag),
		ImagePullPolicy:          cr.Spec.VMStorage.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                build.Resources(cr.Spec.VMStorage.Resources, config.Resource(c.VMClusterDefault.VMStorageDefault.Resource), c.VMClusterDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	vmstorageContainer = build.Probe(vmstorageContainer, cr.Spec.VMStorage)

	operatorContainers := []corev1.Container{vmstorageContainer}
	initContainers := cr.Spec.VMStorage.InitContainers

	if cr.Spec.VMStorage.VMBackup != nil {
		vmBackupManagerContainer, err := build.VMBackupManager(ctx, cr.Spec.VMStorage.VMBackup, c, cr.Spec.VMStorage.Port, cr.Spec.VMStorage.StorageDataPath, cr.Spec.VMStorage.GetStorageVolumeName(), cr.Spec.VMStorage.ExtraArgs, true, cr.Spec.License)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			operatorContainers = append(operatorContainers, *vmBackupManagerContainer)
		}
		if cr.Spec.VMStorage.VMBackup.Restore != nil &&
			cr.Spec.VMStorage.VMBackup.Restore.OnStart != nil &&
			cr.Spec.VMStorage.VMBackup.Restore.OnStart.Enabled {
			vmRestore, err := build.VMRestore(cr.Spec.VMStorage.VMBackup, c, cr.Spec.VMStorage.StorageDataPath, cr.Spec.VMStorage.GetStorageVolumeName())
			if err != nil {
				return nil, err
			}
			if vmRestore != nil {
				initContainers = append(initContainers, *vmRestore)
			}
		}
	}

	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VMStorage.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.VMStorage.TopologySpreadConstraints {
		if cr.Spec.VMStorage.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VMStorage.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.VMStorageSelectorLabels(),
			}
		}
	}

	tgp := &defaultTerminationGracePeriod
	if cr.Spec.VMStorage.TerminationGracePeriodSeconds > 0 {
		tgp = &cr.Spec.VMStorage.TerminationGracePeriodSeconds
	}
	useStrictSecurity := c.EnableStrictSecurity
	if cr.Spec.UseStrictSecurity != nil {
		useStrictSecurity = *cr.Spec.UseStrictSecurity
	}
	vmStoragePodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMStoragePodLabels(),
			Annotations: cr.VMStoragePodAnnotations(),
		},
		Spec: corev1.PodSpec{
			NodeSelector:                  cr.Spec.VMStorage.NodeSelector,
			Volumes:                       volumes,
			InitContainers:                build.AddStrictSecuritySettingsToContainers(initContainers, useStrictSecurity),
			Containers:                    build.AddStrictSecuritySettingsToContainers(containers, useStrictSecurity),
			ServiceAccountName:            cr.GetServiceAccountName(),
			SecurityContext:               build.AddStrictSecuritySettingsToPod(cr.Spec.VMStorage.SecurityContext, useStrictSecurity),
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.VMStorage.Affinity,
			SchedulerName:                 cr.Spec.VMStorage.SchedulerName,
			RuntimeClassName:              cr.Spec.VMStorage.RuntimeClassName,
			Tolerations:                   cr.Spec.VMStorage.Tolerations,
			PriorityClassName:             cr.Spec.VMStorage.PriorityClassName,
			HostNetwork:                   cr.Spec.VMStorage.HostNetwork,
			DNSPolicy:                     cr.Spec.VMStorage.DNSPolicy,
			DNSConfig:                     cr.Spec.VMStorage.DNSConfig,
			RestartPolicy:                 "Always",
			TerminationGracePeriodSeconds: tgp,
			TopologySpreadConstraints:     cr.Spec.VMStorage.TopologySpreadConstraints,
			ReadinessGates:                cr.Spec.VMStorage.ReadinessGates,
		},
	}

	return vmStoragePodSpec, nil
}

func createOrUpdatePodDisruptionBudgetForVMStorage(ctx context.Context, cr *victoriametricsv1beta1.VMCluster, rclient client.Client) error {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			Labels:          cr.FinalLabels(cr.VMStorageSelectorLabels()),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{victoriametricsv1beta1.FinalizerName},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable:   cr.Spec.VMStorage.PodDisruptionBudget.MinAvailable,
			MaxUnavailable: cr.Spec.VMStorage.PodDisruptionBudget.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.Spec.VMStorage.PodDisruptionBudget.SelectorLabelsWithDefaults(cr.VMStorageSelectorLabels()),
			},
		},
	}
	return reconcile.PDB(ctx, rclient, pdb)
}

func createOrUpdateVMInsertHPA(ctx context.Context, rclient client.Client, cluster *victoriametricsv1beta1.VMCluster) error {
	if cluster.Spec.VMInsert.HPA == nil {
		if err := finalize.HPADelete(ctx, rclient, cluster.Spec.VMInsert.GetNameWithPrefix(cluster.Name), cluster.Namespace); err != nil {
			return fmt.Errorf("cannot remove HPA for vminsert: %w", err)
		}
		return nil
	}
	targetRef := v2beta2.CrossVersionObjectReference{
		Name:       cluster.Spec.VMInsert.GetNameWithPrefix(cluster.Name),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	defaultHPA := build.HPA(targetRef, cluster.Spec.VMInsert.HPA, cluster.AsOwner(), cluster.VMInsertSelectorLabels(), cluster.Namespace)
	return reconcile.HPA(ctx, rclient, defaultHPA)
}

func createOrUpdateVMSelectHPA(ctx context.Context, rclient client.Client, cluster *victoriametricsv1beta1.VMCluster) error {
	if cluster.Spec.VMSelect.HPA == nil {
		if err := finalize.HPADelete(ctx, rclient, cluster.Spec.VMSelect.GetNameWithPrefix(cluster.Name), cluster.Namespace); err != nil {
			return fmt.Errorf("cannot remove HPA for vmselect: %w", err)
		}
		return nil
	}
	targetRef := v2beta2.CrossVersionObjectReference{
		Name:       cluster.Spec.VMSelect.GetNameWithPrefix(cluster.Name),
		Kind:       "StatefulSet",
		APIVersion: "apps/v1",
	}
	defaultHPA := build.HPA(targetRef, cluster.Spec.VMSelect.HPA, cluster.AsOwner(), cluster.VMSelectSelectorLabels(), cluster.Namespace)
	return reconcile.HPA(ctx, rclient, defaultHPA)
}

type clusterSvcBuilder struct {
	*victoriametricsv1beta1.VMCluster
	prefixedName      string
	finalLabels       map[string]string
	selectorLabels    map[string]string
	additionalService *victoriametricsv1beta1.AdditionalServiceSpec
}

func (csb *clusterSvcBuilder) PrefixedName() string {
	return csb.prefixedName
}

func (csb *clusterSvcBuilder) AllLabels() map[string]string {
	return csb.finalLabels
}

func (csb *clusterSvcBuilder) SelectorLabels() map[string]string {
	return csb.selectorLabels
}

func (csb *clusterSvcBuilder) GetAdditionalService() *victoriametricsv1beta1.AdditionalServiceSpec {
	return csb.additionalService
}
