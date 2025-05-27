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
	"strconv"
	"strings"

	"github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VMAnomalySpec defines the desired state of VMAnomaly.
// +k8s:openapi-gen=true
type VMAnomalySpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// PodMetadata configures Labels and Annotations which are propagated to the vmanomaly pods.
	// +optional
	PodMetadata *v1beta1.EmbeddedObjectMetadata `json:"podMetadata,omitempty"`
	// ManagedMetadata defines metadata that will be added to the all objects
	// created by operator for the given CustomResource
	ManagedMetadata *v1beta1.ManagedObjectsMetadata `json:"managedMetadata,omitempty"`
	// LogLevel for VMAnomaly to be configured with.
	// INFO, WARN, ERROR, FATAL, PANIC
	// +optional
	// +kubebuilder:validation:Enum=INFO;WARN;ERROR;FATAL;PANIC
	LogLevel string `json:"logLevel,omitempty"`
	// ServiceScrapeSpec that will be added to vmanomaly VMPodScrape spec
	// +optional
	ServiceScrapeSpec *v1beta1.VMServiceScrapeSpec `json:"serviceScrapeSpec,omitempty"`
	// ShardCount - numbers of shards of VMAnomaly
	// in this case operator will use 1 deployment/sts per shard with
	// replicas count according to spec.replicas.
	// +optional
	ShardCount *int `json:"shardCount,omitempty"`
	// UpdateStrategy - overrides default update strategy.
	// works only for deployments, statefulset always use OnDelete.
	// +kubebuilder:validation:Enum=Recreate;RollingUpdate
	// +optional
	UpdateStrategy *appsv1.DeploymentStrategyType `json:"updateStrategy,omitempty"`
	// RollingUpdate - overrides deployment update params.
	// +optional
	RollingUpdate *appsv1.RollingUpdateDeployment `json:"rollingUpdate,omitempty"`
	// PodDisruptionBudget created by operator
	// +optional
	PodDisruptionBudget     *v1beta1.EmbeddedPodDisruptionBudgetSpec `json:"podDisruptionBudget,omitempty"`
	*v1beta1.EmbeddedProbes `json:",inline"`
	// ConfigRawYaml - raw configuration for anomaly,
	// it helps it to start without secret.
	// priority -> hardcoded ConfigRaw -> ConfigRaw, provided by user -> ConfigSecret.
	// +optional
	ConfigRawYaml string `json:"configRawYaml,omitempty"`
	// ConfigSecret is the name of a Kubernetes Secret in the same namespace as the
	// VMAnomaly object, which contains configuration for this VMAnomaly,
	// configuration must be inside secret key: anomaly.yaml.
	// It must be created by user.
	// instance. Defaults to 'vmanomaly-<anomaly-name>'
	// The secret is mounted into /etc/anomaly/config.
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,displayName="Secret with anomaly config",xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	ConfigSecret string `json:"configSecret,omitempty"`
	// StatefulMode enables StatefulSet for `VMAnomaly` instead of Deployment
	// +optional
	StatefulMode bool `json:"statefulMode,omitempty"`
	// StatefulStorage configures storage for StatefulSet
	// +optional
	StatefulStorage *v1beta1.StorageSpec `json:"statefulStorage,omitempty"`
	// StatefulRollingUpdateStrategy allows configuration for strategyType
	// set it to RollingUpdate for disabling operator statefulSet rollingUpdate
	// +optional
	StatefulRollingUpdateStrategy appsv1.StatefulSetUpdateStrategyType `json:"statefulRollingUpdateStrategy,omitempty"`
	// ClaimTemplates allows adding additional VolumeClaimTemplates for VMAnomaly in StatefulMode
	ClaimTemplates []corev1.PersistentVolumeClaim `json:"claimTemplates,omitempty"`

	// License allows to configure license key to be used for enterprise features.
	// Using license key is supported starting from VictoriaMetrics v1.94.0.
	// See [here](https://docs.victoriametrics.com/enterprise)
	// +optional
	License *v1beta1.License `json:"license,omitempty"`
	// ServiceAccountName is the name of the ServiceAccount to use to run the pods
	// +optional
	ServiceAccountName                        string `json:"serviceAccountName,omitempty"`
	v1beta1.CommonDefaultableParams           `json:",inline,omitempty"`
	v1beta1.CommonConfigReloaderParams        `json:",inline,omitempty"`
	v1beta1.CommonApplicationDeploymentParams `json:",inline,omitempty"`
}

