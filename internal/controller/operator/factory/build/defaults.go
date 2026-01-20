package build

import (
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

func getCfg() *config.BaseOperatorConf {
	return config.MustGetBaseConfig()
}

// AddDefaults adds defaulting functions to the runtimeScheme
func AddDefaults(scheme *runtime.Scheme) {
	scheme.AddTypeDefaultingFunc(&corev1.Service{}, addServiceDefaults)
	scheme.AddTypeDefaultingFunc(&appsv1.Deployment{}, addDeploymentDefaults)
	scheme.AddTypeDefaultingFunc(&appsv1.StatefulSet{}, addStatefulsetDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1alpha1.VMDistributed{}, addVMDistributedDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1beta1.VMAuth{}, addVMAuthDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1beta1.VMAgent{}, addVMAgentDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1beta1.VMAlert{}, addVMAlertDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1beta1.VMSingle{}, addVMSingleDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1beta1.VMAlertmanager{}, addVMAlertmanagerDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1beta1.VMCluster{}, addVMClusterDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1beta1.VLogs{}, addVLogsDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1.VLSingle{}, addVLSingleDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1.VLCluster{}, addVLClusterDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1.VLAgent{}, addVLAgentDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1.VTSingle{}, addVTSingleDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1.VTCluster{}, addVTClusterDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1.VMAnomaly{}, addVMAnomalyDefaults)
	scheme.AddTypeDefaultingFunc(&vmv1beta1.VMServiceScrape{}, addVMServiceScrapeDefaults)
}

// defaults according to
// https://github.com/kubernetes/kubernetes/blob/master/pkg/apis/apps/v1/defaults.go
func addStatefulsetDefaults(objI any) {
	obj := objI.(*appsv1.StatefulSet)

	// special case for vm operator defaults
	if obj.Spec.UpdateStrategy.Type == "" {
		obj.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
	}

	if obj.Spec.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType {
		if obj.Spec.UpdateStrategy.RollingUpdate != nil {
			if obj.Spec.UpdateStrategy.RollingUpdate.Partition == nil {
				obj.Spec.UpdateStrategy.RollingUpdate.Partition = ptr.To[int32](0)
			}
		}
	}

	if obj.Spec.PodManagementPolicy == "" {
		obj.Spec.PodManagementPolicy = appsv1.ParallelPodManagement
		if obj.Spec.MinReadySeconds > 0 || obj.Spec.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType {
			obj.Spec.PodManagementPolicy = appsv1.OrderedReadyPodManagement
		}
	}
	if obj.Spec.Template.Spec.SecurityContext == nil {
		obj.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{}
	}

	if obj.Spec.Replicas == nil {
		obj.Spec.Replicas = new(int32)
		*obj.Spec.Replicas = 1
	}
	if obj.Spec.RevisionHistoryLimit == nil {
		obj.Spec.RevisionHistoryLimit = new(int32)
		*obj.Spec.RevisionHistoryLimit = 10
	}
}

// defaults according to
// https://github.com/kubernetes/kubernetes/blob/master/pkg/apis/apps/v1/defaults.go
func addDeploymentDefaults(objI any) {
	obj := objI.(*appsv1.Deployment)
	// Set DeploymentSpec.Replicas to 1 if it is not set.
	if obj.Spec.Replicas == nil {
		obj.Spec.Replicas = new(int32)
		*obj.Spec.Replicas = 1
	}
	strategy := &obj.Spec.Strategy
	// Set default DeploymentStrategyType as RollingUpdate.
	if strategy.Type == "" {
		strategy.Type = appsv1.RollingUpdateDeploymentStrategyType
	}
	if strategy.Type == appsv1.RollingUpdateDeploymentStrategyType {
		if strategy.RollingUpdate == nil {
			rollingUpdate := appsv1.RollingUpdateDeployment{}
			strategy.RollingUpdate = &rollingUpdate
		}
		if strategy.RollingUpdate.MaxUnavailable == nil {
			// Set default MaxUnavailable as 25% by default.
			maxUnavailable := intstr.FromString("25%")
			strategy.RollingUpdate.MaxUnavailable = &maxUnavailable
		}
		if strategy.RollingUpdate.MaxSurge == nil {
			// Set default MaxSurge as 25% by default.
			maxSurge := intstr.FromString("25%")
			strategy.RollingUpdate.MaxSurge = &maxSurge
		}
	}
	if obj.Spec.RevisionHistoryLimit == nil {
		obj.Spec.RevisionHistoryLimit = new(int32)
		*obj.Spec.RevisionHistoryLimit = 10
	}
	if obj.Spec.ProgressDeadlineSeconds == nil {
		obj.Spec.ProgressDeadlineSeconds = new(int32)
		*obj.Spec.ProgressDeadlineSeconds = 600
	}
}

// defaults according to
// https://github.com/kubernetes/kubernetes/blob/master/pkg/apis/core/v1/defaults.go
func addServiceDefaults(objI any) {
	obj := objI.(*corev1.Service)
	if obj.Spec.SessionAffinity == "" {
		obj.Spec.SessionAffinity = corev1.ServiceAffinityNone
	}
	if obj.Spec.SessionAffinity == corev1.ServiceAffinityNone {
		obj.Spec.SessionAffinityConfig = nil
	}
	if obj.Spec.SessionAffinity == corev1.ServiceAffinityClientIP {
		if obj.Spec.SessionAffinityConfig == nil || obj.Spec.SessionAffinityConfig.ClientIP == nil || obj.Spec.SessionAffinityConfig.ClientIP.TimeoutSeconds == nil {
			timeoutSeconds := corev1.DefaultClientIPServiceAffinitySeconds
			obj.Spec.SessionAffinityConfig = &corev1.SessionAffinityConfig{
				ClientIP: &corev1.ClientIPConfig{
					TimeoutSeconds: &timeoutSeconds,
				},
			}
		}
	}
	if obj.Spec.Type == "" {
		obj.Spec.Type = corev1.ServiceTypeClusterIP
	}
	for i := range obj.Spec.Ports {
		sp := &obj.Spec.Ports[i]
		if sp.Protocol == "" {
			sp.Protocol = corev1.ProtocolTCP
		}
		if sp.TargetPort == intstr.FromInt32(0) || sp.TargetPort == intstr.FromString("") {
			sp.TargetPort = intstr.FromInt32(sp.Port)
		}
	}
	// Defaults ExternalTrafficPolicy field for externally-accessible service
	// to Global for consistency.
	if isExternallyAccessible(obj) && obj.Spec.ExternalTrafficPolicy == "" {
		obj.Spec.ExternalTrafficPolicy = corev1.ServiceExternalTrafficPolicyCluster
	}

	if obj.Spec.InternalTrafficPolicy == nil {
		if obj.Spec.Type == corev1.ServiceTypeNodePort || obj.Spec.Type == corev1.ServiceTypeLoadBalancer || obj.Spec.Type == corev1.ServiceTypeClusterIP {
			serviceInternalTrafficPolicyCluster := corev1.ServiceInternalTrafficPolicyCluster
			obj.Spec.InternalTrafficPolicy = &serviceInternalTrafficPolicyCluster
		}
	}
	if obj.Spec.Type == corev1.ServiceTypeLoadBalancer {
		if obj.Spec.AllocateLoadBalancerNodePorts == nil {
			obj.Spec.AllocateLoadBalancerNodePorts = ptr.To(true)
		}
	}
}

// ExternallyAccessible checks if service is externally accessible.
func isExternallyAccessible(service *corev1.Service) bool {
	return service.Spec.Type == corev1.ServiceTypeLoadBalancer ||
		service.Spec.Type == corev1.ServiceTypeNodePort ||
		(service.Spec.Type == corev1.ServiceTypeClusterIP && len(service.Spec.ExternalIPs) > 0)
}

func addVMAuthDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMAuth)
	c := getCfg()

	if cr.Spec.ConfigSecret != "" {
		// Removed if later with ConfigSecret field later
		cr.Spec.SecretRef = &corev1.SecretKeySelector{
			Key: "config.yaml",
			LocalObjectReference: corev1.LocalObjectReference{
				Name: cr.Spec.ConfigSecret,
			},
		}
	}
	cv := config.ApplicationDefaults(c.VMAuthDefault)
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, cr.Spec.License, &cv)
	addDefaultsToConfigReloader(&cr.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.UseDefaultResources, false))
}

func addVMAlertDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMAlert)
	c := getCfg()

	cv := config.ApplicationDefaults(c.VMAlertDefault)
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, cr.Spec.License, &cv)
	addDefaultsToConfigReloader(&cr.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.UseDefaultResources, false))
	if cr.Spec.ConfigReloaderImage == "" {
		panic("cannot be empty")
	}
}

func addVMAgentDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMAgent)
	c := getCfg()

	cv := config.ApplicationDefaults(c.VMAgentDefault)
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, cr.Spec.License, &cv)
	addDefaultsToConfigReloader(&cr.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.UseDefaultResources, false))
}

func addVLAgentDefaults(objI any) {
	cr := objI.(*vmv1.VLAgent)
	c := getCfg()

	cv := config.ApplicationDefaults(c.VLAgentDefault)
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, cr.Spec.License, &cv)
}

func addVMSingleDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMSingle)
	c := getCfg()
	useBackupDefaultResources := c.VMBackup.UseDefaultResources
	cv := config.ApplicationDefaults(c.VMSingleDefault)
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, cr.Spec.License, &cv)
	if cr.Spec.UseDefaultResources != nil {
		useBackupDefaultResources = *cr.Spec.UseDefaultResources
	}
	backupDefaults := &config.ApplicationDefaults{
		Image:               c.VMBackup.Image,
		Version:             c.VMBackup.Version,
		Port:                c.VMBackup.Port,
		UseDefaultResources: c.VMBackup.UseDefaultResources,
		Resource: struct {
			Limit struct {
				Mem string
				Cpu string
			}
			Request struct {
				Mem string
				Cpu string
			}
		}(c.VMBackup.Resource),
	}
	addDefaultsToVMBackup(cr.Spec.VMBackup, useBackupDefaultResources, backupDefaults)
}

func addVLogsDefaults(objI any) {
	cr := objI.(*vmv1beta1.VLogs)
	c := getCfg()
	cv := config.ApplicationDefaults(c.VLogsDefault)
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, nil, &cv)
}

func addVMAnomalyDefaults(objI any) {
	cr := objI.(*vmv1.VMAnomaly)

	// vmanomaly takes up to 2 minutes to start
	if cr.Spec.EmbeddedProbes == nil {
		cr.Spec.EmbeddedProbes = &vmv1beta1.EmbeddedProbes{
			LivenessProbe: &corev1.Probe{
				InitialDelaySeconds: 10,
				FailureThreshold:    16,
				PeriodSeconds:       10,
			},
			ReadinessProbe: &corev1.Probe{
				InitialDelaySeconds: 10,
				FailureThreshold:    16,
				PeriodSeconds:       10,
			},
		}
	}
	c := getCfg()
	cv := config.ApplicationDefaults(c.VMAnomalyDefault)
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, nil, &cv)
	if cr.Spec.Monitoring == nil {
		cr.Spec.Monitoring = &vmv1.VMAnomalyMonitoringSpec{
			Pull: &vmv1.VMAnomalyMonitoringPullSpec{
				Port: "8080",
			},
		}
	}
}

