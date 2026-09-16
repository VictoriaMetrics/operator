package build

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestHTTPRouteExtraRulePaths(t *testing.T) {
	paths := []string{"/A", "/B", "/C", "/D"}
	var extraRules []runtime.RawExtension
	for _, path := range paths {
		extraRules = append(extraRules, runtime.RawExtension{
			Raw: []byte(fmt.Sprintf(`{"matches":[{"path":{"type":"Exact","value":%q}}]}`, path)),
		})
	}

	route, err := HTTPRoute(&vmv1beta1.VMAuth{}, "8427", &vmv1beta1.EmbeddedHTTPRoute{ExtraRules: extraRules})
	require.NoError(t, err)
	require.Len(t, route.Spec.Rules, len(paths))
	for i, rule := range route.Spec.Rules {
		require.Len(t, rule.Matches, 1)
		require.NotNil(t, rule.Matches[0].Path)
		require.NotNil(t, rule.Matches[0].Path.Value)
		assert.Equal(t, paths[i], *rule.Matches[0].Path.Value, "rule %d", i+1)
	}
}

func TestHTTPRouteExtraRulesDoNotInheritFields(t *testing.T) {
	extraRules := []runtime.RawExtension{
		{Raw: []byte(`{"matches":[{"path":{"type":"PathPrefix","value":"/a"}}],"filters":[{"type":"RequestHeaderModifier","requestHeaderModifier":{"add":[{"name":"x-scope","value":"a"}]}}]}`)},
		{Raw: []byte(`{"matches":[{"path":{"type":"PathPrefix","value":"/b"}}]}`)},
		{Raw: []byte(`{"filters":[{"type":"RequestHeaderModifier","requestHeaderModifier":{"add":[{"name":"x-scope","value":"c"}]}}]}`)},
	}

	route, err := HTTPRoute(&vmv1beta1.VMAuth{}, "8427", &vmv1beta1.EmbeddedHTTPRoute{ExtraRules: extraRules})
	require.NoError(t, err)
	require.Len(t, route.Spec.Rules, 3)

	require.Len(t, route.Spec.Rules[0].Matches, 1)
	require.Len(t, route.Spec.Rules[0].Filters, 1)

	require.Len(t, route.Spec.Rules[1].Matches, 1)
	require.NotNil(t, route.Spec.Rules[1].Matches[0].Path.Value)
	assert.Equal(t, "/b", *route.Spec.Rules[1].Matches[0].Path.Value)
	assert.Empty(t, route.Spec.Rules[1].Filters, "second rule must not inherit filters")

	assert.Empty(t, route.Spec.Rules[2].Matches, "third rule must not inherit matches")
	require.Len(t, route.Spec.Rules[2].Filters, 1)
	require.NotNil(t, route.Spec.Rules[2].Filters[0].RequestHeaderModifier)
	require.Len(t, route.Spec.Rules[2].Filters[0].RequestHeaderModifier.Add, 1)
	assert.Equal(t, "c", route.Spec.Rules[2].Filters[0].RequestHeaderModifier.Add[0].Value)
}
