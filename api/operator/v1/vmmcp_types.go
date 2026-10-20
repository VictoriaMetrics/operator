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
	"net/url"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMMCPInstanceType defines type of VictoriaMetrics instance, which is queried by the MCP server
// +kubebuilder:validation:Enum=single;cluster
type VMMCPInstanceType string

const (
	// VMMCPInstanceTypeSingle defines single-node VictoriaMetrics instance
	VMMCPInstanceTypeSingle VMMCPInstanceType = "single"
	// VMMCPInstanceTypeCluster defines VictoriaMetrics cluster
	VMMCPInstanceTypeCluster VMMCPInstanceType = "cluster"
)

// VMMCPSpec defines the desired state of VMMCP
// +k8s:openapi-gen=true
type VMMCPSpec struct {
	// Entrypoint defines root URL of VictoriaMetrics instance, which is queried by the MCP server:
	// VMSingle URL (e.g. http://vmsingle-example.default.svc:8428) for single instance type
	// or vmselect URL (e.g. http://vmselect-example.default.svc:8481) for cluster instance type.
	// Either entrypoint or cloudAPIKey must be set.
	// +kubebuilder:validation:Pattern:="^https?://.+$"
	// +optional
	Entrypoint string `json:"entrypoint,omitempty"`
	// InstanceType defines type of VictoriaMetrics instance set at entrypoint.
	// Defaults to single.
	// +optional
	InstanceType VMMCPInstanceType `json:"instanceType,omitempty"`
	// BearerToken defines a Secret key with a token, which authenticates requests to entrypoint.
	// +optional
	BearerToken *corev1.SecretKeySelector `json:"bearerToken,omitempty"`
	// Headers defines custom HTTP headers in key=value format, which are sent with requests to entrypoint.
	// +optional
	Headers []string `json:"headers,omitempty"`
	// DefaultTenantID defines tenant for requests without explicit tenant to the cluster instance type
	// in accountID or accountID:projectID format.
	// Defaults to 0.
	// +kubebuilder:validation:Pattern:="^[0-9]+(:[0-9]+)?$"
	// +optional
	DefaultTenantID string `json:"defaultTenantID,omitempty"`
	// PassthroughHeaders defines names of HTTP headers, which are forwarded from MCP client requests to entrypoint.
	// They override headers with the same names.
	// It's useful for routing requests by headers with vmauth,
	// see https://docs.victoriametrics.com/victoriametrics/vmauth/#routing-by-header
	// +optional
	PassthroughHeaders []string `json:"passthroughHeaders,omitempty"`
	// CloudAPIKey defines a Secret key with VictoriaMetrics Cloud API key.
	// If it's set, the MCP server works with VictoriaMetrics Cloud deployments instead of entrypoint.
	// See https://docs.victoriametrics.com/victoriametrics-cloud/api/
	// +optional
	CloudAPIKey *corev1.SecretKeySelector `json:"cloudAPIKey,omitempty"`
	// DisableResources disables MCP resources with VictoriaMetrics documentation,
	// while documentation tool keeps working.
	// +optional
	DisableResources bool `json:"disableResources,omitempty"`

	CommonMCPSpec `json:",inline"`
}

// VMMCPStatus defines the observed state of VMMCP
type VMMCPStatus struct {
	vmv1beta1.StatusMetadata `json:",inline"`
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	LastAppliedSpec *VMMCPSpec `json:"lastAppliedSpec,omitempty"`
	// ParsingSpecError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingSpecError string `json:"-" yaml:"-"`
}

// GetStatusMetadata returns metadata for object status
func (cr *VMMCP) GetStatusMetadata() *vmv1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// VMMCP is the MCP server for VictoriaMetrics, which provides AI assistants with access to VictoriaMetrics APIs and documentation.
// See https://github.com/VictoriaMetrics/mcp-victoriametrics
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMMCP App"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Deployment,apps"
// +operator-sdk:gen-csv:customresourcedefinitions.resources="Service,v1"
// +genclient
// +k8s:openapi-gen=true
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmmcps,scope=Namespaced
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus",description="Current status of update rollout"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// VMMCP is the Schema for the API
type VMMCP struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMMCPSpec   `json:"spec,omitempty"`
	Status VMMCPStatus `json:"status,omitempty"`
}

// GetStatus implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMMCP) GetStatus() *VMMCPStatus {
	return &cr.Status
}

// DefaultStatusFields implements reconcile.ObjectWithDeepCopyAndStatus interface
func (cr *VMMCP) DefaultStatusFields(vs *VMMCPStatus) {
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMMCP) UnmarshalJSON(src []byte) error {
	type pcr VMMCP
	type shadow struct {
		*pcr
		Spec json.RawMessage `json:"spec"`
	}
	s := shadow{pcr: (*pcr)(cr)}
	if err := json.Unmarshal(src, &s); err != nil {
		return err
	}
	if len(s.Spec) > 0 {
		if err := vmv1beta1.UnmarshalSpecStrict(s.Spec, &cr.Spec); err != nil {
			cr.Status.ParsingSpecError = fmt.Sprintf("cannot parse VMMCPSpec: %s, err: %s", string(s.Spec), err)
		}
	}
	return nil
}

// +kubebuilder:object:root=true

// VMMCPList contains a list of VMMCP
type VMMCPList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMMCP `json:"items"`
}

