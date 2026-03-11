package build

import (
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

type commonParams struct {
	tag               string
	useStrictSecurity *bool
	license           *vmv1beta1.License
	imagePullSecrets  []corev1.LocalObjectReference
}

func getCfg() *config.BaseOperatorConf {
	return config.MustGetBaseConfig()
}

// AddDefaults adds defaulting functions to the runtimeScheme
func AddDefaults(scheme *runtime.Scheme) {
	scheme.AddTypeDefaultingFunc(&corev1.Service{}, addServiceDefaults)
	scheme.AddTypeDefaultingFunc(&appsv1.Deployment{}, addDeploymentDefaults)
	scheme.AddTypeDefaultingFunc(&appsv1.StatefulSet{}, addStatefulsetDefaults)
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
	scheme.AddTypeDefaultingFunc(&vmv1alpha1.VMDistributed{}, addVMDistributedDefaults)
}

func addVMDistributedDefaults(objI any) {
	cr := objI.(*vmv1alpha1.VMDistributed)

	if cr.Spec.ZoneCommon.ReadyTimeout == nil {
		cr.Spec.ZoneCommon.ReadyTimeout = &metav1.Duration{
			Duration: 5 * time.Minute,
		}
	}
	if cr.Spec.ZoneCommon.UpdatePause == nil {
		cr.Spec.ZoneCommon.UpdatePause = &metav1.Duration{
			Duration: 1 * time.Minute,
		}
	}
	if cr.Spec.License.IsProvided() {
		if !cr.Spec.VMAuth.Spec.License.IsProvided() {
			cr.Spec.VMAuth.Spec.License = cr.Spec.License.DeepCopy()
		}
		if !cr.Spec.ZoneCommon.VMAgent.Spec.License.IsProvided() {
			cr.Spec.ZoneCommon.VMAgent.Spec.License = cr.Spec.License.DeepCopy()
		}
		if !cr.Spec.ZoneCommon.VMCluster.Spec.License.IsProvided() {
			cr.Spec.ZoneCommon.VMCluster.Spec.License = cr.Spec.License.DeepCopy()
		}
	}
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
	cv := config.ApplicationDefaults(c.VMAuth)
	cp := commonParams{
		license: cr.Spec.License,
	}
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, &cp, &cv)
	addDefaultsToConfigReloader(&cr.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.UseDefaultResources, false))
}

func addVMAlertDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMAlert)
	c := getCfg()

	cv := config.ApplicationDefaults(c.VMAlert)
	cp := commonParams{
		license: cr.Spec.License,
	}
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, &cp, &cv)
	addDefaultsToConfigReloader(&cr.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.UseDefaultResources, false))
	if cr.Spec.ConfigReloaderImage == "" {
		panic("cannot be empty")
	}
}

func addVMAgentDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMAgent)
	c := getCfg()

	cv := config.ApplicationDefaults(c.VMAgent)
	cp := commonParams{
		license: cr.Spec.License,
	}
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, &cp, &cv)
	addDefaultsToConfigReloader(&cr.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.UseDefaultResources, false))
	if cr.Spec.IngestOnlyMode == nil {
		cr.Spec.IngestOnlyMode = ptr.To(false)
	}
}

func addVLAgentDefaults(objI any) {
	cr := objI.(*vmv1.VLAgent)
	c := getCfg()

	cv := config.ApplicationDefaults(c.VLAgent)
	cp := commonParams{
		license: cr.Spec.License,
	}
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, &cp, &cv)
}

func addVMSingleDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMSingle)
	c := getCfg()
	cv := config.ApplicationDefaults(c.VMSingle)
	cp := commonParams{
		license: cr.Spec.License,
	}
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, &cp, &cv)
	addDefaultsToConfigReloader(&cr.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.UseDefaultResources, false))
	if cr.Spec.IngestOnlyMode == nil {
		cr.Spec.IngestOnlyMode = ptr.To(true)
	}
	bv := config.ApplicationDefaults(c.VMBackup)
	useBackupDefaultResources := c.VMBackup.UseDefaultResources
	if cr.Spec.UseDefaultResources != nil {
		useBackupDefaultResources = *cr.Spec.UseDefaultResources
	}
	addDefaultsToVMBackup(cr.Spec.VMBackup, useBackupDefaultResources, &bv)
}

func addVLogsDefaults(objI any) {
	cr := objI.(*vmv1beta1.VLogs)
	c := getCfg()
	cv := config.ApplicationDefaults(c.VLogs)
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, nil, &cv)
}

func addVMAnomalyDefaults(objI any) {
	cr := objI.(*vmv1.VMAnomaly)

	// vmanomaly takes up to 2 minutes to start
	if cr.Spec.LivenessProbe == nil {
		cr.Spec.LivenessProbe = &corev1.Probe{
			InitialDelaySeconds: 10,
			FailureThreshold:    16,
			PeriodSeconds:       10,
		}
	}
	if cr.Spec.ReadinessProbe == nil {
		cr.Spec.ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 10,
			FailureThreshold:    16,
			PeriodSeconds:       10,
		}
	}
	c := getCfg()
	cv := config.ApplicationDefaults(c.VMAnomaly)
	cp := commonParams{
		license: cr.Spec.License,
	}
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, &cp, &cv)
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
	cv := config.ApplicationDefaults(c.VLSingle)
	cp := commonParams{
		license: cr.Spec.License,
	}
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, &cp, &cv)
}

