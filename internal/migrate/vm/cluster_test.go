package vm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/migrate"
)

func TestClusterComponents(t *testing.T) {
	target := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "myrelease", Namespace: "default"},
		Spec: vmv1beta1.VMClusterSpec{
			VMStorage: &vmv1beta1.VMStorage{Storage: &vmv1beta1.StorageSpec{}},
			VMInsert:  &vmv1beta1.VMInsert{},
		},
	}

	components := clusterComponents(target)
	require.Len(t, components, 3)

	byLabel := make(map[string]migrate.ClusterComponentSpec)
	for _, c := range components {
		byLabel[c.HelmComponentLabel] = c
	}

	assert.Equal(t, "vmstorage-myrelease", byLabel["vmstorage"].TargetWorkloadName)
	assert.Equal(t, "vmstorage-db", byLabel["vmstorage"].TargetPVCTemplateName)

	// vmselect has no StorageSpec configured on the target — no PVC template name.
	assert.Equal(t, "vmselect-myrelease", byLabel["vmselect"].TargetWorkloadName)
	assert.Empty(t, byLabel["vmselect"].TargetPVCTemplateName)

	// vminsert never has persistent storage.
	assert.Equal(t, "vminsert-myrelease", byLabel["vminsert"].TargetWorkloadName)
	assert.Empty(t, byLabel["vminsert"].TargetPVCTemplateName)
}
