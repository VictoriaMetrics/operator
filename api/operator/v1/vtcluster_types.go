/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	"encoding/json"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VTClusterSpec defines the desired state of VTCluster
type VTClusterSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the
	// VTSelect, VTInsert and VTStorage Pods.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// ClusterVersion defines default images tag for all components.
	// it can be overwritten with component specific image.tag value.
	// +optional
	ClusterVersion string `json:"clusterVersion,omitempty"`
	// ClusterDomainName defines domain name suffix for in-cluster dns addresses
	// aka .cluster.local
	// used by vtinsert and vtselect to build vtstorage address
	// +optional
	ClusterDomainName string `json:"clusterDomainName,omitempty"`

	// ImagePullSecrets An optional list of references to secrets in the same namespace
	// to use for pulling images from registries
	// see https://kubernetes.io/docs/concepts/containers/images/#referring-to-an-imagepullsecrets-on-a-pod
	// +optional
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	Insert  *VTInsert  `json:"insert,omitempty"`
	Select  *VTSelect  `json:"select,omitempty"`
	Storage *VTStorage `json:"storage,omitempty"`

	// Paused If set to true all actions on the underlying managed objects are not
	// going to be performed, except for delete actions.
	// +optional
	Paused bool `json:"paused,omitempty"`
	// UseStrictSecurity enables strict security mode for component
	// it restricts disk writes access
	// uses non-root user out of the box
	// drops not needed security permissions
	// +optional
	UseStrictSecurity *bool `json:"useStrictSecurity,omitempty"`

	// RequestsLoadBalancer configures load-balancing for vtinsert and vtselect requests.
	// It helps to evenly spread load across pods.
	// Usually it's not possible with Kubernetes TCP-based services.
	RequestsLoadBalancer vmv1beta1.VMAuthLoadBalancer `json:"requestsLoadBalancer,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VTClusterSpec) UnmarshalJSON(src []byte) error {
	type pcr VTClusterSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vtcluster spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VTClusterStatus defines the observed state of VTCluster
type VTClusterStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VTClusterStatus) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.StatusMetadata
}

// VTInsert defines vtinsert component configuration at victoria-traces cluster
type VTInsert struct {
	// PodMetadata configures Labels and Annotations which are propagated to the VTSelect pods.
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VTSelect to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VTSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// ServiceSpec that will be added to vtselect service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vtselect VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget       *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*vmv1beta1.EmbeddedProbes `json:",inline"`
	// Configures horizontal pod autoscaling.
	// +optional
	HPA *vmv1beta1.EmbeddedHPA `json:"hpa,omitempty"`

	// UpdateStrategy - overrides default update strategy.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`

	vmv1beta1.CommonDefaultableParams           `json:",inline"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline"`
}

// Probe implements build.probeCRD interface
func (cr *VTInsert) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.EmbeddedProbes
}

// ProbePath implements build.probeCRD interface
func (cr *VTInsert) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VTInsert) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VTInsert) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VTInsert) ProbeNeedLiveness() bool {
	return true
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VTInsert) GetMetricPath() string {
	if cr == nil {
		return healthPath
	}
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VTInsert) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VTInsert) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VTInsert) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.ServiceSpec
}

// VTSelect defines vtselect component configuration at victoria-traces cluster
type VTSelect struct {
	// PodMetadata configures Labels and Annotations which are propagated to the VTSelect pods.
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VTSelect to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VTSelect to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// ServiceSpec that will be added to vtselect service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vtselect VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget       *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*vmv1beta1.EmbeddedProbes `json:",inline"`
	// Configures horizontal pod autoscaling.
	// +optional
	HPA *vmv1beta1.EmbeddedHPA `json:"hpa,omitempty"`

	// UpdateStrategy - overrides default update strategy.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`

	vmv1beta1.CommonDefaultableParams           `json:",inline"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline"`
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VTSelect) GetMetricPath() string {
	if cr == nil {
		return healthPath
	}
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VTSelect) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VTSelect) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// Probe implements build.probeCRD interface
func (cr *VTSelect) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.EmbeddedProbes
}

// ProbePath implements build.probeCRD interface
func (cr *VTSelect) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VTSelect) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VTSelect) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VTSelect) ProbeNeedLiveness() bool {
	return true
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VTSelect) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.ServiceSpec
}