func addVTSingleDefaults(objI any) {
	cr := objI.(*vmv1.VTSingle)
	c := getCfg()
	cv := config.ApplicationDefaults(c.VTSingle)
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, nil, &cv)
}

func addVMAlertmanagerDefaults(objI any) {
	cr := objI.(*vmv1beta1.VMAlertmanager)
	c := getCfg()
	cv := config.ApplicationDefaults(c.VMAlertmanager)
	if cr.Spec.ClusterDomainName == "" {
		cr.Spec.ClusterDomainName = c.ClusterDomainName
	}
	if cr.Spec.ReplicaCount == nil {
		cr.Spec.ReplicaCount = ptr.To[int32](1)
	}
	if cr.Spec.PortName == "" {
		cr.Spec.PortName = "web"
	}
	addDefaultsToCommonParams(&cr.Spec.CommonAppsParams, nil, &cv)
	addDefaultsToConfigReloader(&cr.Spec.CommonConfigReloaderParams, ptr.Deref(cr.Spec.UseDefaultResources, false))
}

const (
	vmStorageDefaultDBPath = "vmstorage-data"
)

func addRequestsLoadBalancerDefaults(lb *vmv1beta1.VMAuthLoadBalancer, cp *commonParams) {
	c := getCfg()
	cv := config.ApplicationDefaults(c.VMAuth)
	addDefaultsToCommonParams(&lb.Spec.CommonAppsParams, cp, &cv)
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
	c := getCfg()
	if cr.Spec.ClusterDomainName == "" {
		cr.Spec.ClusterDomainName = c.ClusterDomainName
	}
	cp := commonParams{
		useStrictSecurity: cr.Spec.UseStrictSecurity,
		tag:               cr.Spec.ClusterVersion,
		license:           cr.Spec.License,
		imagePullSecrets:  cr.Spec.ImagePullSecrets,
	}
	if cr.Spec.VMStorage != nil {
		if cr.Spec.VMStorage.StorageDataPath == "" {
			cr.Spec.VMStorage.StorageDataPath = vmStorageDefaultDBPath
		}
		if cr.Spec.VMStorage.VMInsertPort == "" {
			cr.Spec.VMStorage.VMInsertPort = c.VMCluster.Storage.VMInsertPort
		}
		if cr.Spec.VMStorage.VMSelectPort == "" {
			cr.Spec.VMStorage.VMSelectPort = c.VMCluster.Storage.VMSelectPort
		}
		cv := config.ApplicationDefaults(c.VMCluster.Storage.Common)
		addDefaultsToCommonParams(&cr.Spec.VMStorage.CommonAppsParams, &cp, &cv)

		bv := config.ApplicationDefaults(c.VMBackup)
		useBackupDefaultResources := c.VMBackup.UseDefaultResources
		if cr.Spec.VMStorage.UseDefaultResources != nil {
			useBackupDefaultResources = *cr.Spec.VMStorage.UseDefaultResources
		}
		addDefaultsToVMBackup(cr.Spec.VMStorage.VMBackup, useBackupDefaultResources, &bv)
	}
	if cr.Spec.VMInsert != nil {
		cv := config.ApplicationDefaults(c.VMCluster.Insert)
		addDefaultsToCommonParams(&cr.Spec.VMInsert.CommonAppsParams, &cp, &cv)
	}
	if cr.Spec.VMSelect != nil {
		if cr.Spec.VMSelect.CacheMountPath == "" {
			cr.Spec.VMSelect.CacheMountPath = "/cache"
		}
		cv := config.ApplicationDefaults(c.VMCluster.Select)
		addDefaultsToCommonParams(&cr.Spec.VMSelect.CommonAppsParams, &cp, &cv)
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		addRequestsLoadBalancerDefaults(&cr.Spec.RequestsLoadBalancer, &cp)
	}
}

