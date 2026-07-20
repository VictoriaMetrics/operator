package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVLDistributedDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1alpha1.VLDistributed
		predefinedObjects []runtime.Object
		verify            func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnVLDistributedDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1alpha1.VLDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1alpha1",
			Kind:       "VLDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vldistributed",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1alpha1.VLDistributedSpec{
			Zones: []vmv1alpha1.VLDistributedZone{
				{Name: "zone1"},
			},
		},
	}

	f(opts{
		cr:                cr,
		predefinedObjects: []runtime.Object{cr},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			var vld vmv1alpha1.VLDistributed
			err := cl.Get(ctx, nsnCR, &vld)
			assert.NoError(t, err)
			assert.Empty(t, vld.Finalizers)
		},
	})

	// Retain mode
	crRetain := cr.DeepCopy()
	crRetain.Spec.Retain = true

	f(opts{
		cr: crRetain,
		predefinedObjects: []runtime.Object{
			crRetain,
			&vmv1.VLCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crRetain.Spec.Zones[0].VLClusterName(crRetain),
					Namespace: crRetain.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crRetain.AsOwner(),
						{Name: "other-owner", Kind: "Pod", APIVersion: "v1"},
					},
				},
			},
			&vmv1.VLAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crRetain.Spec.Zones[0].VLAgentName(crRetain),
					Namespace: crRetain.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crRetain.AsOwner(),
					},
				},
			},
			&vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crRetain.VLAuthName(),
					Namespace: crRetain.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crRetain.AsOwner(),
					},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: crRetain.Name, Namespace: crRetain.Namespace}
			var vld vmv1alpha1.VLDistributed
			err := cl.Get(ctx, nsnCR, &vld)
			assert.NoError(t, err)
			assert.Empty(t, vld.Finalizers)

			nsnCluster := types.NamespacedName{Name: crRetain.Spec.Zones[0].VLClusterName(crRetain), Namespace: crRetain.Namespace}
			var cluster vmv1.VLCluster
			err = cl.Get(ctx, nsnCluster, &cluster)
			assert.NoError(t, err)
			assert.Len(t, cluster.OwnerReferences, 1)
			assert.Equal(t, "other-owner", cluster.OwnerReferences[0].Name)

			nsnAgent := types.NamespacedName{Name: crRetain.Spec.Zones[0].VLAgentName(crRetain), Namespace: crRetain.Namespace}
			var agent vmv1.VLAgent
			err = cl.Get(ctx, nsnAgent, &agent)
			assert.NoError(t, err)
			assert.Empty(t, agent.OwnerReferences)

			nsnAuth := types.NamespacedName{Name: crRetain.VLAuthName(), Namespace: crRetain.Namespace}
			var auth vmv1beta1.VMAuth
			err = cl.Get(ctx, nsnAuth, &auth)
			assert.NoError(t, err)
			assert.Empty(t, auth.OwnerReferences)
		},
	})

	// Retain after backend-type change: both VLCluster and VLSingle must be disowned
	// regardless of current BackendType to prevent GC of resources created under the old type.
	crChanged := cr.DeepCopy()
	crChanged.Spec.Retain = true
	crChanged.Spec.BackendType = vmv1alpha1.VLDistributedBackendTypeVLSingle

	f(opts{
		cr: crChanged,
		predefinedObjects: []runtime.Object{
			crChanged,
			&vmv1.VLCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crChanged.Spec.Zones[0].VLClusterName(crChanged),
					Namespace: crChanged.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crChanged.AsOwner(),
					},
				},
			},
			&vmv1.VLSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crChanged.Spec.Zones[0].VLSingleName(crChanged),
					Namespace: crChanged.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crChanged.AsOwner(),
					},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: crChanged.Name, Namespace: crChanged.Namespace}
			var vld vmv1alpha1.VLDistributed
			err := cl.Get(ctx, nsnCR, &vld)
			assert.NoError(t, err)
			assert.Empty(t, vld.Finalizers)

			var cluster vmv1.VLCluster
			err = cl.Get(ctx, types.NamespacedName{Name: crChanged.Spec.Zones[0].VLClusterName(crChanged), Namespace: crChanged.Namespace}, &cluster)
			assert.NoError(t, err)
			assert.Empty(t, cluster.OwnerReferences)

			var single vmv1.VLSingle
			err = cl.Get(ctx, types.NamespacedName{Name: crChanged.Spec.Zones[0].VLSingleName(crChanged), Namespace: crChanged.Namespace}, &single)
			assert.NoError(t, err)
			assert.Empty(t, single.OwnerReferences)
		},
	})
}
