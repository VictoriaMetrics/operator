package factory

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/VictoriaMetrics/operator/api/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/coreos/prometheus-operator/pkg/k8sutil"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	vmStorageDefaultDBPath = "vmstorage-data"
	podRevisionLabel       = "controller-revision-hash"
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
		cr.Status.ClusterStatus = status
		if reconciled {
			cr.Status.UpdateFailCount = 0
		} else {
			cr.Status.UpdateFailCount += 1

		}
		cr.Status.Reason = reason
		cr.Status.LastSync = time.Now().String()
		err := rclient.Status().Update(ctx, cr)
		if err != nil {
			log.Error(err, "cannot update cluster status")
		}
	}()
	if cr.Spec.VMStorage != nil {
		vmStorageSts, err := createOrUpdateVMStorage(ctx, cr, rclient, c)
		if err != nil {
			reason = v1beta1.StorageCreationFailed
			return status, err
		}
		err = performRollingUpdateOnSts(ctx, rclient, vmStorageSts.Name, cr.Namespace, cr.VMStorageSelectorLabels(), c)
		if err != nil {
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
		//wait for expand
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
		//create vmselect
		vmSelectsts, err := createOrUpdateVMSelect(ctx, cr, rclient, c)
		if err != nil {
			reason = v1beta1.SelectCreationFailed
			return status, err
		}
		//create vmselect service
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

		err = performRollingUpdateOnSts(ctx, rclient, vmSelectsts.Name, cr.Namespace, cr.VMSelectSelectorLabels(), c)
		if err != nil {
			reason = v1beta1.SelectRollingUpdateFailed
			return status, err
		}

		//wait for expand
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
		if !c.DisableSelfServiceScrapeCreation {
			err := CreateVMServiceScrapeFromService(ctx, rclient, insertSvc, cr.MetricPathInsert())
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

func createOrUpdateVMSelect(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
	l := log.WithValues("controller", "vmselect", "cluster", cr.Name)
	l.Info("create or update vmselect for cluster")
	newSts, err := genVMSelectSpec(cr, c)
	if err != nil {
		return nil, err
	}
	currentSts := &appsv1.StatefulSet{}
	err = rclient.Get(ctx, types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, currentSts)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("vmselect sts not found, creating new one")
			err := rclient.Create(ctx, newSts)
			if err != nil {
				return nil, fmt.Errorf("cannot create new vmselect sts: %w", err)
			}
			l.Info("new vmselect sts was created")
		} else {
			return nil, fmt.Errorf("cannot get vmselect sts: %w", err)
		}
	}
	l.Info("vmstorage was found, updating it")
	for annotation, value := range currentSts.Annotations {
		newSts.Annotations[annotation] = value
	}

	for annotation, value := range currentSts.Spec.Template.Annotations {
		newSts.Spec.Template.Annotations[annotation] = value
	}
	if currentSts.ManagedFields != nil {
		newSts.ManagedFields = currentSts.ManagedFields
	}

	err = rclient.Update(ctx, newSts)
	if err != nil {
		return nil, fmt.Errorf("cannot update vmstorage sts: %w", err)
	}
	l.Info("vmstorage sts was reconciled")

	return newSts, nil

}

func CreateOrUpdateVMSelectService(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	l := log.WithValues("controller", "vmselect.service.crud")
	newService := genVMSelectService(cr, c)

	currentService := &corev1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: newService.Name}, currentService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for vmselect")
			err := rclient.Create(ctx, newService)
			if err != nil {
				return nil, fmt.Errorf("cannot create new service for vmselect")
			}
		} else {
			return nil, fmt.Errorf("cannot get vmselect service: %w", err)
		}
	}
	for annotation, value := range currentService.Annotations {
		newService.Annotations[annotation] = value
	}
	if currentService.Spec.ClusterIP != "" {
		newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	}
	if currentService.ResourceVersion != "" {
		newService.ResourceVersion = currentService.ResourceVersion
	}
	err = rclient.Update(ctx, newService)
	if err != nil {
		return nil, fmt.Errorf("cannot update vmselect service: %w", err)
	}
	l.Info("vmselect svc reconciled")
	return newService, nil
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
			//create new
			l.Info("vminsert deploy not found, creating new one")
			err := rclient.Create(ctx, newDeployment)
			if err != nil {
				return nil, fmt.Errorf("cannot create new vminsert deploy: %w", err)
			}
			l.Info("new vminsert deploy was created")
		} else {
			return nil, fmt.Errorf("cannot get vminsert deploy: %w", err)
		}
	}
	l.Info("vminsert was found, updating it")
	for annotation, value := range currentDeployment.Annotations {
		newDeployment.Annotations[annotation] = value
	}

	for annotation, value := range currentDeployment.Spec.Template.Annotations {
		newDeployment.Spec.Template.Annotations[annotation] = value
	}

	err = rclient.Update(ctx, newDeployment)
	if err != nil {
		return nil, fmt.Errorf("cannot update vminsert deploy: %w", err)
	}
	l.Info("vminsert deploy was reconciled")

	return newDeployment, nil
}

