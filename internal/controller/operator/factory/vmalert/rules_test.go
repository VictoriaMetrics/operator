package vmalert

import (
	"context"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	"github.com/go-test/deep"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_selectNamespaces(t *testing.T) {
	type args struct {
		selector labels.Selector
	}
	tests := []struct {
		name         string
		args         args
		predefinedNs []runtime.Object
		want         []string
		wantErr      bool
	}{
		{
			name:         "select 1 ns",
			args:         args{selector: labels.SelectorFromValidatedSet(labels.Set{})},
			predefinedNs: []runtime.Object{&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}}},
			want:         []string{"ns1"},
			wantErr:      false,
		},
		{
			name: "select 1 ns with label selector",
			args: args{selector: labels.SelectorFromValidatedSet(labels.Set{"name": "kube-system"})},
			predefinedNs: []runtime.Object{
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "ns1"}},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "kube-system", Labels: map[string]string{"name": "kube-system"}}},
			},
			want:    []string{"kube-system"},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedNs)
			got, err := k8stools.SelectNamespaces(context.TODO(), fclient, tt.args.selector)
			if (err != nil) != tt.wantErr {
				t.Errorf("selectNamespaces() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("selectNamespaces() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSelectRules(t *testing.T) {
	type args struct {
		p *vmv1beta1.VMAlert
		l logr.Logger
	}
	tests := []struct {
		name              string
		args              args
		predefinedObjects []runtime.Object
		want              map[string]string
		wantErr           bool
	}{
		{
			name: "select default rule",
			args: args{
				p: &vmv1beta1.VMAlert{},
				l: logf.Log.WithName("unit-test"),
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
         description: "error writing to remote writer from vmaler {{$labels}}"
         back: "error rate is ok at vmalert "
`,
			},
		},
		{
			name: "select default rule additional rule from another namespace",
			args: args{
				p: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
					Spec:       vmv1beta1.VMAlertSpec{RuleNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{}}, RuleSelector: &metav1.LabelSelector{}},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				// we need namespace for filter + object inside this namespace
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
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
  interval: 10s
  name: error-alert
  rules:
  - alert: alerting
    expr: "10"
    for: 10s
`,
			},
		},
		{
			name: "select default rule, and additional rule from another namespace with namespace filter",
			args: args{
				p: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
					Spec:       vmv1beta1.VMAlertSpec{RuleNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"monitoring": "enabled"}}, RuleSelector: &metav1.LabelSelector{}},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				// we need namespace for filter + object inside this namespace
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring", Labels: map[string]string{"monitoring": "enabled"}}},
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
- interval: 10s
  name: error-alert
  rules:
  - alert: alerting-2
    expr: "10"
    for: 10s
`},
		},
		{
			name: "select all rules with select all",
			args: args{
				p: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
					Spec: vmv1beta1.VMAlertSpec{
						SelectAllByDefault: true,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				// we need namespace for filter + object inside this namespace
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring", Labels: map[string]string{"monitoring": "enabled"}}},
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
- interval: 10s
  name: error-alert
  rules:
  - alert: err indicator
    expr: rate(err_metric[1m]) > 10
    for: 10s
`,
				"monitoring-error-alert-at-monitoring.yaml": `groups:
- interval: 10s
  name: error-alert
  rules:
  - alert: alerting-2
    expr: "10"
    for: 10s
`,
			},
		},
		{
			name: "select none by default",
			args: args{
				p: &vmv1beta1.VMAlert{
					ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitoring"},
					Spec: vmv1beta1.VMAlertSpec{
						SelectAllByDefault: false,
					},
				},
				l: logf.Log.WithName("unit-test"),
			},
			predefinedObjects: []runtime.Object{
				// we need namespace for filter + object inside this namespace
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
				&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring", Labels: map[string]string{"monitoring": "enabled"}}},
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
         description: "error writing to remote writer from vmaler {{$labels}}"
         back: "error rate is ok at vmalert "
`},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, _, err := selectRulesContent(ctx, fclient, tt.args.p)
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
	type args struct {
		cr *vmv1beta1.VMAlert
	}
	tests := []struct {
		name              string
		args              args
		want              []string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "base-rules-empty",
			args: args{cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "base-vmalert",
				},
			}},
		},

		{
			name: "base-rules-gen-with-selector",
			args: args{cr: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "base-vmalert",
				},
				Spec: vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
			}},
			want: []string{"vm-base-vmalert-rulefiles-0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			got, err := CreateOrUpdateRuleConfigMaps(context.TODO(), fclient, tt.args.cr, nil)
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
	type args struct {
		origin []*vmv1beta1.VMRule
	}
	tests := []struct {
		name string
		args args
		want []*vmv1beta1.VMRule
	}{
		{
			name: "dedup group",
			args: args{origin: []*vmv1beta1.VMRule{
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
			}},
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
			args: args{origin: []*vmv1beta1.VMRule{
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
			}},
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
			got := deduplicateRules(context.Background(), tt.args.origin)
			diff := deep.Equal(got, tt.want)
			if len(diff) > 0 {
				t.Errorf("deduplicateRules() %v = %v, want %v", diff, got, tt.want)
			}
		})
	}
}

func Test_rulesCMDiff(t *testing.T) {
	type args struct {
		currentCMs []v1.ConfigMap
		newCMs     []v1.ConfigMap
	}
	tests := []struct {
		name     string
		args     args
		toCreate []v1.ConfigMap
		toUpdate []v1.ConfigMap
	}{
		{
			name: "create one new",
			args: args{
				currentCMs: []v1.ConfigMap{},
				newCMs: []v1.ConfigMap{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rules-cm-1",
						},
					},
				},
			},
			toCreate: []v1.ConfigMap{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "rules-cm-1",
					},
				},
			},
		},
		{
			name: "skip exist",
			args: args{
				currentCMs: []v1.ConfigMap{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rules-cm-1",
						},
					},
				},
				newCMs: []v1.ConfigMap{},
			},
		},
		{
			name: "update one",
			args: args{
				currentCMs: []v1.ConfigMap{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rules-cm-1",
						},
					},
				},
				newCMs: []v1.ConfigMap{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "rules-cm-1",
						},
						Data: map[string]string{"rule": "content"},
					},
				},
			},
			toUpdate: []v1.ConfigMap{
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
			args: args{
				currentCMs: []v1.ConfigMap{
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
				newCMs: []v1.ConfigMap{
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
			toUpdate: []v1.ConfigMap{
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
			got, got1 := rulesCMDiff(tt.args.currentCMs, tt.args.newCMs)
			assert.Equal(t, tt.toCreate, got)
			assert.Equal(t, tt.toUpdate, got1)
		})
	}
}
