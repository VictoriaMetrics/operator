package factory

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/VictoriaMetrics/operator/controllers/factory/finalize"
	"k8s.io/api/autoscaling/v2beta2"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	"github.com/VictoriaMetrics/operator/controllers/factory/psp"
	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmStorageDefaultDBPath = "vmstorage-data"
)

// CreateOrUpdateVMCluster reconciled cluster object with order
// first we check status of vmStorage and waiting for its readiness
// then vmSelect and wait for it readiness as well
// and last one is vmInsert
// we manually handle statefulsets rolling updates
// needed in update checked by revesion status
// its controlled by k8s controller-manager
func CreateOrUpdateVMCluster(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (string, error) {
	var expanding, reconciled bool
	status := v1beta1.ClusterStatusFailed
	var reason string
	defer func() {
		if cr.Status.ClusterStatus == v1beta1.ClusterStatusOperational {
			log.Info("no need for resync")
			return
		}
		var actualVMCluster v1beta1.VMCluster
		if err := rclient.Get(ctx, types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}, &actualVMCluster); err != nil {
			log.Error(err, "cannot update actual status for vmcluster")
			return
		}
		actualVMCluster.Status.ClusterStatus = status
		if reconciled {
			actualVMCluster.Status.UpdateFailCount = 0
		}
		// each sync triggers CR reconcile,
		// it may lead to recursive loop,
		// so change status values only at status change.
		if actualVMCluster.Status.Reason != reason {
			actualVMCluster.Status.LastSync = time.Now().String()
		}
		actualVMCluster.Status.Reason = reason

		err := rclient.Status().Update(ctx, &actualVMCluster)
		if err != nil {
			log.Error(err, "cannot update cluster status")
		}
	}()

	if err := psp.CreateServiceAccountForCRD(ctx, cr, rclient); err != nil {
		reason = v1beta1.InternalOperatorError
		return status, fmt.Errorf("failed create service account: %w", err)
	}

	if c.PSPAutoCreateEnabled {
		log.Info("creating psp for vmcluster")
		if err := psp.CreateOrUpdateServiceAccountWithPSP(ctx, cr, rclient); err != nil {
			reason = v1beta1.InternalOperatorError
			return status, fmt.Errorf("cannot create podsecurity policy for vmsingle, err=%w", err)
		}
	}

	if cr.Spec.VMStorage != nil {
		if cr.Spec.VMStorage.PodDisruptionBudget != nil {
			err := CreateOrUpdatePodDisruptionBudgetForVMStorage(ctx, cr, rclient)
			if err != nil {
				reason = "failed to create vmStorage pdb"
				return status, err
			}
		}
		if err := createOrUpdateVMStorage(ctx, cr, rclient, c); err != nil {
			reason = v1beta1.StorageRollingUpdateFailed
			return status, err
		}

		storageSvc, err := CreateOrUpdateVMStorageService(ctx, cr, rclient, c)
		if err != nil {
			reason = "failed to create vmStorage service"
			return status, err
		}
		if !c.DisableSelfServiceScrapeCreation {
			err := CreateVMServiceScrapeFromService(ctx, rclient, storageSvc, cr.MetricPathStorage(), "http")
			if err != nil {
				log.Error(err, "cannot create VMServiceScrape for vmStorage")
			}
		}
		// wait for expand
		expanding, err = waitForExpanding(ctx, rclient, cr.Namespace, cr.VMStorageSelectorLabels(), *cr.Spec.VMStorage.ReplicaCount)
		if err != nil {
			reason = "failed to check for vmStorage expanding"
			return status, err
		}
		if expanding {
			reason = "vmStorage is expanding"
			status = v1beta1.ClusterStatusExpanding
			return status, err
		}

	}

	if cr.Spec.VMSelect != nil {
		if cr.Spec.VMSelect.PodDisruptionBudget != nil {
			err := CreateOrUpdatePodDisruptionBudgetForVMSelect(ctx, cr, rclient)
			if err != nil {
				reason = "failed to create vmSelect pdb"
				return status, err
			}
		}
		if err := createOrUpdateVMSelect(ctx, cr, rclient, c); err != nil {
			reason = v1beta1.SelectCreationFailed
			return status, err
		}

		if err := createOrUpdateVMSelectHPA(ctx, rclient, cr); err != nil {
			reason = "cannot create HPA"
			return status, err
		}
		// create vmselect service
		selectSvc, err := CreateOrUpdateVMSelectService(ctx, cr, rclient, c)
		if err != nil {
			reason = "failed to create vmSelect service"
			return status, err
		}
		if !c.DisableSelfServiceScrapeCreation {
			err := CreateVMServiceScrapeFromService(ctx, rclient, selectSvc, cr.MetricPathSelect(), "http")
			if err != nil {
				log.Error(err, "cannot create VMServiceScrape for vmSelect")
			}
		}

		// wait for expand
		expanding, err = waitForExpanding(ctx, rclient, cr.Namespace, cr.VMSelectSelectorLabels(), *cr.Spec.VMSelect.ReplicaCount)
		if err != nil {
			reason = "failed to wait for vmSelect expanding"
			return status, err
		}
		if expanding {
			reason = "expanding vmSelect"
			status = v1beta1.ClusterStatusExpanding
			return status, err
		}

	}

	if cr.Spec.VMInsert != nil {
		if cr.Spec.VMInsert.PodDisruptionBudget != nil {
			err := CreateOrUpdatePodDisruptionBudgetForVMInsert(ctx, cr, rclient)
			if err != nil {
				reason = "failed to create vmInsert pdb"
				return status, err
			}
		}
		_, err := createOrUpdateVMInsert(ctx, cr, rclient, c)
		if err != nil {
			reason = v1beta1.InsertCreationFailed
			return status, err
		}
		insertSvc, err := CreateOrUpdateVMInsertService(ctx, cr, rclient, c)
		if err != nil {
			reason = "failed to create vmInsert service"
			return status, err
		}
		if err := createOrUpdateVMInsertHPA(ctx, rclient, cr); err != nil {
			reason = "cannot create HPA"
			return status, err
		}
		if !c.DisableSelfServiceScrapeCreation {
			err := CreateVMServiceScrapeFromService(ctx, rclient, insertSvc, cr.MetricPathInsert(), "http")
			if err != nil {
				log.Error(err, "cannot create VMServiceScrape for vmInsert")
			}
		}
		expanding, err = waitForExpanding(ctx, rclient, cr.Namespace, cr.VMInsertSelectorLabels(), *cr.Spec.VMInsert.ReplicaCount)
		if err != nil {
			reason = "failed to wait for vmInsert expanding"
			return status, err
		}
		if expanding {
			reason = "expanding vmInsert"
			status = v1beta1.ClusterStatusExpanding
			return status, err
		}

	}
	reconciled = true
	status = v1beta1.ClusterStatusOperational
	log.Info("created or updated vmCluster ")
	return status, nil

}

