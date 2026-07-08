package v1beta1

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	strategicpatch "k8s.io/apimachinery/pkg/util/strategicpatch"
)

func TestVMRuleValidate(t *testing.T) {
	type opts struct {
		src     string
		wantErr string
	}
	f := func(o opts) {
		t.Helper()
		var vmr VMRule
		assert.NoError(t, yaml.Unmarshal([]byte(o.src), &vmr))
		if len(o.wantErr) > 0 {
			assert.ErrorContains(t, vmr.Validate(), o.wantErr)
		} else {
			assert.NoError(t, vmr.Validate())
		}
	}

	// bad tenant
	f(opts{
		src: `
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: bad-tenant
spec:
  groups:
    - name: kafka
      tenant: bad-value
      rules:
        - alert: coordinator down
          expr: ml_app_gauge{exec_context="consumer_group_state"} == 0
          for: 60s
          labels:
            severity: critical
            job: "{{ $labels.job }}"
          annotations:
            value: "{{ $value }}"
            description: 'kafka coordinator is down'`,
		wantErr: `at idx=0 bad tenant="bad-value": cannot parse account_id: "bad-value" as int32, err: strconv.ParseInt: parsing "bad-value": invalid syntax`,
	})

	// bad expression
	f(opts{
		src: `
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: bad-expression
spec:
  groups:
    - name: kafka
      rules:
        - alert: coordinator down
          expr: non_exist_func(ml_app_gauge{exec_context="consumer_group_state"}) == 0
          for: 60s
          labels:
            severity: critical
            job: "{{ $labels.job }}"
          annotations:
            value: "{{ $value }}"
            description: 'kafka coordinator is down'`,
		wantErr: `validation failed for VMRule: / group: kafka err: invalid expression for rule "coordinator down": bad MetricsQL expr: "non_exist_func(ml_app_gauge{exec_context=\"consumer_group_state\"}) == 0", err: unsupported function "non_exist_func"`,
	})

	// bad template
	f(opts{
		src: `
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: bad-interval
spec:
  groups:
    - name: kafka
      interval: 10s
      rules:
        - alert: coordinator down
          expr: sum(ml_app_gauge{exec_context="consumer_group_state"}) == 0
          for: 60s
          labels:
            severity: critical
            job: "{{ $labls.job }}"
          annotations:
            value: "{{ $value }}"
            description: 'kafka coordinator is down'`,
		wantErr: "validation failed for VMRule: / group: kafka err: invalid labels for rule \"coordinator down\": errors(1): \n(key: \"job\", value: \"{{ $labls.job }}\"): error parsing template: template: :1: undefined variable \"$labls\"",
	})

	// rule with both record and alert set
	f(opts{
		src: `
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: both-record-and-alert
spec:
  groups:
    - name: kafka
      rules:
        - alert: coordinator down
          record: job:up:sum
          expr: up == 0`,
		wantErr: `rule at group kafka index 0 has both record and alert set`,
	})

	// rule with neither record nor alert set
	f(opts{
		src: `
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: neither-record-nor-alert
spec:
  groups:
    - name: kafka
      rules:
        - expr: up == 0`,
		wantErr: `rule at group kafka index 0 has neither record nor alert set`,
	})

	// duplicate rules
	f(opts{
		src: `
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: duplicate-rules
spec:
  groups:
    - name: kafka
      rules:
        - alert: some indicator
          expr: ml_app_gauge{exec_context="consumer_group_state"} == 0
          for: 60s
        - alert: some indicator
          expr: ml_app_gauge{exec_context="consumer_group_state"} == 0
          for: 60s`,
		wantErr: `validation failed for VMRule: / group: kafka err: "alerting rule \"some indicator\"; expr: \"ml_app_gauge{exec_context=\\\"consumer_group_state\\\"} == 0\"" is a duplicate in group`,
	})

	// with tenant
	f(opts{
		src: `
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: tenant
spec:
  groups:
    - name: kafka
      tenant: 15:13
      rules:
        - alert: coordinator down
          expr: ml_app_gauge{exec_context="consumer_group_state"} == 0
          for: 60s
          labels:
            severity: critical
            job: "{{ $labels.job }}"
          annotations:
            value: "{{ $value }}"
            description: 'kafka coordinator is down'`,
	})

	// with template functions
	f(opts{
		src: `
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: tenant
spec:
  groups:
    - name: kafka
      tenant: 15:13
      rules:
        - alert: coordinator down
          expr: ml_app_gauge{exec_context="consumer_group_state"} == 0
          for: 60s
          labels:
            severity: critical
            job: "{{ $labels.job }}"
          annotations:
            value: "{{ $value }}"
            description: 'kafka coordinator is down'
            first: |
                  {{ with query "some_metric{instance='someinstance'}" }}
                    {{ . | first | value | humanize }}
                  {{ end }}`,
	})

	// with vlogs alert
	f(opts{
		src: `
apiVersion: operator.victoriametrics.com/v1beta1
kind: VMRule
metadata:
  name: vlogs-alert-1
spec:
  groups:
    - name: log-stats
      interval: 5m
      type: vlogs
      rules:
        - alert: TooManyFailedRequest
          expr: 'env: "test" AND service: "nginx" | stats count(*) as requests'
          annotations: 
            description: "Service nginx on env test accepted {{$labels.requests}} requests in the last 5 minutes"`,
	})
}

