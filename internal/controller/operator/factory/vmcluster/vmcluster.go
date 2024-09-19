package vmcluster

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
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/api/autoscaling/v2beta2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateOrUpdateVMCluster reconciled cluster object with order
// first we check status of vmStorage and waiting for its readiness
// then vmSelect and wait for it readiness as well
// and last one is vmInsert
// we manually handle statefulsets rolling updates
// needed in update checked by revesion status
// its controlled by k8s controller-manager
func CreateOrUpdateVMCluster(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {
	if cr.IsOwnsServiceAccount() {
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(cr)); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}

	if cr.Spec.VMStorage != nil {
		if cr.Spec.VMStorage.PodDisruptionBudget != nil {
			// TODO verify lastSpec for missing PDB and detete it if needed
			err := createOrUpdatePodDisruptionBudgetForVMStorage(ctx, cr, rclient)
			if err != nil {
				return err
			}
		}
		if err := createOrUpdateVMStorage(ctx, cr, rclient); err != nil {
			return err
		}

		storageSvc, err := createOrUpdateVMStorageService(ctx, cr, rclient)
		if err != nil {
			return err
		}
		if !ptr.Deref(cr.Spec.VMStorage.DisableSelfServiceScrape, false) {
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(storageSvc, cr.Spec.VMStorage, "http"))
			if err != nil {
				logger.WithContext(ctx).Error(err, "cannot create VMServiceScrape for vmStorage")
			}
		}
	}

	if cr.Spec.VMSelect != nil {
		if cr.Spec.VMSelect.PodDisruptionBudget != nil {
			// TODO verify lastSpec for missing PDB and detete it if needed
			if err := createOrUpdatePodDisruptionBudgetForVMSelect(ctx, cr, rclient); err != nil {
				return err
			}
		}
		if err := createOrUpdateVMSelect(ctx, cr, rclient); err != nil {
			return err
		}

		if err := createOrUpdateVMSelectHPA(ctx, rclient, cr); err != nil {
			return err
		}
		// create vmselect service
		selectSvc, err := createOrUpdateVMSelectService(ctx, cr, rclient)
		if err != nil {
			return err
		}
		if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, false) {
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(selectSvc, cr.Spec.VMSelect, "http"))
			if err != nil {
				logger.WithContext(ctx).Error(err, "cannot create VMServiceScrape for vmSelect")
			}
		}
	}

	if cr.Spec.VMInsert != nil {
		if cr.Spec.VMInsert.PodDisruptionBudget != nil {
			// TODO verify lastSpec for missing PDB and detete it if needed
			if err := createOrUpdatePodDisruptionBudgetForVMInsert(ctx, cr, rclient); err != nil {
				return err
			}
		}
		if err := createOrUpdateVMInsert(ctx, cr, rclient); err != nil {
			return err
		}
		insertSvc, err := createOrUpdateVMInsertService(ctx, cr, rclient)
		if err != nil {
			return err
		}
		if err := createOrUpdateVMInsertHPA(ctx, rclient, cr); err != nil {
			return err
		}
		if !ptr.Deref(cr.Spec.VMInsert.DisableSelfServiceScrape, false) {
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(insertSvc, cr.Spec.VMInsert, "http"))
			if err != nil {
				logger.WithContext(ctx).Error(err, "cannot create VMServiceScrape for vmInsert")
			}
		}

	}
	return nil
}

func createOrUpdateVMSelect(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {

	var prevSts *appsv1.StatefulSet
	if cr.Spec.ParsedLastAppliedSpec != nil && cr.Spec.ParsedLastAppliedSpec.VMSelect != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.Spec.ParsedLastAppliedSpec
		var err error
		prevSts, err = genVMSelectSpec(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev storage spec: %w", err)
		}
	}
	newSts, err := genVMSelectSpec(cr)
	if err != nil {
		return err
	}

	stsOpts := reconcile.STSOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.VMSelectSelectorLabels,
		HPA:            cr.Spec.VMSelect.HPA,
		UpdateReplicaCount: func(count *int32) {
			if cr.Spec.VMSelect.HPA != nil && count != nil {
				cr.Spec.VMSelect.ReplicaCount = count
			}
		},
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, prevSts)
}

