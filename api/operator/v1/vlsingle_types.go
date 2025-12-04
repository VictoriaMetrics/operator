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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VLSingleSpec defines the desired state of VLSingle
// +k8s:openapi-gen=true
type VLSingleSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`

	// PodMetadata configures Labels and Annotations which are propagated to the VLSingle pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`

	vmv1beta1.CommonDefaultableParams           `json:",inline,omitempty"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline,omitempty"`

	// LogLevel for VictoriaLogs to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VLSingle to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// StorageDataPath disables spec.storage option and overrides arg for victoria-logs binary --storageDataPath,
	// its users responsibility to mount proper device into given path.
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage is the definition of how storage will be used by the VLSingle
	// by default it`s empty dir
	// +optional
	Storage *corev1.PersistentVolumeClaimSpec `json:"storage,omitempty"`
	// StorageMeta defines annotations and labels attached to PVC for given vlsingle CR
	// +optional
	StorageMetadata vmv1beta1.EmbeddedObjectMetadata `json:"storageMetadata,omitempty"`
	// RetentionPeriod for the stored logs
	// https://docs.victoriametrics.com/victorialogs/#retention
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|w|y)?$"
	RetentionPeriod string `json:"retentionPeriod,omitempty"`
	// RetentionMaxDiskSpaceUsageBytes for the stored logs
	// VictoriaLogs keeps at least two last days of data in order to guarantee that the logs for the last day can be returned in queries.
	// This means that the total disk space usage may exceed the -retention.maxDiskSpaceUsageBytes,
	// if the size of the last two days of data exceeds the -retention.maxDiskSpaceUsageBytes.
	// https://docs.victoriametrics.com/victorialogs/#retention-by-disk-space-usage
	// +optional
	RetentionMaxDiskSpaceUsageBytes vmv1beta1.BytesString `json:"retentionMaxDiskSpaceUsageBytes,omitempty"`
	// FutureRetention for the stored logs
	// Log entries with timestamps bigger than now+futureRetention are rejected during data ingestion; see https://docs.victoriametrics.com/victorialogs/#retention
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|y)?$"
	FutureRetention string `json:"futureRetention,omitempty"`
	// LogNewStreams Whether to log creation of new streams; this can be useful for debugging of high cardinality issues with log streams; see https://docs.victoriametrics.com/victorialogs/keyconcepts/#stream-fields
	LogNewStreams bool `json:"logNewStreams,omitempty"`
	// Whether to log all the ingested log entries; this can be useful for debugging of data ingestion; see https://docs.victoriametrics.com/victorialogs/data-ingestion/
	LogIngestedRows bool `json:"logIngestedRows,omitempty"`
	// ServiceSpec that will be added to vlsingle service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vlsingle VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// LivenessProbe that will be added to VLSingle pod
	*vmv1beta1.EmbeddedProbes `json:",inline"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// SyslogSpec defines syslog listener configuration
	// +optional
	SyslogSpec *SyslogServerSpec `json:"syslogSpec,omitempty"`
}

// VLSingleStatus defines the observed state of VLSingle
type VLSingleStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VLSingleStatus) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.StatusMetadata
}

// VLSingle is fast, cost-effective and scalable logs database.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VLSingle App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vlsingles,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.status",description="Current status of logs instance update process"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// VLSingle is the Schema for the API
type VLSingle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VLSingleSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VLSingleSpec `json:"-" yaml:"-"`

	Status VLSingleStatus `json:"status,omitempty"`
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLSingle) GetStatus() *VLSingleStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLSingle) DefaultStatusFields(vs *VLSingleStatus) {
}

// +kubebuilder:object:root=true

// VLSingleList contains a list of VLSingle
type VLSingleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VLSingle `json:"items"`
}

func (r *VLSingle) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if r.Spec.PodMetadata != nil {
		for annotation, value := range r.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

// AsOwner returns owner references with current object as owner
func (r *VLSingle) AsOwner() metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion:         r.APIVersion,
		Kind:               r.Kind,
		Name:               r.Name,
		UID:                r.UID,
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}
}

// SetLastSpec implements objectWithLastAppliedState interface
func (cr *VLSingle) SetLastSpec(prevSpec VLSingleSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLSingle) UnmarshalJSON(src []byte) error {
	type pcr VLSingle
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := vmv1beta1.ParseLastAppliedStateTo(cr); err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLSingleSpec) UnmarshalJSON(src []byte) error {
	type pcr VLSingleSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vlsingle spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

func (cr *VLSingle) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VLSingle) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VLSingle) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VLSingle) ProbePort() string {
	return cr.Spec.Port
}

func (cr *VLSingle) ProbeNeedLiveness() bool {
	return false
}

// FinalAnnotations returns annotations to be applied for created objects
func (cr *VLSingle) FinalAnnotations() map[string]string {
	var v map[string]string
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Annotations, v)
	}
	return v
}

// SelectorLabels returns unique labels for object
func (cr *VLSingle) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vlsingle",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// PodLabels returns labels attached to the podMetadata
func (cr *VLSingle) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

// FinalLabels returns combination of selector and managed labels
func (cr *VLSingle) FinalLabels() map[string]string {
	v := cr.SelectorLabels()
	if cr.Spec.ManagedMetadata != nil {
		v = labels.Merge(cr.Spec.ManagedMetadata.Labels, v)
	}
	return v
}

func (cr *VLSingle) PrefixedName() string {
	return fmt.Sprintf("vlsingle-%s", cr.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VLSingle) GetMetricPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// Validate checks if spec is correct
func (cr *VLSingle) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.Name == cr.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", cr.PrefixedName())
	}
	return nil
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VLSingle) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VLSingle) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr *VLSingle) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if serviceAccount belongs to the CR
func (cr *VLSingle) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr *VLSingle) AsURL() string {
	port := cr.Spec.Port
	if port == "" {
		port = "9428"
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
				break
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", vmv1beta1.HTTPProtoFromFlags(cr.Spec.ExtraArgs), cr.PrefixedName(), cr.Namespace, port)
}

// LastAppliedSpecAsPatch return last applied vlsingle spec as patch annotation
func (cr *VLSingle) LastAppliedSpecAsPatch() (client.Patch, error) {
	return vmv1beta1.LastAppliedChangesAsPatch(cr.Spec)
}

// HasSpecChanges compares vlsingle spec with last applied vlsingle spec stored in annotation
func (cr *VLSingle) HasSpecChanges() (bool, error) {
	return vmv1beta1.HasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VLSingle) Paused() bool {
	return cr.Spec.Paused
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VLSingle) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VLSingle{}, &VLSingleList{})
}
