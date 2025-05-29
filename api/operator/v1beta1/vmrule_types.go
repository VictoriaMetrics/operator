package v1beta1

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/VictoriaMetrics/VictoriaMetrics/app/vmalert/config"
	"github.com/VictoriaMetrics/VictoriaMetrics/app/vmalert/notifier"
	"github.com/VictoriaMetrics/VictoriaMetrics/app/vmalert/templates"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MaxConfigMapDataSize is a maximum `Data` field size of a ConfigMap.
// Limit it to the half size of constant value, since it may be different for kubernetes versions.
var MaxConfigMapDataSize = int(float64(corev1.MaxSecretSize) * 0.5)

var initVMAlertTemplatesOnce sync.Once

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
	Interval string `json:"interval,omitempty" yaml:"interval,omitempty"`
	// Rules list of alert rules
	Rules []Rule `json:"rules"`
	// Limit the number of alerts an alerting rule and series a recording
	// rule can produce
	// +optional
	Limit int `json:"limit,omitempty"`
	// Optional
	// Group will be evaluated at the exact offset in the range of [0...interval].
	EvalOffset string `json:"eval_offset,omitempty" yaml:"eval_offset,omitempty"`
	// Optional
	// Adjust the `time` parameter of group evaluation requests to compensate intentional query delay from the datasource.
	EvalDelay string `json:"eval_delay,omitempty" yaml:"eval_delay,omitempty"`
	// Optional
	// The evaluation timestamp will be aligned with group's interval,
	// instead of using the actual timestamp that evaluation happens at.
	// It is enabled by default to get more predictable results
	// and to visually align with graphs plotted via Grafana or vmui.
	EvalAlignment *bool `json:"eval_alignment,omitempty" yaml:"eval_alignment,omitempty"`
	// Concurrency defines how many rules execute at once.
	// +optional
	Concurrency int `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	// Labels optional list of labels added to every rule within a group.
	// It has priority over the external labels.
	// Labels are commonly used for adding environment
	// or tenant-specific tag.
	// +optional
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	// ExtraFilterLabels optional list of label filters applied to every rule's
	// request within a group. Is compatible only with VM datasource.
	// See more details [here](https://docs.victoriametrics.com/#prometheus-querying-api-enhancements)
	// Deprecated, use params instead
	// +optional
	ExtraFilterLabels map[string]string `json:"extra_filter_labels,omitempty" yaml:"extra_filter_labels,omitempty"`
	// Tenant id for group, can be used only with enterprise version of vmalert.
	// See more details [here](https://docs.victoriametrics.com/vmalert#multitenancy).
	// +optional
	Tenant string `json:"tenant,omitempty" yaml:"tenant,omitempty"`
	// Params optional HTTP URL parameters added to each rule request
	// +optional
	Params url.Values `json:"params,omitempty" yaml:"params,omitempty"`
	// Type defines datasource type for enterprise version of vmalert
	// possible values - prometheus,graphite,vlogs
	// +optional
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// Headers contains optional HTTP headers added to each rule request
	// Must be in form `header-name: value`
	// For example:
	//  headers:
	//    - "CustomHeader: foo"
	//    - "CustomHeader2: bar"
	// +optional
	Headers []string `json:"headers,omitempty"`
	// NotifierHeaders contains optional HTTP headers added to each alert request which will send to notifier
	// Must be in form `header-name: value`
	// For example:
	//  headers:
	//    - "CustomHeader: foo"
	//    - "CustomHeader2: bar"
	// +optional
	NotifierHeaders []string `json:"notifier_headers,omitempty" yaml:"notifier_headers,omitempty"`
}

// Rule describes an alerting or recording rule.
// +k8s:openapi-gen=true
type Rule struct {
	// Record represents a query, that will be recorded to dataSource
	// +optional
	Record string `json:"record,omitempty" yaml:"record,omitempty"`
	// Alert is a name for alert
	// +optional
	Alert string `json:"alert,omitempty" yaml:"alert,omitempty"`
	// Expr is query, that will be evaluated at dataSource
	// +optional
	Expr string `json:"expr" yaml:"expr"`
	// Debug enables logging for rule
	// it useful for tracking
	// +optional
	Debug *bool `json:"debug,omitempty"`
	// For evaluation interval in time.Duration format
	// 30s, 1m, 1h  or nanoseconds
	// +optional
	For string `json:"for,omitempty" yaml:"for,omitempty"`
	// KeepFiringFor will make alert continue firing for this long
	// even when the alerting expression no longer has results.
	// Use time.Duration format, 30s, 1m, 1h  or nanoseconds
	// +optional
	KeepFiringFor string `json:"keep_firing_for,omitempty" yaml:"keep_firing_for,omitempty"`
	// Labels will be added to rule configuration
	// +optional
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	// Annotations will be added to rule configuration
	// +optional
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
	// UpdateEntriesLimit defines max number of rule's state updates stored in memory.
	// Overrides `-rule.updateEntriesLimit` in vmalert.
	// +optional
	UpdateEntriesLimit *int `json:"update_entries_limit,omitempty" yaml:"update_entries_limit,omitempty"`
}

// VMRuleStatus defines the observed state of VMRule
type VMRuleStatus struct {
	StatusMetadata `json:",inline"`
}

// GetStatusMetadata implements reconcile.objectWithStatus interface
func (cr *VMRule) GetStatusMetadata() *StatusMetadata {
	return &cr.Status.StatusMetadata
}

// Validate performs semantic validation of object
func (cr *VMRule) Validate() error {
	if MustSkipCRValidation(cr) {
		return nil
	}
	initVMAlertTemplatesOnce.Do(func() {
		testURL, _ := url.Parse("http://test:8429")
		if err := templates.Load(nil, *testURL); err != nil {
			panic(fmt.Sprintf("cannot init vmalert templates for validation: %s", err))
		}
	})
	uniqNames := make(map[string]struct{})
	var totalSize int
	for i := range cr.Spec.Groups {
		// make a copy
		group := cr.Spec.Groups[i].DeepCopy()
		// remove tenant from copy, it's needed to properly validate it with vmalert lib
		// since tenant is only supported at enterprise code
		if group.Tenant != "" {
			if err := validateRuleGroupTenantID(group.Tenant); err != nil {
				return fmt.Errorf("at idx=%d bad tenant=%q: %w", i, group.Tenant, err)
			}
			group.Tenant = ""
		}
		errContext := fmt.Sprintf("VMRule: %s/%s group: %s", cr.Namespace, cr.Name, group.Name)
		if _, ok := uniqNames[group.Name]; ok {
			return fmt.Errorf("duplicate group name: %s", errContext)
		}
		uniqNames[group.Name] = struct{}{}
		groupBytes, err := yaml.Marshal(group)
		if err != nil {
			return fmt.Errorf("cannot marshal %s, err: %w", errContext, err)
		}
		var vmalertGroup config.Group
		totalSize += len(groupBytes)
		if err := yaml.Unmarshal(groupBytes, &vmalertGroup); err != nil {
			return fmt.Errorf("cannot parse vmalert group %s, err: %w, r: \n%s", errContext, err, string(groupBytes))
		}
		if err := vmalertGroup.Validate(notifier.ValidateTemplates, true); err != nil {
			return fmt.Errorf("validation failed for %s err: %w", errContext, err)
		}
	}
	if totalSize > MaxConfigMapDataSize {
		return fmt.Errorf("VMRule's content size: %d exceed single rule limit: %d", totalSize, MaxConfigMapDataSize)
	}
	return nil
}

func validateRuleGroupTenantID(id string) error {
	ids := strings.TrimSpace(string(id))
	idx := strings.Index(ids, ":")
	if idx < 0 {
		if _, err := strconv.ParseInt(ids, 10, 32); err != nil {
			return fmt.Errorf("cannot parse account_id: %q as int32, err: %w", ids, err)
		}
		return nil
	}
	aIDs := ids[:idx]
	pIDs := ids[idx+1:]
	if _, err := strconv.ParseInt(aIDs, 10, 32); err != nil {
		return fmt.Errorf("cannot parse account_id: %q as int32, err: %w", aIDs, err)
	}
	if _, err := strconv.ParseInt(pIDs, 10, 32); err != nil {
		return fmt.Errorf("cannot parse project_id: %q as int32, err: %w", pIDs, err)
	}
	return nil
}

// VMRule defines rule records for vmalert application
// +operator-sdk:gen-csv:customresourcedefinitions.displayName="VMRule"
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=vmrules,scope=Namespaced
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.updateStatus"
// +kubebuilder:printcolumn:name="Sync Error",type="string",JSONPath=".status.reason"
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
	Items []VMRule `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VMRule{}, &VMRuleList{})
}
