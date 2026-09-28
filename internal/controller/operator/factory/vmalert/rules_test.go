package vmalert

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"sort"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

// fillerContent returns n bytes of deterministic, high-entropy filler keyed by seed.
func fillerContent(seed string, n int) string {
	var b strings.Builder
	for b.Len() < n {
		h := sha256.Sum256([]byte(seed + b.String()))
		b.WriteString(hex.EncodeToString(h[:]))
	}
	return b.String()[:n]
}

// groupNamesFromCM decompresses and unmarshals every BinaryData entry, returning the contained
// group names across all of them for easy assertion.
func groupNamesFromCM(t *testing.T, cm corev1.ConfigMap) []string {
	t.Helper()
	keys := make([]string, 0, len(cm.BinaryData))
	for k := range cm.BinaryData {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var names []string
	for _, k := range keys {
		r, err := gzip.NewReader(bytes.NewReader(cm.BinaryData[k]))
		if !assert.NoError(t, err) {
			continue
		}
		decompressed, err := io.ReadAll(r)
		if !assert.NoError(t, err) {
			continue
		}
		var spec vmv1beta1.VMRuleSpec
		if !assert.NoError(t, yaml.Unmarshal(decompressed, &spec)) {
			continue
		}
		for _, g := range spec.Groups {
			names = append(names, g.Name)
		}
	}
	return names
}

func TestSelectRules(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		hasNotifiers      bool
		predefinedObjects []runtime.Object
		want              []ruleFile
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		_, got, err := selectRules(ctx, fclient, o.cr, o.hasNotifiers)
		assert.NoError(t, err)
		assert.Equal(t, o.want, got)
	}

	// no rules selected when SelectAllByDefault=false and no selectors set
	f(opts{
		cr:   &vmv1beta1.VMAlert{},
		want: nil,
	})

	// namespace selector matching all namespaces picks up the VMRule
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
			Spec: vmv1beta1.VMAlertSpec{
				RuleNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{}},
				RuleSelector:          &metav1.LabelSelector{},
			},
		},
		hasNotifiers: true,
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "error-alert", Namespace: "default"},
				Spec: vmv1beta1.VMRuleSpec{
					Groups: []vmv1beta1.RuleGroup{{
						Name:          "error-alert",
						Interval:      "10s",
						Concurrency:   1,
						EvalOffset:    "10s",
						EvalAlignment: ptr.To(false),
						Rules: []vmv1beta1.Rule{
							{Alert: "alerting", Expr: "up", For: "10s"},
						},
					}},
				},
			},
		},
		want: []ruleFile{{key: ruleFileKey("default", "error-alert"), groups: []vmv1beta1.RuleGroup{{
			Name:          "error-alert",
			Interval:      "10s",
			Concurrency:   1,
			EvalOffset:    "10s",
			EvalAlignment: ptr.To(false),
			Rules: []vmv1beta1.Rule{
				{Alert: "alerting", Expr: "up", For: "10s"},
			},
		}}}},
	})

	// namespace label filter only includes matching namespaces;
	// the VMRule in "default" (no matching label) is excluded
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
			Spec: vmv1beta1.VMAlertSpec{
				RuleNamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"monitoring": "enabled"}},
				RuleSelector:          &metav1.LabelSelector{},
			},
		},
		hasNotifiers: true,
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring", Labels: map[string]string{"monitoring": "enabled"}}},
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "error-alert", Namespace: "default"},
				Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{{
					Name: "error-alert", Interval: "10s",
					Rules: []vmv1beta1.Rule{{Record: "recording", Expr: "10", For: "10s"}},
				}}},
			},
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "error-alert-at-monitoring", Namespace: "monitoring"},
				Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{{
					Name: "error-alert", Interval: "10s",
					Rules: []vmv1beta1.Rule{{Alert: "alerting-2", Expr: "10", For: "10s"}},
				}}},
			},
		},
		want: []ruleFile{{key: ruleFileKey("monitoring", "error-alert-at-monitoring"), groups: []vmv1beta1.RuleGroup{{
			Name: "error-alert", Interval: "10s",
			Rules: []vmv1beta1.Rule{{Alert: "alerting-2", Expr: "10", For: "10s"}},
		}}}},
	})

	// duplicate group name across VMRules: harmless now, since each VMRule gets its own file.
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		hasNotifiers: true,
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "monitoring", Labels: map[string]string{"monitoring": "enabled"}}},
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "error-alert", Namespace: "default"},
				Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{{
					Name: "error-alert", Interval: "10s",
					Rules: []vmv1beta1.Rule{{Alert: "err indicator", Expr: "rate(err_metric[1m]) > 10", For: "10s"}},
				}}},
			},
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "error-alert-at-monitoring", Namespace: "monitoring"},
				Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{{
					Name: "error-alert", Interval: "10s",
					Rules: []vmv1beta1.Rule{{Alert: "alerting-2", Expr: "up", For: "10s"}},
				}}},
			},
		},
		want: []ruleFile{
			{key: ruleFileKey("default", "error-alert"), groups: []vmv1beta1.RuleGroup{
				{Name: "error-alert", Interval: "10s",
					Rules: []vmv1beta1.Rule{{Alert: "err indicator", Expr: "rate(err_metric[1m]) > 10", For: "10s"}}},
			}},
			{key: ruleFileKey("monitoring", "error-alert-at-monitoring"), groups: []vmv1beta1.RuleGroup{
				{Name: "error-alert", Interval: "10s",
					Rules: []vmv1beta1.Rule{{Alert: "alerting-2", Expr: "up", For: "10s"}}},
			}},
		},
	})

	// SelectAllByDefault=false with no selectors: nothing is selected
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitoring"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: false},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}},
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "error-alert", Namespace: "default"},
				Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{{
					Name: "error-alert", Interval: "10s",
					Rules: []vmv1beta1.Rule{{Expr: "10", For: "10s"}},
				}}},
			},
		},
		want: nil,
	})

	// hasNotifiers=false: alerting rules are dropped, recording rules in the same group are kept
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		hasNotifiers: false,
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "mixed", Namespace: "default"},
				Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{{
					Name: "mixed", Interval: "10s",
					Rules: []vmv1beta1.Rule{
						{Record: "job:total", Expr: "vector(1)"},
						{Alert: "JobDown", Expr: "up == 0"},
					},
				}}},
			},
		},
		want: []ruleFile{{key: ruleFileKey("default", "mixed"), groups: []vmv1beta1.RuleGroup{{
			Name: "mixed", Interval: "10s",
			Rules: []vmv1beta1.Rule{{Record: "job:total", Expr: "vector(1)"}},
		}}}},
	})

	// hasNotifiers=false: a group with only alerting rules is dropped entirely
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		hasNotifiers: false,
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "alerting-only", Namespace: "default"},
				Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{{
					Name:  "alerting-only",
					Rules: []vmv1beta1.Rule{{Alert: "JobDown", Expr: "up == 0"}},
				}}},
			},
		},
		want: nil,
	})
}

