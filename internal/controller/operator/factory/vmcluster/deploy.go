package vmcluster

import (
	"context"
	"fmt"

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
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/finalize"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

const (
	vmauthLBServiceProxyJobNameLabel = "operator.victoriametrics.com/vmauthlb-proxy-job-name"
	vmauthLBServiceProxyTargetLabel  = "operator.victoriametrics.com/vmauthlb-proxy-name"
)

// CreateOrUpdate reconciled cluster object with order
// first we check status of vmStorage and waiting for its readiness
// then vmSelect and wait for it readiness as well
// and last one is vmInsert
// we manually handle statefulsets rolling updates
// needed in update checked by revesion status
// its controlled by k8s controller-manager
func CreateOrUpdate(ctx context.Context, cr *vmv1beta1.VMCluster, rclient client.Client) error {
	var prevCR *vmv1beta1.VMCluster
	if cr.ParsedLastAppliedSpec != nil {
		prevCR = cr.DeepCopy()
		prevCR.Spec = *cr.ParsedLastAppliedSpec
	}
	if cr.IsOwnsServiceAccount() {
		var prevSA *corev1.ServiceAccount
		if prevCR != nil {
			b := newOptsBuilder(prevCR, prevCR.PrefixedName(), prevCR.SelectorLabels())
			prevSA = build.ServiceAccount(b)
		}
		b := newOptsBuilder(cr, cr.PrefixedName(), cr.SelectorLabels())
		if err := reconcile.ServiceAccount(ctx, rclient, build.ServiceAccount(b), prevSA); err != nil {
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
		if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, false) {

			svs := build.VMServiceScrapeForServiceWithSpec(selectSvc, cr.Spec.VMSelect)
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
			svs := build.VMServiceScrapeForServiceWithSpec(insertSvc, cr.Spec.VMInsert)
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

	var prevDeploy *appsv1.StatefulSet
	if prevCR != nil && prevCR.Spec.VMSelect != nil {
		var err error
		prevDeploy, err = newDeployForVMSelect(prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev storage spec: %w", err)
		}
	}
	newDeploy, err := newDeployForVMSelect(cr)
	if err != nil {
		return err
	}

	stsOpts := reconcile.StatefulSetOptions{
		HasClaim:       len(newDeploy.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.VMSelectSelectorLabels,
		HPA:            cr.Spec.VMSelect.HPA,
		UpdateReplicaCount: func(count *int32) {
			if cr.Spec.VMSelect.HPA != nil && count != nil {
				cr.Spec.VMSelect.ReplicaCount = count
			}
		},
	}
	return reconcile.HandleStatefulSetUpdate(ctx, rclient, stsOpts, newDeploy, prevDeploy)
}

func buildVMSelectService(cr *vmv1beta1.VMCluster) *corev1.Service {
	b := &optsBuilder{
		cr,
		cr.GetVMSelectName(),
		cr.FinalLabels(cr.VMSelectSelectorLabels()),
		cr.VMSelectSelectorLabels(),
		cr.Spec.VMSelect.ServiceSpec,
	}
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
		svc.Name = cr.GetVMSelectLBName()
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmauthLBServiceProxyJobNameLabel] = cr.GetVMSelectName()
	}

	return svc

}

func createOrUpdateVMSelectService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) (*corev1.Service, error) {

	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.VMSelect != nil {
		prevService = buildVMSelectService(prevCR)
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.VMSelect.ServiceSpec)
	}
	svc := buildVMSelectService(cr)
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
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, cr.GetVMSelectName(), cr.Spec.VMSelect.Port, prevPort, "vmselect", cr.VMAuthLBSelectorLabels()); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for vmselect: %w", err)
		}
	}
	return svc, nil
}