func addDefaultsToCommonParams(common *vmv1beta1.CommonAppsParams, cp *commonParams, appDefaults *config.ApplicationDefaults) {
	c := getCfg()
	if common.Image.Repository == "" {
		common.Image.Repository = appDefaults.Image
	}
	common.Image.Repository = formatContainerImage(c.ContainerRegistry, common.Image.Repository)
	useStrictSecurity := c.EnableStrictSecurity
	if common.Image.Tag == "" {
		if cp != nil && len(cp.tag) > 0 {
			common.Image.Tag = cp.tag
		} else {
			common.Image.Tag = appDefaults.Version
		}
	}
	if cp != nil {
		if cp.license.IsProvided() {
			common.Image.Tag = addEntSuffixToTag(common.Image.Tag)
		}
		common.ImagePullSecrets = append(common.ImagePullSecrets, cp.imagePullSecrets...)
		if cp.useStrictSecurity != nil {
			useStrictSecurity = *cp.useStrictSecurity
		}

	}
	if common.DisableSelfServiceScrape == nil {
		common.DisableSelfServiceScrape = &c.DisableSelfServiceScrapeCreation
	}
	if common.Port == "" {
		common.Port = appDefaults.Port
	}
	if common.Image.PullPolicy == "" {
		common.Image.PullPolicy = corev1.PullIfNotPresent
	}
	if common.UseStrictSecurity == nil {
		common.UseStrictSecurity = &useStrictSecurity
	}
	if common.UseDefaultResources == nil && appDefaults.UseDefaultResources {
		common.UseDefaultResources = &appDefaults.UseDefaultResources
	}
	if common.TerminationGracePeriodSeconds == nil {
		common.TerminationGracePeriodSeconds = ptr.To(appDefaults.TerminationGracePeriodSeconds)
	}
	if common.DNSPolicy == "" {
		common.DNSPolicy = corev1.DNSClusterFirst
	}
	if common.SchedulerName == "" {
		common.SchedulerName = "default-scheduler"
	}
	common.Resources = Resources(common.Resources, config.Resource(appDefaults.Resource), ptr.Deref(common.UseDefaultResources, false))
}

func addDefaultsToConfigReloader(common *vmv1beta1.CommonConfigReloaderParams, useDefaultResources bool) {
	c := getCfg()
	if common.ConfigReloaderImage == "" {
		if common.ConfigReloaderImageTag != "" {
			common.ConfigReloaderImage = common.ConfigReloaderImageTag
		} else {
			common.ConfigReloaderImage = c.ConfigReloader.Image
		}
	}

	common.ConfigReloaderImage = formatContainerImage(c.ContainerRegistry, common.ConfigReloaderImage)
	common.ConfigReloaderResources = Resources(common.ConfigReloaderResources, config.Resource(c.ConfigReloader.Resource), useDefaultResources)
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
	if cr.Spec.DiscoveryRole == "" && c.VMServiceScrape.EnforceEndpointSlices {
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
	cp := commonParams{
		useStrictSecurity: cr.Spec.UseStrictSecurity,
		tag:               cr.Spec.ClusterVersion,
		license:           nil,
		imagePullSecrets:  cr.Spec.ImagePullSecrets,
	}
	if cr.Spec.ClusterDomainName == "" {
		cr.Spec.ClusterDomainName = c.ClusterDomainName
	}
	if cr.Spec.Storage != nil {
		if cr.Spec.Storage.StorageDataPath == "" {
			cr.Spec.Storage.StorageDataPath = vtStorageDefaultDBPath
		}
		cv := config.ApplicationDefaults(c.VTCluster.Storage)
		addDefaultsToCommonParams(&cr.Spec.Storage.CommonAppsParams, &cp, &cv)
	}

	if cr.Spec.Insert != nil {
		cv := config.ApplicationDefaults(c.VTCluster.Insert)
		addDefaultsToCommonParams(&cr.Spec.Insert.CommonAppsParams, &cp, &cv)
	}

	if cr.Spec.Select != nil {
		cv := config.ApplicationDefaults(c.VTCluster.Select)
		addDefaultsToCommonParams(&cr.Spec.Select.CommonAppsParams, &cp, &cv)
	}

	if cr.Spec.RequestsLoadBalancer.Enabled {
		addRequestsLoadBalancerDefaults(&cr.Spec.RequestsLoadBalancer, &cp)
	}
}

func addVLClusterDefaults(objI any) {
	cr := objI.(*vmv1.VLCluster)
	c := getCfg()
	cp := commonParams{
		useStrictSecurity: cr.Spec.UseStrictSecurity,
		tag:               cr.Spec.ClusterVersion,
		license:           cr.Spec.License,
		imagePullSecrets:  cr.Spec.ImagePullSecrets,
	}
	if cr.Spec.ClusterDomainName == "" {
		cr.Spec.ClusterDomainName = c.ClusterDomainName
	}
	if cr.Spec.VLStorage != nil {
		if cr.Spec.VLStorage.StorageDataPath == "" {
			cr.Spec.VLStorage.StorageDataPath = vlStorageDefaultDBPath
		}
		cv := config.ApplicationDefaults(c.VLCluster.Storage)
		addDefaultsToCommonParams(&cr.Spec.VLStorage.CommonAppsParams, &cp, &cv)
	}
	if cr.Spec.VLInsert != nil {
		cv := config.ApplicationDefaults(c.VLCluster.Insert)
		addDefaultsToCommonParams(&cr.Spec.VLInsert.CommonAppsParams, &cp, &cv)
	}
	if cr.Spec.VLSelect != nil {
		cv := config.ApplicationDefaults(c.VLCluster.Select)
		addDefaultsToCommonParams(&cr.Spec.VLSelect.CommonAppsParams, &cp, &cv)
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		addRequestsLoadBalancerDefaults(&cr.Spec.RequestsLoadBalancer, &cp)
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