func CreateOrUpdateVMInsertService(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	l := log.WithValues("controller", "vminsert.service.crud")
	newService := genVMInsertService(cr, c)

	currentService := &corev1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: newService.Name}, currentService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for vminsert")
			err := rclient.Create(ctx, newService)
			if err != nil {
				return nil, fmt.Errorf("cannot create new service for vminsert: %w", err)
			}
		} else {
			return nil, fmt.Errorf("cannot get vminsert service: %w", err)
		}
	}
	for annotation, value := range currentService.Annotations {
		newService.Annotations[annotation] = value
	}
	if currentService.Spec.ClusterIP != "" {
		newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	}
	if currentService.ResourceVersion != "" {
		newService.ResourceVersion = currentService.ResourceVersion
	}
	err = rclient.Update(ctx, newService)
	if err != nil {
		return nil, fmt.Errorf("cannot update vminsert service: %w", err)
	}
	l.Info("vminsert svc reconciled")
	return newService, nil

}

func createOrUpdateVMStorage(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
	l := log.WithValues("controller", "vmstorage", "cluster", cr.Name)
	l.Info("create or update vmstorage for cluster")
	newSts, err := GenVMStorageSpec(cr, c)
	if err != nil {
		return nil, err
	}
	currentSts := &appsv1.StatefulSet{}
	err = rclient.Get(ctx, types.NamespacedName{Name: newSts.Name, Namespace: newSts.Namespace}, currentSts)
	if err != nil {
		if errors.IsNotFound(err) {
			//create new
			l.Info("vmstorage sts not found, creating new one")
			err := rclient.Create(ctx, newSts)
			if err != nil {
				return nil, fmt.Errorf("cannot create new vmstorage sts: %w", err)
			}
			l.Info("new vmstorage sts was created")
		} else {
			return nil, fmt.Errorf("cannot get vmstorage sts: %w", err)
		}
	}
	l.Info("vmstorage was found, updating it")
	for annotation, value := range currentSts.Annotations {
		newSts.Annotations[annotation] = value
	}

	for annotation, value := range currentSts.Spec.Template.Annotations {
		newSts.Spec.Template.Annotations[annotation] = value
	}

	err = rclient.Update(ctx, newSts)
	if err != nil {
		return nil, fmt.Errorf("cannot upddate vmstorage sts: %w", err)
	}
	l.Info("vmstorage sts was reconciled")

	return newSts, nil
}

func CreateOrUpdateVMStorageService(ctx context.Context, cr *v1beta1.VMCluster, rclient client.Client, c *config.BaseOperatorConf) (*corev1.Service, error) {
	l := log.WithValues("controller", "vmstorage.service.crud")
	newService := genVMStorageService(cr, c)

	currentService := &corev1.Service{}
	err := rclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: newService.Name}, currentService)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("creating new service for vm vmstorage")
			err := rclient.Create(ctx, newService)
			if err != nil {
				return nil, fmt.Errorf("cannot create new service for vmstorage")
			}
		} else {
			return nil, fmt.Errorf("cannot get vmstorage service: %w", err)
		}
	}
	for annotation, value := range currentService.Annotations {
		newService.Annotations[annotation] = value
	}
	if currentService.Spec.ClusterIP != "" {
		newService.Spec.ClusterIP = currentService.Spec.ClusterIP
	}
	if currentService.ResourceVersion != "" {
		newService.ResourceVersion = currentService.ResourceVersion
	}
	err = rclient.Update(ctx, newService)
	if err != nil {
		return nil, fmt.Errorf("cannot update vmstorage service: %w", err)
	}
	l.Info("vmstorage svc was reconciled")
	return newService, nil

}