// createOrUpdateLBProxyService builds vminsert and vmselect external services to expose vmcluster components for access by vmauth
func createOrUpdateLBProxyService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster, svcName, port, prevPort, targetName string, svcSelectorLabels map[string]string) error {

	fls := cr.FinalLabels(svcSelectorLabels)
	fls = labels.Merge(fls, map[string]string{vmauthLBServiceProxyTargetLabel: targetName})
	t := &optsBuilder{
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
		t := &optsBuilder{
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
		prevDeploy, err = newDeployForVMInsert(prevCR)
		if err != nil {
			return fmt.Errorf("cannot generate prev deploy spec: %w", err)
		}
	}
	newDeploy, err := newDeployForVMInsert(cr)
	if err != nil {
		return err
	}
	return reconcile.Deployment(ctx, rclient, newDeploy, prevDeploy, cr.Spec.VMInsert.HPA != nil)
}

func buildVMInsertService(cr *vmv1beta1.VMCluster) *corev1.Service {
	t := &optsBuilder{
		cr,
		cr.GetVMInsertName(),
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
		svc.Name = cr.GetVMInsertLBName()
		svc.Spec.ClusterIP = corev1.ClusterIPNone
		svc.Spec.Type = corev1.ServiceTypeClusterIP
		svc.Labels[vmauthLBServiceProxyJobNameLabel] = cr.GetVMInsertName()
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
		if err := createOrUpdateLBProxyService(ctx, rclient, cr, prevCR, cr.GetVMInsertName(), cr.Spec.VMInsert.Port, prevPort, "vminsert", cr.VMAuthLBSelectorLabels()); err != nil {
			return nil, fmt.Errorf("cannot create lb svc for vminsert: %w", err)
		}
	}

	return newService, nil
}

func createOrUpdateVMStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	var prevDeploy *appsv1.StatefulSet

	if prevCR != nil && prevCR.Spec.VMStorage != nil {
		var err error
		prevDeploy, err = newDeployForVMStorage(ctx, prevCR)
		if err != nil {
			return fmt.Errorf("cannot build prev storage spec: %w", err)
		}
	}
	newDeploy, err := newDeployForVMStorage(ctx, cr)
	if err != nil {
		return err
	}

	stsOpts := reconcile.StatefulSetOptions{
		HasClaim:       len(newDeploy.Spec.VolumeClaimTemplates) > 0,
		SelectorLabels: cr.VMStorageSelectorLabels,
	}
	return reconcile.HandleStatefulSetUpdate(ctx, rclient, stsOpts, newDeploy, prevDeploy)
}

