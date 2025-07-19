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
	type opts struct {
		cr                *vmv1beta1.VMAlert
		want              map[string]string
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		got, _, err := selectRulesContent(ctx, fclient, opts.cr)
		if err != nil {
			t.Errorf("SelectRules() error = %v", err)
			return
		}
		for ruleName, content := range got {
			if !assert.Equal(t, opts.want[ruleName], content) {
				t.Errorf("SelectRules() got = %v, want %v", content, opts.want[ruleName])
			}
		}
	}

	// select default rule
	o := opts{
		cr: &vmv1beta1.VMAlert{},
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
`},
	}
	f(o)

	// select default rule additional rule from another namespace
	o = opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
			Spec:       vmv1beta1.VMAlertSpec{RuleNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{}}, RuleSelector: &metav1.LabelSelector{}},
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
`},
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
			},
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "error-alert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMRuleSpec{
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
				},
			},
		},
	}
	f(o)

	// select default rule, and additional rule from another namespace with namespace filter
	o = opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
			Spec:       vmv1beta1.VMAlertSpec{RuleNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"monitoring": "enabled"}}, RuleSelector: &metav1.LabelSelector{}},
		},
		want: map[string]string{"monitoring-error-alert-at-monitoring.yaml": `groups:
- name: error-alert
  interval: 10s
  rules:
  - alert: alerting-2
    expr: "10"
    for: 10s
`},
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default"},
			},
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
	}
	f(o)

	// select all rules with select all
	o = opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
			Spec: vmv1beta1.VMAlertSpec{
				SelectAllByDefault: true,
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
`},
		predefinedObjects: []runtime.Object{
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
	}
	f(o)

	// select none by default
	o = opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitoring"},
			Spec: vmv1beta1.VMAlertSpec{
				SelectAllByDefault: false,
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
		predefinedObjects: []runtime.Object{
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
	}
	f(o)
}

func TestCreateOrUpdateRuleConfigMaps(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		want              []string
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		got, err := CreateOrUpdateRuleConfigMaps(context.TODO(), fclient, opts.cr, nil)
		if err != nil {
			t.Errorf("CreateOrUpdateRuleConfigMaps() error = %v", err)
			return
		}
		if !reflect.DeepEqual(got, opts.want) {
			t.Errorf("CreateOrUpdateRuleConfigMaps() got = %v, opts.want %v", got, opts.want)
		}
	}

	// base rules empty
	o := opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base-vmalert",
			},
		},
	}
	f(o)

	// base rules gen with selector
	o = opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base-vmalert",
			},
			Spec: vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		want: []string{"vm-base-vmalert-rulefiles-0"},
	}
	f(o)
}

func Test_deduplicateRules(t *testing.T) {
	type opts struct {
		origin []*vmv1beta1.VMRule
		want   []*vmv1beta1.VMRule
	}
	f := func(opts opts) {
		t.Helper()
		got := deduplicateRules(context.Background(), opts.origin)
		diff := deep.Equal(got, opts.want)
		if len(diff) > 0 {
			t.Errorf("deduplicateRules() %v = %v, want %v", diff, got, opts.want)
		}
	}

	// dedup group
	o := opts{
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
	}
	f(o)

	// dedup group rule
	o = opts{
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
	}
	f(o)
}

func Test_rulesCMDiff(t *testing.T) {
	type opts struct {
		currentCMs []corev1.ConfigMap
		newCMs     []corev1.ConfigMap
		toCreate   []corev1.ConfigMap
		toUpdate   []corev1.ConfigMap
	}
	f := func(opts opts) {
		got, got1 := rulesCMDiff(opts.currentCMs, opts.newCMs)
		assert.Equal(t, opts.toCreate, got)
		assert.Equal(t, opts.toUpdate, got1)
	}

	// create one new
	o := opts{
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
	}
	f(o)

	// skip exist
	o = opts{
		currentCMs: []corev1.ConfigMap{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rules-cm-1",
				},
			},
		},
	}
	f(o)

	// update one
	o = opts{
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
	}
	f(o)

	// update two
	o = opts{
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
	}
	f(o)
}