func addVLSingleDefaults(objI any) {
	cr := objI.(*vmv1.VLSingle)
	c := getCfg()
	cv := config.ApplicationDefaults(c.VLSingleDefault)
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, cr.Spec.License, &cv)
}

func addVTSingleDefaults(objI any) {
	cr := objI.(*vmv1.VTSingle)
	c := getCfg()
	cv := config.ApplicationDefaults(c.VTSingleDefault)
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, nil, &cv)
}

func addVMAlertmanagerDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMAlertmanager)
	c := getCfg()

	if cr.Spec.ClusterDomainName == "" {
		cr.Spec.ClusterDomainName = c.ClusterDomainName
	}
	amcd := c.VMAlertManager
	cv := config.ApplicationDefaults{
		Image:               amcd.AlertmanagerDefaultBaseImage,
		Version:             amcd.AlertManagerVersion,
		Port:                cr.Port(),
		UseDefaultResources: amcd.UseDefaultResources,
		Resource: struct {
			Limit struct {
				Mem string
				Cpu string
			}
			Request struct {
				Mem string
				Cpu string
			}
		}(amcd.Resource),
	}
	if cr.Spec.ReplicaCount == nil {
		cr.Spec.ReplicaCount = ptr.To[int32](1)
	}
	if cr.Spec.Port == "" {
		cr.Spec.Port = "9093"
	}
	if cr.Spec.PortName == "" {
		cr.Spec.PortName = "web"
	}
	if cr.Spec.TerminationGracePeriodSeconds == nil {
		cr.Spec.TerminationGracePeriodSeconds = ptr.To[int64](120)
	}
	addDefaultsToCommonParams(&cr.Spec.CommonDefaultableParams, nil, &cv)
	addDefaultsToConfigReloader(&cr.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.UseDefaultResources, false))
}

const (
	vmStorageDefaultDBPath = "vmstorage-data"
)

var defaultTerminationGracePeriod = int64(30)

