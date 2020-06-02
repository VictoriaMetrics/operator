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
	VmSelect  VmSelect  `json:"vmselect"`
	VmInsert  VmInsert  `json:"vminsert"`
	VmStorage VmStorage `json:"vmstorage"`
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
	Enabled bool `json:"enabled"`
	// +optional
	Name  string `json:"name"`
	Image Image  `json:"image"`
	// +optional
	PriorityClassName string `json:"priorityClassName"`
	// +optional
	FullnameOverride string `json:"fullnameOverride"`
	// +optional
	ExtraArgs map[string]string `json:"extraArgs"`
	// +optional
	Labels map[string]string `json:"labels"`
	// +optional
	Annotations map[string]string `json:"annotations"`
	// +optional
	Tolerations []v1.Toleration `json:"tolerations"`
	// +optional
	NodeSelector v1.NodeSelector `json:"nodeSelector"`
	// +optional
	Affinity v1.Affinity `json:"affinity"`
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations"`
	ReplicaCount   int32             `json:"replicaCount"`
	// +optional
	Resources v1.ResourceRequirements `json:"resources"`
	// +optional
	SecurityContext v1.PodSecurityContext `json:"securityContext"`
	// +optional
	CacheMountPath string `json:"cacheMountPath"`
	// +optional
	Service VmSelectService `json:"service"`
	// +optional
	StatefulSet VmSelectSts `json:"statefulSet"`
	// +optional
	PersistentVolume VmSelectPV `json:"persistentVolume"`
}

func (s VmSelect) GetName() string {
	if s.Name == "" {
		return "vmselect"
	}
	return s.Name
}

type VmSelectHeadlessService struct {
	// +optional
	Annotations map[string]string `json:"annotations"`
	// +optional
	Labels map[string]string `json:"labels"`
	// +optional
	Type string `json:"type"`
}

type VmSelectService struct {
	// +optional
	VmSelectHeadlessService `json:",inline"`
	// +optional
	ClusterIP string `json:"clusterIP"`
	// +optional
	ExternalIPs []string `json:"externalIPs"`
	// +optional
	LoadBalancerIP string `json:"loadBalancerIP"`
	// +optional
	LoadBalancerSourceRanges []string `json:"loadBalancerSourceRanges"`
	// +optional
	ServicePort int `json:"servicePort"`
}

type VmSelectSts struct {
	// +optional
	Enabled bool `json:"enabled"`
	// +optional
	PodManagementPolicy string `json:"podManagementPolicy"`
	// +optional
	Service VmSelectHeadlessService `json:"service"`
}

type VmSelectPV struct {
	// +optional
	Enabled bool `json:"enabled"`
	// +optional
	AccessModes []v1.PersistentVolumeAccessMode `json:"accessModes"`
	// +optional
	Annotations map[string]string `json:"annotations"`
	// +optional
	ExistingClaim string `json:"existingClaim"`
	// +optional
	Size string `json:"size"`
	// +optional
	SubPath string `json:"subPath"`
}

type VmInsert struct {
	// +optional
	Enabled bool `json:"enabled"`
	// +optional
	Name string `json:"name"`
	// +optional
	Image Image `json:"image"`
	// +optional
	PriorityClassName string `json:"priorityClassName"`
	// +optional
	FullnameOverride string `json:"fullnameOverride"`
	// +optional
	ExtraArgs map[string]string `json:"extraArgs"`
	// +optional
	Annotations map[string]string `json:"annotations"`
	// +optional
	Tolerations v1.Toleration `json:"tolerations"`
	// +optional
	NodeSelector v1.NodeSelector `json:"nodeSelector"`
	// +optional
	Affinity v1.Affinity `json:"affinity"`
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations"`
	ReplicaCount   int               `json:"replicaCount"`
	// +optional
	Resources v1.ResourceList `json:"resources"`
	// +optional
	SecurityContext v1.SecurityContext `json:"securityContext"`
	// +optional
	Service VmInsertService `json:"service"`
}

func (i VmInsert) GetName() string {
	if i.Name == "" {
		return "vminsert"
	}
	return i.Name
}

type VmInsertService struct {
	// +optional
	Annotations map[string]string `json:"annotations"`
	// +optional
	Labels map[string]string `json:"labels"`
	// +optional
	ClusterIP string `json:"clusterIP"`
	// +optional
	ExternalIPs map[string]string `json:"externalIPs"`
	// +optional
	LoadBalancerIP string `json:"loadBalancerIP"`
	// +optional
	LoadBalancerSourceRanges map[string]string `json:"loadBalancerSourceRanges"`
	// +optional
	ServicePort int `json:"servicePort"`
	// +optional
	Type string `json:"type"`
}

type VmStorage struct {
	// +optional
	Enabled bool `json:"enabled"`
	// +optional
	Name  string `json:"name"`
	Image Image  `json:"image"`
	// +optional
	PriorityClassName string `json:"priorityClassName"`
	RetentionPeriod   int    `json:"retentionPeriod"`
	// +optional
	ExtraArgs map[string]string `json:"extraArgs"`
	// +optional
	Tolerations []v1.Toleration `json:"tolerations"`
	// +optional
	NodeSelector map[string]string `json:"nodeSelector"`
	// +optional
	ServiceAccountName string `json:"serviceAccountName"`
	// +optional
	Affinity v1.Affinity `json:"affinity"`
	// +optional
	PersistentVolume VmStoragePV `json:"persistentVolume"`
	// +optional
	PodAnnotations map[string]string `json:"podAnnotations"`
	// +optional
	Annotations map[string]string `json:"annotations"`
	// +optional
	Labels       map[string]string `json:"labels"`
	ReplicaCount int32             `json:"replicaCount"`
	// +optional
	PodManagementPolicy v12.PodManagementPolicyType `json:"podManagementPolicy"`
	// +optional
	Resources v1.ResourceRequirements `json:"resources"`
	// +optional
	SecurityContext v1.PodSecurityContext `json:"securityContext"`
	// +optional
	Service VmStorageService `json:"service"`
	// +optional
	TerminationGracePeriodSeconds int64 `json:"terminationGracePeriodSeconds"`
	// +optional
	SchedulerName string `json:"schedulerName"`
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
	Enabled bool `json:"enabled"`
	// +optional
	AccessModes []v1.PersistentVolumeAccessMode `json:"accessModes"`
	// +optional
	Annotations map[string]string `json:"annotations"`
	// +optional
	ExistingClaim string `json:"existingClaim"`
	// +optional
	MountPath string `json:"mountPath"`
	// +optional
	Size string `json:"size"`
	// +optional
	SubPath string `json:"subPath"`
}

func (pv VmStoragePV) Validate() error {
	if !pv.Enabled {
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
	Annotations map[string]string `json:"annotations"`
	// +optional
	Labels map[string]string `json:"labels"`
	// +optional
	ServicePort int32 `json:"servicePort"`
	// +optional
	VminsertPort int32 `json:"vminsertPort"`
	// +optional
	VmselectPort int32 `json:"vmselectPort"`
}
