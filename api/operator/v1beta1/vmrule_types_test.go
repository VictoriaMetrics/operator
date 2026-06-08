package v1beta1

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
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

func TestVMRuleRulesCRDListMapKeys(t *testing.T) {
	rulesSchema := loadVMRuleRulesSchema(t)
	require.NotNil(t, rulesSchema.XListType)
	assert.Equal(t, "map", *rulesSchema.XListType)
	assert.Equal(t, []string{"record", "alert"}, rulesSchema.XListMapKeys)

	rules := []Rule{
		{Alert: "SharedRule", Expr: "up == 0"},
		{Record: "shared_rule", Alert: "SharedRule", Expr: "sum(up)"},
	}
	ruleKeys := make(map[[2]string]struct{}, len(rules))
	for _, rule := range rules {
		ruleKeys[[2]string{rule.Record, rule.Alert}] = struct{}{}
	}
	assert.Len(t, ruleKeys, len(rules))
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

func TestVMRuleValidateSizeLimit(t *testing.T) {
	vmr := VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "too-big",
			Namespace: "default",
		},
		Spec: VMRuleSpec{
			Groups: []RuleGroup{
				{
					Name: "too-big",
					Rules: []Rule{
						{
							Alert: "TooBig",
							Expr:  "up == 0",
							Annotations: map[string]string{
								"summary": strings.Repeat("x", MaxConfigMapDataSize),
							},
						},
					},
				},
			},
		},
	}

	assert.ErrorContains(t, vmr.Validate(), "exceed single rule limit")
}

func loadVMRuleRulesSchema(t *testing.T) apiextensionsv1.JSONSchemaProps {
	t.Helper()

	data, err := os.ReadFile("../../../config/crd/overlay/crd.yaml")
	require.NoError(t, err)

	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		var crd apiextensionsv1.CustomResourceDefinition
		err := decoder.Decode(&crd)
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		if crd.Name != "vmrules.operator.victoriametrics.com" {
			continue
		}
		for _, version := range crd.Spec.Versions {
			if version.Name != "v1beta1" {
				continue
			}
			require.NotNil(t, version.Schema)
			require.NotNil(t, version.Schema.OpenAPIV3Schema)
			spec := version.Schema.OpenAPIV3Schema.Properties["spec"]
			groups := spec.Properties["groups"]
			require.NotNil(t, groups.Items)
			require.NotNil(t, groups.Items.Schema)
			rules := groups.Items.Schema.Properties["rules"]
			return rules
		}
	}

	t.Fatal("VMRule v1beta1 schema not found")
	return apiextensionsv1.JSONSchemaProps{}
}
