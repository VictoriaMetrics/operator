package v1beta1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("VMRule Webhook", func() {
	Context("When creating VMRule under Validating Webhook", func() {
		DescribeTable("fails validation",
			func(srcYAML string, wantErr string) {
				var vmr VMRule
				Expect(yaml.Unmarshal([]byte(srcYAML), &vmr)).To(Succeed())
				Expect(vmr.Validate()).To(MatchError(wantErr))
			},
			Entry("bad tenant", `
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
              description: 'kafka coordinator is down'

        `, `at idx=0 bad tenant="bad-value": cannot parse account_id: "bad-value" as int32, err: strconv.ParseInt: parsing "bad-value": invalid syntax`),
			Entry("bad expression", `
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
              description: 'kafka coordinator is down'

        `, `validation failed for VMRule: / group: kafka err: invalid expression for rule  "coordinator down": bad prometheus expr: "non_exist_func(ml_app_gauge{exec_context=\"consumer_group_state\"}) == 0", err: unsupported function "non_exist_func"`),
			Entry("bad template", `
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
              description: 'kafka coordinator is down'

        `, `validation failed for VMRule: / group: kafka err: invalid labels for rule  "coordinator down": errors(1): key "job", template "{{ $labls.job }}": error parsing annotation template: template: :1: undefined variable "$labls"`),
			Entry("duplicate rules", `
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
            for: 60s
        `, `validation failed for VMRule: / group: kafka err: "alerting rule \"some indicator\"; expr: \"ml_app_gauge{exec_context=\\\"consumer_group_state\\\"} == 0\"" is a duplicate in group`),
		)
		DescribeTable("ok validation",
			func(srcYAML string) {
				var vmr VMRule
				Expect(yaml.Unmarshal([]byte(srcYAML), &vmr)).To(Succeed())
				Expect(vmr.Validate()).To(Succeed())
			},
			Entry("with tenant", `
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

        `),
			Entry("with template functions", `
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
                    {{ end }}

        `),
			Entry("with vlogs alert", `
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
              description: "Service nginx on env test accepted {{$labels.requests}} requests in the last 5 minutes"
        `),
		)
	})
})
