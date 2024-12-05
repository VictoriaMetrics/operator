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
	"context"
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
	// deprecated
	Replicas int32 `json:"replicas,omitempty"`
	// deprecated
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`
	// deprecated
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`
	// deprecated
	UnavailableReplicas int32 `json:"unavailableReplicas,omitempty"`
	StatusMetadata      `json:",inline"`
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

func (r *VLogs) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if r.Spec.PodMetadata != nil {
		for annotation, value := range r.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

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

func (cr *VLogs) setLastSpec(prevSpec VLogsSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VLogs) UnmarshalJSON(src []byte) error {
	type pcr VLogs
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := parseLastAppliedState(cr); err != nil {
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

func (r *VLogs) Probe() *EmbeddedProbes {
	return r.Spec.EmbeddedProbes
}

func (r *VLogs) ProbePath() string {
	return buildPathWithPrefixFlag(r.Spec.ExtraArgs, healthPath)
}

func (r *VLogs) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(r.Spec.ExtraArgs))
}

func (r *VLogs) ProbePort() string {
	return r.Spec.Port
}

func (r *VLogs) ProbeNeedLiveness() bool {
	return false
}

func (r *VLogs) AnnotationsFiltered() map[string]string {
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	dst := filterMapKeysByPrefixes(r.ObjectMeta.Annotations, annotationFilterPrefixes)
	if r.Spec.ManagedMetadata != nil {
		if dst == nil {
			dst = make(map[string]string)
		}
		for k, v := range r.Spec.ManagedMetadata.Annotations {
			dst[k] = v
		}
	}
	return dst
}

func (r *VLogs) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vlogs",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (r *VLogs) PodLabels() map[string]string {
	lbls := r.SelectorLabels()
	if r.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(r.Spec.PodMetadata.Labels, lbls)
}

func (r *VLogs) AllLabels() map[string]string {
	selectorLabels := r.SelectorLabels()
	// fast path
	if r.ObjectMeta.Labels == nil && r.Spec.ManagedMetadata == nil {
		return selectorLabels
	}
	var result map[string]string
	// TODO: @f41gh7 deprecated at will be removed at v0.52.0 release
	if r.ObjectMeta.Labels != nil {
		result = filterMapKeysByPrefixes(r.ObjectMeta.Labels, labelFilterPrefixes)
	}
	if r.Spec.ManagedMetadata != nil {
		result = labels.Merge(result, r.Spec.ManagedMetadata.Labels)
	}
	return labels.Merge(result, selectorLabels)
}

func (r VLogs) PrefixedName() string {
	return fmt.Sprintf("vlogs-%s", r.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (r VLogs) GetMetricPath() string {
	return buildPathWithPrefixFlag(r.Spec.ExtraArgs, metricPath)
}

// GetExtraArgs returns additionally configured command-line arguments
func (r VLogs) GetExtraArgs() map[string]string {
	return r.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (r VLogs) GetServiceScrape() *VMServiceScrapeSpec {
	return r.Spec.ServiceScrapeSpec
}

func (r VLogs) GetServiceAccountName() string {
	if r.Spec.ServiceAccountName == "" {
		return r.PrefixedName()
	}
	return r.Spec.ServiceAccountName
}

func (r VLogs) IsOwnsServiceAccount() bool {
	return r.Spec.ServiceAccountName == ""
}

func (r VLogs) GetNSName() string {
	return r.GetNamespace()
}

func (r *VLogs) AsURL() string {
	port := r.Spec.Port
	if port == "" {
		port = "8429"
	}
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.UseAsDefault {
		for _, svcPort := range r.Spec.ServiceSpec.Spec.Ports {
			if svcPort.Name == "http" {
				port = fmt.Sprintf("%d", svcPort.Port)
				break
			}
		}
	}
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(r.Spec.ExtraArgs), r.PrefixedName(), r.Namespace, port)
}

// LastAppliedSpecAsPatch return last applied vlogs spec as patch annotation
func (r *VLogs) LastAppliedSpecAsPatch() (client.Patch, error) {
	return lastAppliedChangesAsPatch(r.ObjectMeta, r.Spec)
}

// HasSpecChanges compares vlogs spec with last applied vlogs spec stored in annotation
func (r *VLogs) HasSpecChanges() (bool, error) {
	return hasStateChanges(r.ObjectMeta, r.Spec)
}

func (r *VLogs) Paused() bool {
	return r.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (r *VLogs) SetUpdateStatusTo(ctx context.Context, c client.Client, status UpdateStatus, maybeErr error) error {
	return updateObjectStatus(ctx, c, &patchStatusOpts[*VLogs, *VLogsStatus]{
		actualStatus: status,
		cr:           r,
		crStatus:     &r.Status,
		maybeErr:     maybeErr,
	})
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (r *VLogs) GetAdditionalService() *AdditionalServiceSpec {
	return r.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VLogs{}, &VLogsList{})
}
