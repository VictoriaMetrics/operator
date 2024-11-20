package v1beta1

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMSingleSpec defines the desired state of VMSingle
// +k8s:openapi-gen=true
// +kubebuilder:printcolumn:name="Version",type="string",JSONPath=".spec.version",description="The version of VMSingle"
// +kubebuilder:printcolumn:name="RetentionPeriod",type="string",JSONPath=".spec.RetentionPeriod",description="The desired RetentionPeriod for vm single"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMSingleSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the VMSingle pods.
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
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

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMSingle) UnmarshalJSON(src []byte) error {
	type pcr VMSingle
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	prev, err := parseLastAppliedSpec[VMSingleSpec](cr)
	if err != nil {
		return err
	}
	cr.ParsedLastAppliedSpec = prev
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
	// ReplicaCount Total number of non-terminated pods targeted by this VMSingle.
	Replicas int32 `json:"replicas,omitempty"`
	// UpdatedReplicas Total number of non-terminated pods targeted by this VMSingle.
	UpdatedReplicas int32 `json:"updatedReplicas,omitempty"`
	// AvailableReplicas Total number of available pods (ready for at least minReadySeconds) targeted by this VMSingle.
	AvailableReplicas int32 `json:"availableReplicas,omitempty"`
	// UnavailableReplicas Total number of unavailable pods targeted by this VMSingle.
	UnavailableReplicas int32 `json:"unavailableReplicas,omitempty"`

	// UpdateStatus defines a status of single node rollout
	UpdateStatus UpdateStatus `json:"singleStatus,omitempty"`
	// Reason defines a reason in case of update failure
	Reason string `json:"reason,omitempty"`
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
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.singleStatus",description="Current status of single node update process"
type VMSingle struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VMSingleSpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMSingleSpec  `json:"-" yaml:"-"`
	Status                VMSingleStatus `json:"status,omitempty"`
}

func (cr *VMSingle) Probe() *EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMSingle) ProbePath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VMSingle) ProbeScheme() string {
	return strings.ToUpper(protoFromFlags(cr.Spec.ExtraArgs))
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

func (cr VMSingle) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

func (cr VMSingle) AnnotationsFiltered() map[string]string {
	return filterMapKeysByPrefixes(cr.ObjectMeta.Annotations, annotationFilterPrefixes)
}

func (cr VMSingle) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmsingle",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr VMSingle) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}
	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr VMSingle) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil {
		return selectorLabels
	}
	crLabels := filterMapKeysByPrefixes(cr.ObjectMeta.Labels, labelFilterPrefixes)
	return labels.Merge(crLabels, selectorLabels)
}

func (cr VMSingle) PrefixedName() string {
	return fmt.Sprintf("vmsingle-%s", cr.Name)
}

func (cr VMSingle) StreamAggrConfigName() string {
	return fmt.Sprintf("stream-aggr-vmsingle-%s", cr.Name)
}

// GetMetricPath returns prefixed path for metric requests
func (cr VMSingle) GetMetricPath() string {
	return buildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr VMSingle) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr VMSingle) GetServiceScrape() *VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

func (cr VMSingle) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

func (cr VMSingle) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr VMSingle) GetNSName() string {
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
	return fmt.Sprintf("%s://%s.%s.svc:%s", protoFromFlags(cr.Spec.ExtraArgs), cr.PrefixedName(), cr.Namespace, port)
}

// AsCRDOwner implements interface
func (cr *VMSingle) AsCRDOwner() []metav1.OwnerReference {
	return GetCRDAsOwner(Single)
}

// LastAppliedSpecAsPatch return last applied single spec as patch annotation
func (cr *VMSingle) LastAppliedSpecAsPatch() (client.Patch, error) {
	data, err := json.Marshal(cr.Spec)
	if err != nil {
		return nil, fmt.Errorf("possible bug, cannot serialize single specification as json :%w", err)
	}
	patch := fmt.Sprintf(`{"metadata":{"annotations":{%q: %q}}}`, lastAppliedSpecAnnotationName, data)
	return client.RawPatch(types.MergePatchType, []byte(patch)), nil
}

// HasSpecChanges compares single spec with last applied single spec stored in annotation
func (cr *VMSingle) HasSpecChanges() (bool, error) {
	lastAppliedSingleJSON := cr.Annotations[lastAppliedSpecAnnotationName]
	if len(lastAppliedSingleJSON) == 0 {
		return true, nil
	}

	instanceSpecData, err := json.Marshal(cr.Spec)
	if err != nil {
		return false, err
	}
	return !bytes.Equal([]byte(lastAppliedSingleJSON), instanceSpecData), nil
}

func (cr *VMSingle) Paused() bool {
	return cr.Spec.Paused
}

// SetStatusTo changes update status with optional reason of fail
func (cr *VMSingle) SetUpdateStatusTo(ctx context.Context, r client.Client, status UpdateStatus, maybeErr error) error {
	currentStatus := cr.Status.UpdateStatus
	prevStatus := cr.Status.DeepCopy()
	switch status {
	case UpdateStatusExpanding:
	case UpdateStatusFailed:
		if maybeErr != nil {
			cr.Status.Reason = maybeErr.Error()
		}
	case UpdateStatusOperational:
		cr.Status.Reason = ""
	case UpdateStatusPaused:
		if currentStatus == status {
			return nil
		}
	default:
		panic(fmt.Sprintf("BUG: not expected status=%q", status))
	}
	if equality.Semantic.DeepEqual(&cr.Status, prevStatus) && currentStatus == status {
		return nil
	}
	cr.Status.UpdateStatus = status

	return statusPatch(ctx, r, cr.DeepCopy(), cr.Status)
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMSingle) GetAdditionalService() *AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

func init() {
	SchemeBuilder.Register(&VMSingle{}, &VMSingleList{})
}