// PodAnnotations returns annotations attached to the podMetadata
func (cr *VMMCP) PodAnnotations() map[string]string {
	return cr.Spec.podAnnotations()
}

// AsOwner returns owner references with current object as owner
func (cr *VMMCP) AsOwner() metav1.OwnerReference {
	return mcpOwnerReference(cr.TypeMeta, cr.ObjectMeta)
}

// ProbePath implements build.probeCRD interface
func (cr *VMMCP) ProbePath() string {
	return mcpReadinessPath
}

// ProbeScheme implements build.probeCRD interface
func (cr *VMMCP) ProbeScheme() string {
	return string(corev1.URISchemeHTTP)
}

// ProbePort implements build.probeCRD interface
func (cr *VMMCP) ProbePort() string {
	return cr.Spec.Port
}

// ProbeNeedLiveness implements build.probeCRD interface
func (cr *VMMCP) ProbeNeedLiveness() bool {
	return true
}

// UseProxyProtocol implements build.probeCRD interface
func (cr *VMMCP) UseProxyProtocol() bool {
	return false
}

// FinalAnnotations returns annotations to be applied for created objects
func (cr *VMMCP) FinalAnnotations() map[string]string {
	return cr.Spec.finalAnnotations()
}

// SelectorLabels returns unique labels for object
func (cr *VMMCP) SelectorLabels() map[string]string {
	return mcpSelectorLabels("vmmcp", cr.Name)
}

// PodLabels returns labels attached to the podMetadata
func (cr *VMMCP) PodLabels() map[string]string {
	return cr.Spec.podLabels(cr.SelectorLabels())
}

// FinalLabels returns combination of selector and managed labels
func (cr *VMMCP) FinalLabels() map[string]string {
	return cr.ResourceLabels(nil)
}

// ResourceLabels returns combination of selector, resource and managed labels
func (cr *VMMCP) ResourceLabels(input map[string]string) map[string]string {
	return cr.Spec.resourceLabels(cr.SelectorLabels(), input)
}

// PrefixedName format name of the component with hard-coded prefix
func (cr *VMMCP) PrefixedName() string {
	return fmt.Sprintf("vmmcp-%s", cr.Name)
}

// GetMetricsPath returns prefixed path for metric requests
func (cr *VMMCP) GetMetricsPath() string {
	return metricsPath
}

// UseTLS returns true if TLS is enabled
func (cr *VMMCP) UseTLS() bool {
	return false
}

// GetExtraArgs returns additionally configured command-line arguments
func (cr *VMMCP) GetExtraArgs() map[string]string {
	return cr.Spec.ExtraArgs
}

// GetServiceScrape returns overrides for serviceScrape builder
func (cr *VMMCP) GetServiceScrape() *vmv1beta1.VMServiceScrapeSpec {
	return cr.Spec.ServiceScrapeSpec
}

// GetServiceAccountName returns service account name for components
func (cr *VMMCP) GetServiceAccountName() string {
	return cr.Spec.serviceAccountName(cr.PrefixedName())
}

// IsOwnsServiceAccount checks if ServiceAccountName is set explicitly
func (cr *VMMCP) IsOwnsServiceAccount() bool {
	return cr.Spec.ServiceAccountName == ""
}

// GetAdditionalService returns AdditionalServiceSpec settings
func (cr *VMMCP) GetAdditionalService() *vmv1beta1.AdditionalServiceSpec {
	return cr.Spec.ServiceSpec
}

// AsURL returns URL for components access
func (cr *VMMCP) AsURL(isExtra bool) string {
	return cr.Spec.asURL(cr.PrefixedName(), cr.Namespace, isExtra)
}

// Paused checks if resource reconcile should be paused
func (cr *VMMCP) Paused() bool {
	return cr.Spec.Paused
}

// LastSpecUpdated compares spec with last applied spec stored, replaces old spec and returns true if it's updated
func (cr *VMMCP) LastSpecUpdated() bool {
	updated := cr.Status.LastAppliedSpec == nil || !equality.Semantic.DeepEqual(&cr.Spec, cr.Status.LastAppliedSpec)
	cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
	return updated
}

// Validate checks if spec is correct
func (cr *VMMCP) Validate() error {
	if vmv1beta1.MustSkipCRValidation(cr) {
		return nil
	}
	spec := &cr.Spec
	switch {
	case spec.Entrypoint == "" && spec.CloudAPIKey == nil:
		return fmt.Errorf("either spec.entrypoint or spec.cloudAPIKey must be set")
	case spec.Entrypoint != "" && spec.CloudAPIKey != nil:
		return fmt.Errorf("spec.entrypoint and spec.cloudAPIKey cannot be set at the same time")
	}
	if spec.Entrypoint != "" {
		// the CRD pattern checks only the scheme
		u, err := url.Parse(spec.Entrypoint)
		if err != nil {
			return fmt.Errorf("incorrect spec.entrypoint syntax: %q: %w", spec.Entrypoint, err)
		}
		if u.Hostname() == "" {
			return fmt.Errorf("spec.entrypoint=%q must have a host", spec.Entrypoint)
		}
	}
	if err := validateMCPHeaders(spec.Headers); err != nil {
		return err
	}
	if err := validateMCPListItems("passthroughHeaders", spec.PassthroughHeaders); err != nil {
		return err
	}
	return spec.validate(cr.PrefixedName())
}