func genVMSelectSpec(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
	cr = cr.DeepCopy()
	if cr.Spec.VMSelect.Image.Repository == "" {
		cr.Spec.VMSelect.Image.Repository = c.VMClusterDefault.VMSelectDefault.Image
	}
	if cr.Spec.VMSelect.Image.Tag == "" {
		cr.Spec.VMSelect.Image.Tag = c.VMClusterDefault.VMSelectDefault.Version
	}
	if cr.Spec.VMSelect.Port == "" {
		cr.Spec.VMSelect.Port = c.VMClusterDefault.VMSelectDefault.Port
	}

	if cr.Spec.VMSelect.Resources.Requests == nil {
		cr.Spec.VMSelect.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Spec.VMSelect.Resources.Limits == nil {
		cr.Spec.VMSelect.Resources.Limits = corev1.ResourceList{}
	}
	var cpuResourceIsSet bool
	var memResourceIsSet bool

	if _, ok := cr.Spec.VMSelect.Resources.Limits[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.VMSelect.Resources.Limits[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := cr.Spec.VMSelect.Resources.Requests[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.VMSelect.Resources.Requests[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if !cpuResourceIsSet {
		cr.Spec.VMSelect.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMClusterDefault.VMSelectDefault.Resource.Request.Cpu)
		cr.Spec.VMSelect.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMClusterDefault.VMSelectDefault.Resource.Limit.Cpu)

	}
	if !memResourceIsSet {
		cr.Spec.VMSelect.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMClusterDefault.VMSelectDefault.Resource.Request.Mem)
		cr.Spec.VMSelect.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMClusterDefault.VMSelectDefault.Resource.Limit.Mem)
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
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: cr.Spec.VMSelect.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMSelectSelectorLabels(),
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Template:             *podSpec,
			ServiceName:          cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			RevisionHistoryLimit: pointer.Int32Ptr(10),
		},
	}
	if cr.Spec.VMSelect.CacheMountPath != "" {
		storageSpec := cr.Spec.VMSelect.Storage
		if storageSpec == nil {
			stsSpec.Spec.Template.Spec.Volumes = append(stsSpec.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: cr.Spec.VMSelect.GetCacheMountVolmeName(),
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			})
		} else if storageSpec.EmptyDir != nil {
			emptyDir := storageSpec.EmptyDir
			stsSpec.Spec.Template.Spec.Volumes = append(stsSpec.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: cr.Spec.VMSelect.GetCacheMountVolmeName(),
				VolumeSource: corev1.VolumeSource{
					EmptyDir: emptyDir,
				},
			})
		} else {
			pvcTemplate := MakeVolumeClaimTemplate(storageSpec.VolumeClaimTemplate)
			if pvcTemplate.Name == "" {
				pvcTemplate.Name = cr.Spec.VMSelect.GetCacheMountVolmeName()
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

	for arg, value := range cr.Spec.VMSelect.ExtraArgs {
		args = append(args, fmt.Sprintf("-%s=%s", arg, value))
	}

	if cr.Spec.VMStorage != nil && cr.Spec.VMStorage.ReplicaCount != nil {
		if cr.Spec.VMStorage.VMSelectPort == "" {
			cr.Spec.VMStorage.VMSelectPort = c.VMClusterDefault.VMStorageDefault.VMSelectPort
		}
		storageArg := "-storageNode="
		vmstorageCount := *cr.Spec.VMStorage.ReplicaCount
		for i := int32(0); i < vmstorageCount; i++ {
			storageArg += cr.Spec.VMStorage.BuildPodFQDNName(cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMStorage.VMSelectPort, c.ClusterDomainName)
		}
		storageArg = strings.TrimSuffix(storageArg, ",")

		log.Info("built args with vmstorage nodes for vmselect", "vmstorage args", storageArg)
		args = append(args, storageArg)

	}
	selectArg := "-selectNode="
	vmselectCount := *cr.Spec.VMSelect.ReplicaCount
	for i := int32(0); i < vmselectCount; i++ {
		selectArg += cr.Spec.VMSelect.BuildPodFQDNName(cr.Spec.VMSelect.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMSelect.Port, c.ClusterDomainName)
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
			Name:      cr.Spec.VMSelect.GetCacheMountVolmeName(),
			MountPath: cr.Spec.VMSelect.CacheMountPath,
		})
		args = append(args, fmt.Sprintf("-cacheDataPath=%s", cr.Spec.VMSelect.CacheMountPath))
	}

	vmMounts = append(vmMounts, cr.Spec.VMSelect.VolumeMounts...)

	for _, s := range cr.Spec.VMSelect.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.VMSelect.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}

	livenessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.VMSelect.Port),
			Scheme: "HTTP",
			Path:   cr.HealthPathSelect(),
		},
	}
	readinessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.VMSelect.Port),
			Scheme: "HTTP",
			Path:   cr.HealthPathSelect(),
		},
	}
	livenessFailureThreshold := int32(3)
	livenessProbe := &corev1.Probe{
		Handler:          livenessProbeHandler,
		PeriodSeconds:    5,
		TimeoutSeconds:   probeTimeoutSeconds,
		FailureThreshold: livenessFailureThreshold,
		SuccessThreshold: 1,
	}
	readinessProbe := &corev1.Probe{
		Handler:          readinessProbeHandler,
		TimeoutSeconds:   probeTimeoutSeconds,
		PeriodSeconds:    5,
		FailureThreshold: 10,
		SuccessThreshold: 1,
	}

	var additionalContainers []corev1.Container

	operatorContainers := append([]corev1.Container{
		{
			Name:                     "vmselect",
			Image:                    fmt.Sprintf("%s:%s", cr.Spec.VMSelect.Image.Repository, cr.Spec.VMSelect.Image.Tag),
			ImagePullPolicy:          cr.Spec.VMSelect.Image.PullPolicy,
			Ports:                    ports,
			Args:                     args,
			VolumeMounts:             vmMounts,
			LivenessProbe:            livenessProbe,
			ReadinessProbe:           readinessProbe,
			Resources:                cr.Spec.VMSelect.Resources,
			Env:                      envs,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			TerminationMessagePath:   "/dev/termination-log",
		},
	}, additionalContainers...)

	containers, err := k8sutil.MergePatchContainers(operatorContainers, cr.Spec.VMSelect.Containers)
	if err != nil {
		return nil, err
	}

	vmSelectPodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMSelectPodLabels(),
			Annotations: cr.VMSelectPodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:                       volumes,
			InitContainers:                cr.Spec.VMSelect.InitContainers,
			Containers:                    containers,
			ServiceAccountName:            cr.Spec.VMSelect.ServiceAccountName,
			SecurityContext:               cr.Spec.VMSelect.SecurityContext,
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.VMSelect.Affinity,
			SchedulerName:                 cr.Spec.VMSelect.SchedulerName,
			Tolerations:                   cr.Spec.VMSelect.Tolerations,
			PriorityClassName:             cr.Spec.VMSelect.PriorityClassName,
			HostNetwork:                   cr.Spec.VMSelect.HostNetwork,
			DNSPolicy:                     cr.Spec.VMSelect.DNSPolicy,
			RestartPolicy:                 "Always",
			TerminationGracePeriodSeconds: pointer.Int64Ptr(30),
		},
	}

	return vmSelectPodSpec, nil
}