func addVMClusterSpecDefaults(spec *vmv1beta1.VMClusterSpec) {
	c := getCfg()

	// cluster is tricky is has main strictSecurity and per app
	useStrictSecurity := c.EnableStrictSecurity
	if spec.UseStrictSecurity != nil {
		useStrictSecurity = *spec.UseStrictSecurity
	}
	if spec.ClusterDomainName == "" {
		spec.ClusterDomainName = c.ClusterDomainName
	}

	if spec.VMStorage != nil {
		if spec.VMStorage.UseStrictSecurity == nil {
			spec.VMStorage.UseStrictSecurity = &useStrictSecurity
		}
		if spec.VMStorage.DisableSelfServiceScrape == nil {
			spec.VMStorage.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}
		spec.VMStorage.ImagePullSecrets = append(spec.VMStorage.ImagePullSecrets, spec.ImagePullSecrets...)

		useBackupDefaultResources := c.VMBackup.UseDefaultResources
		if spec.VMStorage.UseDefaultResources != nil {
			useBackupDefaultResources = *spec.VMStorage.UseDefaultResources
		}
		backupDefaults := &config.ApplicationDefaults{
			Image:               c.VMBackup.Image,
			Version:             c.VMBackup.Version,
			Port:                c.VMBackup.Port,
			UseDefaultResources: c.VMBackup.UseDefaultResources,
			Resource: struct {
				Limit struct {
					Mem string
					Cpu string
				}
				Request struct {
					Mem string
					Cpu string
				}
			}(c.VMBackup.Resource),
		}
		if spec.VMStorage.Image.Repository == "" {
			spec.VMStorage.Image.Repository = c.VMClusterDefault.VMStorageDefault.Image
		}
		spec.VMStorage.Image.Repository = formatContainerImage(c.ContainerRegistry, spec.VMStorage.Image.Repository)

		if spec.VMStorage.Image.Tag == "" {
			if spec.ClusterVersion != "" {
				spec.VMStorage.Image.Tag = spec.ClusterVersion
			} else {
				spec.VMStorage.Image.Tag = c.VMClusterDefault.VMStorageDefault.Version
			}
		}
		if spec.VMStorage.VMInsertPort == "" {
			spec.VMStorage.VMInsertPort = c.VMClusterDefault.VMStorageDefault.VMInsertPort
		}
		if spec.VMStorage.VMSelectPort == "" {
			spec.VMStorage.VMSelectPort = c.VMClusterDefault.VMStorageDefault.VMSelectPort
		}
		if spec.VMStorage.Port == "" {
			spec.VMStorage.Port = c.VMClusterDefault.VMStorageDefault.Port
		}

		if spec.VMStorage.DNSPolicy == "" {
			spec.VMStorage.DNSPolicy = corev1.DNSClusterFirst
		}
		if spec.VMStorage.SchedulerName == "" {
			spec.VMStorage.SchedulerName = "default-scheduler"
		}
		if spec.VMStorage.Image.PullPolicy == "" {
			spec.VMStorage.Image.PullPolicy = corev1.PullIfNotPresent
		}
		if spec.VMStorage.StorageDataPath == "" {
			spec.VMStorage.StorageDataPath = vmStorageDefaultDBPath
		}
		if spec.VMStorage.TerminationGracePeriodSeconds == nil {
			spec.VMStorage.TerminationGracePeriodSeconds = &defaultTerminationGracePeriod
		}
		if spec.VMStorage.UseDefaultResources == nil {
			spec.VMStorage.UseDefaultResources = &c.VMClusterDefault.UseDefaultResources
		}
		spec.VMStorage.Resources = Resources(spec.VMStorage.Resources,
			config.Resource(c.VMClusterDefault.VMStorageDefault.Resource),
			*spec.VMStorage.UseDefaultResources,
		)
		addDefaultsToVMBackup(spec.VMStorage.VMBackup, useBackupDefaultResources, backupDefaults)
	}

	if spec.VMInsert != nil {
		if spec.VMInsert.UseStrictSecurity == nil {
			spec.VMInsert.UseStrictSecurity = &useStrictSecurity
		}
		if spec.VMInsert.DisableSelfServiceScrape == nil {
			spec.VMInsert.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}
		spec.VMInsert.ImagePullSecrets = append(spec.VMInsert.ImagePullSecrets, spec.ImagePullSecrets...)

		if spec.VMInsert.Image.Repository == "" {
			spec.VMInsert.Image.Repository = c.VMClusterDefault.VMInsertDefault.Image
		}
		spec.VMInsert.Image.Repository = formatContainerImage(c.ContainerRegistry, spec.VMInsert.Image.Repository)
		if spec.VMInsert.Image.Tag == "" {
			if spec.ClusterVersion != "" {
				spec.VMInsert.Image.Tag = spec.ClusterVersion
			} else {
				spec.VMInsert.Image.Tag = c.VMClusterDefault.VMInsertDefault.Version
			}
		}
		if spec.VMInsert.Port == "" {
			spec.VMInsert.Port = c.VMClusterDefault.VMInsertDefault.Port
		}
		if spec.VMInsert.UseDefaultResources == nil {
			spec.VMInsert.UseDefaultResources = &c.VMClusterDefault.UseDefaultResources
		}
		spec.VMInsert.Resources = Resources(spec.VMInsert.Resources,
			config.Resource(c.VMClusterDefault.VMInsertDefault.Resource),
			*spec.VMInsert.UseDefaultResources,
		)

	}
	if spec.VMSelect != nil {
		if spec.VMSelect.UseStrictSecurity == nil {
			spec.VMSelect.UseStrictSecurity = &useStrictSecurity
		}
		if spec.VMSelect.DisableSelfServiceScrape == nil {
			spec.VMSelect.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}

		spec.VMSelect.ImagePullSecrets = append(spec.VMSelect.ImagePullSecrets, spec.ImagePullSecrets...)

		if spec.VMSelect.Image.Repository == "" {
			spec.VMSelect.Image.Repository = c.VMClusterDefault.VMSelectDefault.Image
		}
		spec.VMSelect.Image.Repository = formatContainerImage(c.ContainerRegistry, spec.VMSelect.Image.Repository)
		if spec.VMSelect.Image.Tag == "" {
			if spec.ClusterVersion != "" {
				spec.VMSelect.Image.Tag = spec.ClusterVersion
			} else {
				spec.VMSelect.Image.Tag = c.VMClusterDefault.VMSelectDefault.Version
			}
		}
		if spec.VMSelect.Port == "" {
			spec.VMSelect.Port = c.VMClusterDefault.VMSelectDefault.Port
		}

		if spec.VMSelect.DNSPolicy == "" {
			spec.VMSelect.DNSPolicy = corev1.DNSClusterFirst
		}
		if spec.VMSelect.SchedulerName == "" {
			spec.VMSelect.SchedulerName = "default-scheduler"
		}
		if spec.VMSelect.Image.PullPolicy == "" {
			spec.VMSelect.Image.PullPolicy = corev1.PullIfNotPresent
		}
		// use "/cache" as default cache dir instead of "/tmp" if `CacheMountPath` not set
		if spec.VMSelect.CacheMountPath == "" {
			spec.VMSelect.CacheMountPath = "/cache"
		}
		if spec.VMSelect.UseDefaultResources == nil {
			spec.VMSelect.UseDefaultResources = &c.VMClusterDefault.UseDefaultResources
		}
		spec.VMSelect.Resources = Resources(spec.VMSelect.Resources,
			config.Resource(c.VMClusterDefault.VMSelectDefault.Resource),
			*spec.VMSelect.UseDefaultResources,
		)
	}
	if spec.RequestsLoadBalancer.Enabled {
		addRequestsLoadBalancerDefaults(&spec.RequestsLoadBalancer, useStrictSecurity, spec.License, spec.ImagePullSecrets)
	}
}

func addRequestsLoadBalancerDefaults(lb *vmv1beta1.VMAuthLoadBalancer, useStrictSecurity bool, license *vmv1beta1.License, pullSecrets []corev1.LocalObjectReference) {
	c := getCfg()
	if lb.Spec.UseStrictSecurity == nil {
		lb.Spec.UseStrictSecurity = &useStrictSecurity
	}
	if lb.Spec.DisableSelfServiceScrape == nil {
		lb.Spec.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
	}
	lb.Spec.ImagePullSecrets = append(lb.Spec.ImagePullSecrets, pullSecrets...)
	cv := config.ApplicationDefaults(c.VMAuthDefault)
	addDefaultsToCommonParams(&lb.Spec.CommonDefaultableParams, license, &cv)
	if lb.Spec.EmbeddedProbes == nil {
		lb.Spec.EmbeddedProbes = &vmv1beta1.EmbeddedProbes{}
	}
	if lb.Spec.StartupProbe == nil {
		lb.Spec.StartupProbe = &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{},
			},
		}
	}
	if lb.Spec.AdditionalServiceSpec != nil && !lb.Spec.AdditionalServiceSpec.UseAsDefault {
		lb.Spec.AdditionalServiceSpec.UseAsDefault = true
	}
}

func addVMClusterDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMCluster)
	addVMClusterSpecDefaults(&cr.Spec)
}

func addDefaultsToCommonParams(common *vmv1beta1.CommonDefaultableParams, license *vmv1beta1.License, appDefaults *config.ApplicationDefaults) {
	c := getCfg()

	if common.Image.Repository == "" {
		common.Image.Repository = appDefaults.Image
	}
	if common.DisableSelfServiceScrape == nil {
		common.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
	}
	common.Image.Repository = formatContainerImage(c.ContainerRegistry, common.Image.Repository)
	if common.Image.Tag == "" {
		common.Image.Tag = appDefaults.Version
		if license.IsProvided() {
			common.Image.Tag = addEntSuffixToTag(common.Image.Tag)
		}
	}

	if common.Port == "" {
		common.Port = appDefaults.Port
	}
	if common.Image.PullPolicy == "" {
		common.Image.PullPolicy = corev1.PullIfNotPresent
	}

	if common.UseStrictSecurity == nil && c.EnableStrictSecurity {
		common.UseStrictSecurity = &c.EnableStrictSecurity
	}

	if common.UseDefaultResources == nil && appDefaults.UseDefaultResources {
		common.UseDefaultResources = &appDefaults.UseDefaultResources
	}

	common.Resources = Resources(common.Resources, config.Resource(appDefaults.Resource), ptr.Deref(common.UseDefaultResources, false))
}

func addDefaultsToConfigReloader(common *vmv1beta1.CommonConfigReloaderParams, useDefaultResources bool) {
	c := getCfg()
	if common.ConfigReloaderImage == "" {
		if common.ConfigReloaderImageTag != "" {
			common.ConfigReloaderImage = common.ConfigReloaderImageTag
		} else {
			common.ConfigReloaderImage = c.ConfigReloaderImage
		}
	}

	common.ConfigReloaderImage = formatContainerImage(c.ContainerRegistry, common.ConfigReloaderImage)
	common.ConfigReloaderResources = Resources(common.ConfigReloaderResources, config.Resource{
		Limit: struct {
			Mem string
			Cpu string
		}{
			Cpu: c.ConfigReloaderLimitCPU,
			Mem: c.ConfigReloaderLimitMemory,
		},
		Request: struct {
			Mem string
			Cpu string
		}{
			Cpu: c.ConfigReloaderRequestCPU,
			Mem: c.ConfigReloaderRequestMemory,
		},
	}, useDefaultResources)
}

func addDefaultsToVMBackup(cr *vmv1beta1.VMBackup, useDefaultResources bool, appDefaults *config.ApplicationDefaults) {
	if cr == nil {
		return
	}
	c := getCfg()

	if cr.Image.Repository == "" {
		cr.Image.Repository = appDefaults.Image
	}
	cr.Image.Repository = formatContainerImage(c.ContainerRegistry, cr.Image.Repository)
	if cr.Image.Tag == "" {
		cr.Image.Tag = appDefaults.Version
		// vmbackup always uses enterprise version
		cr.Image.Tag = addEntSuffixToTag(cr.Image.Tag)
	}
	if cr.Port == "" {
		cr.Port = appDefaults.Port
	}
	if cr.Image.PullPolicy == "" {
		cr.Image.PullPolicy = corev1.PullIfNotPresent
	}

	cr.Resources = Resources(cr.Resources, config.Resource(appDefaults.Resource), useDefaultResources)
}

func addVMServiceScrapeDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMServiceScrape)
	if cr == nil {
		return
	}
	c := getCfg()
	if cr.Spec.DiscoveryRole == "" && c.VMServiceScrapeDefault.EnforceEndpointSlices {
		cr.Spec.DiscoveryRole = "endpointslice"
	}
}

const (
	vlStorageDefaultDBPath = "/vlstorage-data"
	vtStorageDefaultDBPath = "/vtstorage-data"
)

