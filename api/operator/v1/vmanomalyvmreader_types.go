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

// VMAnomalyVMReaderSpec defines the desired state of VMAnomalyVMReader.
type VMAnomalyVMReaderSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Datasource URL address
	DatasourceURL string `json:"datasource_url"`
	// For VictoriaMetrics Cluster version only, tenants are identified by accountID, accountID:projectID or multitenant.
	TenantID string `json:"tenant_id,omitempty"`
	// Frequency of the points returned
	SamplingPeriod metav1.Duration `json:"sampling_duration,omitempty"`
	// Performs PromQL/MetricsQL range query
	QueryRangePath string `json:"query_range_path,omitempty"`
	// Absolute or relative URL address where to check availability of the datasource.
	HealthPath        string `json:"health_path,omitempty"`
	v1beta1.BasicAuth `json:",inline"`
	// Timeout for the requests, passed as a string
	Timeout           metav1.Duration `json:"timeout,omitempty"`
	v1beta1.TLSConfig `json:",inline"`
	BearerToken       *corev1.SecretKeySelector `json:"bearer_token,omitempty"`
	// Path to a file, which contains token, that is passed in the standard format with header
	BearerTokenFile string `json:"bearer_token_file,omitempty"`
	// List of strings with series selector.
	ExtraFilters []string `json:"extra_filters,omitempty"`
	// If True, then query will be performed from the last seen timestamp for a given series.
	QueryFromLastSeenTimestamp bool `json:"query_from_last_seen_timestamp,omitempty"`
	// It allows overriding the default -search.latencyOffsetflag of VictoriaMetrics
	LatencyOffset metav1.Duration `json:"latency_duration,omitempty"`
	// Optional argoverrides how search.maxPointsPerTimeseries flagimpacts vmanomaly on splitting long fit_window queries into smaller sub-intervals
	MaxPointsPerQuery int `json:"max_points_per_query,omitempty"`
	// Optional argumentspecifies the IANA timezone to account for local shifts, like DST, in models sensitive to seasonal patterns
	Timezone Location `json:"tz,omitempty"`
	// Optional argumentallows defining valid data ranges for input of all the queries in queries
	DataRange []float64 `json:"data_range,omitempty"`
}

func (s *VMAnomalyVMReader) Validate() error {
	return nil
}

// VMAnomalyVMReaderStatus defines the observed state of VMAnomalyVMReader.
type VMAnomalyVMReaderStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyVMReader is the Schema for the vmanomalyvmreaders API.
type VMAnomalyVMReader struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyVMReaderSpec   `json:"spec,omitempty"`
	Status VMAnomalyVMReaderStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyVMReaderSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyVMReaderSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyVMReader spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyVMReaderList contains a list of VMAnomalyVMReader.
type VMAnomalyVMReaderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyVMReader `json:"items"`
}

func (l *VMAnomalyVMReaderList) ItemsList() []VMAnomalyVMReader {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyVMReader{}, &VMAnomalyVMReaderList{})
}
