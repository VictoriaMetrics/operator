package v1beta1

import (
	"fmt"
	v12 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

const (
	StorageDefaultVolumeName = "vmstorage-volume"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VmClusterSpec defines the desired state of VmCluster
type VmClusterSpec struct {
	// +optional
	VmSelect *VmSelect `json:"vmselect,omitempty"`
	// +optional
	VmInsert *VmInsert `json:"vminsert,omitempty"`
	// +optional
	VmStorage *VmStorage `json:"vmstorage,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VmCluster is the Schema for the vmclusters API
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmclusters,scope=Namespaced
type VmCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              VmClusterSpec   `json:"spec,omitempty"`
	Status            VmClusterStatus `json:"status,omitempty"`
}

func (c *VmCluster) AsOwner() []metav1.OwnerReference {
	return []metav1.OwnerReference{
		{
			APIVersion:         c.APIVersion,
			Kind:               c.Kind,
			Name:               c.Name(),
			UID:                c.UID,
			Controller:         pointer.BoolPtr(true),
			BlockOwnerDeletion: pointer.BoolPtr(true),
		},
	}
}

// Validate validate cluster specification
func (c VmCluster) Validate() error {
	if err := c.Spec.VmStorage.Validate(); err != nil {
		return err
	}
	return nil
}

func (c VmCluster) StorageCommonLables() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      c.Name(),
		"app.kubernetes.io/component": c.Spec.VmStorage.GetName(),
	}
}

const (
	VmStorageStatusExpanding   = "expanding"
	VmStorageStatusOperational = "operational"
)

// VmClusterStatus defines the observed state of VmCluster
type VmClusterStatus struct {
	VmStorage VmStorageStatus `json:"vmStorage"`
}

type VmStorageStatus struct {
	Status string `json:"status"`
}

// Name returns cluster name
func (c VmCluster) Name() string {
	return c.ObjectMeta.Name
}

func (c VmCluster) FullStorageName() string {
	return fmt.Sprintf("%s-%s", c.Name(), c.Spec.VmStorage.GetName())
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VmClusterList contains a list of VmCluster
type VmClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VmCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VmCluster{}, &VmClusterList{})
}

type VmSelect struct {
	// +optional
	Name  string `json:"name,omitempty"`
	Image Image  `json:"image"`
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// +optional
	FullnameOverride string `json:"fullnameOverride,omitempty"`
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// +optional
	NodeSelector v1.NodeSelector `json:"nodeSelector,omitempty"`
	// +optional
	Affinity v1.Affinity `json:"affinity,omitempty"`
	// +optional
	PodMetadata  *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	ReplicaCount *int32                  `json:"replicaCount"`
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// +optional
	SecurityContext v1.PodSecurityContext `json:"securityContext,omitempty"`
	// +optional
	CacheMountPath string `json:"cacheMountPath,omitempty"`
	// +optional
	Service VmSelectService `json:"service,omitempty"`
	// +optional
	StatefulSet *VmSelectSts `json:"statefulSet,omitempty"`
	// +optional
	PersistentVolume *VmSelectPV `json:"persistentVolume,omitempty"`
}

func (s VmSelect) GetName() string {
	if s.Name == "" {
		return "vmselect"
	}
	return s.Name
}

type VmSelectHeadlessService struct {
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	Type string `json:"type,omitempty"`
}

type VmSelectService struct {
	// +optional
	VmSelectHeadlessService `json:",inline"`
	// +optional
	ClusterIP string `json:"clusterIP,omitempty"`
	// +optional
	ExternalIPs []string `json:"externalIPs,omitempty"`
	// +optional
	LoadBalancerIP string `json:"loadBalancerIP,omitempty"`
	// +optional
	LoadBalancerSourceRanges []string `json:"loadBalancerSourceRanges,omitempty"`
	// +optional
	ServicePort int `json:"servicePort,omitempty"`
}

type VmSelectSts struct {
	// +optional
	PodManagementPolicy string `json:"podManagementPolicy,omitempty"`
	// +optional
	Service VmSelectHeadlessService `json:"service,omitempty"`
}

type VmSelectPV struct {
	// +optional
	AccessModes []v1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	ExistingClaim string `json:"existingClaim,omitempty"`
	// +optional
	Size string `json:"size,omitempty"`
	// +optional
	SubPath string `json:"subPath,omitempty"`
}

type VmInsert struct {
	// +optional
	Name  string `json:"name,omitempty"`
	Image Image  `json:"image"`
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
	// +optional
	FullnameOverride string `json:"fullnameOverride,omitempty"`
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Tolerations v1.Toleration `json:"tolerations,omitempty"`
	// +optional
	NodeSelector v1.NodeSelector `json:"nodeSelector,omitempty"`
	// +optional
	Affinity v1.Affinity `json:"affinity,omitempty"`
	// +optional
	PodMetadata  *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	ReplicaCount *int32                  `json:"replicaCount"`
	// +optional
	Resources v1.ResourceList `json:"resources,omitempty"`
	// +optional
	SecurityContext v1.SecurityContext `json:"securityContext,omitempty"`
	// +optional
	Service VmInsertService `json:"service,omitempty"`
}

func (i VmInsert) GetName() string {
	if i.Name == "" {
		return "vminsert"
	}
	return i.Name
}

type VmInsertService struct {
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	ClusterIP string `json:"clusterIP,omitempty"`
	// +optional
	ExternalIPs map[string]string `json:"externalIPs,omitempty"`
	// +optional
	LoadBalancerIP string `json:"loadBalancerIP,omitempty"`
	// +optional
	LoadBalancerSourceRanges map[string]string `json:"loadBalancerSourceRanges,omitempty"`
	// +optional
	ServicePort int `json:"servicePort,omitempty"`
	// +optional
	Type string `json:"type,omitempty"`
}

type VmStorage struct {
	// +optional
	Name  string `json:"name,omitempty"`
	Image Image  `json:"image"`
	// +optional
	PriorityClassName string `json:"priorityClassName,omitempty"`
	RetentionPeriod   int    `json:"retentionPeriod,omitempty"`
	// +optional
	ExtraArgs map[string]string `json:"extraArgs,omitempty"`
	// +optional
	Tolerations []v1.Toleration `json:"tolerations,omitempty"`
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`
	// +optional
	Affinity v1.Affinity `json:"affinity,omitempty"`
	// +optional
	PersistentVolume *VmStoragePV `json:"persistentVolume,omitempty"`
	// +optional
	PodMetadata *EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Labels       map[string]string `json:"labels,omitempty"`
	ReplicaCount *int32            `json:"replicaCount"`
	// +optional
	PodManagementPolicy v12.PodManagementPolicyType `json:"podManagementPolicy,omitempty"`
	// +optional
	Resources v1.ResourceRequirements `json:"resources,omitempty"`
	// +optional
	SecurityContext v1.PodSecurityContext `json:"securityContext,omitempty"`
	// +optional
	Service VmStorageService `json:"service,omitempty"`
	// +optional
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds,omitempty"`
	// +optional
	SchedulerName string `json:"schedulerName,omitempty"`
}

// Validate validates vmstorage specification
func (s VmStorage) Validate() error {
	if err := s.PersistentVolume.Validate(); err != nil {
		return fmt.Errorf("invalid persitance volime spec for %s:%w", s.GetName(), err)
	}
	return nil
}

func (s VmStorage) GetName() string {
	if s.Name == "" {
		return "vmstorage"
	}
	return s.Name
}

type VmStoragePV struct {
	// +optional
	AccessModes []v1.PersistentVolumeAccessMode `json:"accessModes,omitempty"`
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	ExistingClaim string `json:"existingClaim,omitempty"`
	// +optional
	MountPath string `json:"mountPath,omitempty"`
	// +optional
	Size string `json:"size,omitempty"`
	// +optional
	SubPath string `json:"subPath,omitempty"`
}

func (pv *VmStoragePV) Validate() error {
	if pv == nil {
		return nil
	}
	if _, err := resource.ParseQuantity(pv.Size); err != nil {
		return fmt.Errorf("can not parse volume size %s:%w", pv.Size, err)
	}
	return nil
}

type Image struct {
	Repository string        `json:"repository"`
	Tag        string        `json:"tag"`
	PullPolicy v1.PullPolicy `json:"pullPolicy"`
}

type VmStorageService struct {
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// +optional
	ServicePort int32 `json:"servicePort,omitempty"`
	// +optional
	VminsertPort int32 `json:"vminsertPort,omitempty"`
	// +optional
	VmselectPort int32 `json:"vmselectPort,omitempty"`
}