func genVMSelectService(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.VMSelect.Port == "" {
		cr.Spec.VMSelect.Port = c.VMClusterDefault.VMSelectDefault.Port
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMSelect.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMSelectSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
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

func genVMInsertSpec(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) (*appsv1.Deployment, error) {
	cr = cr.DeepCopy()

	if cr.Spec.VMInsert.Image.Repository == "" {
		cr.Spec.VMInsert.Image.Repository = c.VMClusterDefault.VMInsertDefault.Image
	}
	if cr.Spec.VMInsert.Image.Tag == "" {
		cr.Spec.VMInsert.Image.Tag = c.VMClusterDefault.VMInsertDefault.Version
	}
	if cr.Spec.VMInsert.Port == "" {
		cr.Spec.VMInsert.Port = c.VMClusterDefault.VMInsertDefault.Port
	}

	if cr.Spec.VMInsert.Resources.Requests == nil {
		cr.Spec.VMInsert.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Spec.VMInsert.Resources.Limits == nil {
		cr.Spec.VMInsert.Resources.Limits = corev1.ResourceList{}
	}

	var cpuResourceIsSet bool
	var memResourceIsSet bool

	if _, ok := cr.Spec.VMInsert.Resources.Limits[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.VMInsert.Resources.Limits[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := cr.Spec.VMInsert.Resources.Requests[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.VMInsert.Resources.Requests[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if !cpuResourceIsSet {
		cr.Spec.VMInsert.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMClusterDefault.VMInsertDefault.Resource.Request.Cpu)
		cr.Spec.VMInsert.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMClusterDefault.VMInsertDefault.Resource.Limit.Cpu)

	}
	if !memResourceIsSet {
		cr.Spec.VMInsert.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMClusterDefault.VMInsertDefault.Resource.Request.Mem)
		cr.Spec.VMInsert.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMClusterDefault.VMInsertDefault.Resource.Limit.Mem)
	}
	podSpec, err := makePodSpecForVMInsert(cr, c)
	if err != nil {
		return nil, err
	}

	stsSpec := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMInsert.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMInsertSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: cr.Spec.VMInsert.ReplicaCount,
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

	for arg, value := range cr.Spec.VMInsert.ExtraArgs {
		args = append(args, fmt.Sprintf("-%s=%s", arg, value))
	}

	if cr.Spec.VMStorage != nil && cr.Spec.VMStorage.ReplicaCount != nil {
		if cr.Spec.VMStorage.VMInsertPort == "" {
			cr.Spec.VMStorage.VMInsertPort = c.VMClusterDefault.VMStorageDefault.VMInsertPort
		}
		storageArg := "-storageNode="
		storageCount := *cr.Spec.VMStorage.ReplicaCount
		for i := int32(0); i < storageCount; i++ {
			storageArg += cr.Spec.VMStorage.BuildPodFQDNName(cr.Spec.VMStorage.GetNameWithPrefix(cr.Name), i, cr.Namespace, cr.Spec.VMStorage.VMInsertPort, c.ClusterDomainName)
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
	volumes := make([]corev1.Volume, 0)

	volumes = append(volumes, cr.Spec.VMInsert.Volumes...)

	vmMounts := make([]corev1.VolumeMount, 0)

	vmMounts = append(vmMounts, cr.Spec.VMInsert.VolumeMounts...)

	for _, s := range cr.Spec.VMInsert.Secrets {
		volumes = append(volumes, corev1.Volume{
			Name: SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.VMInsert.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}

	livenessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.VMInsert.Port),
			Scheme: "HTTP",
			Path:   cr.HealthPathInsert(),
		},
	}
	readinessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.VMInsert.Port),
			Scheme: "HTTP",
			Path:   cr.HealthPathInsert(),
		},
	}
	livenessFailureThreshold := int32(3)
	livenessProbe := &corev1.Probe{
		Handler:          livenessProbeHandler,
		PeriodSeconds:    5,
		TimeoutSeconds:   probeTimeoutSeconds,
		FailureThreshold: livenessFailureThreshold,
	}
	readinessProbe := &corev1.Probe{
		Handler:          readinessProbeHandler,
		TimeoutSeconds:   probeTimeoutSeconds,
		PeriodSeconds:    5,
		FailureThreshold: 10,
	}

	var additionalContainers []corev1.Container

	operatorContainers := append([]corev1.Container{
		{
			Name:                     "vminsert",
			Image:                    fmt.Sprintf("%s:%s", cr.Spec.VMInsert.Image.Repository, cr.Spec.VMInsert.Image.Tag),
			ImagePullPolicy:          cr.Spec.VMInsert.Image.PullPolicy,
			Ports:                    ports,
			Args:                     args,
			VolumeMounts:             vmMounts,
			LivenessProbe:            livenessProbe,
			ReadinessProbe:           readinessProbe,
			Resources:                cr.Spec.VMInsert.Resources,
			Env:                      envs,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
		},
	}, additionalContainers...)

	containers, err := k8sutil.MergePatchContainers(operatorContainers, cr.Spec.VMInsert.Containers)
	if err != nil {
		return nil, err
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
			ServiceAccountName: cr.Spec.VMInsert.ServiceAccountName,
			SecurityContext:    cr.Spec.VMInsert.SecurityContext,
			ImagePullSecrets:   cr.Spec.ImagePullSecrets,
			Affinity:           cr.Spec.VMInsert.Affinity,
			SchedulerName:      cr.Spec.VMInsert.SchedulerName,
			Tolerations:        cr.Spec.VMInsert.Tolerations,
			PriorityClassName:  cr.Spec.VMInsert.PriorityClassName,
			HostNetwork:        cr.Spec.VMInsert.HostNetwork,
			DNSPolicy:          cr.Spec.VMInsert.DNSPolicy,
		},
	}

	return vmInsertPodSpec, nil

}
func genVMInsertService(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) *corev1.Service {
	cr = cr.DeepCopy()
	if cr.Spec.VMInsert.Port == "" {
		cr.Spec.VMInsert.Port = c.VMClusterDefault.VMInsertDefault.Port
	}
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            cr.Spec.VMInsert.GetNameWithPrefix(cr.Name),
			Namespace:       cr.Namespace,
			Labels:          cr.FinalLabels(cr.VMInsertSelectorLabels()),
			Annotations:     cr.Annotations(),
			OwnerReferences: cr.AsOwner(),
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

func GenVMStorageSpec(cr *v1beta1.VMCluster, c *config.BaseOperatorConf) (*appsv1.StatefulSet, error) {
	cr = cr.DeepCopy()
	if cr.Spec.VMStorage.Image.Repository == "" {
		cr.Spec.VMStorage.Image.Repository = c.VMClusterDefault.VMStorageDefault.Image
	}
	if cr.Spec.VMStorage.Image.Tag == "" {
		cr.Spec.VMStorage.Image.Tag = c.VMClusterDefault.VMStorageDefault.Version
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

	if cr.Spec.VMStorage.Resources.Requests == nil {
		cr.Spec.VMStorage.Resources.Requests = corev1.ResourceList{}
	}
	if cr.Spec.VMStorage.Resources.Limits == nil {
		cr.Spec.VMStorage.Resources.Limits = corev1.ResourceList{}
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

	var cpuResourceIsSet bool
	var memResourceIsSet bool

	if _, ok := cr.Spec.VMStorage.Resources.Limits[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.VMStorage.Resources.Limits[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if _, ok := cr.Spec.VMStorage.Resources.Requests[corev1.ResourceMemory]; ok {
		memResourceIsSet = true
	}
	if _, ok := cr.Spec.VMStorage.Resources.Requests[corev1.ResourceCPU]; ok {
		cpuResourceIsSet = true
	}
	if !cpuResourceIsSet {
		cr.Spec.VMStorage.Resources.Requests[corev1.ResourceCPU] = resource.MustParse(c.VMClusterDefault.VMStorageDefault.Resource.Request.Cpu)
		cr.Spec.VMStorage.Resources.Limits[corev1.ResourceCPU] = resource.MustParse(c.VMClusterDefault.VMStorageDefault.Resource.Limit.Cpu)

	}
	if !memResourceIsSet {
		cr.Spec.VMStorage.Resources.Requests[corev1.ResourceMemory] = resource.MustParse(c.VMClusterDefault.VMStorageDefault.Resource.Request.Mem)
		cr.Spec.VMStorage.Resources.Limits[corev1.ResourceMemory] = resource.MustParse(c.VMClusterDefault.VMStorageDefault.Resource.Limit.Mem)
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
		},
		Spec: appsv1.StatefulSetSpec{
			Replicas: cr.Spec.VMStorage.ReplicaCount,
			Selector: &metav1.LabelSelector{
				MatchLabels: cr.VMStorageSelectorLabels(),
			},
			PodManagementPolicy: appsv1.ParallelPodManagement,
			UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
				Type: appsv1.OnDeleteStatefulSetStrategyType,
			},
			Template:             *podSpec,
			ServiceName:          cr.Spec.VMStorage.GetNameWithPrefix(cr.Name),
			RevisionHistoryLimit: pointer.Int32Ptr(10),
		},
	}
	storageSpec := cr.Spec.VMStorage.Storage
	if storageSpec == nil {
		stsSpec.Spec.Template.Spec.Volumes = append(stsSpec.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: cr.Spec.VMStorage.GetStorageVolumeName(),
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		})
	} else if storageSpec.EmptyDir != nil {
		emptyDir := storageSpec.EmptyDir
		stsSpec.Spec.Template.Spec.Volumes = append(stsSpec.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: cr.Spec.VMStorage.GetStorageVolumeName(),
			VolumeSource: corev1.VolumeSource{
				EmptyDir: emptyDir,
			},
		})
	} else {
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

	for arg, value := range cr.Spec.VMStorage.ExtraArgs {
		args = append(args, fmt.Sprintf("-%s=%s", arg, value))
	}
	if len(cr.Spec.VMStorage.ExtraEnvs) > 0 {
		args = append(args, "-envflag.enable=true")
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
			Name: SanitizeVolumeName("secret-" + cr.Spec.VMStorage.VMBackup.CredentialsSecret.Name),
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
			Name: SanitizeVolumeName("secret-" + s),
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: s,
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("secret-" + s),
			ReadOnly:  true,
			MountPath: path.Join(SecretsDir, s),
		})
	}

	for _, c := range cr.Spec.VMStorage.ConfigMaps {
		volumes = append(volumes, corev1.Volume{
			Name: SanitizeVolumeName("configmap-" + c),
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: c,
					},
				},
			},
		})
		vmMounts = append(vmMounts, corev1.VolumeMount{
			Name:      SanitizeVolumeName("configmap-" + c),
			ReadOnly:  true,
			MountPath: path.Join(ConfigMapsDir, c),
		})
	}

	livenessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.VMStorage.Port),
			Scheme: "HTTP",
			Path:   cr.HealthPathStorage(),
		},
	}
	readinessProbeHandler := corev1.Handler{
		HTTPGet: &corev1.HTTPGetAction{
			Port:   intstr.Parse(cr.Spec.VMStorage.Port),
			Scheme: "HTTP",
			Path:   cr.HealthPathStorage(),
		},
	}
	livenessFailureThreshold := int32(3)
	livenessProbe := &corev1.Probe{
		Handler:          livenessProbeHandler,
		PeriodSeconds:    5,
		TimeoutSeconds:   probeTimeoutSeconds,
		FailureThreshold: livenessFailureThreshold,
		SuccessThreshold: 1,
	}
	readinessProbe := &corev1.Probe{
		Handler:          readinessProbeHandler,
		TimeoutSeconds:   probeTimeoutSeconds,
		PeriodSeconds:    5,
		FailureThreshold: 10,
		SuccessThreshold: 1,
	}

	var additionalContainers []corev1.Container

	operatorContainers := append([]corev1.Container{
		{
			Name:                     "vmstorage",
			Image:                    fmt.Sprintf("%s:%s", cr.Spec.VMStorage.Image.Repository, cr.Spec.VMStorage.Image.Tag),
			ImagePullPolicy:          cr.Spec.VMStorage.Image.PullPolicy,
			Ports:                    ports,
			Args:                     args,
			VolumeMounts:             vmMounts,
			LivenessProbe:            livenessProbe,
			ReadinessProbe:           readinessProbe,
			Resources:                cr.Spec.VMStorage.Resources,
			Env:                      envs,
			TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
			TerminationMessagePath:   "/dev/termination-log",
		},
	}, additionalContainers...)

	if cr.Spec.VMStorage.VMBackup != nil {
		vmBackuper, err := makeSpecForVMBackuper(cr.Spec.VMStorage.VMBackup, c, cr.Spec.VMStorage.Port, cr.Spec.VMStorage.GetStorageVolumeName(), cr.Spec.VMStorage.ExtraArgs)
		if err != nil {
			return nil, err
		}
		operatorContainers = append(operatorContainers, *vmBackuper)
	}

	containers, err := k8sutil.MergePatchContainers(operatorContainers, cr.Spec.VMStorage.Containers)
	if err != nil {
		return nil, err
	}

	vmStoragePodSpec := &corev1.PodTemplateSpec{
		ObjectMeta: metav1.ObjectMeta{
			Labels:      cr.VMStoragePodLabels(),
			Annotations: cr.VMStoragePodAnnotations(),
		},
		Spec: corev1.PodSpec{
			Volumes:                       volumes,
			InitContainers:                cr.Spec.VMStorage.InitContainers,
			Containers:                    containers,
			ServiceAccountName:            cr.Spec.VMStorage.ServiceAccountName,
			SecurityContext:               cr.Spec.VMStorage.SecurityContext,
			ImagePullSecrets:              cr.Spec.ImagePullSecrets,
			Affinity:                      cr.Spec.VMStorage.Affinity,
			SchedulerName:                 cr.Spec.VMStorage.SchedulerName,
			Tolerations:                   cr.Spec.VMStorage.Tolerations,
			PriorityClassName:             cr.Spec.VMStorage.PriorityClassName,
			HostNetwork:                   cr.Spec.VMStorage.HostNetwork,
			DNSPolicy:                     cr.Spec.VMStorage.DNSPolicy,
			RestartPolicy:                 "Always",
			TerminationGracePeriodSeconds: pointer.Int64Ptr(30),
		},
	}

	return vmStoragePodSpec, nil
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
		if PodIsReady(pod) {
			readyCount++
		}
	}
	log.Info("pods available", "count", readyCount, "spec-count", desiredCount)
	return readyCount != desiredCount, nil
}

