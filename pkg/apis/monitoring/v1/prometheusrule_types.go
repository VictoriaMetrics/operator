package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// PrometheusRule defines alerting rules for a Prometheus instance
// +genclient
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PrometheusRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	// Specification of desired alerting rule definitions for Prometheus.
	Spec PrometheusRuleSpec `json:"spec"`
}

// PrometheusRuleList is a list of PrometheusRules.
// +k8s:openapi-gen=true
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type PrometheusRuleList struct {
	metav1.TypeMeta `json:",inline"`
	// Standard list metadata
	// More info: https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#metadata
	metav1.ListMeta `json:"metadata,omitempty"`
	// List of Rules
	// +listType=set
	Items []*PrometheusRule `json:"items"`
}

// PrometheusRuleSpec contains specification parameters for a Rule.
// +k8s:openapi-gen=true
type PrometheusRuleSpec struct {
	// Content of Prometheus rule file
	// +listType=set
	Groups []RuleGroup `json:"groups,omitempty"`
}

// RuleGroup and Rule are copied instead of vendored because the
// upstream Prometheus struct definitions don't have json struct tags.

// RuleGroup is a list of sequentially evaluated recording and alerting rules.
// Note: PartialResponseStrategy is only used by ThanosRuler and will
// be ignored by Prometheus instances.  Valid values for this field are 'warn'
// or 'abort'.  More info: https://github.com/thanos-io/thanos/blob/master/docs/components/rule.md#partial-response
// +k8s:openapi-gen=true
type RuleGroup struct {
	Name     string `json:"name"`
	Interval string `json:"interval,omitempty"`
	// +listType=set
	Rules                   []Rule `json:"rules"`
	PartialResponseStrategy string `json:"partialResponseStrategy,omitempty"`
}

// Rule describes an alerting or recording rule.
// +k8s:openapi-gen=true
type Rule struct {
	Record      string             `json:"record,omitempty"`
	Alert       string             `json:"alert,omitempty"`
	Expr        intstr.IntOrString `json:"expr"`
	For         string             `json:"for,omitempty"`
	Labels      map[string]string  `json:"labels,omitempty"`
	Annotations map[string]string  `json:"annotations,omitempty"`
}



func init() {
	SchemeBuilder.Register(&PrometheusRule{}, &PrometheusRuleList{})
}