func createOrUpdateVMSelect(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) error {
	l := log.WithValues("controller", "vmselect", "cluster", cr.Name)
	l.Info("create or update vmselect for cluster")

	// its tricky part.
	// we need replicas count from hpa to create proper args.
	// note, need to make copy of current crd. to able to change it without side effects.
	cr = cr.DeepCopy()
	var needCreate bool
	var currentSts appsv1.StatefulSet
	err := rclient.Get(ctx, types.NamespacedName{Name: cr.Spec.VMSelect.GetNameWithPrefix(cr.Name), Namespace: cr.Namespace}, &currentSts)
	if err != nil {
		if errors.IsNotFound(err) {
			needCreate = true
		} else {
			return fmt.Errorf("cannot get vmselect sts: %w", err)
		}
	}
	// update replicas count.
	if cr.Spec.VMSelect.HPA != nil && currentSts.Spec.Replicas != nil {
		cr.Spec.VMSelect.ReplicaCount = currentSts.Spec.Replicas
	}

	newSts, err := genVMSelectSpec(cr, c)
	if err != nil {
		return err
	}

	// fast path for create new sts.
	if needCreate {
		l.Info("vmselect sts not found, creating new one")
		if err := rclient.Create(ctx, newSts); err != nil {
			return fmt.Errorf("cannot create new vmselect sts: %w", err)
		}
		return nil
	}

	if currentSts.ManagedFields != nil {
		newSts.ManagedFields = currentSts.ManagedFields
	}
	newSts.Annotations = labels.Merge(currentSts.Annotations, newSts.Annotations)
	// hack for break reconcile loop at kubernetes 1.18
	newSts.Status.Replicas = currentSts.Status.Replicas
	// do not change replicas count.
	if cr.Spec.VMSelect.HPA != nil {
		newSts.Spec.Replicas = currentSts.Spec.Replicas
	}

	stsOpts := k8stools.STSOptions{
		VolumeName:     cr.Spec.VMSelect.GetCacheMountVolumeName,
		SelectorLabels: cr.VMSelectSelectorLabels,
		UpdateStrategy: cr.Spec.VMSelect.UpdateStrategy,
	}
	return k8stools.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, &currentSts, c)
}