// we perform rolling update on sts by manually deleting pods one by one
// we check sts revision (kubernetes controller-manager is responsible for that)
// and compare pods revision label with sts revision
// if it doesnt match - updated is needed
func performRollingUpdateOnSts(ctx context.Context, rclient client.Client, stsName string, ns string, podLabels map[string]string, c *config.BaseOperatorConf) error {
	time.Sleep(time.Second * 2)
	sts := &appsv1.StatefulSet{}
	err := rclient.Get(ctx, types.NamespacedName{Name: stsName, Namespace: ns}, sts)
	if err != nil {
		return err
	}
	var stsVersion string
	if sts.Status.UpdateRevision != sts.Status.CurrentRevision {
		log.Info("sts update is needed", "sts", sts.Name, "currentVersion", sts.Status.CurrentRevision, "desiredVersion", sts.Status.UpdateRevision)
		stsVersion = sts.Status.UpdateRevision
	} else {
		stsVersion = sts.Status.CurrentRevision
	}
	l := log.WithValues("controller", "sts.rollingupdate", "desiredVersion", stsVersion)
	l.Info("checking if update needed")
	podList := &corev1.PodList{}
	labelSelector := labels.SelectorFromSet(podLabels)
	listOps := &client.ListOptions{Namespace: ns, LabelSelector: labelSelector}
	if err := rclient.List(ctx, podList, listOps); err != nil {
		return err
	}
	var updatedNeeded bool
	for _, pod := range podList.Items {
		if pod.Labels[podRevisionLabel] != stsVersion {
			l.Info("pod version doesnt match", "pod", pod.Name, "podVersion", pod.Labels[podRevisionLabel])
			updatedNeeded = true
		}
	}
	if !updatedNeeded {
		l.Info("update isn't needed")
		return nil
	}
	l.Info("update is needed, start building proper order for update")
	// first we must ensure, that already updated pods in ready status
	// then we can update other pods
	// if pod is not ready
	// it must be at first place for update
	podsForUpdate := make([]corev1.Pod, 0, len(podList.Items))
	// if pods were already updated to some version, we have to wait its readiness
	updatedPods := make([]corev1.Pod, 0, len(podList.Items))
	for _, pod := range podList.Items {
		if pod.Labels[podRevisionLabel] == stsVersion {
			updatedPods = append(updatedPods, pod)
			continue
		}
		if !PodIsReady(pod) {
			podsForUpdate = append([]corev1.Pod{pod}, podsForUpdate...)
			continue
		}
		podsForUpdate = append(podsForUpdate, pod)
	}

	l.Info("updated pods with desired version:", "count", len(updatedPods))

	for _, pod := range updatedPods {
		l.Info("checking ready status for already updated pods to desired version", "pod", pod.Name)
		err := waitForPodReady(ctx, rclient, ns, pod.Name, c)
		if err != nil {
			l.Error(err, "cannot get ready status for already updated pod", "pod", pod.Name)
			return err
		}
	}

	for _, pod := range podsForUpdate {
		l.Info("updating pod", "pod", pod.Name)
		//we have to delete pod and wait for it readiness
		err := rclient.Delete(ctx, &pod, &client.DeleteOptions{GracePeriodSeconds: pointer.Int64Ptr(30)})
		if err != nil {
			return err
		}
		err = waitForPodReady(ctx, rclient, ns, pod.Name, c)
		if err != nil {
			return err
		}
		l.Info("pod was updated", "pod", pod.Name)
		time.Sleep(time.Second * 3)
	}

	return nil

}

func PodIsReady(pod corev1.Pod) bool {
	if pod.ObjectMeta.DeletionTimestamp != nil {
		return false
	}

	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == "True" {
			return true
		}
	}
	return false
}

func waitForPodReady(ctx context.Context, rclient client.Client, ns, podName string, c *config.BaseOperatorConf) error {
	// we need some delay
	time.Sleep(c.PodWaitReadyInitDelay)
	return wait.Poll(c.PodWaitReadyIntervalCheck, c.PodWaitReadyTimeout, func() (done bool, err error) {
		pod := &corev1.Pod{}
		err = rclient.Get(ctx, types.NamespacedName{Namespace: ns, Name: podName}, pod)
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
			log.Error(err, "cannot get pod", "pod", podName)
			return false, err
		}
		if PodIsReady(*pod) {
			log.Info("pod update finished with revision", "pod", pod.Name, "revision", pod.Labels[podRevisionLabel])
			return true, nil
		}
		return false, nil
	})
}