// TestSelectRules_DuplicateGroupNameAcrossVMRules asserts that two different VMRule objects
// declaring the same group name don't collide: each keeps its own file, keyed by its own
// namespace/name, so vmalert's per-file uniqueness check never sees them together.
func TestSelectRules_DuplicateGroupNameAcrossVMRules(t *testing.T) {
	ctx := context.Background()
	cr := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vm-alert", Namespace: "monitor"},
		Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
	}
	ruleA := &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule-a", Namespace: "default"},
		Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{{
			Name: "shared-name", Interval: "10s",
			Rules: []vmv1beta1.Rule{{Record: "job:a:total", Expr: "vector(1)"}},
		}}},
	}
	ruleB := &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule-b", Namespace: "default"},
		Spec: vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{{
			Name: "shared-name", Interval: "10s",
			Rules: []vmv1beta1.Rule{{Record: "job:b:total", Expr: "vector(1)"}},
		}}},
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{ruleA, ruleB})
	pos, files, err := selectRules(ctx, fclient, cr, true)
	assert.NoError(t, err)
	assert.Empty(t, pos.rules.Broken())
	if assert.Len(t, files, 2) {
		assert.Equal(t, ruleFileKey("default", "rule-a"), files[0].key)
		assert.Equal(t, ruleFileKey("default", "rule-b"), files[1].key)
	}
}

