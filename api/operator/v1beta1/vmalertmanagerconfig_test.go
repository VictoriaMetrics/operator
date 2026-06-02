package v1beta1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestScrapeConfigCaseIgnore verifies that both snake_case and camelCase field
// names are accepted for scrape config types, thanks to the json "case:ignore"
// tag option processed via jsonv2.Unmarshal in each CRD's UnmarshalJSON.
func TestScrapeConfigCaseIgnore(t *testing.T) {
	t.Run("VMNodeScrape camelCase scrape params", func(t *testing.T) {
		src := `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMNodeScrape",
			"metadata": {"name": "test"},
			"spec": {
				"scrapeInterval": "30s",
				"scrapeTimeout": "10s",
				"honorLabels": true,
				"honorTimestamps": false,
				"path": "/metrics"
			}
		}`
		var ns VMNodeScrape
		assert.NoError(t, json.Unmarshal([]byte(src), &ns))
		assert.Empty(t, ns.Status.ParsingSpecError)
		assert.Equal(t, "30s", ns.Spec.ScrapeInterval)
		assert.Equal(t, "10s", ns.Spec.ScrapeTimeout)
		assert.Equal(t, true, ns.Spec.HonorLabels)
		assert.Equal(t, false, *ns.Spec.HonorTimestamps)
	})

	t.Run("VMNodeScrape snake_case scrape params (canonical, regression)", func(t *testing.T) {
		src := `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMNodeScrape",
			"metadata": {"name": "test"},
			"spec": {
				"scrape_interval": "60s",
				"honorLabels": false
			}
		}`
		var ns VMNodeScrape
		assert.NoError(t, json.Unmarshal([]byte(src), &ns))
		assert.Empty(t, ns.Status.ParsingSpecError)
		assert.Equal(t, "60s", ns.Spec.ScrapeInterval)
		assert.Equal(t, false, ns.Spec.HonorLabels)
	})

	t.Run("VMServiceScrape camelCase endpoint auth fields", func(t *testing.T) {
		src := `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMServiceScrape",
			"metadata": {"name": "test"},
			"spec": {
				"endpoints": [
					{
						"port": "metrics",
						"scrapeInterval": "15s",
						"scrapeTimeout": "5s",
						"honorLabels": true,
						"bearerTokenFile": "/var/run/secrets/token",
						"tlsConfig": {
							"insecureSkipVerify": true
						}
					}
				],
				"selector": {}
			}
		}`
		var ss VMServiceScrape
		assert.NoError(t, json.Unmarshal([]byte(src), &ss))
		assert.Empty(t, ss.Status.ParsingSpecError)
		ep := ss.Spec.Endpoints[0]
		assert.Equal(t, "15s", ep.ScrapeInterval)
		assert.Equal(t, "5s", ep.ScrapeTimeout)
		assert.Equal(t, true, ep.HonorLabels)
		assert.Equal(t, "/var/run/secrets/token", ep.BearerTokenFile)
		assert.Equal(t, true, ep.TLSConfig.InsecureSkipVerify)
	})

	t.Run("mixed snake_case and camelCase in endpoint", func(t *testing.T) {
		src := `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMNodeScrape",
			"metadata": {"name": "test"},
			"spec": {
				"scrape_interval": "30s",
				"scrapeTimeout": "10s",
				"honorLabels": true
			}
		}`
		var ns VMNodeScrape
		assert.NoError(t, json.Unmarshal([]byte(src), &ns))
		assert.Empty(t, ns.Status.ParsingSpecError)
		assert.Equal(t, "30s", ns.Spec.ScrapeInterval)
		assert.Equal(t, "10s", ns.Spec.ScrapeTimeout)
		assert.Equal(t, true, ns.Spec.HonorLabels)
	})
}

// TestCommonAppsParamsCaseIgnore verifies that snake_case alternatives for fields
// in CommonAppsParams (e.g. host_aliases for hostAliases) are accepted.
func TestCommonAppsParamsCaseIgnore(t *testing.T) {
	t.Run("host_aliases accepted for hostAliases", func(t *testing.T) {
		src := `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMSingle",
			"metadata": {"name": "test"},
			"spec": {
				"host_aliases": [
					{"ip": "1.2.3.4", "hostnames": ["my.host"]}
				]
			}
		}`
		var vs VMSingle
		assert.NoError(t, json.Unmarshal([]byte(src), &vs))
		assert.Empty(t, vs.Status.ParsingSpecError)
		assert.Len(t, vs.Spec.HostAliases, 1)
		assert.Equal(t, "1.2.3.4", vs.Spec.CommonAppsParams.HostAliases[0].IP)
		assert.Equal(t, []string{"my.host"}, vs.Spec.CommonAppsParams.HostAliases[0].Hostnames)
	})

	t.Run("hostAliases (camelCase canonical) still works", func(t *testing.T) {
		src := `{
			"apiVersion": "operator.victoriametrics.com/v1beta1",
			"kind": "VMSingle",
			"metadata": {"name": "test"},
			"spec": {
				"hostAliases": [
					{"ip": "5.6.7.8", "hostnames": ["other.host"]}
				]
			}
		}`
		var vs VMSingle
		assert.NoError(t, json.Unmarshal([]byte(src), &vs))
		assert.Empty(t, vs.Status.ParsingSpecError)
		assert.Len(t, vs.Spec.HostAliases, 1)
		assert.Equal(t, "5.6.7.8", vs.Spec.CommonAppsParams.HostAliases[0].IP)
	})
}
