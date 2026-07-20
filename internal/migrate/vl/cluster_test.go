package vl

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
	target := &vmv1.VLCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1.VLClusterSpec{
			VLStorage: &vmv1.VLStorage{Storage: &vmv1beta1.StorageSpec{}},
			VLSelect:  &vmv1.VLSelect{},
			VLInsert:  &vmv1.VLInsert{},
		},
	}

	components := clusterComponents(target)
	require.Len(t, components, 3)

	byLabel := make(map[string]migrate.ClusterComponentSpec)
	for _, c := range components {
		byLabel[c.HelmComponentLabel] = c
	}

	assert.Equal(t, "vlstorage-myrelease", byLabel["vlstorage"].TargetWorkloadName)
	assert.Equal(t, "vlstorage-db", byLabel["vlstorage"].TargetPVCTemplateName)

	assert.Equal(t, "vlselect-myrelease", byLabel["vlselect"].TargetWorkloadName)
	assert.Empty(t, byLabel["vlselect"].TargetPVCTemplateName)

	assert.Equal(t, "vlinsert-myrelease", byLabel["vlinsert"].TargetWorkloadName)
	assert.Empty(t, byLabel["vlinsert"].TargetPVCTemplateName)
}