func TestCreateOrUpdateRuleConfigMaps(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		hasNotifiers      bool
		want              []string
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		got, err := CreateOrUpdateRuleConfigMaps(context.TODO(), fclient, o.cr, nil, o.hasNotifiers)
		assert.NoError(t, err)
		assert.Equal(t, o.want, got)
	}

	// IsUnmanaged when no selectors and SelectAllByDefault=false: returns nil without creating ConfigMaps
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "base-vmalert"},
		},
	})

	// SelectAllByDefault with no rules: still creates an empty placeholder ConfigMap
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "base-vmalert"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		want: []string{"vm-base-vmalert-rulefiles-0"},
	})

	// only recording rules selected: reconciles fine regardless of notifiers
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "base-vmalert"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		want: []string{"vm-base-vmalert-rulefiles-0"},
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "recording-only", Namespace: "default"},
				Spec: vmv1beta1.VMRuleSpec{
					Groups: []vmv1beta1.RuleGroup{{
						Name:  "recording-only",
						Rules: []vmv1beta1.Rule{{Record: "job:total", Expr: "vector(1)"}},
					}},
				},
			},
		},
	})

	// alerting rule selected with notifiers configured: kept as-is
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "base-vmalert"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		hasNotifiers: true,
		want:         []string{"vm-base-vmalert-rulefiles-0"},
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "with-alert", Namespace: "default"},
				Spec: vmv1beta1.VMRuleSpec{
					Groups: []vmv1beta1.RuleGroup{{
						Name: "with-alert",
						Rules: []vmv1beta1.Rule{
							{Record: "job:total", Expr: "vector(1)"},
							{Alert: "JobDown", Expr: "up == 0"},
						},
					}},
				},
			},
		},
	})

	// alerting-only rule selected with no notifiers: group is dropped entirely, empty placeholder
	// ConfigMap is created instead
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "base-vmalert"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		hasNotifiers: false,
		want:         []string{"vm-base-vmalert-rulefiles-0"},
		predefinedObjects: []runtime.Object{
			&vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{Name: "with-alert", Namespace: "default"},
				Spec: vmv1beta1.VMRuleSpec{
					Groups: []vmv1beta1.RuleGroup{{
						Name:  "with-alert",
						Rules: []vmv1beta1.Rule{{Alert: "JobDown", Expr: "up == 0"}},
					}},
				},
			},
		},
	})
}

// TestCreateOrUpdateRuleConfigMaps_EmptyPlaceholder checks the placeholder ConfigMap created for
// zero selected groups has no groups in it, so vmalert's -rule glob has a valid target instead of
// erroring, and going from 0 to 1 group only changes this ConfigMap's content rather than adding a
// new mount to the pod spec.
func TestCreateOrUpdateRuleConfigMaps_EmptyPlaceholder(t *testing.T) {
	ctx := context.TODO()
	cr := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "base-vmalert"},
		Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
	}
	fclient := k8stools.GetTestClientWithObjects(nil)
	names, err := CreateOrUpdateRuleConfigMaps(ctx, fclient, cr, nil, true)
	assert.NoError(t, err)
	if !assert.Equal(t, []string{"vm-base-vmalert-rulefiles-0"}, names) {
		return
	}

	var cm corev1.ConfigMap
	assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: names[0], Namespace: cr.Namespace}, &cm))
	assert.Empty(t, groupNamesFromCM(t, cm))
}