// VMAnomalyStatus defines the observed state of VMAnomaly.
// +k8s:openapi-gen=true
type VMAnomalyStatus struct {
	// Shards represents total number of vmanomaly deployments with uniq scrape targets
	Shards                 int32 `json:"shards,omitempty"`
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMAnomalyStatus) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.StatusMetadata
}

// VMAnomaly is the Schema for the vmanomalies API.
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMAnomaly App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="StatefulSet,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Secret,v1"
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmanomalies,scope=Namespaced
// +kubebuilder:subresource:scale:specpath=.spec.shardCount,statuspath=.status.shards,selectorpath=.status.selector
// +kubebuilder:printcolumn:name="Shards Count",type="integer",JSONPath=".status.shards",description="current number of shards"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type VMAnomaly struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VMAnomalySpec `json:"spec,omitempty"`
	// ParsedLastAppliedSpec contains last-applied configuration spec
	ParsedLastAppliedSpec *VMAnomalySpec `json:"-" yaml:"-"`

	Status VMAnomalyStatus `json:"status,omitempty"`
}

// SetLastSpec implements objectWithLastAppliedState interface
func (cr *VMAnomaly) SetLastSpec(prevSpec VMAnomalySpec) {
	cr.ParsedLastAppliedSpec = &prevSpec
}

// AsOwner returns owner references with current object as owner
func (cr *VMAnomaly) AsOwner() []metav1.OwnerReference {
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

func (cr *VMAnomaly) AnnotationsFiltered() map[string]string {
	if cr.Spec.ManagedMetadata == nil {
		return nil
	}
	dst := make(map[string]string, len(cr.Spec.ManagedMetadata.Annotations))
	for k, v := range cr.Spec.ManagedMetadata.Annotations {
		dst[k] = v
	}
	return dst
}

func (cr *VMAnomaly) PodAnnotations() map[string]string {
	annotations := map[string]string{}
	if cr.Spec.PodMetadata != nil {
		for annotation, value := range cr.Spec.PodMetadata.Annotations {
			annotations[annotation] = value
		}
	}
	return annotations
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAnomaly) GetStatus() *VMAnomalyStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMAnomaly) DefaultStatusFields(vs *VMAnomalyStatus) {
	var shardCnt int32
	if cr.Spec.ShardCount != nil {
		shardCnt = int32(*cr.Spec.ShardCount)
	}
	vs.Shards = shardCnt
}

func (cr *VMAnomaly) SelectorLabels() map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":      "vmanomaly",
		"app.kubernetes.io/instance":  cr.Name,
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}
}

func (cr *VMAnomaly) PodLabels() map[string]string {
	lbls := cr.SelectorLabels()
	if cr.Spec.PodMetadata == nil {
		return lbls
	}

	return labels.Merge(cr.Spec.PodMetadata.Labels, lbls)
}

func (cr *VMAnomaly) AllLabels() map[string]string {
	selectorLabels := cr.SelectorLabels()
	// fast path
	if cr.ObjectMeta.Labels == nil && cr.Spec.ManagedMetadata == nil {
		return selectorLabels
	}
	var result map[string]string
	if cr.Spec.ManagedMetadata != nil {
		result = labels.Merge(result, cr.Spec.ManagedMetadata.Labels)
	}
	return labels.Merge(result, selectorLabels)
}

func (cr *VMAnomaly) PrefixedName() string {
	return fmt.Sprintf("vmanomaly-%s", cr.Name)
}

func (cr *VMAnomaly) TLSAssetName() string {
	return fmt.Sprintf("tls-assets-vmanomaly-%s", cr.Name)
}

func (cr *VMAnomaly) GetServiceAccountName() string {
	if cr.Spec.ServiceAccountName == "" {
		return cr.PrefixedName()
	}
	return cr.Spec.ServiceAccountName
}

// ConfigSecretName returns configuration secret name for alertmanager
func (cr *VMAnomaly) ConfigSecretName() string {
	return fmt.Sprintf("%s-config", cr.PrefixedName())
}

func (cr *VMAnomaly) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

func (cr *VMAnomaly) HealthPath() string {
	return v1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

// GetMetricPath returns prefixed path for metric requests
func (cr *VMAnomaly) GetMetricPath() string {
	return v1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, metricPath)
}

// ExtraArgs returns additionally configured command-line arguments
func (cr *VMAnomaly) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// ServiceScrape returns overrides for serviceScrape builder
func (cr *VMAnomaly) GetServiceScrape() *v1beta1.VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

// Port returns port for accessing anomaly
func (cr *VMAnomaly) Port() string {
	port := cr.Spec.Port
	if port == "" {
		port = "8490"
	}

	return port
}

