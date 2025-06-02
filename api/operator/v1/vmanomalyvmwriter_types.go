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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMAnomalyVMWriterSpec defines the desired state of VMAnomalyVMWriter.
type VMAnomalyVMWriterSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Datasource URL address
	DatasourceURL string `json:"datasource_url"`
	// For VictoriaMetrics Cluster version only, tenants are identified by accountID, accountID:projectID or multitenant.
	TenantID string `json:"tenant_id,omitempty"`
	// Metrics to save the output (in metric names or labels). Must have __name__ key.
	// Must have a value with $VAR placeholder in it to distinguish between resulting metrics
	MetricFormat *VMAnomalyVMWriterMetricFormat `json:"metric_format,omitempty"`
	// Absolute or relative URL address where to check availability of the datasource.
	HealthPath        string `json:"health_path,omitempty"`
	v1beta1.BasicAuth `json:",inline"`
	// Timeout for the requests, passed as a string
	Timeout           metav1.Duration `json:"timeout,omitempty"`
	v1beta1.TLSConfig `json:",inline"`
	BearerToken       *corev1.SecretKeySelector `json:"bearer_token,omitempty"`
	// Path to a file, which contains token, that is passed in the standard format with header
	BearerTokenFile string `json:"bearer_token_file,omitempty"`
}

type VMAnomalyVMWriterMetricFormat struct {
	Name   string            `json:"__name__"`
	For    string            `json:"for"`
	Labels map[string]string `json:",inline"`
}

func (s *VMAnomalyVMWriter) Validate() error {
	return nil
}

// VMAnomalyVMWriterStatus defines the observed state of VMAnomalyVMWriter.
type VMAnomalyVMWriterStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyVMWriter) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyVMWriter is the Schema for the vmanomalyvmwriters API.
type VMAnomalyVMWriter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyVMWriterSpec   `json:"spec,omitempty"`
	Status VMAnomalyVMWriterStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VMAnomalyVMWriterList contains a list of VMAnomalyVMWriter.
type VMAnomalyVMWriterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyVMWriter `json:"items"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyVMWriterSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyVMWriterSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyVMWriter spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

func (l *VMAnomalyVMWriterList) ItemsList() []VMAnomalyVMWriter {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyVMWriter{}, &VMAnomalyVMWriterList{})
}
