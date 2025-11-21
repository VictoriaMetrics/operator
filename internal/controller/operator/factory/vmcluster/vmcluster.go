package vmcluster

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
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

// CreateOrUpdate reconciled cluster object with order
// first we check status of vmStorage and waiting for its readiness
// then vmSelect and wait for it readiness as well
// and last one is vmInsert
// we manually handle statefulsets rolling updates
// needed in update checked by revision status
// its controlled by k8s controller-manager
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {
	var prevCR *vmv1beta1.VMCluster
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if cr.IsOwnsServiceAccount() {
		b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentRoot)
		sa := build.ServiceAccount(b)
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentRoot)
			prevSA = build.ServiceAccount(b)
		}
		if err := reconcile.ServiceAccount(ctx, rclient, sa, prevSA); err != nil {
			return fmt.Errorf("failed create service account: %w", err)
		}
	}
	// handle case for loadbalancing
	if cr.Spec.RequestsLoadBalancer.Enabled {
		// create vmauth deployment
		if err := createOrUpdateVMAuthLB(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
	}

	cfg := config.MustGetBaseConfig()
	if cr.Spec.VMStorage != nil {
		if cr.Spec.VMStorage.PodDisruptionBudget != nil {
			err := createOrUpdatePodDisruptionBudgetForVMStorage(ctx, rclient, cr, prevCR)
			if err != nil {
				return err
			}
		}
		if err := createOrUpdateVMStorage(ctx, rclient, cr, prevCR); err != nil {
			return err
		}

		storageSvc, err := createOrUpdateVMStorageService(ctx, rclient, cr, prevCR)
		if err != nil {
			return err
		}
		if !ptr.Deref(cr.Spec.VMStorage.DisableSelfServiceScrape, cfg.DisableSelfServiceScrapeCreation) {
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(storageSvc, cr.Spec.VMStorage, "vmbackupmanager"))
			if err != nil {
				return fmt.Errorf("cannot create VMServiceScrape for vmStorage: %w", err)
			}
		}
	}

	if cr.Spec.VMSelect != nil {
		if cr.Spec.VMSelect.PodDisruptionBudget != nil {
			if err := createOrUpdatePodDisruptionBudgetForVMSelect(ctx, rclient, cr, prevCR); err != nil {
				return err
			}
		}
		if err := createOrUpdateVMSelect(ctx, rclient, cr, prevCR); err != nil {
			return err
		}

		if err := createOrUpdateVMSelectHPA(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
		// create vmselect service
		selectSvc, err := createOrUpdateVMSelectService(ctx, rclient, cr, prevCR)
		if err != nil {
			return err
		}
		if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, cfg.DisableSelfServiceScrapeCreation) {

			svs := build.VMServiceScrapeForServiceWithSpec(selectSvc, cr.Spec.VMSelect)
			if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
				// for backward compatibility we must keep job label value
				svs.Spec.JobLabel = vmv1beta1.VMAuthLBServiceProxyJobNameLabel
			}
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs)
			if err != nil {
				return fmt.Errorf("cannot create VMServiceScrape for vmSelect: %w", err)
			}
		}
	}

	if cr.Spec.VMInsert != nil {
		if cr.Spec.VMInsert.PodDisruptionBudget != nil {
			if err := createOrUpdatePodDisruptionBudgetForVMInsert(ctx, rclient, cr, prevCR); err != nil {
				return err
			}
		}
		if err := createOrUpdateVMInsert(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
		insertSvc, err := createOrUpdateVMInsertService(ctx, rclient, cr, prevCR)
		if err != nil {
			return err
		}
		if err := createOrUpdateVMInsertHPA(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
		if !ptr.Deref(cr.Spec.VMInsert.DisableSelfServiceScrape, cfg.DisableSelfServiceScrapeCreation) {
			svs := build.VMServiceScrapeForServiceWithSpec(insertSvc, cr.Spec.VMInsert)
			if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
				// for backward compatibility we must keep job label value
				svs.Spec.JobLabel = vmv1beta1.VMAuthLBServiceProxyJobNameLabel
			}
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs)
			if err != nil {
				return fmt.Errorf("cannot create VMServiceScrape for vmInsert: %w", err)
			}
		}
	}

	if err := deletePrevStateResources(ctx, rclient, cr, prevCR); err != nil {
		return fmt.Errorf("failed to remove objects from previous cluster state: %w", err)
	}
	return nil
}

func createOrUpdateVMSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {

	var prevSts *appsv1.StatefulSet
	if prevCR != nil && prevCR.Spec.VMSelect != nil {
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
		HasClaim: len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: func() map[string]string {
			return cr.SelectorLabels(vmv1beta1.ClusterComponentSelect)
		},
		HPA: cr.Spec.VMSelect.HPA,
		UpdateReplicaCount: func(count *int32) {
			if cr.Spec.VMSelect.HPA != nil && count != nil {
				cr.Spec.VMSelect.ReplicaCount = count
			}
		},
		UpdateBehavior: cr.Spec.VMSelect.RollingUpdateStrategyBehavior,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, prevSts)
}

func buildVMSelectService(cr *vmv1beta1.VMCluster) *corev1.Service {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	svc := build.Service(b, cr.Spec.VMSelect.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.PublishNotReadyAddresses = true
		if cr.Spec.VMSelect.ClusterNativePort != "" {
			svc.Spec.Ports = append(svc.Spec.Ports, corev1.ServicePort{
				Name:       "clusternative",
				Protocol:   "TCP",
				Port:       intstr.Parse(cr.Spec.VMSelect.ClusterNativePort).IntVal,
				TargetPort: intstr.Parse(cr.Spec.VMSelect.ClusterNativePort),
			})
		}
	})
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		svc.Name = cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect)
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmv1beta1.VMAuthLBServiceProxyJobNameLabel] = b.PrefixedName()
	}

	return svc

}

func createOrUpdateVMSelectService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) (*corev1.Service, error) {
	svc := buildVMSelectService(cr)
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.VMSelect != nil {
		prevService = buildVMSelectService(prevCR)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.VMSelect.ServiceSpec)
	}
	if err := cr.Spec.VMSelect.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(svc, s)
		if additionalService.Name == svc.Name {
			return fmt.Errorf("vmselect additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile service for vmselect: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, svc, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile vmselect service: %w", err)
	}
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		var prevPort string
		if prevCR != nil && prevCR.Spec.VMSelect != nil {
			prevPort = prevCR.Spec.VMSelect.Port
		}
		kind := vmv1beta1.ClusterComponentSelect
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, kind, cr.Spec.VMSelect.Port, prevPort); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for vmselect: %w", err)
		}
	}
	return svc, nil
}

// createOrUpdateLBProxyService builds vminsert and vmselect external services to expose vmcluster components for access by vmauth
func createOrUpdateLBProxyService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster, kind vmv1beta1.ClusterComponent, port, prevPort string) error {
	builder := func(r *vmv1beta1.VMCluster) *build.ChildBuilder {
		b := build.NewChildBuilder(r, kind)
		b.SetFinalLabels(labels.Merge(b.AllLabels(), map[string]string{
			vmv1beta1.VMAuthLBServiceProxyTargetLabel: string(kind),
		}))
		b.SetSelectorLabels(cr.SelectorLabels(vmv1beta1.ClusterComponentBalancer))
		return b
	}
	b := builder(cr)
	svc := build.Service(b, cr.Spec.RequestsLoadBalancer.Spec.Port, func(svc *corev1.Service) {
		svc.Spec.Ports[0].Port = intstr.Parse(port).IntVal
	})

	var prevSvc *corev1.Service
	if prevCR != nil {
		b = builder(prevCR)
		prevSvc = build.Service(b, prevCR.Spec.RequestsLoadBalancer.Spec.Port, func(svc *corev1.Service) {
			svc.Spec.Ports[0].Port = intstr.Parse(prevPort).IntVal
		})
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc); err != nil {
		return fmt.Errorf("cannot reconcile lb service: %w", err)
	}
	return nil
}

func createOrUpdateVMInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	var prevDeploy *appsv1.Deployment

	if prevCR != nil && prevCR.Spec.VMInsert != nil {
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

func buildVMInsertService(cr *vmv1beta1.VMCluster) *corev1.Service {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	svc := build.Service(b, cr.Spec.VMInsert.Port, func(svc *corev1.Service) {
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
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		svc.Name = cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert)
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmv1beta1.VMAuthLBServiceProxyJobNameLabel] = cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
	}
	return svc
}

func createOrUpdateVMInsertService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) (*corev1.Service, error) {

	newService := buildVMInsertService(cr)
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.VMInsert != nil {
		prevService = buildVMInsertService(prevCR)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.VMInsert.ServiceSpec)
	}
	if err := cr.Spec.VMInsert.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vminsert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile vminsert additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newService, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile vminsert service: %w", err)
	}

	// create extra service for loadbalancing
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
		var prevPort string
		if prevCR != nil && prevCR.Spec.VMInsert != nil {
			prevPort = prevCR.Spec.VMInsert.Port
		}
		kind := vmv1beta1.ClusterComponentInsert
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, kind, cr.Spec.VMInsert.Port, prevPort); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for vminsert: %w", err)
		}
	}

	return newService, nil
}

func createOrUpdateVMStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	var prevSts *appsv1.StatefulSet

	if prevCR != nil && prevCR.Spec.VMStorage != nil {
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
		HasClaim: len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: func() map[string]string {
			return cr.SelectorLabels(vmv1beta1.ClusterComponentStorage)
		},
		UpdateBehavior: cr.Spec.VMStorage.RollingUpdateStrategyBehavior,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, prevSts)
}

func buildVMStorageService(cr *vmv1beta1.VMCluster) *corev1.Service {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
	return build.Service(b, cr.Spec.VMStorage.Port, func(svc *corev1.Service) {
		svc.Spec.ClusterIP = "None"
		svc.Spec.PublishNotReadyAddresses = true
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
}

func createOrUpdateVMStorageService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) (*corev1.Service, error) {
	svc := buildVMStorageService(cr)
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.VMStorage != nil {
		prevService = buildVMStorageService(prevCR)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.VMStorage.ServiceSpec)
	}
	if err := cr.Spec.VMStorage.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(svc, s)
		if additionalService.Name == svc.Name {
			return fmt.Errorf("vmstorage additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, svc.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile vmstorage additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, svc, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile vmstorage service: %w", err)
	}
	return svc, nil
}

func genVMSelectSpec(cr *vmv1beta1.VMCluster) (*appsv1.StatefulSet, error) {
	podSpec, err := makePodSpecForVMSelect(cr)
	if err != nil {
		return nil, err
	}

	commonName := cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            commonName,
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentSelect),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentSelect),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VMSelect.RollingUpdateStrategy,
			},
			Template:    *podSpec,
			ServiceName: commonName,
		},
	}
	if cr.Spec.VMSelect.PersistentVolumeClaimRetentionPolicy != nil {
		stsSpec.Spec.PersistentVolumeClaimRetentionPolicy = cr.Spec.VMSelect.PersistentVolumeClaimRetentionPolicy
	}
	cfg := config.MustGetBaseConfig()
	build.StatefulSetAddCommonParams(stsSpec, ptr.Deref(cr.Spec.VMSelect.UseStrictSecurity, cfg.EnableStrictSecurity), &cr.Spec.VMSelect.CommonApplicationDeploymentParams)
	if cr.Spec.VMSelect.CacheMountPath != "" {
		storageSpec := cr.Spec.VMSelect.PersistentVolume
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
	commonName := cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VMSelect.Port),
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
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
		storageNodeFlag := build.NewFlag("-storageNode", "")
		storageNodeIds := cr.AvailableStorageNodeIDs("select")
		for idx, i := range storageNodeIds {
			storageName := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
			storageNodeFlag.Add(build.PodDNSAddress(storageName, i, cr.Namespace, cr.Spec.VMStorage.VMSelectPort, cr.Spec.ClusterDomainName), idx)
		}
		totalNodes := len(storageNodeIds)
		args = build.AppendFlagsToArgs(args, totalNodes, storageNodeFlag)
	}
	// selectNode arg add for deployments without HPA
	// HPA leads to rolling restart for vmselect statefulset in case of replicas count changes
	if cr.Spec.VMSelect.HPA == nil && cr.Spec.VMSelect.ReplicaCount != nil {
		selectNodeFlag := build.NewFlag("-selectNode", "")
		vmselectCount := *cr.Spec.VMSelect.ReplicaCount
		for idx := int32(0); idx < vmselectCount; idx++ {
			selectNodeFlag.Add(build.PodDNSAddress(commonName, idx, cr.Namespace, cr.Spec.VMSelect.Port, cr.Spec.ClusterDomainName), int(idx))
		}
		args = build.AppendFlagsToArgs(args, int(vmselectCount), selectNodeFlag)
	}

	if len(cr.Spec.VMSelect.ExtraEnvs) > 0 || len(cr.Spec.VMSelect.ExtraEnvsFrom) > 0 {
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

	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

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
		EnvFrom:                  cr.Spec.VMSelect.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	vmselectContainer = build.Probe(vmselectContainer, cr.Spec.VMSelect)
	operatorContainers := []corev1.Container{vmselectContainer}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VMSelect.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.VMSelect.UseStrictSecurity, cfg.EnableStrictSecurity))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VMSelect.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.VMSelect.TopologySpreadConstraints {
		if cr.Spec.VMSelect.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VMSelect.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentSelect),
			}
		}
	}

	vmSelectPodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(vmv1beta1.ClusterComponentSelect),
			Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentSelect),
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