func createOrUpdateVMSelectService(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) (*corev1.Service, error) {

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

	if err := cr.Spec.VMSelect.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newHeadless, s)
		if additionalService.Name == newHeadless.Name {
			return fmt.Errorf("vmselect additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newHeadless.Name)
		} else if err := reconcile.Service(ctx, rclient, additionalService, nil); err != nil {
			return fmt.Errorf("cannot reconcile service for vmselect: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var prevService *corev1.Service
	if cr.Spec.ParsedLastAppliedSpec != nil && cr.Spec.ParsedLastAppliedSpec.VMSelect != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.Spec.ParsedLastAppliedSpec
		prevT := &clusterSvcBuilder{
			prevCR,
			prevCR.Spec.VMSelect.GetNameWithPrefix(prevCR.Name),
			prevCR.FinalLabels(prevCR.VMSelectSelectorLabels()),
			prevCR.VMSelectSelectorLabels(),
			prevCR.Spec.VMSelect.ServiceSpec,
		}

		prevService = build.Service(prevT, prevCR.Spec.VMSelect.Port, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			if prevCR.Spec.VMSelect.ClusterNativePort != "" {
				svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
					Name:       "clusternative",
					Protocol:   "TCP",
					Port:       intstr.Parse(prevCR.Spec.VMSelect.ClusterNativePort).IntVal,
					TargetPort: intstr.Parse(prevCR.Spec.VMSelect.ClusterNativePort),
				})
			}
		})
	}

	if cr.Spec.VMSelect.ServiceSpec == nil &&
		cr.Spec.ParsedLastAppliedSpec != nil &&
		cr.Spec.ParsedLastAppliedSpec.VMSelect != nil &&
		cr.Spec.ParsedLastAppliedSpec.VMSelect.ServiceSpec != nil {
		rca := finalize.RemoveSvcArgs{SelectorLabels: cr.VMSelectSelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: func() string {
			return cr.Spec.VMSelect.GetNameWithPrefix(cr.Name)
		}}
		if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.VMSelect.ServiceSpec); err != nil {
			return nil, err
		}
	}

	if err := reconcile.Service(ctx, rclient, newHeadless, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile vmselect service: %w", err)
	}
	return newHeadless, nil
}