func TestVMRuleRulesRoundTripAndValidate(t *testing.T) {
	src := VMRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1beta1",
			Kind:       "VMRule",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "round-trip",
			Namespace: "default",
		},
		Spec: VMRuleSpec{
			Groups: []RuleGroup{
				{
					Name: "group-a",
					Rules: []Rule{
						{
							Alert: "HighErrorRate",
							Expr:  "up == 0",
							For:   "60s",
						},
						{
							Record: "job:up:sum",
							Expr:   "sum(up) by (job)",
						},
					},
				},
				{
					Name: "group-b",
					Rules: []Rule{
						{
							Alert: "LowDiskSpace",
							Expr:  "node_filesystem_avail_bytes < 1024",
						},
						{
							Record: "job:http_requests:rate5m",
							Expr:   "sum(rate(http_requests_total[5m])) by (job)",
						},
					},
				},
			},
		},
	}

	data, err := json.Marshal(&src)
	require.NoError(t, err)

	var got VMRule
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, src, got)
	assert.NoError(t, got.Validate())
}

func TestVMRuleValidateDuplicateGroupNames(t *testing.T) {
	vmr := VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "duplicate-groups",
			Namespace: "default",
		},
		Spec: VMRuleSpec{
			Groups: []RuleGroup{
				{
					Name:  "same",
					Rules: []Rule{{Alert: "AlwaysDown", Expr: "up == 0"}},
				},
				{
					Name:  "same",
					Rules: []Rule{{Record: "job:up:sum", Expr: "sum(up) by (job)"}},
				},
			},
		},
	}

	assert.ErrorContains(t, vmr.Validate(), "duplicate group name")
}

// TestVMRuleRuleListMapKeyFieldNames verifies that the Rule struct exposes JSON
// fields named "record" and "alert", matching the +listMapKey declarations.
func TestVMRuleRuleListMapKeyFieldNames(t *testing.T) {
	rt := reflect.TypeOf(Rule{})
	var foundRecord, foundAlert bool
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		switch name {
		case "record":
			foundRecord = true
		case "alert":
			foundAlert = true
		}
	}
	assert.True(t, foundRecord, `Rule must have json:"record,..." to match +listMapKey=record`)
	assert.True(t, foundAlert, `Rule must have json:"alert,..." to match +listMapKey=alert`)
}

// TestVMRuleRulesCompositeKeyDistinctness verifies that the (record, alert)
// composite key correctly distinguishes rules across all relevant combinations.
func TestVMRuleRulesCompositeKeyDistinctness(t *testing.T) {
	type ruleKey = [2]string
	keyOf := func(r Rule) ruleKey { return ruleKey{r.Record, r.Alert} }
	uniqueKeys := func(rules []Rule) int {
		seen := make(map[ruleKey]struct{}, len(rules))
		for _, r := range rules {
			seen[keyOf(r)] = struct{}{}
		}
		return len(seen)
	}

	tests := []struct {
		name  string
		rules []Rule
		want  int
	}{
		{
			name: "same alert, different record are distinct keys",
			rules: []Rule{
				{Alert: "HighErrorRate", Record: "r1", Expr: "up == 0"},
				{Alert: "HighErrorRate", Record: "r2", Expr: "up == 0"},
			},
			want: 2,
		},
		{
			name: "same record, different alert are distinct keys",
			rules: []Rule{
				{Record: "job:up:sum", Alert: "a1", Expr: "sum(up)"},
				{Record: "job:up:sum", Alert: "a2", Expr: "sum(up)"},
			},
			want: 2,
		},
		{
			name: "same (record, alert) pair maps to a single key regardless of expr",
			rules: []Rule{
				{Alert: "HighErrorRate", Expr: "up == 0"},
				{Alert: "HighErrorRate", Expr: "up == 1"},
			},
			want: 1,
		},
		{
			name: "rules with neither record nor alert all share the same empty key",
			rules: []Rule{
				{Expr: "up == 0"},
				{Expr: "sum(up)"},
			},
			want: 1,
		},
		{
			name: "alert-only and record-only rules are distinct keys",
			rules: []Rule{
				{Alert: "HighErrorRate", Expr: "up == 0"},
				{Record: "job:up:sum", Expr: "sum(up) by (job)"},
			},
			want: 2,
		},
		{
			name: "rule with both record and alert set is its own unique key",
			rules: []Rule{
				{Alert: "HighErrorRate", Expr: "up == 0"},
				{Record: "job:up:sum", Expr: "sum(up)"},
				{Record: "job:up:sum", Alert: "HighErrorRate", Expr: "combined"},
			},
			want: 3,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, uniqueKeys(tc.rules))
		})
	}
}