func CreateOrUpdateVMSelectService(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	cr = cr.DeepCopy()
	if cr.Spec.VMSelect.Port == "" {
		cr.Spec.VMSelect.Port = c.VMClusterDefault.VMSelectDefault.Port
	}
	additionalService := genVMSelectService(cr)
	mergeServiceSpec(additionalService, cr.Spec.VMSelect.ServiceSpec)

	newHeadless := genVMSelectHeadlessService(cr)

	if cr.Spec.VMSelect.ServiceSpec != nil {
		if additionalService.Name == newHeadless.Name {
			log.Error(fmt.Errorf("vmselect additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newHeadless.Name), "cannot create additional service")
		} else if _, err := reconcileServiceForCRD(ctx, rclient, additionalService); err != nil {
			return nil, err
		}
	}
	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.VMSelectSelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: func() string {
		return cr.Spec.VMSelect.GetNameWithPrefix(cr.Name)
	}}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.VMSelect.ServiceSpec); err != nil {
		return nil, err
	}

	return reconcileServiceForCRD(ctx, rclient, newHeadless)
}

func createOrUpdateVMInsert(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {
	l := log.WithValues("controller", "vminsert", "cluster", cr.Name)
	l.Info("create or update vminsert for cluster")
	newDeployment, err := genVMInsertSpec(cr, c)
	if err != nil {
		return nil, err
	}

	currentDeployment := &appsv1.Deployment{}
	err = rclient.Get(ctx, types.NamespacedName{Name: newDeployment.Name, Namespace: newDeployment.Namespace}, currentDeployment)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("vminsert deploy not found, creating new one")
			if err := rclient.Create(ctx, newDeployment); err != nil {
				return nil, fmt.Errorf("cannot create new vminsert deploy: %w", err)
			}
			l.Info("new vminsert deploy was created")
			return newDeployment, nil
		}
		return nil, fmt.Errorf("cannot get vminsert deploy: %w", err)
	}

	// inherit replicas count if hpa enabled.
	if cr.Spec.VMInsert.HPA != nil {
		newDeployment.Spec.Replicas = currentDeployment.Spec.Replicas
	}

	newDeployment.Annotations = labels.Merge(currentDeployment.Annotations, newDeployment.Annotations)
	newDeployment.Finalizers = v1beta1.MergeFinalizers(newDeployment, v1beta1.FinalizerName)
	err = rclient.Update(ctx, newDeployment)
	if err != nil {
		return nil, fmt.Errorf("cannot update vminsert deploy: %w", err)
	}
	l.Info("vminsert deploy was reconciled")

	return newDeployment, nil
}

// CreateOrUpdateVMInsertService reconciles vminsert services.
func CreateOrUpdateVMInsertService(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	cr = cr.DeepCopy()
	if cr.Spec.VMInsert.Port == "" {
		cr.Spec.VMInsert.Port = c.VMClusterDefault.VMInsertDefault.Port
	}
	additionalService := defaultVMInsertService(cr)
	mergeServiceSpec(additionalService, cr.Spec.VMInsert.ServiceSpec)
	buildAdditionalServicePorts(cr.Spec.VMInsert.InsertPorts, additionalService)

	newService := defaultVMInsertService(cr)
	buildAdditionalServicePorts(cr.Spec.VMInsert.InsertPorts, newService)

	if cr.Spec.VMInsert.ServiceSpec != nil {
		if additionalService.Name == newService.Name {
			log.Error(fmt.Errorf("vminsert additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newService.Name), "cannot create additional service")
		} else if _, err := reconcileServiceForCRD(ctx, rclient, additionalService); err != nil {
			return nil, err
		}
	}
	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.VMInsertSelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: func() string {
		return cr.Spec.VMInsert.GetNameWithPrefix(cr.Name)
	}}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.VMInsert.ServiceSpec); err != nil {
		return nil, err
	}

	return reconcileServiceForCRD(ctx, rclient, newService)
}

func createOrUpdateVMStorage(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) error {
	l := log.WithValues("controller", "vmstorage", "cluster", cr.Name)
	l.Info("create or update vmstorage for cluster")
	newSts, err := GenVMStorageSpec(cr, c)
	if err != nil {
		return err
	}
	currentSts := &appsv1.StatefulSet{}
	err = rclient.Get(ctx, types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, currentSts)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new sts for vmstorage")
			if err := rclient.Create(ctx, newSts); err != nil {
				return fmt.Errorf("cannot create new sts for vmstorage")
			}
			return nil
		}
		return fmt.Errorf("cannot get vmstorage sts: %w", err)
	}
	l.Info("vmstorage was found, updating it")

	newSts.Annotations = labels.Merge(currentSts.Annotations, newSts.Annotations)
	// hack for break reconcile loop at kubernetes 1.18
	newSts.Status.Replicas = currentSts.Status.Replicas

	stsOpts := k8stools.STSOptions{
		VolumeName:     cr.Spec.VMStorage.GetStorageVolumeName,
		SelectorLabels: cr.VMStorageSelectorLabels,
		UpdateStrategy: cr.Spec.VMStorage.UpdateStrategy,
	}
	return k8stools.HandleSTSUpdate(ctx, rclient, stsOpts, newSts, currentSts, c)
}

