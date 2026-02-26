package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
		wantErr: `validation failed for VMRule: / group: kafka err: invalid expression for rule  "coordinator down": bad prometheus expr: "non_exist_func(ml_app_gauge{exec_context=\"consumer_group_state\"}) == 0", err: unsupported function "non_exist_func"`,
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
		wantErr: "validation failed for VMRule: / group: kafka err: invalid labels for rule  \"coordinator down\": errors(1): \n(key: \"job\", value: \"{{ $labls.job }}\"): error parsing template: template: :1: undefined variable \"$labls\"",
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
