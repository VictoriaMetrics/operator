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

// VTSingleSpec defines the desired state of VTSingle
// +k8s:openapi-gen=true
type VTSingleSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`

	// PodMetadata configures Labels and Annotations which are propagated to the VTSingle pods.
	// +optional
	PodMetadata *vmv1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *vmv1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`

	vmv1beta1.CommonDefaultableParams           `json:",inline,omitempty"`
	vmv1beta1.CommonApplicationDeploymentParams `json:",inline,omitempty"`

	// LogLevel for VictoriaTraces to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VTSingle to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// StorageDataPath disables spec.storage option and overrides arg for victoria-traces binary --storageDataPath,
	// its users responsibility to mount proper device into given path.
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage is the definition of how storage will be used by the VTSingle
	// by default it`s empty dir
	// +optional
	Storage *corev1.PersistentVolumeClaimSpec `json:"storage,omitempty"`
	// StorageMeta defines annotations and labels attached to PVC for given vtsingle CR
	// +optional
	StorageMetadata vmv1beta1.EmbeddedObjectMetadata `json:"storageMetadata,omitempty"`
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
	// Log entries with timestamps bigger than now+futureRetention are rejected during data ingestion;
	// see https://docs.victoriametrics.com/victoriatraces/#configure-and-run-victoriatraces
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|y)?$"
	FutureRetention string `json:"futureRetention,omitempty"`
	// LogNewStreams Whether to log creation of new streams; this can be useful for debugging of high cardinality issues with log streams;
	// see https://docs.victoriametrics.com/victoriatraces/#configure-and-run-victoriatraces
	LogNewStreams bool `json:"logNewStreams,omitempty"`
	// Whether to log all the ingested log entries; this can be useful for debugging of data ingestion;
	// see https://docs.victoriametrics.com/victoriatraces/#configure-and-run-victoriatraces
	LogIngestedRows bool `json:"logIngestedRows,omitempty"`
	// ServiceSpec that will be added to vtsingle service spec
	// +optional
	ServiceSpec *vmv1beta1.AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vtsingle VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *vmv1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// LivenessProbe that will be added to VTSingle pod
	*vmv1beta1.EmbeddedProbes `json:",inline"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

// VTSingleStatus defines the observed state of VTSingle
type VTSingleStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VTSingleStatus) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.StatusMetadata
}

// VTSingle is fast, cost-effective and scalable traces database.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VTSingle App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vtsingles,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.status",description="Current status of traces instance update process"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// VTSingle is the Schema for the API
type VTSingle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VTSingleSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VTSingleSpec `json:"-" yaml:"-"`

	Status VTSingleStatus `json:"status,omitempty"`
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VTSingle) GetStatus() *VTSingleStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VTSingle) DefaultStatusFields(vs *VTSingleStatus) {
}

// +kubebuilder:object:root=true

// VTSingleList contains a list of VTSingle
type VTSingleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VTSingle `json:"items"`
}

func (r *VTSingle) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if r.Spec.PodMetadata != nil {
		for annotation, value := range r.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

// AsOwner returns owner references with current object as owner
func (r *VTSingle) AsOwner() metav1.OwnerReference {
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
func (cr *VTSingle) SetLastSpec(prevSpec VTSingleSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VTSingle) UnmarshalJSON(src []byte) error {
	type pcr VTSingle
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := vmv1beta1.ParseLastAppliedStateTo(cr); err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VTSingleSpec) UnmarshalJSON(src []byte) error {
	type pcr VTSingleSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vtsingle spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// Probe implements build.probeCRD interface
func (cr *VTSingle) Probe() *vmv1beta1.EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

// ProbePath implements build.probeCRD interface
func (cr *VTSingle) ProbePath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// ProbeScheme implements build.probeCRD interface
func (cr *VTSingle) ProbeScheme() string {
	return strings.ToUpper(vmv1beta1.HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

// ProbePort implements build.probeCRD interface
func (cr *VTSingle) ProbePort() string {
	return cr.Spec.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (cr *VTSingle) ProbeNeedLiveness() bool {
	return false
}

// AnnotationsFiltered returns global annotations to be applied for created objects
func (cr *VTSingle) AnnotationsFiltered() map[string]string {
	if cr.Spec.ManagedMetadata == nil {
		return nil
	}
	dst := make(map[string]string, len(cr.Spec.ManagedMetadata.Annotations))
	for k, v := range cr.Spec.ManagedMetadata.Annotations {
		dst[k] = v
	}
	return dst
}

// SelectorLabels returns unique labels for object
func (cr *VTSingle) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vtsingle",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

// PodLabels returns labels attached to the podMetadata
func (cr *VTSingle) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

// AllLabels returns combination of selector and managed labels
func (cr *VTSingle) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.Spec.ManagedMetadata == nil {
		return selectorLabels
	}

	return labels.Merge(selectorLabels, cr.Spec.ManagedMetadata.Labels)
}

// PrefixedName format name of the component with hard-coded prefix
func (cr *VTSingle) PrefixedName() string {
	return fmt.Sprintf("vtsingle-%s", cr.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VTSingle) GetMetricPath() string {
	return vmv1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// Validate checks if spec is correct
func (cr *VTSingle) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.Name == cr.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", cr.PrefixedName())
	}
	return nil
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VTSingle) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VTSingle) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

// GetServiceAccountName returns service account name for components
func (cr *VTSingle) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

// IsOwnsServiceAccount checks if ServiceAccountName is set explicitly
func (cr *VTSingle) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// AsURL returns URL for components access
func (cr *VTSingle) AsURL() string {
	port := cr.Spec.Port
	if port == "" {
		port = "10428"
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

// LastAppliedSpecAsPatch return last applied vtsingle spec as patch annotation
func (cr *VTSingle) LastAppliedSpecAsPatch() (client.Patch, error) {
	return vmv1beta1.LastAppliedChangesAsPatch(cr.Spec)
}

// HasSpecChanges compares vtsingle spec with last applied vtsingle spec stored in annotation
func (cr *VTSingle) HasSpecChanges() (bool, error) {
	return vmv1beta1.HasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VTSingle) Paused() bool {
	return cr.Spec.Paused
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VTSingle) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VTSingle{}, &VTSingleList{})
}