func addVTClusterDefaults(objI any) {
	cr := objI.(*vmv1.VTCluster)
	c := getCfg()

	// cluster is tricky is has main strictSecurity and per app
	useStrictSecurity := c.EnableStrictSecurity
	if cr.Spec.UseStrictSecurity != nil {
		useStrictSecurity = *cr.Spec.UseStrictSecurity
	}
	if cr.Spec.ClusterDomainName == "" {
		cr.Spec.ClusterDomainName = c.ClusterDomainName
	}

	if cr.Spec.Storage != nil {
		if cr.Spec.Storage.UseStrictSecurity == nil {
			cr.Spec.Storage.UseStrictSecurity = &useStrictSecurity
		}
		if cr.Spec.Storage.DisableSelfServiceScrape == nil {
			cr.Spec.Storage.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}
		cr.Spec.Storage.ImagePullSecrets = append(cr.Spec.Storage.ImagePullSecrets, cr.Spec.ImagePullSecrets...)

		if cr.Spec.Storage.Image.Repository == "" {
			cr.Spec.Storage.Image.Repository = c.VTClusterDefault.StorageDefault.Image
		}
		cr.Spec.Storage.Image.Repository = formatContainerImage(c.ContainerRegistry, cr.Spec.Storage.Image.Repository)

		if cr.Spec.Storage.Image.Tag == "" {
			if cr.Spec.ClusterVersion != "" {
				cr.Spec.Storage.Image.Tag = cr.Spec.ClusterVersion
			} else {
				cr.Spec.Storage.Image.Tag = c.VTClusterDefault.StorageDefault.Version
			}
		}

		if cr.Spec.Storage.Port == "" {
			cr.Spec.Storage.Port = c.VTClusterDefault.StorageDefault.Port
		}

		if cr.Spec.Storage.DNSPolicy == "" {
			cr.Spec.Storage.DNSPolicy = corev1.DNSClusterFirst
		}
		if cr.Spec.Storage.SchedulerName == "" {
			cr.Spec.Storage.SchedulerName = "default-scheduler"
		}
		if cr.Spec.Storage.Image.PullPolicy == "" {
			cr.Spec.Storage.Image.PullPolicy = corev1.PullIfNotPresent
		}
		if cr.Spec.Storage.StorageDataPath == "" {
			cr.Spec.Storage.StorageDataPath = vtStorageDefaultDBPath
		}
		if cr.Spec.Storage.UseDefaultResources == nil {
			cr.Spec.Storage.UseDefaultResources = &c.VTClusterDefault.UseDefaultResources
		}
		cr.Spec.Storage.Resources = Resources(cr.Spec.Storage.Resources,
			config.Resource(c.VTClusterDefault.StorageDefault.Resource),
			*cr.Spec.Storage.UseDefaultResources,
		)
	}

	if cr.Spec.Insert != nil {
		if cr.Spec.Insert.UseStrictSecurity == nil {
			cr.Spec.Insert.UseStrictSecurity = &useStrictSecurity
		}
		if cr.Spec.Insert.DisableSelfServiceScrape == nil {
			cr.Spec.Insert.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}
		cr.Spec.Insert.ImagePullSecrets = append(cr.Spec.Insert.ImagePullSecrets, cr.Spec.ImagePullSecrets...)

		if cr.Spec.Insert.Image.Repository == "" {
			cr.Spec.Insert.Image.Repository = c.VTClusterDefault.InsertDefault.Image
		}
		cr.Spec.Insert.Image.Repository = formatContainerImage(c.ContainerRegistry, cr.Spec.Insert.Image.Repository)
		if cr.Spec.Insert.Image.Tag == "" {
			if cr.Spec.ClusterVersion != "" {
				cr.Spec.Insert.Image.Tag = cr.Spec.ClusterVersion
			} else {
				cr.Spec.Insert.Image.Tag = c.VTClusterDefault.InsertDefault.Version
			}
		}
		if cr.Spec.Insert.Port == "" {
			cr.Spec.Insert.Port = c.VTClusterDefault.InsertDefault.Port
		}
		if cr.Spec.Insert.UseDefaultResources == nil {
			cr.Spec.Insert.UseDefaultResources = &c.VTClusterDefault.UseDefaultResources
		}
		cr.Spec.Insert.Resources = Resources(cr.Spec.Insert.Resources,
			config.Resource(c.VTClusterDefault.InsertDefault.Resource),
			*cr.Spec.Insert.UseDefaultResources,
		)

	}

	if cr.Spec.Select != nil {
		if cr.Spec.Select.UseStrictSecurity == nil {
			cr.Spec.Select.UseStrictSecurity = &useStrictSecurity
		}
		if cr.Spec.Select.DisableSelfServiceScrape == nil {
			cr.Spec.Select.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}

		cr.Spec.Select.ImagePullSecrets = append(cr.Spec.Select.ImagePullSecrets, cr.Spec.ImagePullSecrets...)

		if cr.Spec.Select.Image.Repository == "" {
			cr.Spec.Select.Image.Repository = c.VTClusterDefault.SelectDefault.Image
		}
		cr.Spec.Select.Image.Repository = formatContainerImage(c.ContainerRegistry, cr.Spec.Select.Image.Repository)
		if cr.Spec.Select.Image.Tag == "" {
			if cr.Spec.ClusterVersion != "" {
				cr.Spec.Select.Image.Tag = cr.Spec.ClusterVersion
			} else {
				cr.Spec.Select.Image.Tag = c.VTClusterDefault.SelectDefault.Version
			}
		}
		if cr.Spec.Select.Port == "" {
			cr.Spec.Select.Port = c.VTClusterDefault.SelectDefault.Port
		}

		if cr.Spec.Select.DNSPolicy == "" {
			cr.Spec.Select.DNSPolicy = corev1.DNSClusterFirst
		}
		if cr.Spec.Select.SchedulerName == "" {
			cr.Spec.Select.SchedulerName = "default-scheduler"
		}
		if cr.Spec.Select.Image.PullPolicy == "" {
			cr.Spec.Select.Image.PullPolicy = corev1.PullIfNotPresent
		}

		if cr.Spec.Select.UseDefaultResources == nil {
			cr.Spec.Select.UseDefaultResources = &c.VTClusterDefault.UseDefaultResources
		}
		cr.Spec.Select.Resources = Resources(cr.Spec.Select.Resources,
			config.Resource(c.VTClusterDefault.SelectDefault.Resource),
			*cr.Spec.Select.UseDefaultResources,
		)
	}

	if cr.Spec.RequestsLoadBalancer.Enabled {
		addRequestsLoadBalancerDefaults(&cr.Spec.RequestsLoadBalancer, useStrictSecurity, nil, cr.Spec.ImagePullSecrets)
	}
}

