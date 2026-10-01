package migrate

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConvertSpec_RejectsInvalidDNS1123Name(t *testing.T) {
	_, err := convertSpec(ChartVMSingle, nil, "", "Not_A_Valid_Name", "default")
	assert.ErrorContains(t, err, "invalid target name")
}

func TestConvertSpec_RejectsOverlongName(t *testing.T) {
	name := strings.Repeat("a", maxTargetNameLength+1)
	_, err := convertSpec(ChartVMSingle, nil, "", name, "default")
	assert.ErrorContains(t, err, "characters")
}

func TestConvertSpec_AcceptsNameAtMaxLength(t *testing.T) {
	name := strings.Repeat("a", maxTargetNameLength)
	_, err := convertSpec(ChartVMSingle, nil, "/nonexistent-values-file.yaml", name, "default")
	// Fails later (reading the values file), but must get past the name-length check first.
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "characters")
}
