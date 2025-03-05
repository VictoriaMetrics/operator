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

// VMSingleSpec defines the desired state of VMSingle
// +k8s:openapi-gen=true
type VMSingleSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMSingle pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
	// LogLevel for victoria metrics single to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// LogFormat for VMSingle to be configured with.
	// +optional
	// +kubebuilder:validation:Enum=default;json
	LogFormat string `json:"logFormat,omitempty"`
	// StorageDataPath disables spec.storage option and overrides arg for victoria-metrics binary --storageDataPath,
	// its users responsibility to mount proper device into given path.
	// It requires to provide spec.volumes and spec.volumeMounts with at least 1 value
	// +optional
	StorageDataPath string `json:"storageDataPath,omitempty"`
	// Storage is the definition of how storage will be used by the VMSingle
	// by default it`s empty dir
	// this option is ignored if storageDataPath is set
	// +optional
	Storage *v1.PersistentVolumeClaimSpec `json:"storage,omitempty"`

	// StorageMeta defines annotations and labels attached to PVC for given vmsingle CR
	// +optional
	StorageMetadata EmbeddedObjectMetadata `json:"storageMetadata,omitempty"`

	// InsertPorts - additional listen ports for data ingestion.
	InsertPorts *InsertPorts `json:"insertPorts,omitempty"`
	// RemovePvcAfterDelete - if true, controller adds ownership to pvc
	// and after VMSingle object deletion - pvc will be garbage collected
	// by controller manager
	// +optional
	RemovePvcAfterDelete bool `json:"removePvcAfterDelete,omitempty"`

	// RetentionPeriod for the stored metrics
	// Note VictoriaMetrics has data/ and indexdb/ folders
	// metrics from data/ removed eventually as soon as partition leaves retention period
	// reverse index data at indexdb rotates once at the half of configured [retention period](https://docs.victoriametrics.com/Single-server-VictoriaMetrics/#retention)
	RetentionPeriod string `json:"retentionPeriod"`
	// VMBackup configuration for backup
	// +optional
	VMBackup *VMBackup `json:"vmBackup,omitempty"`
	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/enterprise)
	// +optional
	License *License `json:"license,omitempty"`
	// ServiceSpec that will be added to vmsingle service spec
	// +optional
	ServiceSpec *AdditionalServiceSpec `json:"serviceSpec,omitempty"`
	// ServiceScrapeSpec that will be added to vmsingle VMServiceScrape spec
	// +optional
	ServiceScrapeSpec *VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// LivenessProbe that will be added to VMSingle pod
	*EmbeddedProbes `json:",inline"`
	// StreamAggrConfig defines stream aggregation configuration for VMSingle
	StreamAggrConfig *StreamAggrConfig `json:"streamAggrConfig,omitempty"`

	*ServiceAccount `json:",inline,omitempty"`

	CommonDefaultableParams           `json:",inline"`
	CommonApplicationDeploymentParams `json:",inline"`
}

// HasAnyStreamAggrRule checks if vmsingle has any defined aggregation rules
func (r *VMSingle) HasAnyStreamAggrRule() bool {
	return r.Spec.StreamAggrConfig.HasAnyRule()
}

func (r *VMSingle) setLastSpec(prevSpec VMSingleSpec) {
	r.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMSingle) UnmarshalJSON(src []byte) error {
	type pr VMSingle
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		return err
	}
	if err := parseLastAppliedState(r); err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (r *VMSingleSpec) UnmarshalJSON(src []byte) error {
	type pr VMSingleSpec
	if err := json.Unmarshal(src, (*pr)(r)); err != nil {
		r.ParsingError = fmt.Sprintf("cannot parse vmsingle spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VMSingleStatus defines the observed state of VMSingle
// +k8s:openapi-gen=true
type VMSingleStatus struct {
	StatusMetadata `json:",inline"`
	// LegacyStatus is deprecated and will be removed at v0.52.0 version
	LegacyStatus UpdateStatus `json:"singleStatus,omitempty"`
}

// VMSingle  is fast, cost-effective and scalable time-series database.
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMSingle App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmsingles,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of single node update process"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMSingle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VMSingleSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMSingleSpec `json:"-" yaml:"-"`

	Status VMSingleStatus `json:"status,omitempty"`
}

func (r *VMSingle) Probe() *EmbeddedProbes {
	return r.Spec.EmbeddedProbes
}

func (r *VMSingle) ProbePath() string {
	return buildPathWithPrefixFlag(r.Spec.ExtraArgs, healthPath)
}

func (r *VMSingle) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(r.Spec.ExtraArgs))
}

func (r *VMSingle) ProbePort() string {
	return r.Spec.Port
}

func (r *VMSingle) ProbeNeedLiveness() bool {
	return false
}

// VMSingleList contains a list of VMSingle
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMSingleList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMSingle `json:"items"`
}