func (cr *VMAnomaly) GetVolumeName() string {
	if cr.Spec.StatefulStorage != nil && cr.Spec.StatefulStorage.VolumeClaimTemplate.Name != "" {
		return cr.Spec.StatefulStorage.VolumeClaimTemplate.Name
	}
	return fmt.Sprintf("vmanomaly-%s-db", cr.Name)
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMAnomaly) GetAdditionalService() *v1beta1.AdditionalServiceSpec {
	return nil
}

func (cr *VMAnomaly) Probe() *v1beta1.EmbeddedProbes {
	return cr.Spec.EmbeddedProbes
}

func (cr *VMAnomaly) ProbePath() string {
	return v1beta1.BuildPathWithPrefixFlag(cr.Spec.ExtraArgs, healthPath)
}

func (cr *VMAnomaly) ProbeScheme() string {
	return strings.ToUpper(v1beta1.HTTPProtoFromFlags(cr.Spec.ExtraArgs))
}

func (cr *VMAnomaly) ProbePort() string {
	return cr.Spec.Port
}

func (*VMAnomaly) ProbeNeedLiveness() bool {
	return true
}

func (cr *VMAnomaly) Validate() error {
	if v1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	if !cr.Spec.License.IsProvided() {
		return fmt.Errorf("No license is provided!. Either spec.license.key or spec.license.keyRef is required")
	}
	if cr.Spec.ConfigSecret == cr.ConfigSecretName() {
		return fmt.Errorf("spec.configSecret uses the same name as built-in config secret used by operator. Please change it's name")
	}
	return nil
}

func (cr *VMAnomaly) GetShardCount() int {
	if cr.Spec.ShardCount == nil {
		return 0
	}
	return *cr.Spec.ShardCount
}

func (cr *VMAnomaly) AddShardSettings(dep runtime.Object, shardNum int) {
	if cr == nil || cr.Spec.ShardCount == nil {
		return
	}
	shardCount := *cr.Spec.ShardCount
	var containers []corev1.Container
	switch dep := dep.(type) {
	case *appsv1.StatefulSet:
		containers = dep.Spec.Template.Spec.Containers
		dep.Name = fmt.Sprintf("%s-%d", dep.Name, shardNum)
		// need to mutate selectors ?
		dep.Spec.Selector.MatchLabels["shard-num"] = strconv.Itoa(shardNum)
		dep.Spec.Template.Labels["shard-num"] = strconv.Itoa(shardNum)
	case *appsv1.Deployment:
		containers = dep.Spec.Template.Spec.Containers
		dep.Name = fmt.Sprintf("%s-%d", dep.Name, shardNum)
		// need to mutate selectors ?
		dep.Spec.Selector.MatchLabels["shard-num"] = strconv.Itoa(shardNum)
		dep.Spec.Template.Labels["shard-num"] = strconv.Itoa(shardNum)
	}
	for i := range containers {
		container := &containers[i]
		if container.Name != "vmanomaly" {
			continue
		}
		envs := container.Env[:0]
		for _, env := range container.Env {
			if env.Name != "VMANOMALY_MEMBERS_COUNT" && env.Name != "VMANOMALY_MEMBER_NUM" {
				envs = append(envs, env)
			}
		}
		container.Env = append(container.Env, []corev1.EnvVar{
			{
				Name:  "VMANOMALY_MEMBERS_COUNT",
				Value: strconv.Itoa(shardCount),
			},
			{
				Name:  "VMANOMALY_MEMBER_NUM",
				Value: strconv.Itoa(shardNum),
			},
		}...)
	}
}

// LastAppliedSpecAsPatch return last applied cluster spec as patch annotation
func (cr *VMAnomaly) LastAppliedSpecAsPatch() (client.Patch, error) {
	return v1beta1.LastAppliedChangesAsPatch(cr.ObjectMeta, cr.Spec)
}

// HasSpecChanges compares spec with last applied cluster spec stored in annotation
func (cr *VMAnomaly) HasSpecChanges() (bool, error) {
	return v1beta1.HasStateChanges(cr.ObjectMeta, cr.Spec)
}

func (cr *VMAnomaly) Paused() bool {
	return cr.Spec.Paused
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomaly) UnmarshalJSON(src []byte) error {
	type pcr VMAnomaly
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		return err
	}
	if err := v1beta1.ParseLastAppliedStateTo(cr); err != nil {
		return err
	}
	return nil
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalySpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalySpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse vmanomaly spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyList contains a list of VMAnomaly.
type VMAnomalyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomaly `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMAnomaly{}, &VMAnomalyList{})
}