func addVLClusterDefaults(objI any) {
	cr := objI.(*vmv1.VLCluster)
	c := getCfg()

	// cluster is tricky is has main strictSecurity and per app
	useStrictSecurity := c.EnableStrictSecurity
	if cr.Spec.UseStrictSecurity != nil {
		useStrictSecurity = *cr.Spec.UseStrictSecurity
	}
	if cr.Spec.ClusterDomainName == "" {
		cr.Spec.ClusterDomainName = c.ClusterDomainName
	}

	if cr.Spec.VLStorage != nil {
		if cr.Spec.VLStorage.UseStrictSecurity == nil {
			cr.Spec.VLStorage.UseStrictSecurity = &useStrictSecurity
		}
		if cr.Spec.VLStorage.DisableSelfServiceScrape == nil {
			cr.Spec.VLStorage.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}
		cr.Spec.VLStorage.ImagePullSecrets = append(cr.Spec.VLStorage.ImagePullSecrets, cr.Spec.ImagePullSecrets...)

		if cr.Spec.VLStorage.Image.Repository == "" {
			cr.Spec.VLStorage.Image.Repository = c.VLClusterDefault.VLStorageDefault.Image
		}
		cr.Spec.VLStorage.Image.Repository = formatContainerImage(c.ContainerRegistry, cr.Spec.VLStorage.Image.Repository)

		if cr.Spec.VLStorage.Image.Tag == "" {
			if cr.Spec.ClusterVersion != "" {
				cr.Spec.VLStorage.Image.Tag = cr.Spec.ClusterVersion
			} else {
				cr.Spec.VLStorage.Image.Tag = c.VLClusterDefault.VLStorageDefault.Version
			}
		}

		if cr.Spec.VLStorage.Port == "" {
			cr.Spec.VLStorage.Port = c.VLClusterDefault.VLStorageDefault.Port
		}

		if cr.Spec.VLStorage.DNSPolicy == "" {
			cr.Spec.VLStorage.DNSPolicy = corev1.DNSClusterFirst
		}
		if cr.Spec.VLStorage.SchedulerName == "" {
			cr.Spec.VLStorage.SchedulerName = "default-scheduler"
		}
		if cr.Spec.VLStorage.Image.PullPolicy == "" {
			cr.Spec.VLStorage.Image.PullPolicy = corev1.PullIfNotPresent
		}
		if cr.Spec.VLStorage.StorageDataPath == "" {
			cr.Spec.VLStorage.StorageDataPath = vlStorageDefaultDBPath
		}
		if cr.Spec.VLStorage.UseDefaultResources == nil {
			cr.Spec.VLStorage.UseDefaultResources = &c.VLClusterDefault.UseDefaultResources
		}
		cr.Spec.VLStorage.Resources = Resources(cr.Spec.VLStorage.Resources,
			config.Resource(c.VLClusterDefault.VLStorageDefault.Resource),
			*cr.Spec.VLStorage.UseDefaultResources,
		)
	}

	if cr.Spec.VLInsert != nil {
		if cr.Spec.VLInsert.UseStrictSecurity == nil {
			cr.Spec.VLInsert.UseStrictSecurity = &useStrictSecurity
		}
		if cr.Spec.VLInsert.DisableSelfServiceScrape == nil {
			cr.Spec.VLInsert.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}
		cr.Spec.VLInsert.ImagePullSecrets = append(cr.Spec.VLInsert.ImagePullSecrets, cr.Spec.ImagePullSecrets...)

		if cr.Spec.VLInsert.Image.Repository == "" {
			cr.Spec.VLInsert.Image.Repository = c.VLClusterDefault.VLInsertDefault.Image
		}
		cr.Spec.VLInsert.Image.Repository = formatContainerImage(c.ContainerRegistry, cr.Spec.VLInsert.Image.Repository)
		if cr.Spec.VLInsert.Image.Tag == "" {
			if cr.Spec.ClusterVersion != "" {
				cr.Spec.VLInsert.Image.Tag = cr.Spec.ClusterVersion
			} else {
				cr.Spec.VLInsert.Image.Tag = c.VLClusterDefault.VLInsertDefault.Version
			}
		}
		if cr.Spec.VLInsert.Port == "" {
			cr.Spec.VLInsert.Port = c.VLClusterDefault.VLInsertDefault.Port
		}
		if cr.Spec.VLInsert.UseDefaultResources == nil {
			cr.Spec.VLInsert.UseDefaultResources = &c.VLClusterDefault.UseDefaultResources
		}
		cr.Spec.VLInsert.Resources = Resources(cr.Spec.VLInsert.Resources,
			config.Resource(c.VLClusterDefault.VLInsertDefault.Resource),
			*cr.Spec.VLInsert.UseDefaultResources,
		)

	}
	if cr.Spec.VLSelect != nil {
		if cr.Spec.VLSelect.UseStrictSecurity == nil {
			cr.Spec.VLSelect.UseStrictSecurity = &useStrictSecurity
		}
		if cr.Spec.VLSelect.DisableSelfServiceScrape == nil {
			cr.Spec.VLSelect.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}

		cr.Spec.VLSelect.ImagePullSecrets = append(cr.Spec.VLSelect.ImagePullSecrets, cr.Spec.ImagePullSecrets...)

		if cr.Spec.VLSelect.Image.Repository == "" {
			cr.Spec.VLSelect.Image.Repository = c.VLClusterDefault.VLSelectDefault.Image
		}
		cr.Spec.VLSelect.Image.Repository = formatContainerImage(c.ContainerRegistry, cr.Spec.VLSelect.Image.Repository)
		if cr.Spec.VLSelect.Image.Tag == "" {
			if cr.Spec.ClusterVersion != "" {
				cr.Spec.VLSelect.Image.Tag = cr.Spec.ClusterVersion
			} else {
				cr.Spec.VLSelect.Image.Tag = c.VLClusterDefault.VLSelectDefault.Version
			}
		}
		if cr.Spec.VLSelect.Port == "" {
			cr.Spec.VLSelect.Port = c.VLClusterDefault.VLSelectDefault.Port
		}

		if cr.Spec.VLSelect.DNSPolicy == "" {
			cr.Spec.VLSelect.DNSPolicy = corev1.DNSClusterFirst
		}
		if cr.Spec.VLSelect.SchedulerName == "" {
			cr.Spec.VLSelect.SchedulerName = "default-scheduler"
		}
		if cr.Spec.VLSelect.Image.PullPolicy == "" {
			cr.Spec.VLSelect.Image.PullPolicy = corev1.PullIfNotPresent
		}

		if cr.Spec.VLSelect.UseDefaultResources == nil {
			cr.Spec.VLSelect.UseDefaultResources = &c.VLClusterDefault.UseDefaultResources
		}
		cr.Spec.VLSelect.Resources = Resources(cr.Spec.VLSelect.Resources,
			config.Resource(c.VLClusterDefault.VLSelectDefault.Resource),
			*cr.Spec.VLSelect.UseDefaultResources,
		)
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		if cr.Spec.RequestsLoadBalancer.Spec.UseStrictSecurity == nil {
			cr.Spec.RequestsLoadBalancer.Spec.UseStrictSecurity = &useStrictSecurity
		}
		if cr.Spec.RequestsLoadBalancer.Spec.DisableSelfServiceScrape == nil {
			cr.Spec.RequestsLoadBalancer.Spec.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
		}
		cr.Spec.RequestsLoadBalancer.Spec.ImagePullSecrets = append(cr.Spec.RequestsLoadBalancer.Spec.ImagePullSecrets, cr.Spec.ImagePullSecrets...)

		cv := config.ApplicationDefaults(c.VMAuthDefault)
		addDefaultsToCommonParams(&cr.Spec.RequestsLoadBalancer.Spec.CommonDefaultableParams, cr.Spec.License, &cv)
		spec := &cr.Spec.RequestsLoadBalancer.Spec
		if spec.EmbeddedProbes == nil {
			spec.EmbeddedProbes = &vmv1beta1.EmbeddedProbes{}
		}
		if spec.StartupProbe == nil {
			spec.StartupProbe = &corev1.Probe{
				ProbeHandler: corev1.ProbeHandler{
					HTTPGet: &corev1.HTTPGetAction{},
				},
			}
		}
		if spec.AdditionalServiceSpec != nil && !spec.AdditionalServiceSpec.UseAsDefault {
			spec.AdditionalServiceSpec.UseAsDefault = true
		}
	}
}