// TestVMRuleGroupsStrategicMergePatch verifies that Groups (which carry struct-tag
// patchStrategy/patchMergeKey) are correctly merged by name under traditional
// strategic merge patch, while Rules — which use SSA list map keys only — are
// replaced atomically (the entire slice is swapped for the patched group).
func TestVMRuleGroupsStrategicMergePatch(t *testing.T) {
	original := VMRuleSpec{
		Groups: []RuleGroup{
			{
				Name:  "alerts",
				Rules: []Rule{{Alert: "HighErrorRate", Expr: "up == 0", For: "5m"}},
			},
			{
				Name:  "recordings",
				Rules: []Rule{{Record: "job:up:sum", Expr: "sum(up) by (job)"}},
			},
		},
	}
	// Patch touches only "alerts"; "recordings" group must survive untouched.
	patch := VMRuleSpec{
		Groups: []RuleGroup{
			{
				Name:  "alerts",
				Rules: []Rule{{Alert: "HighErrorRate", Expr: "up == 0", For: "10m"}},
			},
		},
	}

	origJSON, err := json.Marshal(original)
	require.NoError(t, err)
	patchJSON, err := json.Marshal(patch)
	require.NoError(t, err)

	merged, err := strategicpatch.StrategicMergePatch(origJSON, patchJSON, VMRuleSpec{})
	require.NoError(t, err)

	var result VMRuleSpec
	require.NoError(t, json.Unmarshal(merged, &result))

	require.Len(t, result.Groups, 1, "only single items from a second group should survive")
	byName := make(map[string]RuleGroup, len(result.Groups))
	for _, g := range result.Groups {
		byName[g.Name] = g
	}

	// "recordings" group is unpatched and must be present unchanged.
	recordings, ok := byName["recordings"]
	require.False(t, ok, "recording rules should be dropped")
	require.Len(t, recordings.Rules, 0)

	// "alerts" group was patched; under traditional SMP, its Rules slice is
	// replaced atomically (no patchMergeKey struct tag on Rules).
	alerts, ok := byName["alerts"]
	require.True(t, ok, "alerts group must be present")
	require.Len(t, alerts.Rules, 1)
	assert.Equal(t, "10m", alerts.Rules[0].For, "rule For should be updated to 10m")
}

// TestVMRuleRulesSSAStyleMerge simulates the Server Side Apply merge semantics
// enabled by +listType=map +listMapKey=record +listMapKey=alert: patch rules
// update existing rules matched by the (record, alert) composite key, and new
// rules are appended without discarding unmatched base rules.
func TestVMRuleRulesSSAStyleMerge(t *testing.T) {
	type ruleKey = [2]string
	keyOf := func(r Rule) ruleKey { return ruleKey{r.Record, r.Alert} }

	// mergeRules applies SSA list-map semantics using the (record, alert) key:
	// matched entries are updated in-place; unmatched patch entries are appended.
	mergeRules := func(base, patch []Rule) []Rule {
		index := make(map[ruleKey]int, len(base))
		for i, r := range base {
			index[keyOf(r)] = i
		}
		result := make([]Rule, len(base))
		copy(result, base)
		for _, p := range patch {
			if i, ok := index[keyOf(p)]; ok {
				result[i] = p
			} else {
				result = append(result, p)
			}
		}
		return result
	}

	base := []Rule{
		{Alert: "HighErrorRate", Expr: "up == 0", For: "5m"},
		{Alert: "LowDisk", Expr: "disk_free < 1024"},
		{Record: "job:up:sum", Expr: "sum(up) by (job)"},
	}
	patch := []Rule{
		// key ("", "HighErrorRate") exists in base — update For.
		{Alert: "HighErrorRate", Expr: "up == 0", For: "10m"},
		// key ("job:errors:rate5m", "") is new — should be appended.
		{Record: "job:errors:rate5m", Expr: "sum(rate(errors_total[5m])) by (job)"},
	}

	merged := mergeRules(base, patch)

	require.Len(t, merged, 4, "base had 3 rules; patch updates 1 and appends 1")

	byKey := make(map[ruleKey]Rule, len(merged))
	for _, r := range merged {
		byKey[keyOf(r)] = r
	}

	updated, ok := byKey[ruleKey{"", "HighErrorRate"}]
	require.True(t, ok)
	assert.Equal(t, "10m", updated.For, "HighErrorRate rule For must be updated")

	_, ok = byKey[ruleKey{"", "LowDisk"}]
	assert.True(t, ok, "LowDisk rule must survive as an unpatched base rule")

	_, ok = byKey[ruleKey{"job:up:sum", ""}]
	assert.True(t, ok, "job:up:sum rule must survive as an unpatched base rule")

	_, ok = byKey[ruleKey{"job:errors:rate5m", ""}]
	assert.True(t, ok, "job:errors:rate5m must be appended from the patch")
}