func createOrUpdateVMInsert(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {
	var prevDeploy *appsv1.Deployment

	if cr.Spec.ParsedLastAppliedSpec != nil && cr.Spec.ParsedLastAppliedSpec.VMInsert != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.Spec.ParsedLastAppliedSpec
		var err error
		prevDeploy, err = genVMInsertSpec(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeployment, err := genVMInsertSpec(cr)
	if err != nil {
		return err
	}
	return reconcile.Deployment(ctx, rclient, newDeployment, prevDeploy, cr.Spec.VMInsert.HPA != nil)
}

func createOrUpdateVMInsertService(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) (*corev1.Service, error) {
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

	if err := cr.Spec.VMInsert.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vminsert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		} else if err := reconcile.Service(ctx, rclient, additionalService, nil); err != nil {
			return fmt.Errorf("cannot reconcile vminsert additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	var prevService *corev1.Service
	if cr.Spec.ParsedLastAppliedSpec != nil && cr.Spec.ParsedLastAppliedSpec.VMInsert != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.Spec.ParsedLastAppliedSpec
		prevT := &clusterSvcBuilder{
			cr,
			prevCR.Spec.VMInsert.GetNameWithPrefix(cr.Name),
			prevCR.FinalLabels(cr.VMInsertSelectorLabels()),
			prevCR.VMInsertSelectorLabels(),
			prevCR.Spec.VMInsert.ServiceSpec,
		}
		prevService = build.Service(prevT, prevCR.Spec.VMInsert.Port, func(svc *corev1.Service) {
			build.AppendInsertPortsToService(prevCR.Spec.VMInsert.InsertPorts, svc)
			if prevCR.Spec.VMInsert.ClusterNativePort != "" {
				svc.Spec.Ports = append(svc.Spec.Ports,
					corev1.ServicePort{
						Name:       "clusternative",
						Protocol:   "TCP",
						Port:       intstr.Parse(prevCR.Spec.VMInsert.ClusterNativePort).IntVal,
						TargetPort: intstr.Parse(prevCR.Spec.VMInsert.ClusterNativePort),
					})
			}
		})
	}

	if cr.Spec.VMInsert.ServiceSpec == nil &&
		cr.Spec.ParsedLastAppliedSpec != nil &&
		cr.Spec.ParsedLastAppliedSpec.VMInsert != nil &&
		cr.Spec.ParsedLastAppliedSpec.VMInsert.ServiceSpec != nil {
		rca := finalize.RemoveSvcArgs{SelectorLabels: cr.SelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: cr.PrefixedName}
		if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.VMInsert.ServiceSpec); err != nil {
			return nil, err
		}
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile vminsert service: %w", err)
	}
	return newService, nil
}

func createOrUpdateVMStorage(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {
	var prevSts *appsv1.StatefulSet

	if cr.Spec.ParsedLastAppliedSpec != nil && cr.Spec.ParsedLastAppliedSpec.VMStorage != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.Spec.ParsedLastAppliedSpec
		var err error
		prevSts, err = buildVMStorageSpec(ctx, prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev storage spec: %w", err)
		}
	}
	newSts, err := buildVMStorageSpec(ctx, cr)
	if err != nil {
		return err
	}

	stsOpts := reconcile.STSOptions{
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.VMStorageSelectorLabels,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, prevSts)
}

func createOrUpdateVMStorageService(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) (*corev1.Service, error) {
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
			parsedPort := intstr.Parse(cr.Spec.VMStorage.VMBackup.Port)
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "vmbackupmanager",
				Protocol:   corev1.ProtocolTCP,
				Port:       parsedPort.IntVal,
				TargetPort: parsedPort,
			})
		}
	})

	if err := cr.Spec.VMStorage.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newHeadless, s)
		if additionalService.Name == newHeadless.Name {
			return fmt.Errorf("vmstorage additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newHeadless.Name)
		} else if err := reconcile.Service(ctx, rclient, additionalService, nil); err != nil {
			return fmt.Errorf("cannot reconcile vmstorage additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	var prevService *corev1.Service
	if cr.Spec.ParsedLastAppliedSpec != nil && cr.Spec.ParsedLastAppliedSpec.VMStorage != nil {
		prevCR := cr.DeepCopy()
		prevCR.Spec = *cr.Spec.ParsedLastAppliedSpec
		prevT := &clusterSvcBuilder{
			cr,
			prevCR.Spec.VMStorage.GetNameWithPrefix(prevCR.Name),
			prevCR.FinalLabels(prevCR.VMStorageSelectorLabels()),
			prevCR.VMStorageSelectorLabels(),
			prevCR.Spec.VMStorage.ServiceSpec,
		}

		prevService = build.Service(prevT, prevCR.Spec.VMStorage.Port, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			svc.Spec.Ports = append(svc.Spec.Ports, []corev1.ServicePort{
				{
					Name:       "vminsert",
					Protocol:   "TCP",
					Port:       intstr.Parse(prevCR.Spec.VMStorage.VMInsertPort).IntVal,
					TargetPort: intstr.Parse(prevCR.Spec.VMStorage.VMInsertPort),
				},
				{
					Name:       "vmselect",
					Protocol:   "TCP",
					Port:       intstr.Parse(prevCR.Spec.VMStorage.VMSelectPort).IntVal,
					TargetPort: intstr.Parse(cr.Spec.VMStorage.VMSelectPort),
				},
			}...)
			if prevCR.Spec.VMStorage.VMBackup != nil {
				parsedPort := intstr.Parse(prevCR.Spec.VMStorage.VMBackup.Port)
				svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
					Name:       "vmbackupmanager",
					Protocol:   corev1.ProtocolTCP,
					Port:       parsedPort.IntVal,
					TargetPort: parsedPort,
				})
			}
		})
	}

	if cr.Spec.VMStorage.ServiceSpec == nil &&
		cr.Spec.ParsedLastAppliedSpec != nil &&
		cr.Spec.ParsedLastAppliedSpec.VMStorage != nil &&
		cr.Spec.ParsedLastAppliedSpec.VMStorage.ServiceSpec != nil {
		rca := finalize.RemoveSvcArgs{SelectorLabels: cr.VMStorageSelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: func() string {
			return cr.Spec.VMStorage.GetNameWithPrefix(cr.Name)
		}}
		if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.VMStorage.ServiceSpec); err != nil {
			return nil, err
		}

	}

	if err := reconcile.Service(ctx, rclient, newHeadless, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile vmstorage service: %w", err)
	}
	return newHeadless, nil
}

func genVMSelectSpec(cr *vmv1beta1.VMCluster) (*appsv1.StatefulSet, error) {
	podSpec, err := makePodSpecForVMSelect(cr)
	if err != nil {
		return nil, err
	}

	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMSelectSelectorLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMSelectSelectorLabels(),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VMSelect.RollingUpdateStrategy,
			},
			Template:    *podSpec,
			ServiceName: cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
		},
	}
	build.StatefulSetAddCommonParams(stsSpec, ptr.Deref(cr.Spec.VMSelect.UseStrictSecurity, false), &cr.Spec.VMSelect.CommonApplicationDeploymentParams)
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