func createOrUpdatePodDisruptionBudgetForVMSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	pdb := build.PodDisruptionBudget(b, cr.Spec.VMSelect.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMSelect.PodDisruptionBudget != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)
		prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.VMSelect.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
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
	commonName := cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
	stsSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            commonName,
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentInsert),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
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
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentInsert),
			},
			Template: *podSpec,
		},
	}
	cfg := config.MustGetBaseConfig()
	build.DeploymentAddCommonParams(stsSpec, ptr.Deref(cr.Spec.VMInsert.UseStrictSecurity, cfg.EnableStrictSecurity), &cr.Spec.VMInsert.CommonApplicationDeploymentParams)
	return stsSpec, nil
}

func makePodSpecForVMInsert(cr *vmv1beta1.VMCluster) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VMInsert.Port),
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
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
		storageNodeFlag := build.NewFlag("-storageNode", "")
		storageNodeIds := cr.AvailableStorageNodeIDs("insert")
		for idx, i := range storageNodeIds {
			storageName := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
			storageNodeFlag.Add(build.PodDNSAddress(storageName, i, cr.Namespace, cr.Spec.VMStorage.VMInsertPort, cr.Spec.ClusterDomainName), idx)
		}
		totalNodes := len(storageNodeIds)
		args = build.AppendFlagsToArgs(args, totalNodes, storageNodeFlag)
	}

	if cr.Spec.ReplicationFactor != nil {
		args = append(args, fmt.Sprintf("-replicationFactor=%d", *cr.Spec.ReplicationFactor))
	}
	if len(cr.Spec.VMInsert.ExtraEnvs) > 0 || len(cr.Spec.VMInsert.ExtraEnvsFrom) > 0 {
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
	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

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
		EnvFrom:                  cr.Spec.VMInsert.ExtraEnvsFrom,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	vminsertContainer = build.Probe(vminsertContainer, cr.Spec.VMInsert)
	operatorContainers := []corev1.Container{vminsertContainer}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VMInsert.SecurityContext, operatorContainers, ptr.Deref(cr.Spec.VMInsert.UseStrictSecurity, cfg.EnableStrictSecurity))
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VMInsert.Containers)
	if err != nil {
		return nil, err
	}

	for i := range cr.Spec.VMInsert.TopologySpreadConstraints {
		if cr.Spec.VMInsert.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VMInsert.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentInsert),
			}
		}
	}

	vmInsertPodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(vmv1beta1.ClusterComponentInsert),
			Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentInsert),
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

func createOrUpdatePodDisruptionBudgetForVMInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	pdb := build.PodDisruptionBudget(b, cr.Spec.VMInsert.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMInsert.PodDisruptionBudget != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentInsert)
		prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.VMInsert.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func buildVMStorageSpec(ctx context.Context, cr *vmv1beta1.VMCluster) (*appsv1.StatefulSet, error) {

	commonName := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
	podSpec, err := makePodSpecForVMStorage(ctx, cr)
	if err != nil {
		return nil, err
	}

	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            commonName,
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentStorage),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			Finalizers:      []string{vmv1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentStorage),
			},
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VMStorage.RollingUpdateStrategy,
			},
			Template:    *podSpec,
			ServiceName: commonName,
		},
	}
	if cr.Spec.VMStorage.PersistentVolumeClaimRetentionPolicy != nil {
		stsSpec.Spec.PersistentVolumeClaimRetentionPolicy = cr.Spec.VMStorage.PersistentVolumeClaimRetentionPolicy
	}
	cfg := config.MustGetBaseConfig()
	build.StatefulSetAddCommonParams(stsSpec, ptr.Deref(cr.Spec.VMStorage.UseStrictSecurity, cfg.EnableStrictSecurity), &cr.Spec.VMStorage.CommonApplicationDeploymentParams)
	storageSpec := cr.Spec.VMStorage.Storage
	storageSpec.IntoSTSVolume(cr.Spec.VMStorage.GetStorageVolumeName(), &stsSpec.Spec)
	stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, cr.Spec.VMStorage.ClaimTemplates...)

	return stsSpec, nil
}

