package vmalert

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-test/deep"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestSelectRules(t *testing.T) {
	tests := []struct {
		name              string
		cr                *vmv1beta1.VMAlert
		predefinedObjects []runtime.Object
		want              map[string]string
		wantErr           bool
	}{
		{
			name: "select default rule",
			cr:   &vmv1beta1.VMAlert{},
			want: map[string]string{
				"default-vmalert.yaml": `
groups:
- name: vmAlertGroup
  rules:
     - alert: error writing to remote
       for: 1m
       expr: rate(vmalert_remotewrite_errors_total[1m]) > 0
       labels:
         host: "{{ $labels.instance }}"
       annotations:
         summary: " error writing to remote writer from vmaler{{ $value|humanize }}"
         description: "error writing to remote writer from vmalert {{$labels}}"
         back: "error rate is ok at vmalert "
`,
			},
		},
		{
			name: "select default rule additional rule from another namespace",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
				Spec:       vmv1beta1.VMAlertSpec{RuleNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{}}, RuleSelector: &metav1.LabelSelector{}},
			},
			predefinedObjects: []runtime.Object{
				// we need namespace for filter + object inside this namespace
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&vmv1beta1.VMRule{ObjectMeta: metav1.ObjectMeta{Name: "error-alert", Namespace: "default"}, Spec: vmv1beta1.VMRuleSpec{
					Groups: []vmv1beta1.RuleGroup{{
						Name: "error-alert", Interval: "10s",
						Concurrency:   1,
						EvalOffset:    "10s",
						EvalDelay:     "40s",
						EvalAlignment: ptr.To(false),
						Rules: []vmv1beta1.Rule{
							{Alert: "alerting", Expr: "10", For: "10s", Labels: nil, Annotations: nil},
						},
					}},
				}},
			},
			want: map[string]string{
				"default-error-alert.yaml": `groups:
- concurrency: 1
  eval_alignment: false
  eval_delay: 40s
  eval_offset: 10s
  name: error-alert
  interval: 10s
  rules:
  - alert: alerting
    expr: "10"
    for: 10s
`,
			},
		},
		{
			name: "select default rule, and additional rule from another namespace with namespace filter",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
				Spec:       vmv1beta1.VMAlertSpec{RuleNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"monitoring": "enabled"}}, RuleSelector: &metav1.LabelSelector{}},
			},
			predefinedObjects: []runtime.Object{
				// we need namespace for filter + object inside this namespace
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring", Labels: map[string]string{"monitoring": "enabled"}}},
				&vmv1beta1.VMRule{ObjectMeta: metav1.ObjectMeta{Name: "error-alert", Namespace: "default"}, Spec: vmv1beta1.VMRuleSpec{
					Groups: []vmv1beta1.RuleGroup{{Name: "error-alert", Interval: "10s", Rules: []vmv1beta1.Rule{
						{Record: "recording", Expr: "10", For: "10s", Labels: nil, Annotations: nil},
					}}},
				}},
				&vmv1beta1.VMRule{ObjectMeta: metav1.ObjectMeta{Name: "error-alert-at-monitoring", Namespace: "monitoring"}, Spec: vmv1beta1.VMRuleSpec{
					Groups: []vmv1beta1.RuleGroup{{Name: "error-alert", Interval: "10s", Rules: []vmv1beta1.Rule{
						{Alert: "alerting-2", Expr: "10", For: "10s", Labels: nil, Annotations: nil},
					}}},
				}},
			},
			want: map[string]string{"monitoring-error-alert-at-monitoring.yaml": `groups:
- name: error-alert
  interval: 10s
  rules:
  - alert: alerting-2
    expr: "10"
    for: 10s
`},
		},
		{
			name: "select all rules with select all",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
				Spec: vmv1beta1.VMAlertSpec{
					SelectAllByDefault: true,
				},
			},
			predefinedObjects: []runtime.Object{
				// we need namespace for filter + object inside this namespace
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring", Labels: map[string]string{"monitoring": "enabled"}}},
				&vmv1beta1.VMRule{
					ObjectMeta: metav1.ObjectMeta{Name: "error-alert", Namespace: "default"},
					Spec: vmv1beta1.VMRuleSpec{
						Groups: []vmv1beta1.RuleGroup{{Name: "error-alert", Interval: "10s", Rules: []vmv1beta1.Rule{
							{Alert: "err indicator", Expr: "rate(err_metric[1m]) > 10", For: "10s", Labels: nil, Annotations: nil},
						}}},
					},
				},
				&vmv1beta1.VMRule{
					ObjectMeta: metav1.ObjectMeta{Name: "error-alert-at-monitoring", Namespace: "monitoring"},
					Spec: vmv1beta1.VMRuleSpec{
						Groups: []vmv1beta1.RuleGroup{{Name: "error-alert", Interval: "10s", Rules: []vmv1beta1.Rule{
							{Alert: "alerting-2", Expr: "10", For: "10s", Labels: nil, Annotations: nil},
						}}},
					},
				},
			},
			want: map[string]string{
				"default-error-alert.yaml": `groups:
- name: error-alert
  interval: 10s
  rules:
  - alert: err indicator
    expr: rate(err_metric[1m]) > 10
    for: 10s
`,
				"monitoring-error-alert-at-monitoring.yaml": `groups:
- name: error-alert
  interval: 10s
  rules:
  - alert: alerting-2
    expr: "10"
    for: 10s
`,
			},
		},
		{
			name: "select none by default",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitoring"},
				Spec: vmv1beta1.VMAlertSpec{
					SelectAllByDefault: false,
				},
			},
			predefinedObjects: []runtime.Object{
				// we need namespace for filter + object inside this namespace
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring", Labels: map[string]string{"monitoring": "enabled"}}},
				&vmv1beta1.VMRule{
					ObjectMeta: metav1.ObjectMeta{Name: "error-alert", Namespace: "default"},
					Spec: vmv1beta1.VMRuleSpec{
						Groups: []vmv1beta1.RuleGroup{{Name: "error-alert", Interval: "10s", Rules: []vmv1beta1.Rule{
							{Alert: "", Expr: "10", For: "10s", Labels: nil, Annotations: nil},
						}}},
					},
				},
				&vmv1beta1.VMRule{
					ObjectMeta: metav1.ObjectMeta{Name: "error-alert-at-monitoring", Namespace: "monitoring"},
					Spec: vmv1beta1.VMRuleSpec{
						Groups: []vmv1beta1.RuleGroup{{Name: "error-alert", Interval: "10s", Rules: []vmv1beta1.Rule{
							{Alert: "", Expr: "10", For: "10s", Labels: nil, Annotations: nil},
						}}},
					},
				},
			},
			want: map[string]string{"default-vmalert.yaml": `
groups:
- name: vmAlertGroup
  rules:
     - alert: error writing to remote
       for: 1m
       expr: rate(vmalert_remotewrite_errors_total[1m]) > 0
       labels:
         host: "{{ $labels.instance }}"
       annotations:
         summary: " error writing to remote writer from vmaler{{ $value|humanize }}"
         description: "error writing to remote writer from vmalert {{$labels}}"
         back: "error rate is ok at vmalert "
`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, _, err := selectRulesContent(ctx, fclient, tt.cr)
			if (err != nil) != tt.wantErr {
				t.Errorf("SelectRules() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			for ruleName, content := range got {
				if !assert.Equal(t, tt.want[ruleName], content) {
					t.Errorf("SelectRules() got = %v, want %v", content, tt.want[ruleName])
				}
			}
		})
	}
}

