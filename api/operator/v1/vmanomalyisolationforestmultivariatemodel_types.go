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

// VMAnomalyIsolationForestMultivariateModelSpec defines the desired state of VMAnomalyIsolationForestMultivariateModel.
type VMAnomalyIsolationForestMultivariateModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// The amount of contamination of the data set, i.e. the proportion of outliers in the data set.
	// Used when fitting to define the threshold on the scores of the samples.
	Contamination string `json:"contamination,omitempty"`
	// List of seasonality to encode through cyclical encoding , i.e. dow (day of week)
	SeasonalFeatures []string `json:"seasonal_features,omitempty"`
	// Inner model args (key-value pairs). See accepted params in model documentation
	Args Args `json:"args,omitempty"`
}

func (s *VMAnomalyIsolationForestMultivariateModel) Validate() error {
	return nil
}

// VMAnomalyIsolationForestMultivariateModelStatus defines the observed state of VMAnomalyIsolationForestMultivariateModel.
type VMAnomalyIsolationForestMultivariateModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyIsolationForestMultivariateModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyIsolationForestMultivariateModel is the Schema for the vmanomalyisolationforestmultivariatemodels API.
type VMAnomalyIsolationForestMultivariateModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyIsolationForestMultivariateModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyIsolationForestMultivariateModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyIsolationForestMultivariateModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyIsolationForestMultivariateModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyIsolationForestMultivariateModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyIsolationForestMultivariateModelList contains a list of VMAnomalyIsolationForestMultivariateModel.
type VMAnomalyIsolationForestMultivariateModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyIsolationForestMultivariateModel `json:"items"`
}

func (l *VMAnomalyIsolationForestMultivariateModelList) ItemsList() []VMAnomalyIsolationForestMultivariateModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyIsolationForestMultivariateModel{}, &VMAnomalyIsolationForestMultivariateModelList{})
}
