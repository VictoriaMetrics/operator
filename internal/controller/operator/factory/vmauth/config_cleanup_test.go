package vmauth

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestConfigCleanup(t *testing.T) {
	type opts struct {
		change     func(*vmv1beta1.VMAuth)
		keepRBAC   bool
		keepConfig bool
	}
	f := func(o opts) {
		t.Helper()
		synctest.Test(t, func(t *testing.T) {
			ctx := context.Background()
			cr := &vmv1beta1.VMAuth{
				TypeMeta:   metav1.TypeMeta{APIVersion: vmv1beta1.SchemeGroupVersion.String(), Kind: "VMAuth"},
				ObjectMeta: metav1.ObjectMeta{Name: "cleanup", Namespace: "default", UID: "cleanup-owner"},
			}
			c := k8stools.GetTestClientWithObjects(nil)
			build.AddDefaults(c.Scheme())
			c.Scheme().Default(cr)
			require.NoError(t, CreateOrUpdate(ctx, cr, c))
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()

			objects := []struct {
				object client.Object
				keep   bool
			}{
				{&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}}, o.keepRBAC},
				{&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}}, o.keepRBAC},
				{&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace}}, o.keepRBAC},
				{&corev1.Secret{ObjectMeta: buildConfigSecretMeta(cr)}, o.keepConfig},
			}
			for _, obj := range objects {
				require.NoError(t, c.Get(ctx, client.ObjectKeyFromObject(obj.object), obj.object))
			}
			o.change(cr)
			c.Scheme().Default(cr)
			for range 2 {
				require.NoError(t, CreateOrUpdate(ctx, cr, c))
				cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
			}
			for _, obj := range objects {
				err := c.Get(ctx, client.ObjectKeyFromObject(obj.object), obj.object.DeepCopyObject().(client.Object))
				if obj.keep {
					require.NoError(t, err, "%T", obj.object)
				} else {
					require.True(t, k8serrors.IsNotFound(err), "%T: %v", obj.object, err)
				}
			}
		})
	}
	f(opts{
		change:     func(cr *vmv1beta1.VMAuth) { cr.Spec.ServiceAccountName = "external" },
		keepConfig: true,
	})
	f(opts{
		change: func(cr *vmv1beta1.VMAuth) {
			cr.Spec.SecretRef = &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "external"}}
		},
		keepRBAC: true,
	})
	f(opts{
		change:   func(cr *vmv1beta1.VMAuth) { cr.Spec.LocalPath = "/external" },
		keepRBAC: true,
	})
	f(opts{
		change: func(cr *vmv1beta1.VMAuth) {
			cr.Spec.SecretRef = &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: cr.ConfigSecretName()}}
		},
		keepRBAC:   true,
		keepConfig: true,
	})
	f(opts{
		change: func(cr *vmv1beta1.VMAuth) {
			cr.Spec.ServiceAccountName = "external"
			cr.Spec.SecretRef = &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "external"}}
		},
	})
}