func makePodSpecForVMStorage(ctx context.Context, cr *vmv1beta1.VMCluster) (*corev1.PodTemplateSpec, error) {
	cfg := config.MustGetBaseConfig()
	args := []string{
		fmt.Sprintf("-vminsertAddr=:%s", cr.Spec.VMStorage.VMInsertPort),
		fmt.Sprintf("-vmselectAddr=:%s", cr.Spec.VMStorage.VMSelectPort),
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VMStorage.Port),
	}
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if cr.Spec.RetentionPeriod != "" {
		args = append(args, fmt.Sprintf("-retentionPeriod=%s", cr.Spec.RetentionPeriod))
	}

	if cr.Spec.VMStorage.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VMStorage.LogLevel))
	}
	if cr.Spec.VMStorage.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VMStorage.LogFormat))
	}

	if len(cr.Spec.VMStorage.ExtraEnvs) > 0 || len(cr.Spec.VMStorage.ExtraEnvsFrom) > 0 {
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

	volumes, vmMounts = build.LicenseVolumeTo(volumes, vmMounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

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
		EnvFrom:                  cr.Spec.VMStorage.ExtraEnvsFrom,
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
	useStrictSecurity := ptr.Deref(cr.Spec.VMStorage.UseStrictSecurity, cfg.EnableStrictSecurity)
	build.AddStrictSecuritySettingsToContainers(cr.Spec.VMStorage.SecurityContext, initContainers, useStrictSecurity)
	ic, err := k8stools.MergePatchContainers(initContainers, cr.Spec.VMStorage.InitContainers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch vmstorage init containers: %w", err)
	}

	build.AddStrictSecuritySettingsToContainers(cr.Spec.VMStorage.SecurityContext, operatorContainers, useStrictSecurity)
	containers, err := k8stools.MergePatchContainers(operatorContainers, cr.Spec.VMStorage.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch vmstorage containers: %w", err)
	}

	for i := range cr.Spec.VMStorage.TopologySpreadConstraints {
		if cr.Spec.VMStorage.TopologySpreadConstraints[i].LabelSelector == nil {
			cr.Spec.VMStorage.TopologySpreadConstraints[i].LabelSelector = &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentStorage),
			}
		}
	}

	vmStoragePodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.PodLabels(vmv1beta1.ClusterComponentStorage),
			Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentStorage),
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

func createOrUpdatePodDisruptionBudgetForVMStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
	pdb := build.PodDisruptionBudget(b, cr.Spec.VMStorage.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMStorage.PodDisruptionBudget != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentStorage)
		prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.VMStorage.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func createOrUpdateVMInsertHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	if cr.Spec.VMInsert.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.PrefixedName(vmv1beta1.ClusterComponentInsert),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentInsert)
	newHPA := build.HPA(b, targetRef, cr.Spec.VMInsert.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VMInsert.HPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentInsert)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.VMInsert.HPA)
	}
	return reconcile.HPA(ctx, rclient, newHPA, prevHPA)
}

func createOrUpdateVMSelectHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	if cr.Spec.VMSelect.HPA == nil {
		return nil
	}
	commonName := cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       commonName,
		Kind:       "StatefulSet",
		APIVersion: "apps/v1",
	}
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentSelect)
	defaultHPA := build.HPA(b, targetRef, cr.Spec.VMSelect.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VMSelect.HPA != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentSelect)
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.VMSelect.HPA)
	}

	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA)
}

