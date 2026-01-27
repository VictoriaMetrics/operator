package vmdistributed

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestFetchMetricValues(t *testing.T) {
	f := func(body, metric, dimension string, expected map[string]float64) {
		t.Helper()
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, body)
		}))
		defer ts.Close()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		values, err := fetchMetricValues(ctx, ts.Client(), ts.URL, metric, dimension)
		assert.NoError(t, err)

		assert.Equal(t, expected, values)
	}

	// test metric
	f(`
test_metric{path="no path"} 24
`, "test_metric", "path", map[string]float64{
		"no path": 24,
	})
}

func TestSetOwnerRefIfNeeded(t *testing.T) {
	cr := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vdc",
			Namespace: "default",
			UID:       k8stypes.UID("owner-uid"),
		},
	}
	vmc := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1beta1.GroupVersion.String(),
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc",
			Namespace: "default",
		},
	}
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr, vmc})
	modified, err := setOwnerRefIfNeeded(cr, vmc, rclient.Scheme())
	assert.NoError(t, err)
	assert.True(t, modified)
	assert.Len(t, vmc.OwnerReferences, 1)
	assert.Equal(t, "vdc", vmc.OwnerReferences[0].Name)

	// second call should detect owner ref already set
	modified2, err := setOwnerRefIfNeeded(cr, vmc, rclient.Scheme())
	assert.NoError(t, err)
	assert.False(t, modified2)
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
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := waitForStatus(ctx, rclient, vmc.DeepCopy(), 1*time.Second, vmv1beta1.UpdateStatusOperational)
		if isErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}

	// success
	f(vmv1beta1.UpdateStatusOperational, false)

	// fail
	f(vmv1beta1.UpdateStatusExpanding, true)
}
