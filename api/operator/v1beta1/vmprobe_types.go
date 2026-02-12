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

package v1beta1

import (
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ json.Unmarshaler = (*VMProbeSpec)(nil)

// VMProbeSpec contains specification parameters for a Probe.
// +k8s:openapi-gen=true
type VMProbeSpec struct {
	// ParsingError contents error with context if operator was failed to parse json object from kubernetes api server
	ParsingError string `json:"-" yaml:"-"`
	// The job name assigned to scraped metrics by default.
	JobName string `json:"jobName,omitempty"`
	// Specification for the prober to use for probing targets.
	// The prober.URL parameter is required. Targets cannot be probed if left empty.
	VMProberSpec VMProberSpec `json:"vmProberSpec"`
	// The module to use for probing specifying how to probe the target.
	// Example module configuring in the blackbox exporter:
	// https://github.com/prometheus/blackbox_exporter/blob/master/example.yml
	Module string `json:"module,omitempty"`
	// Targets defines a set of static and/or dynamically discovered targets to be probed using the prober.
	Targets VMProbeTargets `json:"targets,omitempty"`
	// MetricRelabelConfigs to apply to samples after scrapping.
	// +optional
	MetricRelabelConfigs []*RelabelConfig `json:"metricRelabelConfigs,omitempty"`

	EndpointAuth         `json:",inline"`
	EndpointScrapeParams `json:",inline"`
	// ScrapeClass defined scrape class to apply
	// +optional
	ScrapeClassName *string `json:"scrapeClass,omitempty"`
}

// UnmarshalJSON implements json.Unmarshaler interface
func (cr *VMProbeSpec) UnmarshalJSON(src []byte) error {
	type pcr VMProbeSpec
	if err := json.Unmarshal(src, (*pcr)(cr)); err != nil {
		cr.ParsingError = fmt.Sprintf("cannot parse spec: %s, err: %s", string(src), err)
		return nil
	}
	return nil
}

// VMProbeTargets defines a set of static and dynamically discovered targets for the prober.
// +k8s:openapi-gen=true
type VMProbeTargets struct {
	// StaticConfig defines static targets which are considers for probing.
	// +deprecated={deprecated_in: "v0.68.0", removed_in: "v0.71.0", replacements: {static}}
	// +optional
	StaticConfig *VMProbeTargetStatic `json:"staticConfig,omitempty"`
	// Static defines static targets which are considers for probing.
	// +optional
	Static *VMProbeTargetStatic `json:"static,omitempty"`
	// Kubernetes defines kubernetes targets, which are considered for probing.
	// +optional
	Kubernetes []*VMProbeTargetKubernetes `json:"kubernetes,omitempty"`
	// Ingress defines the set of dynamically discovered Ingress objects which hosts are considered for probing.
	// +deprecated={deprecated_in: "v0.68.0", removed_in: "v0.71.0", replacements: {kubernetes}}
	// +optional
	Ingress *VMProbeTargetKubernetes `json:"ingress,omitempty"`
}

// VMProbeTargetStatic defines the set of static targets considered for probing.
// +k8s:openapi-gen=true
type VMProbeTargetStatic struct {
	// Targets is a list of URLs to probe using the configured prober.
	Targets []string `json:"targets"`
	// Labels assigned to all metrics scraped from the targets.
	Labels map[string]string `json:"labels,omitempty"`
	// RelabelConfigs to apply to samples during service discovery.
	RelabelConfigs []*RelabelConfig `json:"relabelingConfigs,omitempty"`
}

// VMProbeTargetKubernetes defines the set of k8s objects considered for probing.
// +k8s:openapi-gen=true
type VMProbeTargetKubernetes struct {
	// Role defines k8s role name
	// +kubebuilder:validation:Enum=service;ingress;pod;node
	Role string `json:"role,omitempty"`
	// Select k8s objects by labels.
	Selector metav1.LabelSelector `json:"selector,omitempty"`
	// Select k8s objects by namespace.
	NamespaceSelector NamespaceSelector `json:"namespaceSelector,omitempty"`
	// RelabelConfigs to apply to samples during service discovery.
	RelabelConfigs []*RelabelConfig `json:"relabelingConfigs,omitempty"`
}

func (t *VMProbeTargetKubernetes) validate(prefix string) error {
	if t == nil {
		return nil
	}
	if _, err := metav1.LabelSelectorAsSelector(&t.Selector); err != nil {
		return fmt.Errorf("invalid %s.selector: %w", prefix, err)
	}
	if err := checkRelabelConfigs(t.RelabelConfigs); err != nil {
		return fmt.Errorf("invalid %s.relabelingConfigs: %w", prefix, err)
	}
	return nil
}

// VMProberSpec contains specification parameters for the Prober used for probing.
// +k8s:openapi-gen=true
type VMProberSpec struct {
	// Mandatory URL of the prober.
	URL string `json:"url"`
	// HTTP scheme to use for scraping.
	// Defaults to `http`.
	// +optional
	// +kubebuilder:validation:Enum=http;https
	Scheme string `json:"scheme,omitempty"`
	// Path to collect metrics from.
	// Defaults to `/probe`.
	Path string `json:"path,omitempty"`
}

// VMProbe defines a probe for targets, that will be executed with prober,
// like blackbox exporter.
// It helps to monitor reachability of target with various checks.
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus"
// +kubebuilder:printcolumn:name="Sync Error",type="string",JSONPath=".status.reason"
// +genclient
// +k8s:openapi-gen=true
type VMProbe struct {
	// +optional
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VMProbeSpec        `json:"spec"`
	Status ScrapeObjectStatus `json:"status,omitempty"`
}

// VMProbeList contains a list of VMProbe
// +kubebuilder:object:root=true
type VMProbeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VMProbe `json:"items"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMProbe) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
}

// AsKey returns unique key for object
func (cr *VMProbe) AsKey(_ bool) string {
	return cr.Namespace + "/" + cr.Name
}

// Validate returns error if CR is invalid
func (cr *VMProbe) Validate() error {
	if MustSkipCRValidation(cr) {
		return nil
	}
	if err := checkRelabelConfigs(cr.Spec.MetricRelabelConfigs); err != nil {
		return fmt.Errorf("invalid metricRelabelConfigs: %w", err)
	}
	for i, t := range cr.Spec.Targets.Kubernetes {
		if len(t.Role) == 0 {
			return fmt.Errorf("spec.targets.kubernetes[%d].role is required", i)
		}
		if err := t.validate(fmt.Sprintf("spec.targets.kubernetes[%d]", i)); err != nil {
			return err
		}
	}
	if cr.Spec.Targets.StaticConfig != nil && cr.Spec.Targets.Static != nil {
		return fmt.Errorf("both spec.targets.staticConfig and spec.targets.static are set, which is not allowed. Use spec.targets.static")
	}
	if cr.Spec.Targets.StaticConfig != nil {
		if err := checkRelabelConfigs(cr.Spec.Targets.StaticConfig.RelabelConfigs); err != nil {
			return fmt.Errorf("invalid spec.targets.staticConfig.relabelingConfigs: %w", err)
		}
	}
	if cr.Spec.Targets.Static != nil {
		if err := checkRelabelConfigs(cr.Spec.Targets.Static.RelabelConfigs); err != nil {
			return fmt.Errorf("invalid spec.targets.static.relabelingConfigs: %w", err)
		}
	}
	return cr.Spec.Targets.Ingress.validate("spec.targets.ingress")
}

func init() {
	SchemeBuilder.Register(&VMProbe{}, &VMProbeList{})
}
