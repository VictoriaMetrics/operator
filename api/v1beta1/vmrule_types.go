package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// VMRuleSpec defines the desired state of VMRule
type VMRuleSpec struct {
	// Groups list of group rules
	Groups []RuleGroup `json:"groups"`
}

// RuleGroup is a list of sequentially evaluated recording and alerting rules.
// +k8s:openapi-gen=true
type RuleGroup struct {
	// Name of group
	Name string `json:"name"`
	// evaluation interval for group
	// +optional
	Interval string `json:"interval,omitempty"`
	// Rules list of alert rules
	Rules []Rule `json:"rules"`
	// Concurrency defines how many rules execute at once.
	// +optional
	Concurrency int `json:"concurrency,omitempty"`
}

// Rule describes an alerting or recording rule.
// +k8s:openapi-gen=true
type Rule struct {
	// Record represents a query, that will be recorded to dataSource
	// +optional
	Record string `json:"record,omitempty"`
	// Alert is a name for alert
	// +optional
	Alert string `json:"alert,omitempty"`
	// Expr is query, that will be evaluated at dataSource
	// +optional
	Expr intstr.IntOrString `json:"expr"`
	// For evaluation interval in time.Duration format
	// 30s, 1m, 1h  or nanoseconds
	// +optional
	For string `json:"for,omitempty"`
	// Labels will be added to rule configuration
	// +optional
	Labels map[string]string `json:"labels,omitempty"`
	// Annotations will be added to rule configuration
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`
}

// VMRuleStatus defines the observed state of VMRule
type VMRuleStatus struct {
}

// VMRule defines rule records for vmalert application
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMRule"
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmrules,scope=Namespaced
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type VMRule struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec VMRuleSpec `json:"spec"`
	// +optional
	Status VMRuleStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VMRuleList contains a list of VMRule
type VMRuleList struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ListMeta `json:"metadata,omitempty"`
	// Items list of VMRule
	Items []*VMRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMRule{}, &VMRuleList{})
}
