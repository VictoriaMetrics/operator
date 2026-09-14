package vmalert

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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

// groupNamesFromCM decompresses and unmarshals the rules.yaml BinaryData entry,
// returning the contained group names for easy assertion.
func groupNamesFromCM(t *testing.T, cm corev1.ConfigMap) []string {
	t.Helper()
	data, ok := cm.BinaryData[rulesFilename]
	if !ok {
		return nil
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if !assert.NoError(t, err) {
		return nil
	}
	decompressed, err := io.ReadAll(r)
	if !assert.NoError(t, err) {
		return nil
	}
	var spec vmv1beta1.VMRuleSpec
	if !assert.NoError(t, yaml.Unmarshal(decompressed, &spec)) {
		return nil
	}
	names := make([]string, 0, len(spec.Groups))
	for _, g := range spec.Groups {
		names = append(names, g.Name)
	}
	return names
}

func groupsCompressedSize(t *testing.T, groups []vmv1beta1.RuleGroup) int {
	t.Helper()
	data, err := yaml.Marshal(vmv1beta1.VMRuleSpec{Groups: groups})
	assert.NoError(t, err)
	compressed, err := build.GzipConfig(data)
	assert.NoError(t, err)
	return len(compressed)
}

func TestSelectRules(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMAlert
		hasNotifiers      bool
		predefinedObjects []runtime.Object
		want              []vmv1beta1.RuleGroup
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
		want: []vmv1beta1.RuleGroup{{
			Name:          "error-alert",
			Interval:      "10s",
			Concurrency:   1,
			EvalOffset:    "10s",
			EvalAlignment: ptr.To(false),
			Rules: []vmv1beta1.Rule{
				{Alert: "alerting", Expr: "up", For: "10s"},
			},
		}},
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
		want: []vmv1beta1.RuleGroup{{
			Name: "error-alert", Interval: "10s",
			Rules: []vmv1beta1.Rule{{Alert: "alerting-2", Expr: "10", For: "10s"}},
		}},
	})

	// SelectAllByDefault with duplicate group name across VMRules: both are returned; packing
	// will place them in separate ConfigMaps since group names must be unique within a file.
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
		// both groups are returned; "default" sorts first so its group appears at index 0
		want: []vmv1beta1.RuleGroup{
			{Name: "error-alert", Interval: "10s",
				Rules: []vmv1beta1.Rule{{Alert: "err indicator", Expr: "rate(err_metric[1m]) > 10", For: "10s"}}},
			{Name: "error-alert", Interval: "10s",
				Rules: []vmv1beta1.Rule{{Alert: "alerting-2", Expr: "up", For: "10s"}}},
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
		want: []vmv1beta1.RuleGroup{{
			Name: "mixed", Interval: "10s",
			Rules: []vmv1beta1.Rule{{Record: "job:total", Expr: "vector(1)"}},
		}},
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

	// SelectAllByDefault with no rules: no VMRules selected → no ConfigMaps created
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "base-vmalert"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
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

	// alerting-only rule selected with no notifiers: group is dropped entirely, no ConfigMaps created
	f(opts{
		cr: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "base-vmalert"},
			Spec:       vmv1beta1.VMAlertSpec{SelectAllByDefault: true},
		},
		hasNotifiers: false,
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

func TestRuleRebalance(t *testing.T) {
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
	assert.Contains(t, cm0.BinaryData, rulesFilename, "cm-0 must have rules.yaml")
	assert.Equal(t, []string{"rule-b"}, groupNamesFromCM(t, cm0))

	// adding a second rule forces a split; rule-b stays in cm-0, rule-a gets the new cm-1.
	ruleA := mkRule(ns, "rule-a", "job:a:total")
	assert.NoError(t, fclient.Create(ctx, ruleA))

	names, err = CreateOrUpdateRuleConfigMaps(ctx, fclient, cr, nil, true)
	assert.NoError(t, err)
	assert.Equal(t, []string{firstRuleCM, secondRuleCM}, names)

	assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: firstRuleCM, Namespace: ns}, &cm0))
	assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: secondRuleCM, Namespace: ns}, &cm1))

	assert.Equal(t, []string{"rule-b"}, groupNamesFromCM(t, cm0), "rule-b must stay in cm-0")
	assert.Equal(t, []string{"rule-a"}, groupNamesFromCM(t, cm1), "new rule-a gets cm-1")

	// two VMRules sharing the same group name must land in separate ConfigMaps even if both fit
	// within the size limit, because group names must be unique within a single rules.yaml file.
	config.MustGetBaseConfig().ConfigDataBudgetBytes = origLimit
	ruleConflict := &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{Name: "rule-conflict", Namespace: ns},
		Spec: vmv1beta1.VMRuleSpec{
			Groups: []vmv1beta1.RuleGroup{{
				Name:  "rule-a", // same group name as ruleA
				Rules: []vmv1beta1.Rule{{Record: "job:conflict:total", Expr: "vector(1)"}},
			}},
		},
	}
	fclient2 := k8stools.GetTestClientWithObjects([]runtime.Object{ruleA, ruleConflict})
	names, err = CreateOrUpdateRuleConfigMaps(ctx, fclient2, cr, nil, true)
	assert.NoError(t, err)
	assert.Equal(t, []string{firstRuleCM, secondRuleCM}, names, "conflicting group names must land in separate ConfigMaps")

	var cm0c, cm1c corev1.ConfigMap
	assert.NoError(t, fclient2.Get(ctx, types.NamespacedName{Name: firstRuleCM, Namespace: ns}, &cm0c))
	assert.NoError(t, fclient2.Get(ctx, types.NamespacedName{Name: secondRuleCM, Namespace: ns}, &cm1c))
	g0 := groupNamesFromCM(t, cm0c)
	g1 := groupNamesFromCM(t, cm1c)
	assert.Equal(t, []string{"rule-a"}, g0)
	assert.Equal(t, []string{"rule-a"}, g1)
}