func deletePrevStateResources(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	if prevCR == nil {
		// fast path
		return nil
	}
	vmst := cr.Spec.VMStorage
	vmse := cr.Spec.VMSelect
	vmis := cr.Spec.VMInsert
	prevSpec := prevCR.Spec
	prevSt := prevSpec.VMStorage
	prevSe := prevSpec.VMSelect
	prevIs := prevSpec.VMInsert

	prevLB := prevSpec.RequestsLoadBalancer
	newLB := cr.Spec.RequestsLoadBalancer

	cfg := config.MustGetBaseConfig()
	disableSelfScrape := cfg.DisableSelfServiceScrapeCreation
	owner := cr.AsOwner()

	if prevSt != nil {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentStorage)
		if vmst == nil {
			if err := finalize.OnVMStorageDelete(ctx, rclient, cr, prevSt); err != nil {
				return fmt.Errorf("cannot remove storage from prev state: %w", err)
			}
		} else {
			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: commonName}
			if vmst.PodDisruptionBudget == nil && prevSt.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}, &owner); err != nil {
					return fmt.Errorf("cannot remove PDB from prev storage: %w", err)
				}
			}
			if ptr.Deref(vmst.DisableSelfServiceScrape, disableSelfScrape) && !ptr.Deref(prevSt.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}, &owner); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev storage: %w", err)
				}
			}
			prevSvc, currSvc := prevSt.ServiceSpec, vmst.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, commonName, cr.Namespace, prevSvc, currSvc, &owner); err != nil {
				return fmt.Errorf("cannot remove vmstorage additional service: %w", err)
			}
		}
	}

	if prevSe != nil {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentSelect)
		commonInternalName := cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect)
		if vmse == nil {
			if err := finalize.OnVMSelectDelete(ctx, rclient, cr, prevSe); err != nil {
				return fmt.Errorf("cannot remove select from prev state: %w", err)
			}
		} else {
			commonObjMeta := metav1.ObjectMeta{
				Namespace: cr.Namespace, Name: commonName}
			if vmse.PodDisruptionBudget == nil && prevSe.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}, &owner); err != nil {
					return fmt.Errorf("cannot remove PDB from prev select: %w", err)
				}
			}
			if vmse.HPA == nil && prevSe.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}, &owner); err != nil {
					return fmt.Errorf("cannot remove HPA from prev select: %w", err)
				}
			}
			if ptr.Deref(vmse.DisableSelfServiceScrape, disableSelfScrape) && !ptr.Deref(prevSe.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}, &owner); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev select: %w", err)
				}
			}
			prevSvc, currSvc := prevSe.ServiceSpec, vmse.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, commonName, cr.Namespace, prevSvc, currSvc, &owner); err != nil {
				return fmt.Errorf("cannot remove vmselect additional service: %w", err)
			}
		}
		// transition to load-balancer state
		// have to remove prev service scrape
		if newLB.Enabled && !newLB.DisableSelectBalancing && (!prevLB.Enabled || prevLB.DisableSelectBalancing) {
			// remove service scrape because service was renamed
			if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: commonName, Namespace: cr.Namespace},
				}, &owner); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for non-lb select svc: %w", err)
				}
			}
		}
		// disabled loadbalancer only for component
		// transit to the k8s service balancing mode
		if prevLB.Enabled && !prevLB.DisableSelectBalancing && (!newLB.Enabled || newLB.DisableSelectBalancing) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name:      commonInternalName,
				Namespace: cr.Namespace,
			}}, &owner); err != nil {
				return fmt.Errorf("cannot remove vmselect lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      commonInternalName,
						Namespace: cr.Namespace,
					},
				}, &owner); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for lb select svc: %w", err)
				}
			}
		}
	}

	if prevIs != nil {
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentInsert)
		commonInternalName := cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert)
		if vmis == nil {
			if err := finalize.OnVMInsertDelete(ctx, rclient, cr, prevIs); err != nil {
				return fmt.Errorf("cannot remove insert from prev state: %w", err)
			}
		} else {
			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: commonName}
			if vmis.PodDisruptionBudget == nil && prevIs.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}, &owner); err != nil {
					return fmt.Errorf("cannot remove PDB from prev insert: %w", err)
				}
			}
			if vmis.HPA == nil && prevIs.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}, &owner); err != nil {
					return fmt.Errorf("cannot remove HPA from prev insert: %w", err)
				}
			}
			if ptr.Deref(vmis.DisableSelfServiceScrape, disableSelfScrape) && !ptr.Deref(prevIs.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}, &owner); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev insert: %w", err)
				}
			}
			prevSvc, currSvc := prevIs.ServiceSpec, vmis.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, commonName, cr.Namespace, prevSvc, currSvc, &owner); err != nil {
				return fmt.Errorf("cannot remove vminsert additional service: %w", err)
			}
		}
		// transition to load-balancer state
		// have to remove prev service scrape
		if newLB.Enabled && !newLB.DisableInsertBalancing && (!prevLB.Enabled || prevLB.DisableInsertBalancing) {
			// remove service scrape because service was renamed
			if !ptr.Deref(cr.Spec.VMInsert.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: commonName, Namespace: cr.Namespace},
				}, &owner); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for non-lb insert svc: %w", err)
				}
			}
		}
		// disabled loadbalancer only for component
		// transit to the k8s service balancing mode
		if prevLB.Enabled && !prevLB.DisableInsertBalancing && (!newLB.Enabled || newLB.DisableInsertBalancing) {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name:      commonInternalName,
				Namespace: cr.Namespace,
			}}, &owner); err != nil {
				return fmt.Errorf("cannot remove vminsert lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.VMInsert.DisableSelfServiceScrape, disableSelfScrape) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{
						Name:      commonInternalName,
						Namespace: cr.Namespace,
					},
				}, &owner); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for lb vminsert svc: %w", err)
				}
			}
		}
	}

	if prevLB.Enabled && !newLB.Enabled {
		if err := finalize.OnVMClusterLoadBalancerDelete(ctx, rclient, prevCR); err != nil {
			return fmt.Errorf("failed to remove loadbalancer components enabled at prev state: %w", err)
		}
	}
	if newLB.Enabled {
		// case for child objects
		commonName := cr.PrefixedName(vmv1beta1.ClusterComponentBalancer)
		if prevLB.Spec.PodDisruptionBudget != nil && newLB.Spec.PodDisruptionBudget == nil {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      commonName,
					Namespace: cr.Namespace,
				},
			}, &owner); err != nil {
				return fmt.Errorf("cannot delete PodDisruptionBudget for cluster lb: %w", err)
			}
		}
	}

	return nil
}

