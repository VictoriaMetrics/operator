package v1beta1

import (
	"fmt"
	"path"

	"k8s.io/api/autoscaling/v2beta2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	vmPathPrefixFlagName = "http.pathPrefix"
	healthPath           = "/health"
	metricPath           = "/metrics"
	reloadPath           = "/-/reload"
	snapshotCreate       = "/snapshot/create"
	snapshotDelete       = "/snapshot/delete"
	// FinalizerName name of our finalizer.
	FinalizerName            = "apps.victoriametrics.com/finalizer"
	SkipValidationAnnotation = "operator.victoriametrics.com/skip-validation"
	SkipValidationValue      = "true"
)

var (
	// GroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: "operator.victoriametrics.com", Version: "v1beta1"}
)

// skip validation, if object has annotation.
func mustSkipValidation(cr client.Object) bool {
	return cr.GetAnnotations()[SkipValidationAnnotation] == SkipValidationValue
}

func MergeFinalizers(src client.Object, finalizer string) []string {
	if !IsContainsFinalizer(src.GetFinalizers(), finalizer) {
		srcF := src.GetFinalizers()
		srcF = append(srcF, finalizer)
		src.SetFinalizers(srcF)
	}
	return src.GetFinalizers()
}

// IsContainsFinalizer check if finalizers is set.
func IsContainsFinalizer(src []string, finalizer string) bool {
	for _, s := range src {
		if s == finalizer {
			return true
		}
	}
	return false
}

// RemoveFinalizer - removes given finalizer from finalizers list.
func RemoveFinalizer(src []string, finalizer string) []string {
	dst := src[:0]
	for _, s := range src {
		if s == finalizer {
			continue
		}
		dst = append(dst, s)
	}
	return dst
}

// EmbeddedObjectMetadata contains a subset of the fields included in k8s.io/apimachinery/pkg/apis/meta/v1.ObjectMeta
// Only fields which are relevant to embedded resources are included.
type EmbeddedObjectMetadata struct {
	// Name must be unique within a namespace. Is required when creating resources, although
	// some resources may allow a client to request the generation of an appropriate name
	// automatically. Name is primarily intended for creation idempotence and configuration
	// definition.
	// Cannot be updated.
	// More info: http://kubernetes.io/docs/user-guide/identifiers#names
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`

	// Labels Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects. May match selectors of replication controllers
	// and services.
	// More info: http://kubernetes.io/docs/user-guide/labels
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors=true
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.displayName="PodLabels"
	// +operator-sdk:gen-csv:customresourcedefinitions.specDescriptors.x-descriptors="urn:alm:descriptor:com.tectonic.ui:label"
	// +optional
	Labels map[string]string `json:"labels,omitempty" protobuf:"bytes,11,rep,name=labels"`

	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	// More info: http://kubernetes.io/docs/user-guide/annotations
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" protobuf:"bytes,12,rep,name=annotations"`
}

// StorageSpec defines the configured storage for a group Prometheus servers.
// If neither `emptyDir` nor `volumeClaimTemplate` is specified, then by default an [EmptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir) will be used.
// +k8s:openapi-gen=true
type StorageSpec struct {
	// Deprecated: subPath usage will be disabled by default in a future release, this option will become unnecessary.
	// DisableMountSubPath allows to remove any subPath usage in volume mounts.
	// +optional
	DisableMountSubPath bool `json:"disableMountSubPath,omitempty"`
	// EmptyDirVolumeSource to be used by the Prometheus StatefulSets. If specified, used in place of any volumeClaimTemplate. More
	// info: https://kubernetes.io/docs/concepts/storage/volumes/#emptydir
	// +optional
	EmptyDir *v1.EmptyDirVolumeSource `json:"emptyDir,omitempty"`
	// A PVC spec to be used by the VMAlertManager StatefulSets.
	// +optional
	VolumeClaimTemplate EmbeddedPersistentVolumeClaim `json:"volumeClaimTemplate,omitempty"`
}

// EmbeddedPersistentVolumeClaim is an embedded version of k8s.io/api/core/v1.PersistentVolumeClaim.
// It contains TypeMeta and a reduced ObjectMeta.
type EmbeddedPersistentVolumeClaim struct {
	metav1.TypeMeta `json:",inline"`

	// EmbeddedMetadata contains metadata relevant to an EmbeddedResource.
	// +optional
	EmbeddedObjectMetadata `json:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`

	// Spec defines the desired characteristics of a volume requested by a pod author.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Spec v1.PersistentVolumeClaimSpec `json:"spec,omitempty" protobuf:"bytes,2,opt,name=spec"`

	// Status represents the current information/status of a persistent volume claim.
	// Read-only.
	// More info: https://kubernetes.io/docs/concepts/storage/persistent-volumes#persistentvolumeclaims
	// +optional
	Status v1.PersistentVolumeClaimStatus `json:"status,omitempty" protobuf:"bytes,3,opt,name=status"`
}

