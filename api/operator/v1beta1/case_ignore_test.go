package v1beta1

import (
	"encoding/json/v2"
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

// TestCommonAppsParamsHostAliasesCompat verifies that the snake_case host_aliases
// field is still accepted alongside hostAliases (build.PodTemplateAddCommonParams
// applies the documented priority between the two).
func TestCommonAppsParamsHostAliasesCompat(t *testing.T) {
	t.Run("host_aliases decodes into HostAliasesUnderScore", func(t *testing.T) {
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
		assert.Empty(t, vs.Spec.HostAliases)
		assert.Len(t, vs.Spec.HostAliasesUnderScore, 1)
		assert.Equal(t, "1.2.3.4", vs.Spec.HostAliasesUnderScore[0].IP)
		assert.Equal(t, []string{"my.host"}, vs.Spec.HostAliasesUnderScore[0].Hostnames)
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

// TestRelabelConfigSourceTargetLabelCaseIgnore verifies that the original Prometheus
// relabel_config spelling (source_labels, target_label) is accepted alongside
// sourceLabels/targetLabel: case:ignore folds away case, dashes, and underscores, so
// no extra handling is needed in RelabelConfig.UnmarshalJSON for this.
func TestRelabelConfigSourceTargetLabelCaseIgnore(t *testing.T) {
	t.Run("source_labels/target_label accepted", func(t *testing.T) {
		var rc RelabelConfig
		src := `{"source_labels": ["__address__"], "target_label": "address"}`
		assert.NoError(t, json.Unmarshal([]byte(src), &rc))
		assert.Equal(t, []string{"__address__"}, rc.SourceLabels)
		assert.Equal(t, "address", rc.TargetLabel)
	})

	t.Run("sourceLabels/targetLabel (camelCase canonical) still works", func(t *testing.T) {
		var rc RelabelConfig
		src := `{"sourceLabels": ["__address__"], "targetLabel": "address"}`
		assert.NoError(t, json.Unmarshal([]byte(src), &rc))
		assert.Equal(t, []string{"__address__"}, rc.SourceLabels)
		assert.Equal(t, "address", rc.TargetLabel)
	})

	t.Run("setting both spellings at once is rejected as ambiguous", func(t *testing.T) {
		var rc RelabelConfig
		src := `{"sourceLabels": ["__new__"], "source_labels": ["__old__"]}`
		assert.Error(t, json.Unmarshal([]byte(src), &rc))
	})
}
