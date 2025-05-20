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

	// RetentionPeriod defines how long to retain stored metrics, specified as a duration (e.g., "1d", "1w", "1m").
	// Data with timestamps outside the RetentionPeriod is automatically deleted. The minimum allowed value is 1d, or 24h.
	// The default value is 1 (one month).
	// See [retention](https://docs.victoriametrics.com/victoriametrics/single-server-victoriametrics/#retention) docs for details.
	// +optional
	// +kubebuilder:validation:Pattern:="^[0-9]+(h|d|y)?$"
	RetentionPeriod string `json:"retentionPeriod,omitempty"`
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

	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	CommonDefaultableParams           `json:",inline"`
	CommonApplicationDeploymentParams `json:",inline"`
}

// HasAnyStreamAggrRule checks if vmsingle has any defined aggregation rules
func (cr *VMSingle) HasAnyStreamAggrRule() bool {
	return cr.Spec.StreamAggrConfig.HasAnyRule()
}

// SetLastSpec implements objectWithLastAppliedState interface
func (cr *VMSingle) SetLastSpec(prevSpec VMSingleSpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMSingle) UnmarshalJSON(src []byte) error {
	type pcr VMSingle
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := ParseLastAppliedStateTo(cr); err != nil {
		return err
	}

	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMSingleSpec) UnmarshalJSON(src []byte) error {
	type pcr VMSingleSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmsingle spec: %s, err: %s", string(src), err)
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

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMSingle) GetStatus() *VMSingleStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMSingle) DefaultStatusFields(vs *VMSingleStatus) {
	vs.LegacyStatus = vs.UpdateStatus
}

func (cr *VMSingle) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMSingle) ProbePath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VMSingle) ProbeScheme() string {
	return strings.ToUpper(HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VMSingle) ProbePort() string {
	return cr.Spec.Port
}

func (cr *VMSingle) ProbeNeedLiveness() bool {
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
func (cr *VMSingle) AsOwner() []metav1.OwnerReference {
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

func (cr *VMSingle) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr *VMSingle) AnnotationsFiltered() map[string]string {
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

func (cr *VMSingle) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmsingle",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr *VMSingle) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr *VMSingle) AllLabels() map[string]string {
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

func (cr *VMSingle) PrefixedName() string {
	return fmt.Sprintf("vmsingle-%s", cr.Name)
}

func (cr *VMSingle) StreamAggrConfigName() string {
	return fmt.Sprintf("stream-aggr-vmsingle-%s", cr.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VMSingle) GetMetricPath() string {
	return BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMSingle) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMSingle) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr *VMSingle) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr *VMSingle) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// GetNSName implements build.builderOpts interface
func (cr *VMSingle) GetNSName() string {
	return cr.GetNamespace()
}

func (cr *VMSingle) AsURL() string {
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

// LastAppliedSpecAsPatch return last applied single spec as patch annotation
func (cr *VMSingle) LastAppliedSpecAsPatch() (client.Patch, error) {
	return LastAppliedChangesAsPatch(cr.ObjectMeta, cr.Spec)
}

// HasSpecChanges compares single spec with last applied single spec stored in annotation
func (cr *VMSingle) HasSpecChanges() (bool, error) {
	return HasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VMSingle) Paused() bool {
	return cr.Spec.Paused
}

func (cr *VMSingle) Validate() error {
	if MustSkipCRValidation(cr) {
		return nil
	}
	if cr.Spec.ServiceSpec != nil && cr.Spec.ServiceSpec.Name == cr.PrefixedName() {
		return fmt.Errorf("spec.serviceSpec.Name cannot be equal to prefixed name=%q", cr.PrefixedName())
	}

	if cr.Spec.VMBackup != nil {
		if err := cr.Spec.VMBackup.validate(cr.Spec.License); err != nil {
			return err
		}
	}
	if cr.Spec.StorageDataPath != "" {
		if len(cr.Spec.Volumes) == 0 {
			return fmt.Errorf("spec.volumes must have at least 1 value for spec.storageDataPath=%q", cr.Spec.StorageDataPath)
		}
		var storageVolumeFound bool
		for _, volume := range cr.Spec.Volumes {
			if volume.Name == "data" {
				storageVolumeFound = true
				break
			}
		}
		if cr.Spec.VMBackup != nil {
			if !storageVolumeFound {
				return fmt.Errorf("spec.volumes must have at least 1 value with `name: data` for spec.storageDataPath=%q."+
					" It's required by operator to correctly mount backup volumeMount", cr.Spec.StorageDataPath)
			}
		}
		if len(cr.Spec.VolumeMounts) == 0 && !storageVolumeFound {
			return fmt.Errorf("spec.volumeMounts must have at least 1 value OR spec.volumes must have volume.name `data` for spec.storageDataPath=%q", cr.Spec.StorageDataPath)
		}
	}
	return nil
}

// GetStatusMetadata returns metadata for object status
func (cr *VMSingleStatus) GetStatusMetadata() *StatusMetadata {
	return &cr.StatusMetadata
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMSingle) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMSingle{}, &VMSingleList{})
}