func TestCreateOrUpdateRuleConfigMaps(t *testing.T) {
	tests := []struct {
		name              string
		cr                *vmv1beta1.VMAlert
		want              []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "base-rules-empty",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "base-vmalert",
				},
			},
		},

		{
			name: "base-rules-gen-with-selector",
			cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "base-vmalert",
				},
				Spec: vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
			},
			want: []string{"vm-base-vmalert-rulefiles-0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := CreateOrUpdateRuleConfigMaps(context.TODO(), fclient, tt.cr, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("CreateOrUpdateRuleConfigMaps() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CreateOrUpdateRuleConfigMaps() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_deduplicateRules(t *testing.T) {
	tests := []struct {
		name   string
		origin []*vmv1beta1.VMRule
		want   []*vmv1beta1.VMRule
	}{
		{
			name: "dedup group",
			origin: []*vmv1beta1.VMRule{
				{
					Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{
						{
							Name: "group-1",
							Rules: []vmv1beta1.Rule{
								{
									Alert: "alert1",
								},
							},
						},
						{
							Name: "group-2",
							Rules: []vmv1beta1.Rule{
								{
									Alert: "alert1",
								},
							},
						},
					}},
				},
			},
			want: []*vmv1beta1.VMRule{
				{
					Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{
						{
							Name: "group-1",
							Rules: []vmv1beta1.Rule{
								{
									Alert: "alert1",
								},
							},
						},
						{
							Name: "group-2",
							Rules: []vmv1beta1.Rule{
								{
									Alert: "alert1",
								},
							},
						},
					}},
				},
			},
		},
		{
			name: "dedup group rule",
			origin: []*vmv1beta1.VMRule{
				{
					Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{
						{
							Name: "group-1",
							Rules: []vmv1beta1.Rule{
								{
									Alert: "alert1",
								},
							},
						},
						{
							Name: "group-2-with-duplicate",
							Rules: []vmv1beta1.Rule{
								{
									Alert:  "alert2",
									Labels: map[string]string{"label1": "value1"},
								},
								{
									Alert: "alert2",
								},
								{
									Alert:  "alert2",
									Labels: map[string]string{"label1": "value1"},
								},
							},
						},
					}},
				},
			},
			want: []*vmv1beta1.VMRule{
				{
					Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{
						{
							Name: "group-1",
							Rules: []vmv1beta1.Rule{
								{
									Alert: "alert1",
								},
							},
						},
						{
							Name: "group-2-with-duplicate",
							Rules: []vmv1beta1.Rule{
								{
									Alert:  "alert2",
									Labels: map[string]string{"label1": "value1"},
								},
								{
									Alert: "alert2",
								},
							},
						},
					}},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deduplicateRules(context.Background(), tt.origin)
			diff := deep.Equal(got, tt.want)
			if len(diff) > 0 {
				t.Errorf("deduplicateRules() %v = %v, want %v", diff, got, tt.want)
			}
		})
	}
}

