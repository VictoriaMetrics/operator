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

// VMAnomalyProphetModelSpec defines the desired state of VMAnomalyProphetModel.
type VMAnomalyProphetModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Additional seasonal components to include in Prophet.
	Seasonalities *VMAnomalyProphetModelSeasonality `json:"seasonality,omitempty"`
	// Specifies timezone-aware seasonal components. Requires tz_aware=True.
	// Supported options include minute, hod (hour of day), dow (day of week), and month (month of year).
	TZSeasonalities *VMAnomalyProphetModelSeasonality `json:"tz_seasonality,omitempty"`
	// Scale is used to adjust the margins between yhat and [yhat_lower, yhat_upper]
	Scale float64 `json:"scale"`
	// Enables handling of timezone-aware timestamps.
	TZAware bool `json:"tz_aware,omitempty"`
	// If set to True, applies cyclical encoding technique to timezone-aware seasonalities.
	// Should be used with tz_aware=True and tz_seasonalities.
	TZUseCyclicalEncoding bool `json:"tz_use_cyclical_encoding,omitempty"`
}

func (s *VMAnomalyProphetModel) Validate() error {
	return nil
}

type VMAnomalyProphetModelSeasonality struct {
	Name         string  `json:"name"`
	Period       float64 `json:"period"`
	FourierOrder int     `json:"fourier_order,omitempty"`
	PriorScale   int     `json:"prior_scale,omitempty"`
}

// VMAnomalyProphetModelStatus defines the observed state of VMAnomalyProphetModel.
type VMAnomalyProphetModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyProphetModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyProphetModel is the Schema for the vmanomalyprophetmodels API.
type VMAnomalyProphetModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyProphetModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyProphetModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyProphetModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyProphetModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyProphetModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyProphetModelList contains a list of VMAnomalyProphetModel.
type VMAnomalyProphetModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyProphetModel `json:"items"`
}

func (l *VMAnomalyProphetModelList) ItemsList() []VMAnomalyProphetModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyProphetModel{}, &VMAnomalyProphetModelList{})
}
