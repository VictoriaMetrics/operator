package vt

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

func TestClusterComponents(t *testing.T) {
	target := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1.VTClusterSpec{
			Storage: &vmv1.VTStorage{Storage: &vmv1beta1.StorageSpec{}},
			Select:  &vmv1.VTSelect{},
			Insert:  &vmv1.VTInsert{},
		},
	}

	components := clusterComponents(target)
	require.Len(t, components, 3)

	byLabel := make(map[string]migrate.ClusterComponentSpec)
	for _, c := range components {
		byLabel[c.HelmComponentLabel] = c
	}

	assert.Equal(t, "vtstorage-myrelease", byLabel["vtstorage"].TargetWorkloadName)
	assert.Equal(t, "vtstorage-db", byLabel["vtstorage"].TargetPVCTemplateName)

	assert.Equal(t, "vtselect-myrelease", byLabel["vtselect"].TargetWorkloadName)
	assert.Empty(t, byLabel["vtselect"].TargetPVCTemplateName)

	assert.Equal(t, "vtinsert-myrelease", byLabel["vtinsert"].TargetWorkloadName)
	assert.Empty(t, byLabel["vtinsert"].TargetPVCTemplateName)
}

func TestClusterComponents_DisabledComponentIsExcluded(t *testing.T) {
	target := &vmv1.VTCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1.VTClusterSpec{
			Storage: &vmv1.VTStorage{Storage: &vmv1beta1.StorageSpec{}},
			Insert:  &vmv1.VTInsert{},
		},
	}

	components := clusterComponents(target)
	require.Len(t, components, 2)

	for _, c := range components {
		assert.NotEqual(t, "vtselect", c.HelmComponentLabel)
	}
}
