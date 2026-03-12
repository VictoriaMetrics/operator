package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVMDistributedDelete(t *testing.T) {
	type opts struct {
		cr                *vmv1alpha1.VMDistributed
		predefinedObjects []runtime.Object
		verify            func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		err := OnVMDistributedDelete(ctx, cl, o.cr)
		assert.NoError(t, err)

		if o.verify != nil {
			o.verify(cl)
		}
	}

	cr := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1alpha1",
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       "test-vmdistributed",
			Namespace:  "default",
			Finalizers: []string{vmv1beta1.FinalizerName},
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			Zones: []vmv1alpha1.VMDistributedZone{
				{Name: "zone1"},
			},
		},
	}

	f(opts{
		cr:                cr,
		predefinedObjects: []runtime.Object{cr},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: cr.Name, Namespace: cr.Namespace}
			var vmd vmv1alpha1.VMDistributed
			err := cl.Get(ctx, nsnCR, &vmd)
			assert.NoError(t, err)
			assert.Empty(t, vmd.Finalizers)
		},
	})

	// Retain mode
	crRetain := cr.DeepCopy()
	crRetain.Spec.Retain = true

	f(opts{
		cr: crRetain,
		predefinedObjects: []runtime.Object{
			crRetain,
			&vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crRetain.Spec.Zones[0].VMClusterName(crRetain),
					Namespace: crRetain.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crRetain.AsOwner(),
						{Name: "other-owner", Kind: "Pod", APIVersion: "v1"},
					},
				},
			},
			&vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crRetain.Spec.Zones[0].VMAgentName(crRetain),
					Namespace: crRetain.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crRetain.AsOwner(),
					},
				},
			},
			&vmv1beta1.VMAuth{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crRetain.VMAuthName(),
					Namespace: crRetain.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crRetain.AsOwner(),
					},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: crRetain.Name, Namespace: crRetain.Namespace}
			var vmd vmv1alpha1.VMDistributed
			err := cl.Get(ctx, nsnCR, &vmd)
			assert.NoError(t, err)
			assert.Empty(t, vmd.Finalizers)

			nsnCluster := types.NamespacedName{Name: crRetain.Spec.Zones[0].VMClusterName(crRetain), Namespace: crRetain.Namespace}
			var cluster vmv1beta1.VMCluster
			err = cl.Get(ctx, nsnCluster, &cluster)
			assert.NoError(t, err)
			assert.Len(t, cluster.OwnerReferences, 1)
			assert.Equal(t, "other-owner", cluster.OwnerReferences[0].Name)

			nsnAgent := types.NamespacedName{Name: crRetain.Spec.Zones[0].VMAgentName(crRetain), Namespace: crRetain.Namespace}
			var agent vmv1beta1.VMAgent
			err = cl.Get(ctx, nsnAgent, &agent)
			assert.NoError(t, err)
			assert.Empty(t, agent.OwnerReferences)

			nsnAuth := types.NamespacedName{Name: crRetain.VMAuthName(), Namespace: crRetain.Namespace}
			var auth vmv1beta1.VMAuth
			err = cl.Get(ctx, nsnAuth, &auth)
			assert.NoError(t, err)
			assert.Empty(t, auth.OwnerReferences)
		},
	})
}
