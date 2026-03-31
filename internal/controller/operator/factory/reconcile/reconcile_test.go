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
