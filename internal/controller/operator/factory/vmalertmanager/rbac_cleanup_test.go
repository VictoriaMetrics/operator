package vmalertmanager

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestRBACCleanup(t *testing.T) {
	type opts struct {
		change func(*vmv1beta1.VMAlertmanager)
		owner  string
		keep   bool
	}
	f := func(t *testing.T, o opts) {
		t.Helper()
		synctest.Test(t, func(t *testing.T) {
			ctx := context.Background()
			cr := &vmv1beta1.VMAlertmanager{
				TypeMeta:   metav1.TypeMeta{APIVersion: vmv1beta1.SchemeGroupVersion.String(), Kind: "VMAlertmanager"},
				ObjectMeta: metav1.ObjectMeta{Name: "cleanup", Namespace: "default", UID: "cleanup-owner"},
				Spec:       vmv1beta1.VMAlertmanagerSpec{RollingUpdateStrategy: appsv1.RollingUpdateStatefulSetStrategyType},
			}
			c := k8stools.GetTestClientWithObjects(nil)
			build.AddDefaults(c.Scheme())
			c.Scheme().Default(cr)
			require.NoError(t, CreateOrUpdateConfig(ctx, c, cr, nil))
			require.NoError(t, CreateOrUpdateAlertManager(ctx, cr, c))
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
			objects := []client.Object{buildRole(cr), buildRoleBinding(cr)}
			for _, obj := range objects {
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj), obj))
				obj.SetFinalizers([]string{vmv1beta1.FinalizerName})
				switch o.owner {
				case "external":
					obj.SetOwnerReferences(nil)
				case "foreign":
					owner := cr.AsOwner()
					owner.Name = "other"
					owner.UID = "other-owner"
					obj.SetOwnerReferences([]metav1.OwnerReference{owner})
				}
				require.NoError(t, c.Update(ctx, obj))
			}
			o.change(cr)
			c.Scheme().Default(cr)
			require.NoError(t, CreateOrUpdateAlertManager(ctx, cr, c))
			// Cleanup must remain safe after the last-applied spec advances.
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
			require.NoError(t, CreateOrUpdateAlertManager(ctx, cr, c))
			for _, obj := range objects {
				got := obj.DeepCopyObject().(client.Object)
				err := c.Get(ctx, client.ObjectKeyFromObject(obj), got)
				if !o.keep {
					assert.True(t, k8serrors.IsNotFound(err), "%T: %v", obj, err)
					continue
				}
				require.NoError(t, err)
				assert.Equal(t, obj.GetOwnerReferences(), got.GetOwnerReferences())
				if o.owner != "" {
					assert.Equal(t, obj.GetFinalizers(), got.GetFinalizers())
				}
			}
		})
	}
	externalSA := func(cr *vmv1beta1.VMAlertmanager) { cr.Spec.ServiceAccountName = "external" }
	t.Run("external SA", func(t *testing.T) { f(t, opts{change: externalSA}) })
	t.Run("explicit managed SA name", func(t *testing.T) {
		f(t, opts{change: func(cr *vmv1beta1.VMAlertmanager) { cr.Spec.ServiceAccountName = cr.PrefixedName() }})
	})
	t.Run("owned SA", func(t *testing.T) {
		f(t, opts{change: func(cr *vmv1beta1.VMAlertmanager) { cr.Spec.ReplicaCount = ptr.To(int32(2)) }, keep: true})
	})
	t.Run("disabled VM reloader", func(t *testing.T) {
		f(t, opts{change: func(cr *vmv1beta1.VMAlertmanager) { cr.Spec.UseVMConfigReloader = ptr.To(false) }, keep: true})
	})
	t.Run("unowned RBAC", func(t *testing.T) { f(t, opts{change: externalSA, owner: "external", keep: true}) })
	t.Run("foreign RBAC", func(t *testing.T) { f(t, opts{change: externalSA, owner: "foreign", keep: true}) })
}