func CreateOrUpdateVMStorageService(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	newHeadless := genVMStorageHeadlessService(cr, c)
	additionalService := genVMStorageService(cr, c)
	mergeServiceSpec(additionalService, cr.Spec.VMStorage.ServiceSpec)

	if cr.Spec.VMStorage.ServiceSpec != nil {
		if additionalService.Name == newHeadless.Name {
			log.Error(fmt.Errorf("vmstorage additional service name: %q cannot be the same as crd.prefixedname: %q", additionalService.Name, newHeadless.Name), "cannot create additional service")
		} else if _, err := reconcileServiceForCRD(ctx, rclient, additionalService); err != nil {
			return nil, err
		}
	}

	rca := finalize.RemoveSvcArgs{SelectorLabels: cr.VMStorageSelectorLabels, GetNameSpace: cr.GetNamespace, PrefixedName: func() string {
		return cr.Spec.VMStorage.GetNameWithPrefix(cr.Name)
	}}
	if err := finalize.RemoveOrphanedServices(ctx, rclient, rca, cr.Spec.VMStorage.ServiceSpec); err != nil {
		return nil, err
	}

	return reconcileServiceForCRD(ctx, rclient, newHeadless)
}

func genVMSelectSpec(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
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
	if cr.Spec.VMSelect.SecurityContext == nil {
		cr.Spec.VMSelect.SecurityContext = &corev1.PodSecurityContext{}
	}
	podSpec, err := makePodSpecForVMSelect(cr, c)
	if err != nil {
		return nil, err
	}

	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMSelectSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: cr.Spec.VMSelect.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMSelectSelectorLabels(),
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VMSelect.UpdateStrategy(),
			},
			Template:             *podSpec,
			ServiceName:          cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			RevisionHistoryLimit: pointer.Int32Ptr(10),
		},
	}
	if cr.Spec.VMSelect.CacheMountPath != "" {
		storageSpec := cr.Spec.VMSelect.Storage
		// hack, storage is deprecated.
		if storageSpec == nil && cr.Spec.VMSelect.StorageSpec != nil {
			storageSpec = cr.Spec.VMSelect.StorageSpec
		}
		switch {
		case storageSpec == nil:
			stsSpec.Spec.Template.Spec.Volumes = append(stsSpec.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: cr.Spec.VMSelect.GetCacheMountVolumeName(),
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
		case storageSpec.EmptyDir != nil:
			emptyDir := storageSpec.EmptyDir
			stsSpec.Spec.Template.Spec.Volumes = append(stsSpec.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: cr.Spec.VMSelect.GetCacheMountVolumeName(),
				VolumeSource: corev1.VolumeSource{
					EmptyDir: emptyDir,
				},
			})
		default:
			pvcTemplate := MakeVolumeClaimTemplate(storageSpec.VolumeClaimTemplate)
			if pvcTemplate.Name == "" {
				pvcTemplate.Name = cr.Spec.VMSelect.GetCacheMountVolumeName()
			}
			if storageSpec.VolumeClaimTemplate.Spec.AccessModes == nil {
				pvcTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
			} else {
				pvcTemplate.Spec.AccessModes = storageSpec.VolumeClaimTemplate.Spec.AccessModes
			}
			pvcTemplate.Spec.Resources = storageSpec.VolumeClaimTemplate.Spec.Resources
			pvcTemplate.Spec.Selector = storageSpec.VolumeClaimTemplate.Spec.Selector
			stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, *pvcTemplate)
		}
	}
	return stsSpec, nil
}

