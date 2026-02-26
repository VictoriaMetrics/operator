package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
)

func TestVMAuthValidate(t *testing.T) {
	type opts struct {
		src     string
		wantErr string
	}
	f := func(o opts) {
		t.Helper()
		var amc VMAuth
		assert.NoError(t, yaml.Unmarshal([]byte(o.src), &amc))
		if len(o.wantErr) > 0 {
			assert.ErrorContains(t, amc.Validate(), o.wantErr)
		} else {
			assert.NoError(t, amc.Validate())
		}
	}

	// invalid ingress
	f(opts{
		src: `
apiVersion: v1 
kind: VMAuth
metadata:
  name: must-fail
spec:
  ingress:
    tlsHosts: 
      - host-1
      - host-2`,
		wantErr: `spec.ingress.tlsSecretName cannot be empty with non-empty spec.ingress.tlsHosts`,
	})

	// both configSecret and external config is defined at the same time
	f(opts{
		src: `
apiVersion: v1 
kind: VMAuth
metadata:
  name: must-fail
spec:
  configSecret: some-value
  externalConfig:
    secretRef:
      key: secret
      name: access`,
		wantErr: `spec.configSecret and spec.externalConfig.secretRef cannot be used at the same time`,
	})

	// incorrect unauthorized access config, missing backends"
	f(opts{
		src: `
apiVersion: v1 
kind: VMAuth
metadata:
  name: must-fail
spec:
  unauthorizedUserAccessSpec:
    default_url: 
      - http://url-1`,
		wantErr: "incorrect cr.spec.UnauthorizedUserAccess syntax: at least one of `url_map`, `url_prefix` or `targetRefs` must be defined",
	})

	// incorrect unauthorized access config, bad metric_labels syntax
	f(opts{
		src: `
apiVersion: v1 
kind: VMAuth
metadata:
  name: must-fail
spec:
  unauthorizedUserAccessSpec:
    metric_labels:
      124124asff: 12fsaf
    url_prefix: http://some-dst
    default_url: 
      - http://url-1`,
		wantErr: `incorrect cr.spec.UnauthorizedUserAccess syntax: incorrect metricLabelName="124124asff", must match pattern="^[a-zA-Z_:.][a-zA-Z0-9_:.]*$"`,
	})

	// incorrect unauthorized access config url_map"
	f(opts{
		src: `
apiVersion: v1 
kind: VMAuth
metadata:
  name: must-fail
spec:
  unauthorizedUserAccessSpec:
    metric_labels:
      label: 12fsaf-value
    url_map:
      - url_prefix: http://some-url
        src_paths: ["/path-1"]
      - url_prefix: http://some-url-2
    default_url: 
      - http://url-1`,
		wantErr: `incorrect cr.spec.UnauthorizedUserAccess syntax: incorrect url_map at idx=1: incorrect url_map config at least of one src_paths,src_hosts,src_query_args or src_headers must be defined`,
	})

	// both unauthorizedUserAccessSpec and UnauthorizedUserAccess defined
	f(opts{
		src: `
apiVersion: v1 
kind: VMAuth
metadata:
  name: must-fail
spec:
  unauthorizedAccessConfig: 
    - url_prefix: http://some-url
      src_paths: ["/path-1"]
    - url_prefix: http://some-url-2
      src_paths: ["/path-1"]
  unauthorizedUserAccessSpec:
    metric_labels:
      label: 12fsaf-value
    url_map:
      - url_prefix: http://some-url
        src_paths: ["/path-1"]
    default_url: 
      - http://url-1`,
		wantErr: "at most one option can be used `spec.unauthorizedAccessConfig` or `spec.unauthorizedUserAccessSpec`, got both",
	})
}
