package v1beta1

import (
	"encoding/json"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v2"
)

var _ = Describe("VMAuth Webhook", func() {
	Context("When creating VMAuth under Validating Webhook", func() {
		DescribeTable("fail validation",
			func(srcYAML string, wantErrText string) {
				var amc VMAuth
				Expect(yaml.Unmarshal([]byte(srcYAML), &amc)).To(Succeed())
				cfgJSON, err := json.Marshal(amc)
				Expect(err).ShouldNot(HaveOccurred())
				Expect(json.Unmarshal(cfgJSON, &amc)).ShouldNot(HaveOccurred())
				Expect(amc.sanityCheck()).To(MatchError(wantErrText))
			},
			Entry("invalid ingress", `
        apiVersion: v1 
        kind: VMAuth
        metadata:
          name: must-fail
        spec:
          ingress:
            tlsHosts: 
            - host-1
            - host-2
        `, `spec.ingress.tlsSecretName cannot be empty with non-empty spec.ingress.tlsHosts`),
			Entry("both configSecret and external config is defined at the same time", `
        apiVersion: v1 
        kind: VMAuth
        metadata:
          name: must-fail
        spec:
         configSecret: some-value
         externalConfig:
           secretRef:
             key: secret
             name: access
        `, `spec.configSecret and spec.externalConfig.secretRef cannot be used at the same time`),
			Entry("incorrect unauthorized access config, missing backends", `
        apiVersion: v1 
        kind: VMAuth
        metadata:
          name: must-fail
        spec:
         unauthorizedUserAccess:
            default_url: 
            - http://url-1
        `, "incorrect r.spec.UnauthorizedUserAccess syntax: at least one of `url_map` or `url_prefix` must be defined"),
			Entry("incorrect unauthorized access config, bad metric_labels syntax", `
        apiVersion: v1 
        kind: VMAuth
        metadata:
          name: must-fail
        spec:
         unauthorizedUserAccess:
            metric_labels:
                124124asff: 12fsaf
            url_prefix: http://some-dst
            default_url: 
            - http://url-1
        `, `incorrect r.spec.UnauthorizedUserAccess syntax: incorrect metricLabelName="124124asff", must match pattern="^[a-zA-Z_:.][a-zA-Z0-9_:.]*$"`),
			Entry("incorrect unauthorized access config url_map", `
        apiVersion: v1 
        kind: VMAuth
        metadata:
          name: must-fail
        spec:
         unauthorizedUserAccess:
            metric_labels:
                label: 12fsaf-value
            url_map:
            - url_prefix: http://some-url
              src_paths: ["/path-1"]
            - url_prefix: http://some-url-2
            default_url: 
            - http://url-1
        `, `incorrect r.spec.UnauthorizedUserAccess syntax: incorrect url_map at idx=1: incorrect url_map config at least of one src_paths,src_hosts,src_query_args or src_headers must be defined`),
		)
	})
})