// TestCreateOrUpdateRuleConfigMaps_SplitsAcrossBuckets exercises the size-based bucket split.
func TestCreateOrUpdateRuleConfigMaps_SplitsAcrossBuckets(t *testing.T) {
	ctx := context.Background()

	mkRule := func(ns, name, recordName string) *vmv1beta1.VMRule {
		return &vmv1beta1.VMRule{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: vmv1beta1.VMRuleSpec{
				Groups: []vmv1beta1.RuleGroup{{
					Name:  name,
					Rules: []vmv1beta1.Rule{{Record: recordName, Expr: "vector(1)", Labels: map[string]string{"filler": fillerContent(name, 200)}}},
				}},
			},
		}
	}

	singleGroupData, err := yaml.Marshal(vmv1beta1.VMRuleSpec{Groups: []vmv1beta1.RuleGroup{mkRule("default", "rule-x", "job:x:total").Spec.Groups[0]}})
	assert.NoError(t, err)
	singleGroupCompressed, err := build.GzipConfig(singleGroupData)
	assert.NoError(t, err)

	origLimit := config.MustGetBaseConfig().ConfigDataBudgetBytes
	config.MustGetBaseConfig().ConfigDataBudgetBytes = len(singleGroupCompressed)
	defer func() { config.MustGetBaseConfig().ConfigDataBudgetBytes = origLimit }()

	ns := "default"
	cr := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "recording", Namespace: ns},
		Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
	}

	ruleB := mkRule(ns, "rule-b", "job:b:total")
	firstRuleCM := "vm-recording-rulefiles-0"
	secondRuleCM := "vm-recording-rulefiles-1"

	// one rule fits in a single ConfigMap
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{ruleB})
	names, err := CreateOrUpdateRuleConfigMaps(ctx, fclient, cr, nil, true)
	assert.NoError(t, err)
	assert.Equal(t, []string{firstRuleCM}, names)

	var cm0, cm1 corev1.ConfigMap
	assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: firstRuleCM, Namespace: ns}, &cm0))
	assert.Contains(t, cm0.BinaryData, ruleFileKey(ns, "rule-b"))
	assert.Equal(t, []string{"rule-b"}, groupNamesFromCM(t, cm0))

	// adding a second rule that doesn't fit alongside it forces a split into two ConfigMaps.
	ruleA := mkRule(ns, "rule-a", "job:a:total")
	assert.NoError(t, fclient.Create(ctx, ruleA))

	names, err = CreateOrUpdateRuleConfigMaps(ctx, fclient, cr, nil, true)
	assert.NoError(t, err)
	assert.Equal(t, []string{firstRuleCM, secondRuleCM}, names)

	assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: firstRuleCM, Namespace: ns}, &cm0))
	assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: secondRuleCM, Namespace: ns}, &cm1))
	assert.ElementsMatch(t, []string{"rule-a", "rule-b"},
		append(groupNamesFromCM(t, cm0), groupNamesFromCM(t, cm1)...),
		"both groups are present, split across the two buckets")
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

// TestCreateOrUpdate_RecordingRulesOnlyNoNotifier reproduces
// https://github.com/VictoriaMetrics/operator/issues/2388: a VMAlert selecting only
// recording-rule VMRules (no alerting rules) must reconcile successfully without any
// notifier configured.
func TestCreateOrUpdate_RecordingRulesOnlyNoNotifier(t *testing.T) {
	ctx := context.TODO()
	ns := "default"
	cr := &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{Name: "vmalert", Namespace: ns},
		Spec: vmv1beta1.VMAlertSpec{
			Datasource:         vmv1beta1.VMAlertDatasourceSpec{URL: "http://vmsingle:8428"},
			SelectAllByDefault: true,
		},
	}
	rule := &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{Name: "recording-only", Namespace: ns},
		Spec: vmv1beta1.VMRuleSpec{
			Groups: []vmv1beta1.RuleGroup{{
				Name:  "recording-only",
				Rules: []vmv1beta1.Rule{{Record: "foo1", Expr: "vector(1)"}},
			}},
		},
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{rule})
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	hasNotifiers := cr.HasNotifiersConfigured()
	assert.False(t, hasNotifiers, "no notifiers configured on this VMAlert")

	cmNames, err := CreateOrUpdateRuleConfigMaps(ctx, fclient, cr, nil, hasNotifiers)
	assert.NoError(t, err)

	synctest.Test(t, func(t *testing.T) {
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient, cmNames))
	})
}