func buildLBConfigSecretMeta(cr *vmv1beta1.VMCluster) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       cr.Namespace,
		Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
		Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
	}
}

func buildVMAuthLBSecret(cr *vmv1beta1.VMCluster) *corev1.Secret {
	targetHostSuffix := fmt.Sprintf("%s.svc", cr.Namespace)
	if cr.Spec.ClusterDomainName != "" {
		targetHostSuffix += fmt.Sprintf(".%s", cr.Spec.ClusterDomainName)
	}
	insertPort := "8480"
	selectPort := "8481"
	if cr.Spec.VMSelect != nil {
		selectPort = cr.Spec.VMSelect.Port
	}
	if cr.Spec.VMInsert != nil {
		insertPort = cr.Spec.VMInsert.Port
	}
	lbScrt := &corev1.Secret{
		ObjectMeta: buildLBConfigSecretMeta(cr),
		StringData: map[string]string{"config.yaml": fmt.Sprintf(`
unauthorized_user:
  url_map:
  - src_paths:
    - "/insert/.*"
    url_prefix: "http://srv+%s.%s:%s"
    discover_backend_ips: true
  - src_paths:
    - "/.*"
    url_prefix: "http://srv+%s.%s:%s"
    discover_backend_ips: true
      `, cr.PrefixedInternalName(vmv1beta1.ClusterComponentInsert), targetHostSuffix, insertPort,
			cr.PrefixedInternalName(vmv1beta1.ClusterComponentSelect), targetHostSuffix, selectPort,
		)},
	}
	return lbScrt
}

func buildVMAuthLBDeployment(cr *vmv1beta1.VMCluster) (*appsv1.Deployment, error) {
	spec := cr.Spec.RequestsLoadBalancer.Spec
	const configMountName = "vmauth-lb-config"
	volumes := []corev1.Volume{
		{
			Name: configMountName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
				},
			},
		},
	}
	volumes = append(volumes, spec.Volumes...)
	vmounts := []corev1.VolumeMount{
		{
			MountPath: "/opt/vmauth-config/",
			Name:      configMountName,
		},
	}
	vmounts = append(vmounts, spec.VolumeMounts...)

	args := []string{
		"-auth.config=/opt/vmauth-config/config.yaml",
		"-configCheckInterval=30s",
	}
	if spec.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", spec.LogLevel))

	}
	if spec.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", spec.LogFormat))
	}

	cfg := config.MustGetBaseConfig()
	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", spec.Port))
	if cfg.EnableTCP6 {
		args = append(args, "-enableTCP6")
	}
	if len(spec.ExtraEnvs) > 0 || len(spec.ExtraEnvsFrom) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	volumes, vmounts = build.LicenseVolumeTo(volumes, vmounts, cr.Spec.License, vmv1beta1.SecretsDir)
	args = build.LicenseArgsTo(args, cr.Spec.License, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, spec.ExtraArgs, "-")
	sort.Strings(args)
	vmauthLBCnt := corev1.Container{
		Name: "vmauth",
		Ports: []corev1.ContainerPort{
			{
				Protocol:      corev1.ProtocolTCP,
				Name:          "http",
				ContainerPort: intstr.Parse(spec.Port).IntVal,
			},
		},
		Args:            args,
		Env:             spec.ExtraEnvs,
		EnvFrom:         spec.ExtraEnvsFrom,
		Resources:       spec.Resources,
		Image:           fmt.Sprintf("%s:%s", spec.Image.Repository, spec.Image.Tag),
		ImagePullPolicy: spec.Image.PullPolicy,
		VolumeMounts:    vmounts,
	}
	vmauthLBCnt = build.Probe(vmauthLBCnt, &spec)
	containers := []corev1.Container{
		vmauthLBCnt,
	}
	var err error

	build.AddStrictSecuritySettingsToContainers(spec.SecurityContext, containers, ptr.Deref(spec.UseStrictSecurity, cfg.EnableStrictSecurity))
	containers, err = k8stools.MergePatchContainers(containers, spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch containers: %w", err)
	}
	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.RequestsLoadBalancer.Spec.UpdateStrategy != nil {
		strategyType = *cr.Spec.RequestsLoadBalancer.Spec.UpdateStrategy
	}
	lbDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       cr.Namespace,
			Name:            cr.PrefixedName(vmv1beta1.ClusterComponentBalancer),
			Labels:          cr.FinalLabels(vmv1beta1.ClusterComponentBalancer),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.SelectorLabels(vmv1beta1.ClusterComponentBalancer),
			},
			Strategy: appsv1.DeploymentStrategy{
				Type:          strategyType,
				RollingUpdate: cr.Spec.RequestsLoadBalancer.Spec.RollingUpdate,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.PodLabels(vmv1beta1.ClusterComponentBalancer),
					Annotations: cr.PodAnnotations(vmv1beta1.ClusterComponentBalancer),
				},
				Spec: corev1.PodSpec{
					Volumes:            volumes,
					InitContainers:     spec.InitContainers,
					Containers:         containers,
					ServiceAccountName: cr.GetServiceAccountName(),
				},
			},
		},
	}
	build.DeploymentAddCommonParams(lbDep, ptr.Deref(cr.Spec.RequestsLoadBalancer.Spec.UseStrictSecurity, cfg.EnableStrictSecurity), &spec.CommonApplicationDeploymentParams)

	return lbDep, nil
}