func makePodSpecForVMSelect(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VMSelect.Port),
	}
	if cr.Spec.VMSelect.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VMSelect.LogLevel))
	}
	if cr.Spec.VMSelect.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VMSelect.LogFormat))
	}
	if cr.Spec.ReplicationFactor != nil {
		var dedupIsSet bool
		for arg := range cr.Spec.VMSelect.ExtraArgs {
			if strings.Contains(arg, "dedup.minScrapeInterval") {
				dedupIsSet = true
			}
		}
		if !dedupIsSet {
			args = append(args, "-dedup.minScrapeInterval=1ms")
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

		log.Info("built args with vmstorage nodes for vmselect", "vmstorage args", storageArg)
		args = append(args, storageArg)

	}
	selectArg := "-selectNode="
	vmselectCount := *cr.Spec.VMSelect.ReplicaCount
	for i := int32(0); i < vmselectCount; i++ {
		selectArg += cr.Spec.VMSelect.BuildPodName(cr.Spec.VMSelect.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMSelect.Port, c.ClusterDomainName)
	}
	selectArg = strings.TrimSuffix(selectArg, ",")

	log.Info("args for vmselect ", "args", selectArg)
	args = append(args, selectArg)

	if len(cr.Spec.VMSelect.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
	}

	var envs []corev1.EnvVar
	envs = append(envs, cr.Spec.VMSelect.ExtraEnvs...)

	var ports []corev1.ContainerPort
	ports = append(ports, corev1.ContainerPort{Name: "http", Protocol: "TCP", ContainerPort: intstr.Parse(cr.Spec.VMSelect.Port).IntVal})
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
			MountPath: path.Join(SecretsDir, s),
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
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}

	args = addExtraArgsOverrideDefaults(args, cr.Spec.VMSelect.ExtraArgs)
	sort.Strings(args)
	vmselectContainer := corev1.Container{
		Name:                     "vmselect",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VMSelect.Image.Repository, cr.Spec.VMSelect.Image.Tag),
		ImagePullPolicy:          cr.Spec.VMSelect.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                buildResources(cr.Spec.VMSelect.Resources, config.Resource(c.VMClusterDefault.VMSelectDefault.Resource), c.VMClusterDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	vmselectContainer = buildProbe(vmselectContainer, cr.Spec.VMSelect.EmbeddedProbes, cr.HealthPathSelect, cr.Spec.VMSelect.Port, true)

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

	vmSelectPodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMSelectPodLabels(),
			Annotations: cr.VMSelectPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			NodeSelector:                  cr.Spec.VMSelect.NodeSelector,
			Volumes:                       volumes,
			InitContainers:                cr.Spec.VMSelect.InitContainers,
			Containers:                    containers,
			ServiceAccountName:            cr.GetServiceAccountName(),
			SecurityContext:               cr.Spec.VMSelect.SecurityContext,
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.VMSelect.Affinity,
			SchedulerName:                 cr.Spec.VMSelect.SchedulerName,
			RuntimeClassName:              cr.Spec.VMSelect.RuntimeClassName,
			Tolerations:                   cr.Spec.VMSelect.Tolerations,
			PriorityClassName:             cr.Spec.VMSelect.PriorityClassName,
			HostNetwork:                   cr.Spec.VMSelect.HostNetwork,
			DNSPolicy:                     cr.Spec.VMSelect.DNSPolicy,
			RestartPolicy:                 "Always",
			TerminationGracePeriodSeconds: pointer.Int64Ptr(30),
			TopologySpreadConstraints:     cr.Spec.VMSelect.TopologySpreadConstraints,
		},
	}

	return vmSelectPodSpec, nil
}

func genVMSelectService(cr *v1beta1.VMCluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMSelectSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: cr.VMSelectSelectorLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       intstr.Parse(cr.Spec.VMSelect.Port).IntVal,
					TargetPort: intstr.Parse(cr.Spec.VMSelect.Port),
				},
			},
		},
	}
}
func genVMSelectHeadlessService(cr *v1beta1.VMCluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMSelectSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			Selector:  cr.VMSelectSelectorLabels(),
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       intstr.Parse(cr.Spec.VMSelect.Port).IntVal,
					TargetPort: intstr.Parse(cr.Spec.VMSelect.Port),
				},
			},
		},
	}
}

func CreateOrUpdatePodDisruptionBudgetForVMSelect(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client) error {
	pdb := &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			Labels:          cr.FinalLabels(cr.VMSelectSelectorLabels()),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			MinAvailable:   cr.Spec.VMSelect.PodDisruptionBudget.MinAvailable,
			MaxUnavailable: cr.Spec.VMSelect.PodDisruptionBudget.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMSelectSelectorLabels(),
			},
		},
	}
	return reconcilePDB(ctx, rclient, cr.Kind, pdb)
}