func makePodSpecForVMSelect(cr *vmv1beta1.VMCluster) (*corev1.PodTemplateSpec, error) {
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

		storageArg := "-storageNode="
		for _, i := range cr.AvailableStorageNodeIDs("select") {
			storageArg += cr.Spec.VMStorage.BuildPodName(cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMStorage.VMSelectPort, cr.Spec.ClusterDomainName)
		}
		storageArg = strings.TrimSuffix(storageArg, ",")
		args = append(args, storageArg)

	}
	// selectNode arg add for deployments without HPA
	// HPA leads to rolling restart for vmselect statefulset in case of replicas count changes
	if cr.Spec.VMSelect.HPA == nil {
		selectArg := "-selectNode="
		vmselectCount := *cr.Spec.VMSelect.ReplicaCount
		for i := int32(0); i < vmselectCount; i++ {
			selectArg += cr.Spec.VMSelect.BuildPodName(cr.Spec.VMSelect.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMSelect.Port, cr.Spec.ClusterDomainName)
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
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
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
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}

	volumes, vmMounts = cr.Spec.License.MaybeAddToVolumes(volumes, vmMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VMSelect.ExtraArgs, "-")
	sort.Strings(args)
	vmselectContainer := corev1.Container{
		Name:                     "vmselect",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VMSelect.Image.Repository, cr.Spec.VMSelect.Image.Tag),
		ImagePullPolicy:          cr.Spec.VMSelect.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.VMSelect.Resources,
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	vmselectContainer = build.Probe(vmselectContainer, cr.Spec.VMSelect)
	operatorContainers := []corev1.Container{vmselectContainer}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VMSelect.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.UseStrictSecurity, false))
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

	vmSelectPodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMSelectPodLabels(),
			Annotations: cr.VMSelectPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.VMSelect.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
			RestartPolicy:      "Always",
		},
	}

	return vmSelectPodSpec, nil
}

func createOrUpdatePodDisruptionBudgetForVMSelect(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			Labels:          cr.FinalLabels(cr.VMSelectSelectorLabels()),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{vmv1beta1.FinalizerName},
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

func genVMInsertSpec(cr *vmv1beta1.VMCluster) (*appsv1.Deployment, error) {

	podSpec, err := makePodSpecForVMInsert(cr)
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
			Finalizers:      []string{vmv1beta1.FinalizerName},
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
	build.DeploymentAddCommonParams(stsSpec, ptr.Deref(cr.Spec.VMInsert.UseStrictSecurity, false), &cr.Spec.VMInsert.CommonApplicationDeploymentParams)
	return stsSpec, nil
}

func makePodSpecForVMInsert(cr *vmv1beta1.VMCluster) (*corev1.PodTemplateSpec, error) {
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
		storageArg := "-storageNode="
		for _, i := range cr.AvailableStorageNodeIDs("insert") {
			storageArg += cr.Spec.VMStorage.BuildPodName(cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMStorage.VMInsertPort, cr.Spec.ClusterDomainName)
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
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
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
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}
	volumes, vmMounts = cr.Spec.License.MaybeAddToVolumes(volumes, vmMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VMInsert.ExtraArgs, "-")
	sort.Strings(args)

	vminsertContainer := corev1.Container{
		Name:                     "vminsert",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VMInsert.Image.Repository, cr.Spec.VMInsert.Image.Tag),
		ImagePullPolicy:          cr.Spec.VMInsert.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.VMInsert.Resources,
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	vminsertContainer = build.Probe(vminsertContainer, cr.Spec.VMInsert)
	operatorContainers := []corev1.Container{vminsertContainer}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VMInsert.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.UseStrictSecurity, false))
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

	vmInsertPodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMInsertPodLabels(),
			Annotations: cr.VMInsertPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:            volumes,
			InitContainers:     cr.Spec.VMInsert.InitContainers,
			Containers:         containers,
			ServiceAccountName: cr.GetServiceAccountName(),
		},
	}

	return vmInsertPodSpec, nil
}