// VTStorage defines vtstorage component configuration at victoria-traces cluster
type VTStorage struct {
	// RetentionPeriod for the stored traces
	// https://docs.victoriametrics.com/victoriatraces/#configure-and-run-victoriatraces
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|w|y)?$"
	RetentionPeriod string `json:"retentionPeriod,omitempty"`
	// RetentionMaxDiskSpaceUsageBytes for the stored traces
	// VictoriaTraces keeps at least two last days of data in order to guarantee that the traces for the last day can be returned in queries.
	// This means that the total disk space usage may exceed the -retention.maxDiskSpaceUsageBytes,
	// if the size of the last two days of data exceeds the -retention.maxDiskSpaceUsageBytes.
	// https://docs.victoriametrics.com/victoriatraces/#configure-and-run-victoriatraces
	// +optional
	RetentionMaxDiskSpaceUsageBytes vmv1beta1.BytesString `json:"retentionMaxDiskSpaceUsageBytes,omitempty"`
	// FutureRetention for the stored traces
	// Log entries with timestamps bigger than now+futureRetention are rejected during data ingestion
	// see https://docs.victoriametrics.com/victoriatraces/#configure-and-run-victoriatraces
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|w|y)?$"
	FutureRetention string `json:"futureRetention,omitempty"`
	// LogNewStreams Whether to log creation of new streams; this can be useful for debugging of high cardinality issues with log streams
	// see https://docs.victoriametrics.com/victoriatraces/#configure-and-run-victoriatraces
	LogNewStreams bool `json:"logNewStreams,omitempty"`
	// Whether to log all the ingested log entries; this can be useful for debugging of data ingestion
	// see https://docs.victoriametrics.com/victoriatraces/#configure-and-run-victoriatraces
	LogIngestedRows bool `json:"logIngestedRows,omitempty"`

	// PodMetadata configures Labels and Annotations which are propagated to the VTStorage pods.
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// LogFormat for VTStorage to be configured with.
	// default or json
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// LogLevel for VTStorage to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`

	// ServiceSpec that will be added to vtselect service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vtselect VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget       *vmv1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*vmv1beta1.EmbeddedProbes `json:",inline"`
	// RollingUpdateStrategy defines strategy for application updates
	// Default is OnDelete, in this case operator handles update process
	// Can be changed for RollingUpdate
	// +optional
	RollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"rollingUpdateStrategy,omitempty"`
	// ClaimTemplates allows adding additional VolumeClaimTemplates for StatefulSet
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	// StorageDataPath - path to storage data
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage configures persistent volume for VTStorage
	// +optional
	Storage *vmv1beta1.StorageSpec `json:"storage,omitempty"`
	// PersistentVolumeClaimRetentionPolicy allows configuration of PVC retention policy
	// +optional
	PersistentVolumeClaimRetentionPolicy *appsv1.StatefulSetPersistentVolumeClaimRetentionPolicy `json:"persistentVolumeClaimRetentionPolicy,omitempty"`

	// MaintenanceInsertNodeIDs - excludes given node ids from insert requests routing, must contain pod suffixes - for pod-0, id will be 0 and etc.
	// lets say, you have pod-0, pod-1, pod-2, pod-3. to exclude pod-0 and pod-3 from insert routing, define nodeIDs: [0,3].
	// Useful at storage expanding, when you want to rebalance some data at cluster.
	// +optional
	MaintenanceInsertNodeIDs []int32 `json:"maintenanceInsertNodeIDs,omitempty"`
	// MaintenanceInsertNodeIDs - excludes given node ids from select requests routing, must contain pod suffixes - for pod-0, id will be 0 and etc.
	MaintenanceSelectNodeIDs []int32 `json:"maintenanceSelectNodeIDs,omitempty"`

	vmv1beta1.CommonDefaultableParams           `json:",inline"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline"`

	// RollingUpdateStrategyBehavior defines customized behavior for rolling updates.
	// It applies if the RollingUpdateStrategy is set to OnDelete, which is the default.
	// +optional
	RollingUpdateStrategyBehavior *vmv1beta1.StatefulSetUpdateStrategyBehavior `json:"rollingUpdateStrategyBehavior,omitempty"`
}