func genVMInsertSpec(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {
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
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cr.Spec.VMInsert.ReplicaCount,
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

func makePodSpecForVMInsert(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
	args := []string{
		fmt.Sprintf("-httpListenAddr=:%s", cr.Spec.VMInsert.Port),
	}
	if cr.Spec.VMInsert.LogLevel != "" {
		args = append(args, fmt.Sprintf("-loggerLevel=%s", cr.Spec.VMInsert.LogLevel))
	}
	if cr.Spec.VMInsert.LogFormat != "" {
		args = append(args, fmt.Sprintf("-loggerFormat=%s", cr.Spec.VMInsert.LogFormat))
	}

	args = buildArgsForAdditionalPorts(args, cr.Spec.VMInsert.InsertPorts)

	if cr.Spec.VMStorage != nil && cr.Spec.VMStorage.ReplicaCount != nil {
		if cr.Spec.VMStorage.VMInsertPort == "" {
			cr.Spec.VMStorage.VMInsertPort = c.VMClusterDefault.VMStorageDefault.VMInsertPort
		}
		storageArg := "-storageNode="
		for _, i := range cr.AvailableStorageNodeIDs("insert") {
			storageArg += cr.Spec.VMStorage.BuildPodName(cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMStorage.VMInsertPort, c.ClusterDomainName)
		}
		storageArg = strings.TrimSuffix(storageArg, ",")
		log.Info("args for vminsert ", "storage arg", storageArg)

		args = append(args, storageArg)

	}
	if cr.Spec.ReplicationFactor != nil {
		log.Info("replication enabled for vminsert, with factor", "replicationFactor", *cr.Spec.ReplicationFactor)
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
	ports = buildAdditionalContainerPorts(ports, cr.Spec.VMInsert.InsertPorts)
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
			MountPath: path.Join(SecretsDir, s),
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
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}
	args = addExtraArgsOverrideDefaults(args, cr.Spec.VMInsert.ExtraArgs)
	sort.Strings(args)

	vminsertContainer := corev1.Container{
		Name:                     "vminsert",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VMInsert.Image.Repository, cr.Spec.VMInsert.Image.Tag),
		ImagePullPolicy:          cr.Spec.VMInsert.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                buildResources(cr.Spec.VMInsert.Resources, config.Resource(c.VMClusterDefault.VMInsertDefault.Resource), c.VMClusterDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
	}

	vminsertContainer = buildProbe(vminsertContainer, cr.Spec.VMInsert.EmbeddedProbes, cr.HealthPathInsert, cr.Spec.VMInsert.Port, true)

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

	vmInsertPodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMInsertPodLabels(),
			Annotations: cr.VMInsertPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			NodeSelector:              cr.Spec.VMInsert.NodeSelector,
			Volumes:                   volumes,
			InitContainers:            cr.Spec.VMInsert.InitContainers,
			Containers:                containers,
			ServiceAccountName:        cr.GetServiceAccountName(),
			SecurityContext:           cr.Spec.VMInsert.SecurityContext,
			ImagePullSecrets:          cr.Spec.ImagePullSecrets,
			Affinity:                  cr.Spec.VMInsert.Affinity,
			SchedulerName:             cr.Spec.VMInsert.SchedulerName,
			RuntimeClassName:          cr.Spec.VMInsert.RuntimeClassName,
			Tolerations:               cr.Spec.VMInsert.Tolerations,
			PriorityClassName:         cr.Spec.VMInsert.PriorityClassName,
			HostNetwork:               cr.Spec.VMInsert.HostNetwork,
			DNSPolicy:                 cr.Spec.VMInsert.DNSPolicy,
			TopologySpreadConstraints: cr.Spec.VMInsert.TopologySpreadConstraints,
		},
	}

	return vmInsertPodSpec, nil

}

func defaultVMInsertService(cr *v1beta1.VMCluster) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMInsert.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMInsertSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: cr.VMInsertSelectorLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       intstr.Parse(cr.Spec.VMInsert.Port).IntVal,
					TargetPort: intstr.Parse(cr.Spec.VMInsert.Port),
				},
			},
		},
	}
}

func CreateOrUpdatePodDisruptionBudgetForVMInsert(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client) error {
	pdb := &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMInsert.GetNameWithPrefix(cr.Name),
			Labels:          cr.FinalLabels(cr.VMInsertSelectorLabels()),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			MinAvailable:   cr.Spec.VMInsert.PodDisruptionBudget.MinAvailable,
			MaxUnavailable: cr.Spec.VMInsert.PodDisruptionBudget.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMInsertSelectorLabels(),
			},
		},
	}
	return reconcilePDB(ctx, rclient, cr.Kind, pdb)
}