func createOrUpdateVMAuthLBService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	builder := func(r *vmv1beta1.VMCluster) *build.ChildBuilder {
		b := build.NewChildBuilder(r, vmv1beta1.ClusterComponentBalancer)
		b.SetFinalLabels(labels.Merge(b.AllLabels(), map[string]string{
			vmv1beta1.VMAuthLBServiceProxyTargetLabel: "vmauth",
		}))
		return b
	}
	b := builder(cr)
	svc := build.Service(b, cr.Spec.RequestsLoadBalancer.Spec.Port, nil)
	var prevSvc *corev1.Service
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Enabled {
		b = builder(prevCR)
		prevSvc = build.Service(b, prevCR.Spec.RequestsLoadBalancer.Spec.Port, nil)
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc); err != nil {
		return fmt.Errorf("cannot reconcile vmauthlb service: %w", err)
	}
	svs := build.VMServiceScrapeForServiceWithSpec(svc, &cr.Spec.RequestsLoadBalancer.Spec)
	svs.Spec.Selector.MatchLabels[vmv1beta1.VMAuthLBServiceProxyTargetLabel] = "vmauth"
	if err := reconcile.VMServiceScrapeForCRD(ctx, rclient, svs); err != nil {
		return fmt.Errorf("cannot reconcile vmauthlb vmservicescrape: %w", err)
	}
	return nil
}

func createOrUpdateVMAuthLB(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {

	var prevSecretMeta *metav1.ObjectMeta
	if prevCR != nil {
		prevSecretMeta = ptr.To(buildLBConfigSecretMeta(prevCR))
	}
	if err := reconcile.Secret(ctx, rclient, buildVMAuthLBSecret(cr), prevSecretMeta); err != nil {
		return fmt.Errorf("cannot reconcile vmauth lb secret: %w", err)
	}
	lbDep, err := buildVMAuthLBDeployment(cr)
	if err != nil {
		return fmt.Errorf("cannot build deployment for vmauth loadbalancing: %w", err)
	}
	var prevLB *appsv1.Deployment
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Enabled {
		prevLB, err = buildVMAuthLBDeployment(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev deployment for vmauth loadbalancing: %w", err)
		}
	}
	if err := reconcile.Deployment(ctx, rclient, lbDep, prevLB, false); err != nil {
		return fmt.Errorf("cannot reconcile vmauth lb deployment: %w", err)
	}
	if err := createOrUpdateVMAuthLBService(ctx, rclient, cr, prevCR); err != nil {
		return err
	}
	if cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget != nil {
		if err := createOrUpdatePodDisruptionBudgetForVMAuthLB(ctx, rclient, cr, prevCR); err != nil {
			return fmt.Errorf("cannot create or update PodDisruptionBudget for vmauth lb: %w", err)
		}
	}
	return nil
}

func createOrUpdatePodDisruptionBudgetForVMAuthLB(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	b := build.NewChildBuilder(cr, vmv1beta1.ClusterComponentBalancer)
	pdb := build.PodDisruptionBudget(b, cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget != nil {
		b = build.NewChildBuilder(prevCR, vmv1beta1.ClusterComponentBalancer)
		prevPDB = build.PodDisruptionBudget(b, prevCR.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}