func createOrUpdatePodDisruptionBudgetForVMInsert(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMInsert.GetNameWithPrefix(cr.Name),
			Labels:          cr.FinalLabels(cr.VMInsertSelectorLabels()),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{vmv1beta1.FinalizerName},
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

func buildVMStorageSpec(ctx context.Context, cr *vmv1beta1.VMCluster) (*appsv1.StatefulSet, error) {

	podSpec, err := makePodSpecForVMStorage(ctx, cr)
	if err != nil {
		return nil, err
	}

	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMStorageSelectorLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMStorageSelectorLabels(),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VMStorage.RollingUpdateStrategy,
			},
			Template:    *podSpec,
			ServiceName: cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
		},
	}
	build.StatefulSetAddCommonParams(stsSpec, ptr.Deref(cr.Spec.VMStorage.UseStrictSecurity, false), &cr.Spec.VMStorage.CommonApplicationDeploymentParams)
	storageSpec := cr.Spec.VMStorage.Storage
	storageSpec.IntoSTSVolume(cr.Spec.VMStorage.GetStorageVolumeName(), &stsSpec.Spec)
	stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.VMStorage.ClaimTemplates...)

	return stsSpec, nil
}

func makePodSpecForVMStorage(ctx context.Context, cr *vmv1beta1.VMCluster) (*corev1.PodTemplateSpec, error) {
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
			MountPath: path.Join(vmv1beta1.SecretsDir, s),
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
			MountPath: path.Join(vmv1beta1.ConfigMapsDir, c),
		})
	}

	volumes, vmMounts = cr.Spec.License.MaybeAddToVolumes(volumes, vmMounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, cr.Spec.VMStorage.ExtraArgs, "-")
	sort.Strings(args)
	vmstorageContainer := corev1.Container{
		Name:                     "vmstorage",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VMStorage.Image.Repository, cr.Spec.VMStorage.Image.Tag),
		ImagePullPolicy:          cr.Spec.VMStorage.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                cr.Spec.VMStorage.Resources,
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	vmstorageContainer = build.Probe(vmstorageContainer, cr.Spec.VMStorage)

	operatorContainers := []corev1.Container{vmstorageContainer}
	var initContainers []corev1.Container

	if cr.Spec.VMStorage.VMBackup != nil {
		vmBackupManagerContainer, err := build.VMBackupManager(ctx, cr.Spec.VMStorage.VMBackup, cr.Spec.VMStorage.Port, cr.Spec.VMStorage.StorageDataPath, cr.Spec.VMStorage.GetStorageVolumeName(), cr.Spec.VMStorage.ExtraArgs, true, cr.Spec.License)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			operatorContainers = append(operatorContainers, *vmBackupManagerContainer)
		}
		if cr.Spec.VMStorage.VMBackup.Restore != nil &&
			cr.Spec.VMStorage.VMBackup.Restore.OnStart != nil &&
			cr.Spec.VMStorage.VMBackup.Restore.OnStart.Enabled {
			vmRestore, err := build.VMRestore(cr.Spec.VMStorage.VMBackup, cr.Spec.VMStorage.StorageDataPath, cr.Spec.VMStorage.GetStorageVolumeName())
			if err != nil {
				return nil, err
			}
			if vmRestore != nil {
				initContainers = append(initContainers, *vmRestore)
			}
		}
	}
	useStrictSecurity := ptr.Deref(cr.Spec.VMStorage.UseStrictSecurity, false)
	build.AddStrictSecuritySettingsToContainers(cr.Spec.VMStorage.SecurityContext, initContainers, useStrictSecurity)
	build.AddStrictSecuritySettingsToContainers(cr.Spec.VMStorage.SecurityContext, operatorContainers, useStrictSecurity)
	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.VMStorage.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch vmstorage init containers: %w", err)
	}
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VMStorage.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch vmstorage containers: %w", err)
	}

	for i := range cr.Spec.VMStorage.TopologySpreadConstraints {
		if cr.Spec.VMStorage.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VMStorage.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.VMStorageSelectorLabels(),
			}
		}
	}

	vmStoragePodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMStoragePodLabels(),
			Annotations: cr.VMStoragePodAnnotations(),
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

func createOrUpdatePodDisruptionBudgetForVMStorage(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {
	pdb := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			Labels:          cr.FinalLabels(cr.VMStorageSelectorLabels()),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{vmv1beta1.FinalizerName},
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

func createOrUpdateVMInsertHPA(ctx context.Context, rclient client.Client, cluster *vmv1beta1.VMCluster) error {
	if cluster.Spec.VMInsert.HPA == nil {
		// TODO verify with lastSpec
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

func createOrUpdateVMSelectHPA(ctx context.Context, rclient client.Client, cluster *vmv1beta1.VMCluster) error {
	if cluster.Spec.VMSelect.HPA == nil {
		// TODO verify with lastSpec

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
	*vmv1beta1.VMCluster
	prefixedName      string
	finalLabels       map[string]string
	selectorLabels    map[string]string
	additionalService *vmv1beta1.AdditionalServiceSpec
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

func (csb *clusterSvcBuilder) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return csb.additionalService
}