func GenVMStorageSpec(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
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
	if cr.Spec.VMStorage.SecurityContext == nil {
		cr.Spec.VMStorage.SecurityContext = &corev1.PodSecurityContext{}
	}
	if cr.Spec.VMStorage.Image.PullPolicy == "" {
		cr.Spec.VMStorage.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if cr.Spec.VMStorage.StorageDataPath == "" {
		cr.Spec.VMStorage.StorageDataPath = vmStorageDefaultDBPath
	}
	podSpec, err := makePodSpecForVMStorage(cr, c)
	if err != nil {
		return nil, err
	}

	stsSpec := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMStorageSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: cr.Spec.VMStorage.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMStorageSelectorLabels(),
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: cr.Spec.VMStorage.UpdateStrategy(),
			},
			Template:             *podSpec,
			ServiceName:          cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			RevisionHistoryLimit: pointer.Int32Ptr(10),
		},
	}
	storageSpec := cr.Spec.VMStorage.Storage
	switch {
	case storageSpec == nil:
		stsSpec.Spec.Template.Spec.Volumes = append(stsSpec.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: cr.Spec.VMStorage.GetStorageVolumeName(),
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	case storageSpec.EmptyDir != nil:
		emptyDir := storageSpec.EmptyDir
		stsSpec.Spec.Template.Spec.Volumes = append(stsSpec.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: cr.Spec.VMStorage.GetStorageVolumeName(),
			VolumeSource: corev1.VolumeSource{
				EmptyDir: emptyDir,
			},
		})
	default:
		pvcTemplate := MakeVolumeClaimTemplate(storageSpec.VolumeClaimTemplate)
		if pvcTemplate.Name == "" {
			pvcTemplate.Name = cr.Spec.VMStorage.GetStorageVolumeName()
		}
		if storageSpec.VolumeClaimTemplate.Spec.AccessModes == nil {
			pvcTemplate.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}
		} else {
			pvcTemplate.Spec.AccessModes = storageSpec.VolumeClaimTemplate.Spec.AccessModes
		}
		pvcTemplate.Spec.Resources = storageSpec.VolumeClaimTemplate.Spec.Resources
		pvcTemplate.Spec.Selector = storageSpec.VolumeClaimTemplate.Spec.Selector
		stsSpec.Spec.VolumeClaimTemplates = append(stsSpec.Spec.VolumeClaimTemplates, *pvcTemplate)
	}

	return stsSpec, nil
}

func makePodSpecForVMStorage(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) (*corev1.PodTemplateSpec, error) {
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

	if cr.Spec.ReplicationFactor != nil {
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
			MountPath: path.Join(SecretsDir, s),
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
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}

	args = addExtraArgsOverrideDefaults(args, cr.Spec.VMStorage.ExtraArgs)
	sort.Strings(args)
	vmstorageContainer := corev1.Container{
		Name:                     "vmstorage",
		Image:                    fmt.Sprintf("%s:%s", cr.Spec.VMStorage.Image.Repository, cr.Spec.VMStorage.Image.Tag),
		ImagePullPolicy:          cr.Spec.VMStorage.Image.PullPolicy,
		Ports:                    ports,
		Args:                     args,
		VolumeMounts:             vmMounts,
		Resources:                buildResources(cr.Spec.VMStorage.Resources, config.Resource(c.VMClusterDefault.VMStorageDefault.Resource), c.VMClusterDefault.UseDefaultResources),
		Env:                      envs,
		TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		TerminationMessagePath:   "/dev/termination-log",
	}

	vmstorageContainer = buildProbe(vmstorageContainer, cr.Spec.VMStorage.EmbeddedProbes, cr.HealthPathStorage, cr.Spec.VMStorage.Port, false)

	operatorContainers := []corev1.Container{vmstorageContainer}

	if cr.Spec.VMStorage.VMBackup != nil {
		vmBackupManagerContainer, err := makeSpecForVMBackuper(cr.Spec.VMStorage.VMBackup, c, cr.Spec.VMStorage.Port, cr.Spec.VMStorage.StorageDataPath, cr.Spec.VMStorage.GetStorageVolumeName(), cr.Spec.VMStorage.ExtraArgs, true)
		if err != nil {
			return nil, err
		}
		if vmBackupManagerContainer != nil {
			operatorContainers = append(operatorContainers, *vmBackupManagerContainer)
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

	vmStoragePodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMStoragePodLabels(),
			Annotations: cr.VMStoragePodAnnotations(),
		},
		Spec: corev1.PodSpec{
			NodeSelector:                  cr.Spec.VMStorage.NodeSelector,
			Volumes:                       volumes,
			InitContainers:                cr.Spec.VMStorage.InitContainers,
			Containers:                    containers,
			ServiceAccountName:            cr.GetServiceAccountName(),
			SecurityContext:               cr.Spec.VMStorage.SecurityContext,
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.VMStorage.Affinity,
			SchedulerName:                 cr.Spec.VMStorage.SchedulerName,
			RuntimeClassName:              cr.Spec.VMStorage.RuntimeClassName,
			Tolerations:                   cr.Spec.VMStorage.Tolerations,
			PriorityClassName:             cr.Spec.VMStorage.PriorityClassName,
			HostNetwork:                   cr.Spec.VMStorage.HostNetwork,
			DNSPolicy:                     cr.Spec.VMStorage.DNSPolicy,
			RestartPolicy:                 "Always",
			TerminationGracePeriodSeconds: pointer.Int64Ptr(30),
			TopologySpreadConstraints:     cr.Spec.VMStorage.TopologySpreadConstraints,
		},
	}

	return vmStoragePodSpec, nil
}

