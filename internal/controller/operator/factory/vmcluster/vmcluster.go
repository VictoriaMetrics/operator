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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
	appsv1 "k8s.io/api/apps/v1"
	v2 "k8s.io/api/autoscaling/v2"
	"k8s.io/api/autoscaling/v2beta2"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmauthLBServiceProxyJobNameLabel = "operator.victoriametrics.com/vmauthlb-proxy-job-name"
	vmauthLBServiceProxyTargetLabel  = "operator.victoriametrics.com/vmauthlb-proxy-name"
)

// CreateOrUpdateVMCluster reconciled cluster object with order
// first we check status of vmStorage and waiting for its readiness
// then vmSelect and wait for it readiness as well
// and last one is vmInsert
// we manually handle statefulsets rolling updates
// needed in update checked by revesion status
// its controlled by k8s controller-manager
func CreateOrUpdateVMCluster(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {
	var prevCR *vmv1beta1.VMCluster
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
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
	// handle case for loadbalancing
	if cr.Spec.RequestsLoadBalancer.Enabled {
		// create vmauth deployment
		if err := createOrUpdateVMAuthLB(ctx, rclient, cr, prevCR); err != nil {
			return err
		}
	}

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
		if !ptr.Deref(cr.Spec.VMStorage.DisableSelfServiceScrape, false) {
			err := reconcile.VMServiceScrapeForCRD(ctx, rclient, build.VMServiceScrapeForServiceWithSpec(storageSvc, cr.Spec.VMStorage, "http", "vmbackupmanager"))
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
		if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, false) {

			svs := build.VMServiceScrapeForServiceWithSpec(selectSvc, cr.Spec.VMSelect, "http")
			if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
				// for backward compatibility we must keep job label value
				svs.Spec.JobLabel = vmauthLBServiceProxyJobNameLabel
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
		if !ptr.Deref(cr.Spec.VMInsert.DisableSelfServiceScrape, false) {
			svs := build.VMServiceScrapeForServiceWithSpec(insertSvc, cr.Spec.VMInsert, "http")
			if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
				// for backward compatibility we must keep job label value
				svs.Spec.JobLabel = vmauthLBServiceProxyJobNameLabel
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

func buildVMSelectService(cr *vmv1beta1.VMCluster) *corev1.Service {
	t := &clusterSvcBuilder{
		cr,
		cr.GetSelectName(),
		cr.FinalLabels(cr.VMSelectSelectorLabels()),
		cr.VMSelectSelectorLabels(),
		cr.Spec.VMSelect.ServiceSpec,
	}
	svc := build.Service(t, cr.Spec.VMSelect.Port, func(svc *corev1.Service) {
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
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		svc.Name = cr.GetSelectLBName()
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmauthLBServiceProxyJobNameLabel] = cr.GetSelectName()
	}

	return svc

}

func createOrUpdateVMSelectService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) (*corev1.Service, error) {

	svc := buildVMSelectService(cr)
	if err := cr.Spec.VMSelect.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(svc, s)
		if additionalService.Name == svc.Name {
			return fmt.Errorf("vmselect additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, svc.Name)
		} else if err := reconcile.Service(ctx, rclient, additionalService, nil); err != nil {
			return fmt.Errorf("cannot reconcile service for vmselect: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	var prevService *corev1.Service
	if prevCR != nil && prevCR.Spec.VMSelect != nil {
		prevService = buildVMSelectService(prevCR)
	}

	if err := reconcile.Service(ctx, rclient, svc, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile vmselect service: %w", err)
	}
	if cr.Spec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
		var prevPort string
		if prevCR != nil && prevCR.Spec.VMSelect != nil {
			prevPort = prevCR.Spec.VMSelect.Port
		}
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, cr.GetSelectName(), cr.Spec.VMSelect.Port, prevPort, "vmselect", cr.VMAuthLBSelectorLabels()); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for vmselect: %w", err)
		}
	}
	return svc, nil
}

func createOrUpdateLBProxyService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster, svcName, port, prevPort, targetName string, svcSelectorLabels map[string]string) error {

	fls := cr.FinalLabels(svcSelectorLabels)
	fls = labels.Merge(fls, map[string]string{vmauthLBServiceProxyTargetLabel: targetName})
	t := &clusterSvcBuilder{
		cr,
		svcName,
		fls,
		cr.VMAuthLBSelectorLabels(),
		nil,
	}

	svc := build.Service(t, cr.Spec.RequestsLoadBalancer.Spec.Port, func(svc *corev1.Service) {
		svc.Spec.Ports[0].Port = intstr.Parse(port).IntVal
	})

	var prevSvc *corev1.Service
	if prevCR != nil {
		fls := prevCR.FinalLabels(svcSelectorLabels)
		fls = labels.Merge(fls, map[string]string{vmauthLBServiceProxyTargetLabel: targetName})
		t := &clusterSvcBuilder{
			prevCR,
			svcName,
			fls,
			prevCR.VMAuthLBSelectorLabels(),
			nil,
		}

		prevSvc = build.Service(t, prevCR.Spec.RequestsLoadBalancer.Spec.Port, func(svc *corev1.Service) {
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
	t := &clusterSvcBuilder{
		cr,
		cr.GetInsertName(),
		cr.FinalLabels(cr.VMInsertSelectorLabels()),
		cr.VMInsertSelectorLabels(),
		cr.Spec.VMInsert.ServiceSpec,
	}

	svc := build.Service(t, cr.Spec.VMInsert.Port, func(svc *corev1.Service) {
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
		svc.Name = cr.GetInsertLBName()
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmauthLBServiceProxyJobNameLabel] = cr.GetInsertName()
	}
	return svc
}

func createOrUpdateVMInsertService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) (*corev1.Service, error) {

	newService := buildVMInsertService(cr)
	if err := cr.Spec.VMInsert.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newService, s)
		if additionalService.Name == newService.Name {
			return fmt.Errorf("vminsert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name)
		} else if err := reconcile.Service(ctx, rclient, additionalService, nil); err != nil {
			// TODO: @f41gh7 use prevCR
			return fmt.Errorf("cannot reconcile vminsert additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	var prevService *corev1.Service
	if prevCR != nil && prevCR.Spec.VMInsert != nil {
		prevService = buildVMInsertService(prevCR)
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
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, cr.GetInsertName(), cr.Spec.VMInsert.Port, prevPort, "vminsert", cr.VMAuthLBSelectorLabels()); err != nil {
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
		HasClaim:       len(newSts.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.VMStorageSelectorLabels,
	}
	return reconcile.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, prevSts)
}

func createOrUpdateVMStorageService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) (*corev1.Service, error) {
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
			// TODO: @f41gh7 use prevCR
			return fmt.Errorf("cannot reconcile vmstorage additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	var prevService *corev1.Service
	if prevCR != nil && prevCR.Spec.VMStorage != nil {
		prevT := &clusterSvcBuilder{
			prevCR,
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
			Name:            cr.GetSelectName(),
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
			ServiceName: cr.GetSelectName(),
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
	if cr.Spec.VMSelect.HPA == nil && cr.Spec.VMSelect.ReplicaCount != nil {
		selectArg := "-selectNode="
		vmselectCount := *cr.Spec.VMSelect.ReplicaCount
		for i := int32(0); i < vmselectCount; i++ {
			selectArg += cr.Spec.VMSelect.BuildPodName(cr.GetSelectName(), i, cr.Namespace, cr.Spec.VMSelect.Port, cr.Spec.ClusterDomainName)
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

func createOrUpdatePodDisruptionBudgetForVMSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	t := newSvcBuilder(cr, cr.GetSelectName(), cr.VMSelectSelectorLabels())
	pdb := build.PodDisruptionBudget(t, cr.Spec.VMSelect.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMSelect.PodDisruptionBudget != nil {
		t = newSvcBuilder(prevCR, prevCR.GetSelectName(), prevCR.VMSelectSelectorLabels())
		prevPDB = build.PodDisruptionBudget(t, prevCR.Spec.VMSelect.PodDisruptionBudget)
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
	stsSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetInsertName(),
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

func createOrUpdatePodDisruptionBudgetForVMInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	t := newSvcBuilder(cr, cr.GetInsertName(), cr.VMInsertSelectorLabels())
	pdb := build.PodDisruptionBudget(t, cr.Spec.VMInsert.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMInsert.PodDisruptionBudget != nil {
		t = newSvcBuilder(prevCR, prevCR.GetInsertName(), prevCR.VMInsertSelectorLabels())
		prevPDB = build.PodDisruptionBudget(t, prevCR.Spec.VMInsert.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
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

func createOrUpdatePodDisruptionBudgetForVMStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	t := newSvcBuilder(cr, cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), cr.VMStorageSelectorLabels())
	pdb := build.PodDisruptionBudget(t, cr.Spec.VMStorage.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMStorage.PodDisruptionBudget != nil {
		t = newSvcBuilder(prevCR, prevCR.Spec.VMStorage.GetNameWithPrefix(prevCR.Name), prevCR.VMStorageSelectorLabels())
		prevPDB = build.PodDisruptionBudget(t, prevCR.Spec.VMStorage.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func createOrUpdateVMInsertHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	if cr.Spec.VMInsert.HPA == nil {
		return nil
	}
	targetRef := v2beta2.CrossVersionObjectReference{
		Name:       cr.GetInsertName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	defaultHPA := build.HPA(targetRef, cr.Spec.VMInsert.HPA, cr.AsOwner(), cr.VMInsertSelectorLabels(), cr.Namespace)
	var prevHPA *v2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VMInsert.HPA != nil {
		prevHPA = build.HPA(targetRef, prevCR.Spec.VMInsert.HPA, cr.AsOwner(), prevCR.VMInsertSelectorLabels(), cr.Namespace)
	}
	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA)
}

func createOrUpdateVMSelectHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	if cr.Spec.VMSelect.HPA == nil {
		return nil
	}
	targetRef := v2beta2.CrossVersionObjectReference{
		Name:       cr.GetSelectName(),
		Kind:       "StatefulSet",
		APIVersion: "apps/v1",
	}
	defaultHPA := build.HPA(targetRef, cr.Spec.VMSelect.HPA, cr.AsOwner(), cr.VMSelectSelectorLabels(), cr.Namespace)
	var prevHPA *v2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VMSelect.HPA != nil {
		prevHPA = build.HPA(targetRef, prevCR.Spec.VMSelect.HPA, cr.AsOwner(), prevCR.VMSelectSelectorLabels(), cr.Namespace)
	}

	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA)
}

type clusterSvcBuilder struct {
	*vmv1beta1.VMCluster
	prefixedName      string
	finalLabels       map[string]string
	selectorLabels    map[string]string
	additionalService *vmv1beta1.AdditionalServiceSpec
}

// PrefixedName implements build.svcBuilderArgs interface
func (csb *clusterSvcBuilder) PrefixedName() string {
	return csb.prefixedName
}

// AllLabels implements build.svcBuilderArgs interface
func (csb *clusterSvcBuilder) AllLabels() map[string]string {
	return csb.finalLabels
}

// SelectorLabels implements build.svcBuilderArgs interface
func (csb *clusterSvcBuilder) SelectorLabels() map[string]string {
	return csb.selectorLabels
}

// GetAdditionalService implements build.svcBuilderArgs interface
func (csb *clusterSvcBuilder) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return csb.additionalService
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
	if prevSt != nil {
		if vmst == nil {
			if err := finalize.OnVMStorageDelete(ctx, rclient, cr, prevSt); err != nil {
				return fmt.Errorf("cannot remove storage from prev state: %w", err)
			}
		} else {
			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: prevSt.GetNameWithPrefix(cr.Name)}
			if vmst.PodDisruptionBudget == nil && prevSt.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev storage: %w", err)
				}
			}
			if ptr.Deref(vmst.DisableSelfServiceScrape, false) && !ptr.Deref(prevSt.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev storage: %w", err)
				}
			}
			prevSvc, currSvc := prevSt.ServiceSpec, vmst.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, vmst.GetNameWithPrefix(cr.Name), cr.Namespace, prevSvc, currSvc); err != nil {
				return fmt.Errorf("cannot remove vmstorage additional service: %w", err)
			}
		}
	}

	if prevSe != nil {
		if vmse == nil {
			if err := finalize.OnVMSelectDelete(ctx, rclient, cr, prevSe); err != nil {
				return fmt.Errorf("cannot remove select from prev state: %w", err)
			}
		} else {
			commonObjMeta := metav1.ObjectMeta{
				Namespace: cr.Namespace, Name: cr.GetSelectName()}
			if vmse.PodDisruptionBudget == nil && prevSe.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev select: %w", err)
				}
			}
			if vmse.HPA == nil && prevSe.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &v2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove HPA from prev select: %w", err)
				}
			}
			if ptr.Deref(vmse.DisableSelfServiceScrape, false) && !ptr.Deref(prevSe.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev select: %w", err)
				}
			}
			prevSvc, currSvc := prevSe.ServiceSpec, vmse.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, cr.GetSelectName(), cr.Namespace, prevSvc, currSvc); err != nil {
				return fmt.Errorf("cannot remove vmselect additional service: %w", err)
			}
		}
		// transition to load-balancer state
		// have to remove prev service scrape
		if (!prevSpec.RequestsLoadBalancer.Enabled &&
			cr.Spec.RequestsLoadBalancer.Enabled &&
			!cr.Spec.RequestsLoadBalancer.DisableSelectBalancing) ||
			// second case load balancer was enabled, but disabled for select component
			(prevSpec.RequestsLoadBalancer.DisableInsertBalancing &&
				!cr.Spec.RequestsLoadBalancer.DisableInsertBalancing) {
			// remove service scrape because service was renamed
			if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetSelectName(), Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for non-lb select svc: %w", err)
				}
			}
		}
		// disabled loadbalancer only for component
		// transit to the k8s service balancing mode
		if prevSpec.RequestsLoadBalancer.Enabled &&
			!prevSpec.RequestsLoadBalancer.DisableSelectBalancing &&
			cr.Spec.RequestsLoadBalancer.DisableSelectBalancing {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name:      cr.GetSelectLBName(),
				Namespace: cr.Namespace,
			}}); err != nil {
				return fmt.Errorf("cannot remove vmselect lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetSelectLBName(), Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for lb select svc: %w", err)
				}
			}
		}
	}

	if prevIs != nil {
		if vmis == nil {
			if err := finalize.OnVMInsertDelete(ctx, rclient, cr, prevIs); err != nil {
				return fmt.Errorf("cannot remove insert from prev state: %w", err)
			}
		} else {

			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: cr.GetInsertName()}
			if vmis.PodDisruptionBudget == nil && prevIs.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev insert: %w", err)
				}
			}
			if vmis.HPA == nil && prevIs.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &v2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove HPA from prev insert: %w", err)
				}
			}
			if ptr.Deref(vmis.DisableSelfServiceScrape, false) && !ptr.Deref(prevIs.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev insert: %w", err)
				}
			}
			prevSvc, currSvc := prevIs.ServiceSpec, vmis.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, cr.GetInsertName(), cr.Namespace, prevSvc, currSvc); err != nil {
				return fmt.Errorf("cannot remove vminsert additional service: %w", err)
			}
		}
		// transition to load-balancer state
		// have to remove prev service scrape
		if (!prevSpec.RequestsLoadBalancer.Enabled &&
			cr.Spec.RequestsLoadBalancer.Enabled &&
			!cr.Spec.RequestsLoadBalancer.DisableInsertBalancing) ||
			// second case load balancer was enabled, but disabled for insert component
			(prevSpec.RequestsLoadBalancer.DisableInsertBalancing &&
				!cr.Spec.RequestsLoadBalancer.DisableInsertBalancing) {
			// remove service scrape because service was renamed
			if !ptr.Deref(cr.Spec.VMInsert.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetInsertName(), Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for non-lb insert svc: %w", err)
				}
			}
		}
		// disabled loadbalancer only for component
		// transit to the k8s service balancing mode
		if prevSpec.RequestsLoadBalancer.Enabled &&
			!prevSpec.RequestsLoadBalancer.DisableInsertBalancing &&
			cr.Spec.RequestsLoadBalancer.DisableInsertBalancing {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &corev1.Service{ObjectMeta: metav1.ObjectMeta{
				Name:      cr.GetInsertLBName(),
				Namespace: cr.Namespace,
			}}); err != nil {
				return fmt.Errorf("cannot remove vminsert lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.VMInsert.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetInsertLBName(), Namespace: cr.Namespace},
				}); err != nil {
					return fmt.Errorf("cannot delete vmservicescrape for lb vminsert svc: %w", err)
				}
			}
		}
	}

	if prevSpec.RequestsLoadBalancer.Enabled && !cr.Spec.RequestsLoadBalancer.Enabled {
		if err := finalize.OnVMClusterLoadBalancerDelete(ctx, rclient, prevCR); err != nil {
			return fmt.Errorf("failed to remove loadbalancer components enabled at prev state: %w", err)
		}
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		// case for child objects
		prevLBSpec := prevSpec.RequestsLoadBalancer.Spec
		lbSpec := cr.Spec.RequestsLoadBalancer.Spec
		if prevLBSpec.PodDisruptionBudget != nil && lbSpec.PodDisruptionBudget == nil {
			if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cr.GetVMAuthLBName(),
					Namespace: cr.Namespace,
				},
			}); err != nil {
				return fmt.Errorf("cannot delete PodDisruptionBudget for cluster lb: %w", err)
			}
		}
	}

	return nil
}

