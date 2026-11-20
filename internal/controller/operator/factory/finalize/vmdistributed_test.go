package finalize

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestOnVMDistributedDelete(t *testing.T) {
	type opts struct {
		cr                  *vmv1alpha1.VMDistributed
		predefinedObjects   []runtime.Object
		disabledControllers []string
		vmSingleCRDMissing  bool
		verify              func(client client.Client)
	}

	ctx := context.TODO()

	f := func(o opts) {
		t.Helper()
		build.SetDisabledControllers(sets.New(o.disabledControllers...))
		defer build.SetDisabledControllers(nil)
		fns := k8stools.GetInterceptorsWithObjects(nil)
		if o.vmSingleCRDMissing {
			fns.Get = func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*vmv1beta1.VMSingle); ok {
					return &meta.NoKindMatchError{GroupKind: schema.GroupKind{Group: vmv1beta1.SchemeGroupVersion.Group, Kind: "VMSingle"}}
				}
				return cl.Get(ctx, key, obj, opts...)
			}
		}
		cl := k8stools.GetTestClientWithObjectsAndInterceptors(o.predefinedObjects, fns)

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

	// Retain with VMSingle backend: VMSingle must be disowned, otherwise it is garbage collected
	crSingle := cr.DeepCopy()
	crSingle.Spec.Retain = true
	crSingle.Spec.BackendType = vmv1alpha1.VMDistributedBackendTypeVMSingle

	f(opts{
		cr: crSingle,
		predefinedObjects: []runtime.Object{
			crSingle,
			&vmv1beta1.VMSingle{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crSingle.Spec.Zones[0].VMSingleName(crSingle),
					Namespace: crSingle.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crSingle.AsOwner(),
					},
				},
			},
			&vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      crSingle.Spec.Zones[0].VMAgentName(crSingle),
					Namespace: crSingle.Namespace,
					OwnerReferences: []metav1.OwnerReference{
						crSingle.AsOwner(),
					},
				},
			},
		},
		verify: func(cl client.Client) {
			nsnCR := types.NamespacedName{Name: crSingle.Name, Namespace: crSingle.Namespace}
			var vmd vmv1alpha1.VMDistributed
			err := cl.Get(ctx, nsnCR, &vmd)
			assert.NoError(t, err)
			assert.Empty(t, vmd.Finalizers)

			var single vmv1beta1.VMSingle
			err = cl.Get(ctx, types.NamespacedName{Name: crSingle.Spec.Zones[0].VMSingleName(crSingle), Namespace: crSingle.Namespace}, &single)
			assert.NoError(t, err)
			assert.Empty(t, single.OwnerReferences)

			var agent vmv1beta1.VMAgent
			err = cl.Get(ctx, types.NamespacedName{Name: crSingle.Spec.Zones[0].VMAgentName(crSingle), Namespace: crSingle.Namespace}, &agent)
			assert.NoError(t, err)
			assert.Empty(t, agent.OwnerReferences)
		},
	})

	// Retain with disabled VMSingle controller: VMSingle must be disowned anyway,
	// since it may be reconciled by another operator instance
	crDisabled := crSingle.DeepCopy()
	singleOwned := func(cr *vmv1alpha1.VMDistributed) *vmv1beta1.VMSingle {
		return &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:            cr.Spec.Zones[0].VMSingleName(cr),
				Namespace:       cr.Namespace,
				OwnerReferences: []metav1.OwnerReference{cr.AsOwner()},
			},
		}
	}
	f(opts{
		cr:                  crDisabled,
		predefinedObjects:   []runtime.Object{crDisabled, singleOwned(crDisabled)},
		disabledControllers: []string{"VMSingle", "VMAuth"},
		verify: func(cl client.Client) {
			var vmd vmv1alpha1.VMDistributed
			assert.NoError(t, cl.Get(ctx, types.NamespacedName{Name: crDisabled.Name, Namespace: crDisabled.Namespace}, &vmd))
			assert.Empty(t, vmd.Finalizers)

			var single vmv1beta1.VMSingle
			assert.NoError(t, cl.Get(ctx, types.NamespacedName{Name: crDisabled.Spec.Zones[0].VMSingleName(crDisabled), Namespace: crDisabled.Namespace}, &single))
			assert.Empty(t, single.OwnerReferences)
		},
	})

	// Retain with VMSingle CRD not installed: the missing kind must not block CR deletion
	crNoCRD := crRetain.DeepCopy()
	f(opts{
		cr: crNoCRD,
		predefinedObjects: []runtime.Object{
			crNoCRD,
			&vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:            crNoCRD.Spec.Zones[0].VMAgentName(crNoCRD),
					Namespace:       crNoCRD.Namespace,
					OwnerReferences: []metav1.OwnerReference{crNoCRD.AsOwner()},
				},
			},
		},
		disabledControllers: []string{"VMSingle"},
		vmSingleCRDMissing:  true,
		verify: func(cl client.Client) {
			var vmd vmv1alpha1.VMDistributed
			assert.NoError(t, cl.Get(ctx, types.NamespacedName{Name: crNoCRD.Name, Namespace: crNoCRD.Namespace}, &vmd))
			assert.Empty(t, vmd.Finalizers)

			var agent vmv1beta1.VMAgent
			assert.NoError(t, cl.Get(ctx, types.NamespacedName{Name: crNoCRD.Spec.Zones[0].VMAgentName(crNoCRD), Namespace: crNoCRD.Namespace}, &agent))
			assert.Empty(t, agent.OwnerReferences)
		},
	})
}