func genVMStorageHeadlessService(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.VMStorage.Port == "" {
		cr.Spec.VMStorage.Port = c.VMClusterDefault.VMStorageDefault.Port
	}
	if cr.Spec.VMStorage.VMSelectPort == "" {
		cr.Spec.VMStorage.VMSelectPort = c.VMClusterDefault.VMStorageDefault.VMSelectPort
	}
	if cr.Spec.VMStorage.VMInsertPort == "" {
		cr.Spec.VMStorage.VMInsertPort = c.VMClusterDefault.VMStorageDefault.VMInsertPort
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMStorageSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "None",
			Selector:  cr.VMStorageSelectorLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       intstr.Parse(cr.Spec.VMStorage.Port).IntVal,
					TargetPort: intstr.Parse(cr.Spec.VMStorage.Port),
				},
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
			},
		},
	}
}

func genVMStorageService(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.VMStorage.Port == "" {
		cr.Spec.VMStorage.Port = c.VMClusterDefault.VMStorageDefault.Port
	}
	if cr.Spec.VMStorage.VMSelectPort == "" {
		cr.Spec.VMStorage.VMSelectPort = c.VMClusterDefault.VMStorageDefault.VMSelectPort
	}
	if cr.Spec.VMStorage.VMInsertPort == "" {
		cr.Spec.VMStorage.VMInsertPort = c.VMClusterDefault.VMStorageDefault.VMInsertPort
	}

	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMStorageSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: corev1.ServiceSpec{
			// headless removed, it should prevent common configuration errors.
			Type:     corev1.ServiceTypeClusterIP,
			Selector: cr.VMStorageSelectorLabels(),
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       intstr.Parse(cr.Spec.VMStorage.Port).IntVal,
					TargetPort: intstr.Parse(cr.Spec.VMStorage.Port),
				},
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
			},
		},
	}
}

func CreateOrUpdatePodDisruptionBudgetForVMStorage(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client) error {
	pdb := &policyv1beta1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			Labels:          cr.FinalLabels(cr.VMStorageSelectorLabels()),
			OwnerReferences: cr.AsOwner(),
			Namespace:       cr.Namespace,
			Finalizers:      []string{v1beta1.FinalizerName},
		},
		Spec: policyv1beta1.PodDisruptionBudgetSpec{
			MinAvailable:   cr.Spec.VMStorage.PodDisruptionBudget.MinAvailable,
			MaxUnavailable: cr.Spec.VMStorage.PodDisruptionBudget.MaxUnavailable,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMStorageSelectorLabels(),
			},
		},
	}
	return reconcilePDB(ctx, rclient, cr.Kind, pdb)
}

func waitForExpanding(ctx context.Context, kclient client.Client, namespace string, lbs map[string]string, desiredCount int32) (bool, error) {
	log.Info("check pods availability")
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(lbs)
	listOps := &client.ListOptions{Namespace: namespace, LabelSelector: labelSelector}
	if err := kclient.List(ctx, podList, listOps); err != nil {
		return false, err
	}
	var readyCount int32
	for _, pod := range podList.Items {
		if k8stools.PodIsReady(pod) {
			readyCount++
		}
	}
	log.Info("pods available", "count", readyCount, "spec-count", desiredCount)
	return readyCount != desiredCount, nil
}

func createOrUpdateVMInsertHPA(ctx context.Context, rclient client.Client, cluster *v1beta1.VMCluster) error {
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
	defaultHPA := buildHPASpec(targetRef, cluster.Spec.VMInsert.HPA, cluster.AsOwner(), cluster.VMInsertSelectorLabels(), cluster.Namespace)
	return reconcileHPA(ctx, rclient, defaultHPA)
}

func createOrUpdateVMSelectHPA(ctx context.Context, rclient client.Client, cluster *v1beta1.VMCluster) error {
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
	defaultHPA := buildHPASpec(targetRef, cluster.Spec.VMSelect.HPA, cluster.AsOwner(), cluster.VMInsertSelectorLabels(), cluster.Namespace)
	return reconcileHPA(ctx, rclient, defaultHPA)
}