func addEntSuffixToTag(versionTag string) string {
	// expected version tag is:
	// vX.Y.Z with optional suffix -
	if !strings.HasPrefix(versionTag, "v") || strings.Count(versionTag, ".") != 2 {
		return versionTag
	}
	if idx := strings.Index(versionTag, "@"); idx != -1 {
		return versionTag
	}
	if idx := strings.Index(versionTag, "-"); idx > 0 {
		suffix := versionTag[idx:]
		switch suffix {
		case "-enterprise", "-enterprise-cluster":
		case "-cluster":
			versionTag = versionTag[:idx] + "-enterprise-cluster"
		}
	} else {
		versionTag += "-enterprise"
	}

	return versionTag
}

func addVMDistributedDefaults(objI any) {
	cr, ok := objI.(*vmv1alpha1.VMDistributed)
	if !ok {
		return
	}
	c := getCfg()
	cv := config.ApplicationDefaults(c.VMAuthDefault)
	if cr.Spec.VMAuth.Spec == nil {
		cr.Spec.VMAuth.Spec = &vmv1beta1.VMAuthSpec{}
	}
	addDefaultsToCommonParams(&cr.Spec.VMAuth.Spec.CommonDefaultableParams, cr.Spec.License, &cv)
	addDefaultsToConfigReloader(&cr.Spec.VMAuth.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.VMAuth.Spec.UseDefaultResources, false))

	cv = config.ApplicationDefaults(c.VMAgentDefault)
	if cr.Spec.VMAgent.Spec == nil {
		cr.Spec.VMAgent.Spec = &vmv1alpha1.CustomVMAgentSpec{}
	}
	addDefaultsToCommonParams(&cr.Spec.VMAgent.Spec.CommonDefaultableParams, cr.Spec.License, &cv)

	for i := range cr.Spec.Zones.VMClusters {
		cluster := &cr.Spec.Zones.VMClusters[i]
		if cluster.Spec == nil {
			cluster.Spec = &vmv1beta1.VMClusterSpec{}
		}
		if cluster.Spec.VMSelect == nil {
			cluster.Spec.VMSelect = &vmv1beta1.VMSelect{}
		}
		addVMClusterSpecDefaults(cluster.Spec)
	}
}
