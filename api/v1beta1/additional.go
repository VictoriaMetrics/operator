package v1beta1

import (
	"path"

	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const (
	vmPathPrefixFlagName = "http.pathPrefix"
	healthPath           = "/health"
	metricPath           = "/metrics"
	reloadPath           = "/-/reload"
	snapshotCreate       = "/snapshot/create"
	snapshotDelete       = "/snapshot/delete"
	// FinalizerName name of our finalizer.
	FinalizerName = "apps.victoriametrics.com/finalizer"
)

var (
	// GroupVersion is group version used to register these objects
	SchemeGroupVersion = schema.GroupVersion{Group: "operator.victoriametrics.com", Version: "v1beta1"}
)

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
	// +optional
	Username v1.SecretKeySelector `json:"username,omitempty"`
	// The secret in the service scrape namespace that contains the password
	// for authentication.
	// +optional
	Password v1.SecretKeySelector `json:"password,omitempty"`
}

// ServiceSpec is be added into CRD spec to support custom service information
// If its defined operator will create separate service with user defined parameters,
// some of (selector, labels, type) - can be inherited from default service definition.
// +k8s:openapi-gen=true
type ServiceSpec struct {
	// EmbeddedObjectMetadata defines objectMeta for additional service.
	EmbeddedObjectMetadata `json:"metadata,omitempty"`
	//// Name name for the additional service.
	//// Cannot be the same as default CRD service name
	//// and its mandratory parameter.
	//// its users responsibility to check if name is uniq.
	//Name string `json:"name"`
	//// Labels - additional labels, service has selector labels
	//// and its cannot be changed.
	//// +optional
	//Labels map[string]string `json:"labels,omitempty"`
	//// Annotations - annotations for service.
	//// +optional
	//Annotations map[string]string `json:"annotations,omitempty"`
	// ServiceSpec describes the attributes that a user creates on a service.
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/
	Spec v1.ServiceSpec `json:"spec"`
}

// NameOrDefault returns name of default value with suffix
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
