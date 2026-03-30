package vmalert

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestSelectRules(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		predefinedObjects []runtime.Object
		want              map[string]string
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		_, got, err := selectRules(ctx, fclient, o.cr)
		assert.NoError(t, err)
		for ruleName, content := range got {
			assert.Equal(t, o.want[ruleName], content)
		}
	}

	// select default rule
	f(opts{
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
`,
		},
	})

	// select default rule additional rule from another namespace
	f(opts{
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
	})

	// select default rule, and additional rule from another namespace with namespace filter
	f(opts{
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
`,
		},
	})

	// select all rules with select all
	f(opts{
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
	})

	// select none by default
	f(opts{
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
	})
}

func TestCreateOrUpdateRuleConfigMaps(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		want              []string
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		got, err := CreateOrUpdateRuleConfigMaps(context.TODO(), fclient, o.cr, nil)
		assert.NoError(t, err)
		assert.Equal(t, o.want, got)
	}

	// base-rules-empty
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base-vmalert",
			},
		},
	})

	// base-rules-gen-with-selector
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "default",
				Name:      "base-vmalert",
			},
			Spec: vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		want: []string{"vm-base-vmalert-rulefiles-0"},
	})
}

func TestRuleRebalance(t *testing.T) {
	ctx := context.Background()

	// Each rule's serialized YAML is ~76 bytes.
	origLimit := vmv1beta1.MaxConfigMapDataSize
	vmv1beta1.MaxConfigMapDataSize = 80
	defer func() { vmv1beta1.MaxConfigMapDataSize = origLimit }()

	mkRule := func(ns, name, recordName string) *vmv1beta1.VMRule {
		return &vmv1beta1.VMRule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: vmv1beta1.VMRuleSpec{
				Groups: []vmv1beta1.RuleGroup{{
					Name:  name,
					Rules: []vmv1beta1.Rule{{Record: recordName, Expr: "vector(1)"}},
				}},
			},
		}
	}

	ns := "default"
	cr := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "recording", Namespace: ns},
		Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
	}

	ruleB := mkRule(ns, "rule-b", "job:b:total")

	firstRuleCM := "vm-recording-rulefiles-0"
	secondRuleCM := "vm-recording-rulefiles-1"

	// One rule fits the configmap
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{ruleB})
	names, err := CreateOrUpdateRuleConfigMaps(ctx, fclient, cr, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{firstRuleCM}, names)

	var cm0 corev1.ConfigMap
	var cm1 corev1.ConfigMap
	assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: firstRuleCM, Namespace: ns}, &cm0))
	assert.Contains(t, cm0.Data, "default-rule-b.yaml", "rule-b should be in cm-0 initially")

	// Bin-packing puts rule-a in cm-0 and spills rule-b to cm-1.
	ruleA := mkRule(ns, "rule-a", "job:a:total")
	assert.NoError(t, fclient.Create(ctx, ruleA))

	names, err = CreateOrUpdateRuleConfigMaps(ctx, fclient, cr, nil)
	assert.NoError(t, err)
	assert.Equal(t, []string{firstRuleCM, secondRuleCM}, names)
	assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: firstRuleCM, Namespace: ns}, &cm0))
	assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: secondRuleCM, Namespace: ns}, &cm1))

	assert.Contains(t, cm0.Data, "default-rule-a.yaml")
	assert.NotContains(t, cm0.Data, "default-rule-b.yaml", "rule-b must be removed from cm-0 after moving to cm-1")
	assert.Contains(t, cm1.Data, "default-rule-b.yaml")
}

func Test_deduplicateRules(t *testing.T) {
	f := func(origin, want []*vmv1beta1.VMRule) {
		t.Helper()
		got := deduplicateRules(context.Background(), origin)
		assert.Equal(t, got, want)
	}

	// dedup group
	f([]*vmv1beta1.VMRule{
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
	}, []*vmv1beta1.VMRule{
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
	})

	// dedup group rule
	f([]*vmv1beta1.VMRule{
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
	}, []*vmv1beta1.VMRule{
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
	})
}