func createOrUpdateVMStorageService(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) (*corev1.Service, error) {
	t := &optsBuilder{
		cr,
		cr.GetVMStorageName(),
		cr.FinalLabels(cr.VMStorageSelectorLabels()),
		cr.VMStorageSelectorLabels(),
		cr.Spec.VMStorage.ServiceSpec,
	}
	var prevService, prevAdditionalService *corev1.Service
	if prevCR != nil && prevCR.Spec.VMStorage != nil {
		prevT := &optsBuilder{
			prevCR,
			prevCR.GetVMStorageName(),
			prevCR.FinalLabels(prevCR.VMStorageSelectorLabels()),
			prevCR.VMStorageSelectorLabels(),
			prevCR.Spec.VMStorage.ServiceSpec,
		}

		prevService = build.Service(prevT, prevCR.Spec.VMStorage.Port, func(svc *corev1.Service) {
			svc.Spec.ClusterIP = "None"
			svc.Spec.PublishNotReadyAddresses = true
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
		prevAdditionalService = build.AdditionalServiceFromDefault(prevService, prevCR.Spec.VMStorage.ServiceSpec)

	}
	newHeadless := build.Service(t, cr.Spec.VMStorage.Port, func(svc *corev1.Service) {
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

	if err := cr.Spec.VMStorage.ServiceSpec.IsSomeAndThen(func(s *vmv1beta1.AdditionalServiceSpec) error {
		additionalService := build.AdditionalServiceFromDefault(newHeadless, s)
		if additionalService.Name == newHeadless.Name {
			return fmt.Errorf("vmstorage additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newHeadless.Name)
		}
		if err := reconcile.Service(ctx, rclient, additionalService, prevAdditionalService); err != nil {
			return fmt.Errorf("cannot reconcile vmstorage additional service: %w", err)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	if err := reconcile.Service(ctx, rclient, newHeadless, prevService); err != nil {
		return nil, fmt.Errorf("cannot reconcile vmstorage service: %w", err)
	}
	return newHeadless, nil
}

func newDeployForVMSelect(cr *vmv1beta1.VMCluster) (*appsv1.StatefulSet, error) {
	podSpec, err := newPodSpecForVMSelect(cr)
	if err != nil {
		return nil, err
	}

	app := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetVMSelectName(),
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
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.VMSelectPodLabels(),
					Annotations: cr.VMSelectPodAnnotations(),
				},
				Spec: *podSpec,
			},
			ServiceName: cr.GetVMSelectName(),
		},
	}
	build.StatefulSetAddCommonParams(app, ptr.Deref(cr.Spec.VMSelect.UseStrictSecurity, false), &cr.Spec.VMSelect.CommonApplicationDeploymentParams)
	if cr.Spec.VMSelect.CacheMountPath != "" {
		storageSpec := cr.Spec.VMSelect.Storage
		// hack, storage is deprecated.
		if storageSpec == nil && cr.Spec.VMSelect.StorageSpec != nil {
			storageSpec = cr.Spec.VMSelect.StorageSpec
		}
		storageSpec.IntoStatefulSetVolume(cr.Spec.VMSelect.GetCacheMountVolumeName(), &app.Spec)
	}
	app.Spec.VolumeClaimTemplates = append(app.Spec.VolumeClaimTemplates, cr.Spec.VMSelect.ClaimTemplates...)
	return app, nil
}

func createOrUpdatePodDisruptionBudgetForVMSelect(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	t := newOptsBuilder(cr, cr.GetVMSelectName(), cr.VMSelectSelectorLabels())
	pdb := build.PodDisruptionBudget(t, cr.Spec.VMSelect.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMSelect.PodDisruptionBudget != nil {
		t = newOptsBuilder(prevCR, prevCR.GetVMSelectName(), prevCR.VMSelectSelectorLabels())
		prevPDB = build.PodDisruptionBudget(t, prevCR.Spec.VMSelect.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func newDeployForVMInsert(cr *vmv1beta1.VMCluster) (*appsv1.Deployment, error) {

	podSpec, err := newPodSpecForVMInsert(cr)
	if err != nil {
		return nil, err
	}

	strategyType := appsv1.RollingUpdateDeploymentStrategyType
	if cr.Spec.VMInsert.UpdateStrategy != nil {
		strategyType = *cr.Spec.VMInsert.UpdateStrategy
	}
	app := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetVMInsertName(),
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
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.VMInsertPodLabels(),
					Annotations: cr.VMInsertPodAnnotations(),
				},
				Spec: *podSpec,
			},
		},
	}
	build.DeploymentAddCommonParams(app, ptr.Deref(cr.Spec.VMInsert.UseStrictSecurity, false), &cr.Spec.VMInsert.CommonApplicationDeploymentParams)
	return app, nil
}

func createOrUpdatePodDisruptionBudgetForVMInsert(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	t := newOptsBuilder(cr, cr.GetVMInsertName(), cr.VMInsertSelectorLabels())
	pdb := build.PodDisruptionBudget(t, cr.Spec.VMInsert.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMInsert.PodDisruptionBudget != nil {
		t = newOptsBuilder(prevCR, prevCR.GetVMInsertName(), prevCR.VMInsertSelectorLabels())
		prevPDB = build.PodDisruptionBudget(t, prevCR.Spec.VMInsert.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func newDeployForVMStorage(ctx context.Context, cr *vmv1beta1.VMCluster) (*appsv1.StatefulSet, error) {
	podSpec, err := newPodSpecForVMStorage(ctx, cr)
	if err != nil {
		return nil, err
	}

	app := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.GetVMStorageName(),
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
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      cr.VMStoragePodLabels(),
					Annotations: cr.VMStoragePodAnnotations(),
				},
				Spec: *podSpec,
			},
			ServiceName: cr.GetVMStorageName(),
		},
	}
	build.StatefulSetAddCommonParams(app, ptr.Deref(cr.Spec.VMStorage.UseStrictSecurity, false), &cr.Spec.VMStorage.CommonApplicationDeploymentParams)
	storageSpec := cr.Spec.VMStorage.Storage
	storageSpec.IntoStatefulSetVolume(cr.Spec.VMStorage.GetStorageVolumeName(), &app.Spec)
	app.Spec.VolumeClaimTemplates = append(app.Spec.VolumeClaimTemplates, cr.Spec.VMStorage.ClaimTemplates...)

	return app, nil
}

func createOrUpdatePodDisruptionBudgetForVMStorage(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	t := newOptsBuilder(cr, cr.GetVMStorageName(), cr.VMStorageSelectorLabels())
	pdb := build.PodDisruptionBudget(t, cr.Spec.VMStorage.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.VMStorage.PodDisruptionBudget != nil {
		t = newOptsBuilder(prevCR, prevCR.GetVMStorageName(), prevCR.VMStorageSelectorLabels())
		prevPDB = build.PodDisruptionBudget(t, prevCR.Spec.VMStorage.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func createOrUpdateVMInsertHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	if cr.Spec.VMInsert.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.GetVMInsertName(),
		Kind:       "Deployment",
		APIVersion: "apps/v1",
	}
	t := newOptsBuilder(cr, cr.GetVMInsertName(), cr.VMInsertSelectorLabels())
	newHPA := build.HPA(t, targetRef, cr.Spec.VMInsert.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VMInsert.HPA != nil {
		t = newOptsBuilder(prevCR, prevCR.GetVMInsertName(), prevCR.VMInsertSelectorLabels())
		prevHPA = build.HPA(t, targetRef, prevCR.Spec.VMInsert.HPA)
	}
	return reconcile.HPA(ctx, rclient, newHPA, prevHPA)
}

func createOrUpdateVMSelectHPA(ctx context.Context, rclient client.Client, cr, prevCR *vmv1beta1.VMCluster) error {
	if cr.Spec.VMSelect.HPA == nil {
		return nil
	}
	targetRef := autoscalingv2.CrossVersionObjectReference{
		Name:       cr.GetVMSelectName(),
		Kind:       "StatefulSet",
		APIVersion: "apps/v1",
	}
	b := newOptsBuilder(cr, cr.GetVMSelectName(), cr.VMSelectSelectorLabels())
	defaultHPA := build.HPA(b, targetRef, cr.Spec.VMSelect.HPA)
	var prevHPA *autoscalingv2.HorizontalPodAutoscaler
	if prevCR != nil && prevCR.Spec.VMSelect.HPA != nil {
		b := newOptsBuilder(prevCR, prevCR.GetVMSelectName(), prevCR.VMSelectSelectorLabels())
		prevHPA = build.HPA(b, targetRef, prevCR.Spec.VMSelect.HPA)
	}

	return reconcile.HPA(ctx, rclient, defaultHPA, prevHPA)
}

type optsBuilder struct {
	*vmv1beta1.VMCluster
	prefixedName      string
	finalLabels       map[string]string
	selectorLabels    map[string]string
	additionalService *vmv1beta1.AdditionalServiceSpec
}

// PrefixedName implements build.svcBuilderArgs interface
func (csb *optsBuilder) PrefixedName() string {
	return csb.prefixedName
}

// AllLabels implements build.svcBuilderArgs interface
func (csb *optsBuilder) AllLabels() map[string]string {
	return csb.finalLabels
}

// SelectorLabels implements build.svcBuilderArgs interface
func (csb *optsBuilder) SelectorLabels() map[string]string {
	return csb.selectorLabels
}

// GetAdditionalService implements build.svcBuilderArgs interface
func (csb *optsBuilder) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
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
			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: cr.GetVMStorageName()}
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
			if err := reconcile.AdditionalServices(ctx, rclient, cr.GetVMStorageName(), cr.Namespace, prevSvc, currSvc); err != nil {
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
				Namespace: cr.Namespace, Name: cr.GetVMSelectName()}
			if vmse.PodDisruptionBudget == nil && prevSe.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev select: %w", err)
				}
			}
			if vmse.HPA == nil && prevSe.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove HPA from prev select: %w", err)
				}
			}
			if ptr.Deref(vmse.DisableSelfServiceScrape, false) && !ptr.Deref(prevSe.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev select: %w", err)
				}
			}
			prevSvc, currSvc := prevSe.ServiceSpec, vmse.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, cr.GetVMSelectName(), cr.Namespace, prevSvc, currSvc); err != nil {
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
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMSelectName(), Namespace: cr.Namespace},
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
				Name:      cr.GetVMSelectLBName(),
				Namespace: cr.Namespace,
			}}); err != nil {
				return fmt.Errorf("cannot remove vmselect lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.VMSelect.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMSelectLBName(), Namespace: cr.Namespace},
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

			commonObjMeta := metav1.ObjectMeta{Namespace: cr.Namespace, Name: cr.GetVMInsertName()}
			if vmis.PodDisruptionBudget == nil && prevIs.PodDisruptionBudget != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &policyv1.PodDisruptionBudget{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove PDB from prev insert: %w", err)
				}
			}
			if vmis.HPA == nil && prevIs.HPA != nil {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove HPA from prev insert: %w", err)
				}
			}
			if ptr.Deref(vmis.DisableSelfServiceScrape, false) && !ptr.Deref(prevIs.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{ObjectMeta: commonObjMeta}); err != nil {
					return fmt.Errorf("cannot remove serviceScrape from prev insert: %w", err)
				}
			}
			prevSvc, currSvc := prevIs.ServiceSpec, vmis.ServiceSpec
			if err := reconcile.AdditionalServices(ctx, rclient, cr.GetVMInsertName(), cr.Namespace, prevSvc, currSvc); err != nil {
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
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMInsertName(), Namespace: cr.Namespace},
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
				Name:      cr.GetVMInsertLBName(),
				Namespace: cr.Namespace,
			}}); err != nil {
				return fmt.Errorf("cannot remove vminsert lb service: %w", err)
			}
			if !ptr.Deref(cr.Spec.VMInsert.DisableSelfServiceScrape, false) {
				if err := finalize.SafeDeleteWithFinalizer(ctx, rclient, &vmv1beta1.VMServiceScrape{
					ObjectMeta: metav1.ObjectMeta{Name: cr.GetVMInsertLBName(), Namespace: cr.Namespace},
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
      `, cr.GetVMInsertLBName(), targetHostSuffix, insertPort,
			cr.GetVMSelectLBName(), targetHostSuffix, selectPort,
		)},
	}
	return lbScrt
}

func buildVMAuthLB(cr *vmv1beta1.VMCluster) (*appsv1.Deployment, error) {
	podSpec, err := newPodSpecForVMAuthLB(cr)
	if err != nil {
		return nil, err
	}
	dep := &appsv1.Deployment{
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
					Labels:      cr.VMAuthLBPodLabels(),
					Annotations: cr.VMAuthLBPodAnnotations(),
				},
				Spec: *podSpec,
			},
		},
	}
	spec := cr.Spec.RequestsLoadBalancer.Spec
	build.DeploymentAddCommonParams(dep, ptr.Deref(spec.UseStrictSecurity, false), &spec.CommonApplicationDeploymentParams)
	return dep, nil
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
	t := &optsBuilder{
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
	svs := build.VMServiceScrapeForServiceWithSpec(svc, &cr.Spec.RequestsLoadBalancer.Spec)
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
	if err := reconcile.Secret(ctx, rclient, buildVMAuthLBSecret(cr), prevSecretMeta); err != nil {
		return fmt.Errorf("cannot reconcile vmauth lb secret: %w", err)
	}
	lbDep, err := buildVMAuthLB(cr)
	if err != nil {
		return fmt.Errorf("cannot build deployment for vmauth loadbalancing: %w", err)
	}
	var prevLB *appsv1.Deployment
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Enabled {
		prevLB, err = buildVMAuthLB(prevCR)
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

	t := newOptsBuilder(cr, cr.GetVMAuthLBName(), cr.VMAuthLBSelectorLabels())
	pdb := build.PodDisruptionBudget(t, cr.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget)
	var prevPDB *policyv1.PodDisruptionBudget
	if prevCR != nil && prevCR.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget != nil {
		t = newOptsBuilder(prevCR, prevCR.GetVMAuthLBName(), prevCR.VMAuthLBSelectorLabels())
		prevPDB = build.PodDisruptionBudget(t, prevCR.Spec.RequestsLoadBalancer.Spec.PodDisruptionBudget)
	}
	return reconcile.PDB(ctx, rclient, pdb, prevPDB)
}

func newOptsBuilder(cr *vmv1beta1.VMCluster, name string, selectorLabels map[string]string) *optsBuilder {
	return &optsBuilder{
		VMCluster:      cr,
		prefixedName:   name,
		finalLabels:    cr.FinalLabels(selectorLabels),
		selectorLabels: selectorLabels,
	}
}
