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

// VMAnomalyAutoTunedModelSpec defines the desired state of VMAnomalyAutoTunedModel.
type VMAnomalyAutoTunedModelSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// Built-in model class to tune
	TunedClassName string `json:"tuned_class_name"`
	// Optimization parameters for unsupervised model tuning.
	// Control % of found anomalies, as well as a tradeoff between time spent and the accuracy.
	OptimizationParams VMAnomalyAutoTunedOptimizationParams `json:"optimization_params,omitempty"`
}

func (s *VMAnomalyAutoTunedModel) Validate() error {
	return nil
}

type VMAnomalyAutoTunedOptimizationParams struct {
	// Expected percentage of anomalies that can be seen in training data, from (0, 0.5) interval.
	AnomalyPercentage float64 `json:"anomaly_percentage"`
	// This option allows particular business-specific parameters such as detection_direction or min_dev_from_expected to remain unchanged during optimizations,
	// retaining their default values.
	OptimizedBusinessParams []string `json:"optimized_business_params,omitempty"`
	// Random seed for reproducibility and deterministic nature of underlying optimizations.
	Seed int `json:"seed,omitempty"`
	// How many folds to create for hyperparameter tuning out of your data.
	// The higher, the longer it takes but the better the results can be.
	Splits int `json:"n_splits,omitempty"`
	// How many trials to sample from hyperparameter search space.
	// The higher, the longer it takes but the better the results can be.
	Trails int `json:"n_trails,omitempty"`
	// How many seconds in total can be spent on each model to tune hyperparameters
	Timeout metav1.Duration `json:"timeout,omitempty"`
}

// VMAnomalyAutoTunedModelStatus defines the observed state of VMAnomalyAutoTunedModel.
type VMAnomalyAutoTunedModelStatus struct {
	v1beta1.StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMAnomalyAutoTunedModel) GetStatusMetadata() *v1beta1.StatusMetadata {
	return &cr.Status.StatusMetadata
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// VMAnomalyAutoTunedModel is the Schema for the vmanomalyautotunedmodels API.
type VMAnomalyAutoTunedModel struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMAnomalyAutoTunedModelSpec   `json:"spec,omitempty"`
	Status VMAnomalyAutoTunedModelStatus `json:"status,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMAnomalyAutoTunedModelSpec) UnmarshalJSON(src []byte) error {
	type pcr VMAnomalyAutoTunedModelSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse VMAnomalyAutoTunedModel spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// +kubebuilder:object:root=true

// VMAnomalyAutoTunedModelList contains a list of VMAnomalyAutoTunedModel.
type VMAnomalyAutoTunedModelList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMAnomalyAutoTunedModel `json:"items"`
}

func (l *VMAnomalyAutoTunedModelList) ItemsList() []VMAnomalyAutoTunedModel {
	return l.Items
}

func init() {
	SchemeBuilder.Register(&VMAnomalyAutoTunedModel{}, &VMAnomalyAutoTunedModelList{})
}