// GetStorageVolumeName returns formatted name for vtstorage volume
func (cr *VTStorage) GetStorageVolumeName() string {
	if cr.Storage != nil && cr.Storage.VolumeClaimTemplate.Name != "" {
		return cr.Storage.VolumeClaimTemplate.Name
	}
	return "vtstorage-db"
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VTStorage) GetMetricPath() string {
	if cr == nil {
		return healthPath
	}
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VTStorage) GetExtraArgs() map[string]string {
	return cr.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VTStorage) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.ServiceScrapeSpec
}

// Probe implements build.probeCRD interface
func (cr *VTStorage) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.EmbeddedProbes
}

// ProbePath implements build.probeCRD interface
func (cr *VTStorage) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VTStorage) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VTStorage) ProbePort() string {
	return cr.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (*VTStorage) ProbeNeedLiveness() bool {
	return false
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VTStorage) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.ServiceSpec
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VTCluster is fast, cost-effective and scalable traces database.
// +kubebuilder:printcolumn:name="Insert Count",type="string",JSONPath=".spec.vtinsert.replicaCount",description="replicas of VTInsert"
// +kubebuilder:printcolumn:name="Storage Count",type="string",JSONPath=".spec.vtstorage.replicaCount",description="replicas of VTStorage"
// +kubebuilder:printcolumn:name="Select Count",type="string",JSONPath=".spec.vtselect.replicaCount",description="replicas of VTSelect"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of cluster"
// +genclient
type VTCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VTClusterSpec   `json:"spec,omitempty"`
	Status VTClusterStatus `json:"status,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VTClusterSpec `json:"-" yaml:"-"`
}

// SetLastSpec implements objectWithLastAppliedState interface
func (cr *VTCluster) SetLastSpec(prevSpec VTClusterSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VTCluster) UnmarshalJSON(src []byte) error {
	type pcr VTCluster
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := vmv1beta1.ParseLastAppliedStateTo(cr); err != nil {
		return err
	}
	return nil
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VTCluster) GetStatus() *VTClusterStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VTCluster) DefaultStatusFields(vs *VTClusterStatus) {
}

// AsOwner returns owner references with current object as owner
func (cr *VTCluster) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         cr.APIVersion,
			Kind:               cr.Kind,
			Name:               cr.Name,
			UID:                cr.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

// VMAuthLBSelectorLabels defines selector labels for vmauth balancer
func (cr *VTCluster) VMAuthLBSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vtclusterlb-vmauth-balancer",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VMAuthLBPodLabels returns pod labels for vtclusterlb-vmauth-balancer cluster component
func (cr *VTCluster) VMAuthLBPodLabels() map[string]string {
	selectorLabels := cr.VMAuthLBSelectorLabels()
	if cr.Spec.RequestsLoadBalancer.Spec.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.RequestsLoadBalancer.Spec.PodMetadata.Labels, selectorLabels)
}

// VMAuthLBPodAnnotations returns pod annotations for vmstorage cluster component
func (cr *VTCluster) VMAuthLBPodAnnotations() map[string]string {
	if cr.Spec.RequestsLoadBalancer.Spec.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.RequestsLoadBalancer.Spec.PodMetadata.Annotations
}

// GetVMAuthLBName returns prefixed name for the loadbalanacer components
func (cr *VTCluster) GetVMAuthLBName() string {
	return fmt.Sprintf("vtclusterlb-%s", cr.Name)
}

// GetVTSelectLBName returns headless proxy service name for select component
func (cr *VTCluster) GetVTSelectLBName() string {
	return prefixedName(cr.Name, "vtselectint")
}

// GetVTInsertLBName returns headless proxy service name for insert component
func (cr *VTCluster) GetVTInsertLBName() string {
	return prefixedName(cr.Name, "vtinsertint")
}

// GetVTInsertName returns insert component name
func (cr *VTCluster) GetVTInsertName() string {
	return prefixedName(cr.Name, "vtinsert")
}

// GetLVSelectName returns select component name
func (cr *VTCluster) GetVTSelectName() string {
	return prefixedName(cr.Name, "vtselect")
}

// GetVTStorageName returns select component name
func (cr *VTCluster) GetVTStorageName() string {
	return prefixedName(cr.Name, "vtstorage")
}

//nolint:dupl,lll
func (cr *VTCluster) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.Select != nil {
		vms := cr.Spec.Select
		if vms.ServiceSpec != nil && vms.ServiceSpec.Name == cr.GetVTSelectName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVTSelectName())
		}
		if vms.HPA != nil {
			if err := vms.HPA.Validate(); err != nil {
				return err
			}
		}
	}
	if cr.Spec.Insert != nil {
		vti := cr.Spec.Insert
		if vti.ServiceSpec != nil && vti.ServiceSpec.Name == cr.GetVTInsertName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVTInsertName())
		}
		if vti.HPA != nil {
			if err := vti.HPA.Validate(); err != nil {
				return err
			}
		}
	}
	if cr.Spec.Storage != nil {
		vts := cr.Spec.Storage
		if vts.ServiceSpec != nil && vts.ServiceSpec.Name == cr.GetVTStorageName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVTStorageName())
		}
	}
	if cr.Spec.RequestsLoadBalancer.Enabled {
		rlb := cr.Spec.RequestsLoadBalancer.Spec
		if rlb.AdditionalServiceSpec != nil && rlb.AdditionalServiceSpec.Name == cr.GetVMAuthLBName() {
			return fmt.Errorf(".serviceSpec.Name cannot be equal to prefixed name=%q", cr.GetVMAuthLBName())
		}
	}

	return nil
}

// VTSelectSelectorLabels returns selector labels for select cluster component
func (cr *VTCluster) VTSelectSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vtselect",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VTSelectPodLabels returns pod labels for select cluster component
func (cr *VTCluster) VTSelectPodLabels() map[string]string {
	selectorLabels := cr.VTSelectSelectorLabels()
	if cr.Spec.Select == nil || cr.Spec.Select.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.Select.PodMetadata.Labels, selectorLabels)
}

// VTInsertSelectorLabels returns selector labels for insert cluster component
func (cr *VTCluster) VTInsertSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vtinsert",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VTInsertPodLabels returns pod labels for vtinsert cluster component
func (cr *VTCluster) VTInsertPodLabels() map[string]string {
	selectorLabels := cr.VTInsertSelectorLabels()
	if cr.Spec.Insert == nil || cr.Spec.Insert.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.Insert.PodMetadata.Labels, selectorLabels)
}

// VTStorageSelectorLabels  returns pod labels for vtstorage cluster component
func (cr *VTCluster) VTStorageSelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vtstorage",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// VTStoragePodLabels returns pod labels for the vmstorage cluster component
func (cr *VTCluster) VTStoragePodLabels() map[string]string {
	selectorLabels := cr.VTStorageSelectorLabels()
	if cr.Spec.Storage == nil || cr.Spec.Storage.PodMetadata == nil {
		return selectorLabels
	}
	return labels.Merge(cr.Spec.Storage.PodMetadata.Labels, selectorLabels)
}

// AvailableStorageNodeIDs returns ids of the storage nodes for the provided component
func (cr *VTCluster) AvailableStorageNodeIDs(requestsType string) []int32 {
	var result []int32
	if cr.Spec.Storage == nil || cr.Spec.Storage.ReplicaCount == nil {
		return result
	}
	maintenanceNodes := make(map[int32]struct{})
	switch requestsType {
	case "select":
		for _, i := range cr.Spec.Storage.MaintenanceSelectNodeIDs {
			maintenanceNodes[i] = struct{}{}
		}
	case "insert":
		for _, i := range cr.Spec.Storage.MaintenanceInsertNodeIDs {
			maintenanceNodes[i] = struct{}{}
		}
	default:
		panic("BUG unsupported requestsType: " + requestsType)
	}
	for i := int32(0); i < *cr.Spec.Storage.ReplicaCount; i++ {
		if _, ok := maintenanceNodes[i]; ok {
			continue
		}
		result = append(result, i)
	}
	return result
}

var globalVTClusterLabels = map[string]string{"app.kubernetes.io/part-of": "vtcluster"}

// FinalLabels adds cluster labels to the base labels and filters by prefix if needed
func (cr *VTCluster) FinalLabels(selectorLabels map[string]string) map[string]string {
	baseLabels := labels.Merge(globalVTClusterLabels, selectorLabels)
	if cr.Spec.ManagedMetadata == nil {
		// fast path
		return baseLabels
	}
	return labels.Merge(cr.Spec.ManagedMetadata.Labels, baseLabels)
}

// VTSelectPodAnnotations returns pod annotations for select cluster component
func (cr *VTCluster) VTSelectPodAnnotations() map[string]string {
	if cr.Spec.Select == nil || cr.Spec.Select.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.Select.PodMetadata.Annotations
}

// VTInsertPodAnnotations returns pod annotations for insert cluster component
func (cr *VTCluster) VTInsertPodAnnotations() map[string]string {
	if cr.Spec.Insert == nil || cr.Spec.Insert.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.Insert.PodMetadata.Annotations
}

// VTStoragePodAnnotations returns pod annotations for storage cluster component
func (cr *VTCluster) VTStoragePodAnnotations() map[string]string {
	if cr.Spec.Storage == nil || cr.Spec.Storage.PodMetadata == nil {
		return make(map[string]string)
	}
	return cr.Spec.Storage.PodMetadata.Annotations
}

// FinalAnnotations returns global annotations to be applied by objects generate for vtcluster
func (cr *VTCluster) FinalAnnotations() map[string]string {
	if cr.Spec.ManagedMetadata == nil {
		return map[string]string{}
	}
	return cr.Spec.ManagedMetadata.Annotations
}

// AnnotationsFiltered implements finalize.crdObject interface
func (cr *VTCluster) AnnotationsFiltered() map[string]string {
	return cr.FinalAnnotations()
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VTCluster) LastAppliedSpecAsPatch() (client.Patch, error) {
	return vmv1beta1.LastAppliedChangesAsPatch(cr.ObjectMeta, cr.Spec)
}

// HasSpecChanges compares cluster spec with last applied cluster spec stored in annotation
func (cr *VTCluster) HasSpecChanges() (bool, error) {
	return vmv1beta1.HasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VTCluster) Paused() bool {
	return cr.Spec.Paused
}

// GetServiceAccountName returns service account name for all vtcluster components
func (cr *VTCluster) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr *VTCluster) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// PrefixedName format name of the component with hard-coded prefix
func (cr *VTCluster) PrefixedName() string {
	return fmt.Sprintf("vtcluster-%s", cr.Name)
}

// SelectorLabels defines labels for objects generated used by all cluster components
func (cr *VTCluster) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vtcluster",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// AsURL implements stub for interface.
func (cr *VTCluster) AsURL() string {
	return "unknown"
}

// SelectURL returns url to access VTSelect component
func (cr *VTCluster) SelectURL() string {
	if cr.Spec.Select == nil {
		return ""
	}
	port := cr.Spec.Select.Port
	if port == "" {
		port = "10471"
	}
	if cr.Spec.Select.ServiceSpec != nil && cr.Spec.Select.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.Select.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(cr.Spec.Select.ExtraArgs), cr.GetVTSelectName(), cr.Namespace, port)
}

// InsertURL returns url to access VTInsert component
func (cr *VTCluster) InsertURL() string {
	if cr.Spec.Insert == nil {
		return ""
	}
	port := cr.Spec.Insert.Port
	if port == "" {
		port = "10481"
	}
	if cr.Spec.Insert.ServiceSpec != nil && cr.Spec.Insert.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.Insert.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(cr.Spec.Insert.ExtraArgs), cr.GetVTInsertName(), cr.Namespace, port)
}

// StorageURL returns url to access VTStorage component
func (cr *VTCluster) StorageURL() string {
	if cr.Spec.Storage == nil {
		return ""
	}
	port := cr.Spec.Storage.Port
	if port == "" {
		port = "10491"
	}
	if cr.Spec.Storage.ServiceSpec != nil && cr.Spec.Storage.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.Storage.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(cr.Spec.Storage.ExtraArgs), cr.GetVTStorageName(), cr.Namespace, port)
}

// +kubebuilder:object:root=true

// VTClusterList contains a list of VTCluster
type VTClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VTCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VTCluster{}, &VTClusterList{})
}
