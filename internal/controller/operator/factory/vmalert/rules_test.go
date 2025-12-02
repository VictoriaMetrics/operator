package vmalert

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
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
		predefinedObjects []runtime.Object
		want              map[string]string
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		got, _, err := selectRulesContent(ctx, fclient, o.cr)
		if err != nil {
			t.Errorf("SelectRules() error = %v", err)
			return
		}
		for ruleName, content := range got {
			if !assert.Equal(t, o.want[ruleName], content) {
				t.Errorf("SelectRules(): %s", cmp.Diff(content, o.want[ruleName]))
			}
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
		if err != nil {
			t.Errorf("CreateOrUpdateRuleConfigMaps() error = %v", err)
			return
		}
		if !reflect.DeepEqual(got, o.want) {
			t.Errorf("CreateOrUpdateRuleConfigMaps(): %s", cmp.Diff(got, o.want))
		}
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

func Test_deduplicateRules(t *testing.T) {
	f := func(origin, want []*vmv1beta1.VMRule) {
		t.Helper()
		got := deduplicateRules(context.Background(), origin)
		if diff := cmp.Diff(got, want); len(diff) > 0 {
			t.Errorf("deduplicateRules() %s", diff)
		}
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

func Test_rulesCMDiff(t *testing.T) {
	type opts struct {
		oldCMs   []corev1.ConfigMap
		newCMs   []corev1.ConfigMap
		toCreate []corev1.ConfigMap
		toUpdate []corev1.ConfigMap
	}

	f := func(o opts) {
		t.Helper()
		got, got1 := rulesCMDiff(o.oldCMs, o.newCMs)
		assert.Equal(t, o.toCreate, got)
		assert.Equal(t, o.toUpdate, got1)
	}

	// create one new
	f(opts{
		oldCMs: []corev1.ConfigMap{},
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
	})

	// skip exist
	f(opts{
		oldCMs: []corev1.ConfigMap{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name: "rules-cm-1",
				},
			},
		},
		newCMs: []corev1.ConfigMap{},
	})

	// update one
	f(opts{
		oldCMs: []corev1.ConfigMap{
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
	})

	// update two
	f(opts{
		oldCMs: []corev1.ConfigMap{
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
	})
}