func Test_rulesCMDiff(t *testing.T) {
	tests := []struct {
		name       string
		currentCMs []corev1.ConfigMap
		newCMs     []corev1.ConfigMap
		toCreate   []corev1.ConfigMap
		toUpdate   []corev1.ConfigMap
	}{
		{
			name:       "create one new",
			currentCMs: []corev1.ConfigMap{},
			newCMs: []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rules-cm-1",
					},
				},
			},
			toCreate: []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rules-cm-1",
					},
				},
			},
		},
		{
			name: "skip exist",
			currentCMs: []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rules-cm-1",
					},
				},
			},
			newCMs: []corev1.ConfigMap{},
		},
		{
			name: "update one",
			currentCMs: []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rules-cm-1",
					},
				},
			},
			newCMs: []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rules-cm-1",
					},
					Data: map[string]string{"rule": "content"},
				},
			},
			toUpdate: []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "rules-cm-1",
						Annotations: map[string]string{},
						Finalizers:  []string{vmv1beta1.FinalizerName},
					},
					Data: map[string]string{"rule": "content"},
				},
			},
		},
		{
			name: "update two",
			currentCMs: []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "rules-cm-0",
						Annotations: map[string]string{},
						Finalizers:  []string{vmv1beta1.FinalizerName},
					},
					Data: map[string]string{"rule": "outdated-content"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "rules-cm-1",
						Annotations: map[string]string{},
						Finalizers:  []string{vmv1beta1.FinalizerName},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rules-cm-3",
					},
				},
			},
			newCMs: []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "rules-cm-0",
						Annotations: map[string]string{},
						Finalizers:  []string{vmv1beta1.FinalizerName},
					},
					Data: map[string]string{"rule": "new-content"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "rules-cm-1",
						Annotations: map[string]string{},
						Finalizers:  []string{vmv1beta1.FinalizerName},
					},
					Data: map[string]string{"rule": "new-content"},
				},
			},
			toUpdate: []corev1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "rules-cm-0",
						Annotations: map[string]string{},
						Finalizers:  []string{vmv1beta1.FinalizerName},
					},
					Data: map[string]string{"rule": "new-content"},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:        "rules-cm-1",
						Annotations: map[string]string{},
						Finalizers:  []string{vmv1beta1.FinalizerName},
					},
					Data: map[string]string{"rule": "new-content"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, got1 := rulesCMDiff(tt.currentCMs, tt.newCMs)
			assert.Equal(t, tt.toCreate, got)
			assert.Equal(t, tt.toUpdate, got1)
		})
	}
}