// BasicAuth allow an endpoint to authenticate over basic authentication
// More info: https://prometheus.io/docs/operating/configuration/#endpoints
// +k8s:openapi-gen=true
type BasicAuth struct {
	// The secret in the service scrape namespace that contains the username
	// for authentication.
	// It must be at them same namespace as CRD
	// +optional
	Username v1.SecretKeySelector `json:"username,omitempty"`
	// The secret in the service scrape namespace that contains the password
	// for authentication.
	// It must be at them same namespace as CRD
	// +optional
	Password v1.SecretKeySelector `json:"password,omitempty"`
	// PasswordFile defines path to password file at disk
	// +optional
	PasswordFile string `json:"password_file,omitempty"`
}

// ServiceSpec defines additional service for CRD with user-defined params.
// by default, some of fields can be inherited from default service definition for the CRD:
// labels,selector, ports.
// if metadata.name is not defined, service will have format {{CRD_TYPE}}-{{CRD_NAME}}-additional-service.
// +k8s:openapi-gen=true
type ServiceSpec struct {
	// EmbeddedObjectMetadata defines objectMeta for additional service.
	EmbeddedObjectMetadata `json:"metadata,omitempty"`
	// ServiceSpec describes the attributes that a user creates on a service.
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/
	Spec v1.ServiceSpec `json:"spec"`
}

// NameOrDefault returns name or default value with suffix
func (ss *ServiceSpec) NameOrDefault(defaultName string) string {
	if ss.Name != "" {
		return ss.Name
	}
	return defaultName + "-additional-service"
}

func buildPathWithPrefixFlag(flags map[string]string, defaultPath string) string {
	if prefix, ok := flags[vmPathPrefixFlagName]; ok {
		return path.Join(prefix, defaultPath)
	}
	return defaultPath
}

type EmbeddedPodDisruptionBudgetSpec struct {
	// An eviction is allowed if at least "minAvailable" pods selected by
	// "selector" will still be available after the eviction, i.e. even in the
	// absence of the evicted pod.  So for example you can prevent all voluntary
	// evictions by specifying "100%".
	// +optional
	MinAvailable *intstr.IntOrString `json:"minAvailable,omitempty"`

	// An eviction is allowed if at most "maxUnavailable" pods selected by
	// "selector" are unavailable after the eviction, i.e. even in absence of
	// the evicted pod. For example, one can prevent all voluntary evictions
	// by specifying 0. This is a mutually exclusive setting with "minAvailable".
	// +optional
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
}

// EmbeddedProbes - it allows to override some probe params.
// its not necessary to specify all options,
// operator will replace missing spec with default values.
type EmbeddedProbes struct {
	// LivenessProbe that will be added CRD pod
	// +optional
	LivenessProbe *v1.Probe `json:"livenessProbe,omitempty"`
	// ReadinessProbe that will be added CRD pod
	// +optional
	ReadinessProbe *v1.Probe `json:"readinessProbe,omitempty"`
	// StartupProbe that will be added to CRD pod
	// +optional
	StartupProbe *v1.Probe `json:"startupProbe,omitempty"`
}

// EmbeddedHPA embeds HorizontalPodAutoScaler spec v2.
// https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/horizontal-pod-autoscaler-v2beta2/
type EmbeddedHPA struct {
	MinReplicas *int32                                   `json:"minReplicas,omitempty"`
	MaxReplicas int32                                    `json:"maxReplicas,omitempty"`
	Metrics     []v2beta2.MetricSpec                     `json:"metrics,omitempty"`
	Behaviour   *v2beta2.HorizontalPodAutoscalerBehavior `json:"behaviour,omitempty"`
}

func (cr *EmbeddedHPA) sanityCheck() error {
	if cr.MinReplicas != nil && *cr.MinReplicas > cr.MaxReplicas {
		return fmt.Errorf("minReplicas cannot be greater then maxReplicas")
	}
	if cr.Behaviour == nil && len(cr.Metrics) == 0 {
		return fmt.Errorf("at least behaviour or metrics property must be configuread")
	}
	return nil
}

// DiscoverySelector can be used at CRD components discovery
type DiscoverySelector struct {
	Namespace *NamespaceSelector    `json:"namespaceSelector,omitempty"`
	Labels    *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

func (ds *DiscoverySelector) AsListOptions() (*client.ListOptions, error) {
	if ds.Labels == nil {
		return &client.ListOptions{}, nil
	}
	s, err := metav1.LabelSelectorAsSelector(ds.Labels)
	if err != nil {
		return nil, err
	}
	return &client.ListOptions{
		LabelSelector: s,
	}, nil
}
