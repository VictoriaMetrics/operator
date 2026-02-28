package reconcile

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func init() {
	InitDeadlines(10*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond, 10*time.Millisecond)
}

func TestWaitForStatus(t *testing.T) {
	f := func(status vmv1beta1.UpdateStatus, isErr bool) {
		vmc := &vmv1beta1.VMCluster{
			TypeMeta: metav1.TypeMeta{
				APIVersion: vmv1beta1.GroupVersion.String(),
				Kind:       "VMCluster",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmc",
				Namespace: "default",
			},
			Status: vmv1beta1.VMClusterStatus{
				StatusMetadata: vmv1beta1.StatusMetadata{
					UpdateStatus: status,
				},
			},
		}
		rclient := k8stools.GetTestClientWithObjects([]runtime.Object{vmc})
		synctest.Test(t, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			err := waitForStatus(ctx, rclient, vmc.DeepCopy(), 1*time.Second, vmv1beta1.UpdateStatusOperational)
			if isErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	// success
	f(vmv1beta1.UpdateStatusOperational, false)

	// fail
	f(vmv1beta1.UpdateStatusExpanding, true)
}

func TestMergeMaps(t *testing.T) {
	type opts struct {
		prev, new, existing map[string]string
		strategy            vmv1beta1.MetadataStrategy
		want                map[string]string
	}
	f := func(o opts) {
		t.Helper()
		got := mergeMaps(o.existing, o.new, o.prev, o.strategy)
		assert.Equal(t, got, o.want)
	}

	// delete not existing label
	f(opts{
		existing: map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
		new:      map[string]string{"label1": "value1", "label2": "value4"},
		strategy: vmv1beta1.MetadataStrategyPreferProm,
		want:     map[string]string{"label1": "value1", "label2": "value4"},
	})

	// add new label
	f(opts{
		existing: map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
		new:      map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
		strategy: vmv1beta1.MetadataStrategyPreferProm,
		want:     map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
	})

	// add new label with VM priority
	f(opts{
		existing: map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
		new:      map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
		strategy: vmv1beta1.MetadataStrategyPreferVM,
		want:     map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
	})

	// remove all labels
	f(opts{
		new:      map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
		strategy: vmv1beta1.MetadataStrategyPreferVM,
	})

	// remove keep old labels
	f(opts{
		existing: map[string]string{"label1": "value1", "label2": "value4"},
		strategy: vmv1beta1.MetadataStrategyPreferVM,
		want:     map[string]string{"label1": "value1", "label2": "value4"},
	})

	// merge all labels with VMPriority
	f(opts{
		existing: map[string]string{"label1": "value1", "label2": "value4"},
		new:      map[string]string{"label1": "value2", "label2": "value4", "missinglabel": "value10"},
		strategy: vmv1beta1.MetadataStrategyMergeVMPriority,
		want:     map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
	})
}