func bucketAssignment(buckets []ruleBucket) map[string][]int {
	assignment := make(map[string][]int)
	for _, b := range buckets {
		for _, g := range b.groups {
			assignment[g.Name] = append(assignment[g.Name], b.index)
		}
	}
	return assignment
}

func maxBucketIndex(buckets []ruleBucket) int {
	max := -1
	for _, b := range buckets {
		if b.index > max {
			max = b.index
		}
	}
	return max
}

func groupNames(groups []vmv1beta1.RuleGroup) []string {
	names := make([]string, 0, len(groups))
	for _, g := range groups {
		names = append(names, g.Name)
	}
	return names
}

// fourEvenBuckets builds 12 equally-sized rule groups and a limit that packs them into 4 buckets
// of 3, then returns the fresh (no prior state) packing to use as prevAssignment in later calls.
func fourEvenBuckets(t *testing.T) (groups []vmv1beta1.RuleGroup, limit int, before []ruleBucket) {
	t.Helper()
	mkGroup := func(i int) vmv1beta1.RuleGroup {
		name := fmt.Sprintf("group-%02d", i)
		return vmv1beta1.RuleGroup{
			Name:  name,
			Rules: []vmv1beta1.Rule{{Record: name, Expr: "vector(1)", Labels: map[string]string{"filler": fillerContent(name, 200)}}},
		}
	}
	groups = make([]vmv1beta1.RuleGroup, 12)
	for i := range groups {
		groups[i] = mkGroup(i)
	}

	maxSize3, minSize4 := 0, -1
	for i := 0; i+3 <= len(groups); i++ {
		sz := groupsCompressedSize(t, groups[i:i+3])
		if sz > maxSize3 {
			maxSize3 = sz
		}
	}
	for i := 0; i+4 <= len(groups); i++ {
		sz := groupsCompressedSize(t, groups[i:i+4])
		if minSize4 == -1 || sz < minSize4 {
			minSize4 = sz
		}
	}
	if !assert.Less(t, maxSize3, minSize4, "test data must allow a limit that fits any 3 groups but not 4") {
		return
	}
	limit = maxSize3

	before, err := packRuleGroups(groups, limit, nil, -1)
	if !assert.NoError(t, err) {
		return
	}
	if !assert.Len(t, before, 4, "12 equally-sized groups must fill 4 buckets of 3") {
		return
	}
	for _, bucket := range before {
		if !assert.Len(t, bucket.groups, 3) {
			return
		}
	}
	return groups, limit, before
}

// TestPackRuleGroups_StableOnRemoval reproduces
// https://github.com/VictoriaMetrics/operator/issues/2610: removing one rule group must not
// reshuffle buckets that didn't need to change, regardless of where the removed group sits.
func TestPackRuleGroups_StableOnRemoval(t *testing.T) {
	groups, limit, before := fourEvenBuckets(t)
	prevAssignment, maxIndex := bucketAssignment(before), maxBucketIndex(before)

	tail, err := packRuleGroups(groups[:len(groups)-1], limit, prevAssignment, maxIndex)
	assert.NoError(t, err)
	if assert.Len(t, tail, 4) {
		for i := 0; i < 3; i++ {
			assert.Equal(t, before[i], tail[i], "bucket %d must stay untouched", i)
		}
		assert.Len(t, tail[3].groups, 2, "last bucket loses the removed group, nothing else")
	}

	var mid []vmv1beta1.RuleGroup
	for _, g := range groups {
		if g.Name != "group-04" {
			mid = append(mid, g)
		}
	}
	midResult, err := packRuleGroups(mid, limit, prevAssignment, maxIndex)
	assert.NoError(t, err)
	if assert.Len(t, midResult, 4) {
		assert.Equal(t, before[0], midResult[0], "bucket 0 must stay untouched")
		assert.Equal(t, before[2], midResult[2], "bucket 2 must stay untouched")
		assert.Equal(t, before[3], midResult[3], "bucket 3 must stay untouched")
		assert.Equal(t, 1, midResult[1].index)
		assert.ElementsMatch(t, []string{"group-03", "group-05"}, groupNames(midResult[1].groups), "bucket 1 keeps its other two survivors")
	}
}

// TestPackRuleGroups_SparseIndicesOnEmptyBucket reproduces the second half of
// https://github.com/VictoriaMetrics/operator/issues/2610: emptying a whole bucket must drop it
// from the result rather than getting compacted into a neighbor.
func TestPackRuleGroups_SparseIndicesOnEmptyBucket(t *testing.T) {
	groups, limit, before := fourEvenBuckets(t)
	prevAssignment, maxIndex := bucketAssignment(before), maxBucketIndex(before)

	var shrunk []vmv1beta1.RuleGroup
	for _, g := range groups {
		switch g.Name {
		case "group-06", "group-07", "group-08":
			continue
		default:
			shrunk = append(shrunk, g)
		}
	}

	result, err := packRuleGroups(shrunk, limit, prevAssignment, maxIndex)
	assert.NoError(t, err)
	if assert.Len(t, result, 3, "bucket 2 disappears instead of being compacted away") {
		assert.Equal(t, before[0], result[0])
		assert.Equal(t, before[1], result[1])
		assert.Equal(t, before[3], result[2], "surviving bucket 3 keeps index 3, not renumbered to 2")
	}
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
