package manager

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestParseDisabledControllerNames(t *testing.T) {
	orig := *disableControllerForCRD
	defer func() { *disableControllerForCRD = orig }()

	*disableControllerForCRD = ""
	got, err := parseDisabledControllerNames()
	require.NoError(t, err)
	assert.Empty(t, got)

	*disableControllerForCRD = "VMRule,VMUser"
	got, err = parseDisabledControllerNames()
	require.NoError(t, err)
	assert.True(t, got.Has("VMRule"))
	assert.True(t, got.Has("VMUser"))
	assert.Len(t, got, 2)

	*disableControllerForCRD = "NotARealController"
	_, err = parseDisabledControllerNames()
	assert.Error(t, err)
}

func TestEffectiveDisabledControllerNames(t *testing.T) {
	bs := &config.BaseOperatorConf{}

	got := effectiveDisabledControllerNames(bs, sets.New("VMAuth"))
	assert.True(t, got.Has("VMAuth"))
	assert.True(t, got.Has("VMUser"), "VMUser must be disabled when VMAuth is disabled")

	got = effectiveDisabledControllerNames(bs, sets.New("VMAlert"))
	assert.True(t, got.Has("VMRule"), "VMRule must be disabled when VMAlert is disabled")

	got = effectiveDisabledControllerNames(bs, sets.New("VMAnomaly"))
	assert.True(t, got.Has("VMAnomalyConfig"), "VMAnomalyConfig must be disabled when VMAnomaly is disabled")

	got = effectiveDisabledControllerNames(bs, sets.New("VMAlertmanager"))
	assert.True(t, got.Has("VMAlertmanagerConfig"), "VMAlertmanagerConfig must be disabled when VMAlertmanager is disabled")

	got = effectiveDisabledControllerNames(bs, sets.New("VMAgent"))
	assert.False(t, got.Has("VMServiceScrape"), "VMServiceScrape must stay enabled while VMSingle is still enabled")

	got = effectiveDisabledControllerNames(bs, sets.New("VMAgent", "VMSingle"))
	assert.True(t, got.Has("VMServiceScrape"), "VMServiceScrape must be disabled when both VMAgent and VMSingle are disabled")

	got = effectiveDisabledControllerNames(bs, sets.New[string]())
	assert.False(t, got.Has("VMUser"))
	assert.False(t, got.Has("VMRule"))
	assert.False(t, got.Has("VMAnomalyConfig"))
	assert.False(t, got.Has("VMAlertmanagerConfig"))
	assert.False(t, got.Has("VMServiceScrape"))
}