func buildLBConfigSecretMeta(cr *vmv1beta1.VMCluster) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Namespace:       cr.Namespace,
		Name:            cr.GetVMAuthLBName(),
		Labels:          cr.FinalLabels(cr.VMAuthLBSelectorLabels()),
		Annotations:     cr.AnnotationsFiltered(),
		OwnerReferences: cr.AsOwner(),
	}
}

func buildVMauthLBSecret(cr *vmv1beta1.VMCluster) *corev1.Secret {
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
      `, cr.GetInsertLBName(), targetHostSuffix, insertPort,
			cr.GetSelectLBName(), targetHostSuffix, selectPort,
		)},
	}
	return lbScrt
}

func buildVMauthLBDeployment(cr *vmv1beta1.VMCluster) (*appsv1.Deployment, error) {
	spec := cr.Spec.RequestsLoadBalancer.Spec
	const configMountName = "vmauth-lb-config"
	volumes := []corev1.Volume{
		{
			Name: configMountName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: cr.GetVMAuthLBName(),
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

	args = append(args, fmt.Sprintf("-httpListenAddr=:%s", spec.Port))
	if len(spec.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}
	volumes, vmounts = cr.Spec.License.MaybeAddToVolumes(volumes, vmounts, vmv1beta1.SecretsDir)
	args = cr.Spec.License.MaybeAddToArgs(args, vmv1beta1.SecretsDir)

	args = build.AddExtraArgsOverrideDefaults(args, spec.ExtraArgs, "-")
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

	build.AddStrictSecuritySettingsToContainers(spec.SecurityContext, containers, ptr.Deref(spec.UseStrictSecurity, false))
	containers, err = k8stools.MergePatchContainers(containers, spec.Containers)
	if err != nil {
		return nil, fmt.Errorf("cannot patch containers: %w", err)
	}
	lbDep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:       cr.Namespace,
			Name:            cr.GetVMAuthLBName(),
			Labels:          cr.FinalLabels(cr.VMAuthLBSelectorLabels()),
			Annotations:     cr.AnnotationsFiltered(),
			OwnerReferences: cr.AsOwner(),
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMAuthLBSelectorLabels(),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: cr.VMAuthLBSelectorLabels(),
				},
				Spec: corev1.PodSpec{
					Volumes:        volumes,
					InitContainers: spec.InitContainers,
					Containers:     containers,
				},
			},
		},
	}
	build.DeploymentAddCommonParams(lbDep, ptr.Deref(cr.Spec.RequestsLoadBalancer.Spec.UseStrictSecurity, false), &spec.CommonApplicationDeploymentParams)

	return lbDep, nil
}

func createOrUpdateVMAuthLBService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	lbls := cr.VMAuthLBSelectorLabels()

	// add proxy label directly to the service.labels
	// it'll be used below for vmservicescrape matcher
	// otherwise, it could lead to the multiple targets discovery
	// for the single pod
	// it's caused by multiple services pointed to the same pods
	fls := cr.FinalLabels(lbls)
	fls = labels.Merge(fls, map[string]string{vmauthLBServiceProxyTargetLabel: "vmauth"})
	t := &clusterSvcBuilder{
		VMCluster:         cr,
		prefixedName:      cr.GetVMAuthLBName(),
		finalLabels:       fls,
		selectorLabels:    lbls,
		additionalService: cr.Spec.RequestsLoadBalancer.Spec.AdditionalServiceSpec,
	}
	svc := build.Service(t, cr.Spec.RequestsLoadBalancer.Spec.Port, nil)
	var prevSvc *corev1.Service
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Enabled {
		t.VMCluster = prevCR
		t.additionalService = prevCR.Spec.RequestsLoadBalancer.Spec.AdditionalServiceSpec
		prevSvc = build.Service(t, prevCR.Spec.RequestsLoadBalancer.Spec.Port, nil)
	}

	if err := reconcile.Service(ctx, rclient, svc, prevSvc); err != nil {
		return fmt.Errorf("cannot reconcile vmauthlb service: %w", err)
	}
	svs := build.VMServiceScrapeForServiceWithSpec(svc, &cr.Spec.RequestsLoadBalancer.Spec, "http")
	svs.Spec.Selector.MatchLabels[vmauthLBServiceProxyTargetLabel] = "vmauth"
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
	if err := reconcile.Secret(ctx, rclient, buildVMauthLBSecret(cr), prevSecretMeta); err != nil {
		return fmt.Errorf("cannot reconcile vmauth lb secret: %w", err)
	}
	lbDep, err := buildVMauthLBDeployment(cr)
	if err != nil {
		return fmt.Errorf("cannot build deployment for vmauth loadbalancing: %w", err)
	}
	var prevLB *appsv1.Deployment
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Enabled {
		prevLB, err = buildVMauthLBDeployment(prevCR)
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

	t := newSvcBuilder(cr, cr.GetVMAuthLBName(), cr.VMAuthLBSelectorLabels())
	pdb := build.PodDisruptionBudget(t, cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget != nil {
		t = newSvcBuilder(prevCR, prevCR.GetVMAuthLBName(), prevCR.VMAuthLBSelectorLabels())
		prevPDB = build.PodDisruptionBudget(t, prevCR.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func newSvcBuilder(cr *vmv1beta1.VMCluster, name string, selectorLabels map[string]string) *clusterSvcBuilder {
	return &clusterSvcBuilder{
		VMCluster:      cr,
		prefixedName:   name,
		finalLabels:    cr.FinalLabels(selectorLabels),
		selectorLabels: selectorLabels,
	}
}