// AsOwner returns owner references with current object as owner
func (r *VMSingle) AsOwner() []metav1.OwnerReference {
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

func (r *VMSingle) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if r.Spec.PodMetadata != nil {
		for annotation, value := range r.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (r *VMSingle) AnnotationsFiltered() map[string]string {
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

func (r *VMSingle) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmsingle",
		"app.kubernetes.io/instance":  r.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (r *VMSingle) PodLabels() map[string]string {
	lbls := r.SelectorLabels()
	if r.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(r.Spec.PodMetadata.Labels, lbls)
}

func (r *VMSingle) AllLabels() map[string]string {
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

func (r *VMSingle) PrefixedName() string {
	return fmt.Sprintf("vmsingle-%s", r.Name)
}

func (r *VMSingle) StreamAggrConfigName() string {
	return fmt.Sprintf("stream-aggr-vmsingle-%s", r.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (r *VMSingle) GetMetricPath() string {
	return buildPathWithPrefixFlag(r.Spec.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (r *VMSingle) GetExtraArgs() map[string]string {
	return r.Spec.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (r *VMSingle) GetServiceScrape() *VMServiceScrapeSpec {
	return r.Spec.ServiceScrapeSpec
}

func (r *VMSingle) GetServiceAccount() *ServiceAccount {
	sa := r.Spec.ServiceAccount
	if sa == nil {
		sa = &ServiceAccount{
			Name:           r.PrefixedName(),
			AutomountToken: true,
		}
	}
	return sa
}

func (r *VMSingle) IsOwnsServiceAccount() bool {
	if r.Spec.ServiceAccount != nil && r.Spec.ServiceAccount.Name != "" {
		return r.Spec.ServiceAccount.Name == ""
	}
	return false
}

// GetNSName implements build.builderOpts interface
func (r *VMSingle) GetNSName() string {
	return r.GetNamespace()
}

func (r *VMSingle) AsURL() string {
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

// AsCRDOwner implements interface
func (r *VMSingle) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Single)
}

// LastAppliedSpecAsPatch return last applied single spec as patch annotation
func (r *VMSingle) LastAppliedSpecAsPatch() (client.Patch, error) {
	return lastAppliedChangesAsPatch(r.ObjectMeta, r.Spec)
}

// HasSpecChanges compares single spec with last applied single spec stored in annotation
func (r *VMSingle) HasSpecChanges() (bool, error) {
	return hasStateChanges(r.ObjectMeta, r.Spec)
}

func (r *VMSingle) Paused() bool {
	return r.Spec.Paused
}

func (r *VMSingle) Validate() error {
	if mustSkipValidation(r) {
		return nil
	}
	if r.Spec.ServiceSpec != nil && r.Spec.ServiceSpec.Name == r.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", r.PrefixedName())
	}

	if r.Spec.VMBackup != nil {
		if err := r.Spec.VMBackup.validate(r.Spec.License); err != nil {
			return err
		}
	}
	if r.Spec.StorageDataPath != "" {
		if len(r.Spec.Volumes) == 0 {
			return fmt.Errorf("spec.volumes must have at least 1 value for spec.storageDataPath=%q", r.Spec.StorageDataPath)
		}
		var storageVolumeFound bool
		for _, volume := range r.Spec.Volumes {
			if volume.Name == "data" {
				storageVolumeFound = true
				break
			}
		}
		if r.Spec.VMBackup != nil {
			if !storageVolumeFound {
				return fmt.Errorf("spec.volumes must have at least 1 value with `name: data` for spec.storageDataPath=%q."+
					" It's required by operator to correctly mount backup volumeMount", r.Spec.StorageDataPath)
			}
		}
		if len(r.Spec.VolumeMounts) == 0 && !storageVolumeFound {
			return fmt.Errorf("spec.volumeMounts must have at least 1 value OR spec.volumes must have volume.name `data` for spec.storageDataPath=%q", r.Spec.StorageDataPath)
		}
	}
	return nil
}

// SetStatusTo changes update status with optional reason of fail
func (r *VMSingle) SetUpdateStatusTo(ctx context.Context, c client.Client, status UpdateStatus, maybeErr error) error {
	return updateObjectStatus(ctx, c, &patchStatusOpts[*VMSingle, *VMSingleStatus]{
		actualStatus: status,
		r:            r,
		rStatus:      &r.Status,
		maybeErr:     maybeErr,
		mutateCurrentBeforeCompare: func(vs *VMSingleStatus) {
			vs.LegacyStatus = vs.UpdateStatus
		},
	})
}

// GetStatusMetadata returns metadata for object status
func (r *VMSingleStatus) GetStatusMetadata() *StatusMetadata {
	return &r.StatusMetadata
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (r *VMSingle) GetAdditionalService() *AdditionalServiceSpec {
	return r.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMSingle{}, &VMSingleList{})
}
