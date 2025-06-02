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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// VMAnomalyOnlineQuantileModelSpec defines the desired state of VMAnomalyOnlineQuantileModel.
type VMAnomalyOnlineQuantileModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// The quantiles to estimate. yhat_lower, yhat, yhat_upper are the quantile order. By default (0.01, 0.5, 0.99).
	Quantiles []float64 `json:"quantiles,omitempty"`
	// The interval for the seasonal adjustment. If not set, the model will equal to a simple online quantile model. By default not set.
	SeasonalInterval metav1.Duration `json:"seasonal_interval,omitempty"`
	// The minimum interval to estimate quantiles for. By default not set.
	MinSubseason string `json:"min_subseason"`
	// Whether to internally apply a log1p(abs(x)) * sign(x) transformation to the data to stabilize internal quantile estimation.
	// Does not affect the scale of produced output (i.e. yhat) By default False.
	UseTransform bool `json:"use_transform,omitempty"`
	// The smoothing parameter for the global quantiles. i.e. the output is a weighted average of the global and seasonal quantiles (if seasonal_interval and min_subseason args are set).
	// Should be from [0, 1] interval, where 0 means no smoothing and 1 means using only global quantile values.
	GlobalSmoothing float64 `json:"global_smooth,omitempty"`
	// Is used to adjust the margins between yhat and [yhat_lower, yhat_upper]. New margin = |yhat_* - yhat_lower| * scale. Defaults to 1 (no scaling is applied)
	Scale float64 `json:"scale,omitempty"`
	// The start date for the seasonal adjustment, as a reference point to start counting the intervals. By default ‘1970-01-01’.
	SeasonStartsFrom Time `json:"season_starts_from,omitempty"`
	// the minimum number of samples to be seen (n_samples_seen_ property) before computing the anomaly score.
	// Otherwise, the anomaly score will be 0, as there is not enough data to trust the model’s predictions. Defaults to 16.
	MinSamplesSeen int `json:"min_n_samples_seen,omitempty"`
	// The compression parameter for the underlying t-digests. Higher values mean higher accuracy but higher memory usage. By default 100.
	Compression int `json:"compression,omitempty"`
}

func (s *VMAnomalyOnlineQuantileModel) Validate() error {
	return nil
}

// VMAnomalyOnlineQuantileModelStatus defines the observed state of VMAnomalyOnlineQuantileModel.
type VMAnomalyOnlineQuantileModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyOnlineQuantileModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyOnlineQuantileModel is the Schema for the vmanomalyonlinequantilemodels API.
type VMAnomalyOnlineQuantileModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyOnlineQuantileModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyOnlineQuantileModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyOnlineQuantileModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyOnlineQuantileModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyOnlineQuantileModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyOnlineQuantileModelList contains a list of VMAnomalyOnlineQuantileModel.
type VMAnomalyOnlineQuantileModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyOnlineQuantileModel `json:"items"`
}

func (l *VMAnomalyOnlineQuantileModelList) ItemsList() []VMAnomalyOnlineQuantileModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyOnlineQuantileModel{}, &VMAnomalyOnlineQuantileModelList{})
}
