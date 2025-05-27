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

package v1beta1

import (
	"encoding/json"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VLogsSpec defines the desired state of VLogs
// +k8s:openapi-gen=true
// VLogs is deprecated, migrate to the VLSingle
// +kubebuilder:validation:Schemaless
// +kubebuilder:pruning:PreserveUnknownFields
type VLogsSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`

	// PodMetadata configures Labels and Annotations which are propagated to the VLogs pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *ManagedObjectsMetadata `json:"managedMetadata,omitempty"`

	CommonDefaultableParams           `json:",inline,omitempty"`
	CommonApplicationDeploymentParams `json:",inline,omitempty"`

	// LogLevel for VictoriaLogs to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VLogs to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// StorageDataPath disables spec.storage option and overrides arg for victoria-logs binary --storageDataPath,
	// its users responsibility to mount proper device into given path.
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage is the definition of how storage will be used by the VLogs
	// by default it`s empty dir
	// +optional
	Storage *v1.PersistentVolumeClaimSpec `json:"storage,omitempty"`
	// StorageMeta defines annotations and labels attached to PVC for given vlogs CR
	// +optional
	StorageMetadata EmbeddedObjectMetadata `json:"storageMetadata,omitempty"`
	// RemovePvcAfterDelete - if true, controller adds ownership to pvc
	// and after VLogs object deletion - pvc will be garbage collected
	// by controller manager
	// +optional
	RemovePvcAfterDelete bool `json:"removePvcAfterDelete,omitempty"`
	// RetentionPeriod for the stored logs
	RetentionPeriod string `json:"retentionPeriod"`
	// FutureRetention for the stored logs
	// Log entries with timestamps bigger than now+futureRetention are rejected during data ingestion; see https://docs.victoriametrics.com/victorialogs/#retention
	FutureRetention string `json:"futureRetention,omitempty"`
	// LogNewStreams Whether to log creation of new streams; this can be useful for debugging of high cardinality issues with log streams; see https://docs.victoriametrics.com/victorialogs/keyconcepts/#stream-fields
	LogNewStreams bool `json:"logNewStreams,omitempty"`
	// Whether to log all the ingested log entries; this can be useful for debugging of data ingestion; see https://docs.victoriametrics.com/victorialogs/data-ingestion/
	LogIngestedRows bool `json:"logIngestedRows,omitempty"`
	// ServiceSpec that will be added to vlogs service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vlogs VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// LivenessProbe that will be added to VLogs pod
	*EmbeddedProbes `json:",inline"`

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
}

// VLogsStatus defines the observed state of VLogs
type VLogsStatus struct {
	StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VLogsStatus) GetStatusMetadata() *StatusMetadata {
	return &cr.StatusMetadata
}

// VLogs is fast, cost-effective and scalable logs database.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VLogs App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vlogs,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.status",description="Current status of logs instance update process"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// VLogs is the Schema for the vlogs API
type VLogs struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VLogsSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VLogsSpec `json:"-" yaml:"-"`

	Status VLogsStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VLogsList contains a list of VLogs
type VLogsList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VLogs `json:"items"`
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLogs) GetStatus() *VLogsStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VLogs) DefaultStatusFields(vs *VLogsStatus) {
}

func (r *VLogs) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if r.Spec.PodMetadata != nil {
		for annotation, value := range r.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

// AsOwner returns owner references with current object as owner
func (r *VLogs) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         r.APIVersion,
			Kind:               r.Kind,
			Name:               r.Name,
			UID:                r.UID,
			Controller:         ptr.To(true),
			BlockOwnerDeletion: ptr.To(true),
		},
	}
}

// SetLastSpec implements objectWithLastAppliedState interface
func (cr *VLogs) SetLastSpec(prevSpec VLogsSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLogs) UnmarshalJSON(src []byte) error {
	type pcr VLogs
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := ParseLastAppliedStateTo(cr); err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLogsSpec) UnmarshalJSON(src []byte) error {
	type pcr VLogsSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vlogs spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

func (cr *VLogs) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VLogs) ProbePath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VLogs) ProbeScheme() string {
	return strings.ToUpper(HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VLogs) ProbePort() string {
	return cr.Spec.Port
}

func (cr *VLogs) ProbeNeedLiveness() bool {
	return false
}

func (cr *VLogs) AnnotationsFiltered() map[string]string {
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	dst := filterMapKeysByPrefixes(cr.ObjectMeta.Annotations, annotationFilterPrefixes)
	if cr.Spec.ManagedMetadata != nil {
		if dst == nil {
			dst = make(map[string]string)
		}
		for k, v := range cr.Spec.ManagedMetadata.Annotations {
			dst[k] = v
		}
	}
	return dst
}

func (cr *VLogs) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vlogs",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr *VLogs) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr *VLogs) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil && cr.Spec.ManagedMetadata == nil {
		return selectorLabels
	}
	var result map[string]string
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	if cr.ObjectMeta.Labels != nil {
		result = filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	}
	if cr.Spec.ManagedMetadata != nil {
		result = labels.Merge(result, cr.Spec.ManagedMetadata.Labels)
	}
	return labels.Merge(result, selectorLabels)
}

func (cr *VLogs) PrefixedName() string {
	return fmt.Sprintf("vlogs-%s", cr.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VLogs) GetMetricPath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// Validate checks if spec is correct
func (cr *VLogs) Validate() error {
	if MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.Name == cr.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", cr.PrefixedName())
	}
	return nil
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VLogs) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VLogs) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr *VLogs) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr *VLogs) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr *VLogs) AsURL() string {
	port := cr.Spec.Port
	if port == "" {
		port = "8429"
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.UseAsDefault {
		for _, svcPort := range cr.Spec.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
				break
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", HTTPProtoFromFlags(cr.Spec.ExtraArgs), cr.PrefixedName(), cr.Namespace, port)
}

// LastAppliedSpecAsPatch return last applied vlogs spec as patch annotation
func (cr *VLogs) LastAppliedSpecAsPatch() (client.Patch, error) {
	return LastAppliedChangesAsPatch(cr.ObjectMeta, cr.Spec)
}

// HasSpecChanges compares vlogs spec with last applied vlogs spec stored in annotation
func (cr *VLogs) HasSpecChanges() (bool, error) {
	return HasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VLogs) Paused() bool {
	return cr.Spec.Paused
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VLogs) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VLogs{}, &VLogsList{})
}
