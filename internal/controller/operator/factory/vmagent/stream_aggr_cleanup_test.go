package vmagent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestStreamAggrCleanup(t *testing.T) {
	rules := &vmv1beta1.StreamAggrConfig{Rules: []vmv1beta1.StreamAggrRule{{
		Interval: "1m", Outputs: []string{"total"},
	}}}
	type opts struct {
		name       string
		remote     bool
		referenced bool
		keep       bool
		mutate     func(*corev1.ConfigMap)
		wantExists bool
	}
	f := func(o opts) {
		t.Helper()
		t.Run(o.name, func(t *testing.T) {
			ctx := context.Background()
			cr := &vmv1beta1.VMAgent{
				TypeMeta:   metav1.TypeMeta{APIVersion: vmv1beta1.SchemeGroupVersion.String(), Kind: "VMAgent"},
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default", UID: "test-uid"},
			}
			aggr := rules
			if o.referenced {
				aggr = &vmv1beta1.StreamAggrConfig{RuleConfigMap: &corev1.ConfigMapKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: "external"}, Key: "rules",
				}}
			}
			if o.remote {
				cr.Spec.RemoteWrite = []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote", StreamAggrConfig: aggr}}
			} else {
				cr.Spec.StreamAggrConfig = aggr
			}
			external := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "external", Namespace: cr.Namespace}, Data: map[string]string{"rules": "- interval: 1m\n  outputs: [total]\n"}}
			cl := k8stools.GetTestClientWithObjects([]runtime.Object{external})
			ac := build.NewAssetsCache(ctx, cl, nil)
			require.NoError(t, createOrUpdateStreamAggrConfig(ctx, cl, cr, nil, ac))
			key := types.NamespacedName{Name: build.ResourceName(build.StreamAggrConfigResourceKind, cr), Namespace: cr.Namespace}
			var cm corev1.ConfigMap
			require.NoError(t, cl.Get(ctx, key, &cm))
			if o.mutate != nil {
				o.mutate(&cm)
				require.NoError(t, cl.Update(ctx, &cm))
			}
			prevCR := cr.DeepCopy()
			if !o.keep {
				cr.Spec.StreamAggrConfig = nil
				cr.Spec.RemoteWrite = nil
			}
			for range 2 {
				require.NoError(t, deleteOrphaned(ctx, cl, cr, config.MustGetBaseConfig()))
				require.NoError(t, createOrUpdateStreamAggrConfig(ctx, cl, cr, prevCR, ac))
				err := cl.Get(ctx, key, &cm)
				if o.wantExists {
					require.NoError(t, err)
				} else {
					require.True(t, k8serrors.IsNotFound(err), "expected removed ConfigMap, got %v", err)
				}
				var got corev1.ConfigMap
				require.NoError(t, cl.Get(ctx, client.ObjectKeyFromObject(external), &got))
				require.Equal(t, external.Data, got.Data)
			}
		})
	}

	f(opts{name: "remove global rules"})
	f(opts{name: "remove remote rules", remote: true})
	f(opts{name: "remove referenced global rules", referenced: true})
	f(opts{name: "remove referenced remote rules", remote: true, referenced: true})
	f(opts{name: "keep global rules", keep: true, wantExists: true})
	f(opts{name: "keep remote rules", remote: true, keep: true, wantExists: true})
	f(opts{name: "keep referenced rules", referenced: true, keep: true, wantExists: true})
	f(opts{name: "unowned same name", mutate: func(cm *corev1.ConfigMap) {
		cm.OwnerReferences = nil
	}, wantExists: true})
	f(opts{name: "foreign owner", mutate: func(cm *corev1.ConfigMap) {
		cm.OwnerReferences[0].Name = "other"
	}, wantExists: true})
	f(opts{name: "legacy finalizer", mutate: func(cm *corev1.ConfigMap) {
		cm.Finalizers = []string{vmv1beta1.FinalizerName}
	}})
}

func TestStreamAggrCleanupErrors(t *testing.T) {
	f := func(operation string) {
		t.Helper()
		t.Run(operation, func(t *testing.T) {
			ctx := context.Background()
			cr := &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"}}
			cm := &corev1.ConfigMap{ObjectMeta: build.ResourceMeta(build.StreamAggrConfigResourceKind, cr)}
			cm.Finalizers = []string{vmv1beta1.FinalizerName}
			wantErr := errors.New("cleanup failed")
			fns := interceptor.Funcs{
				Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if _, ok := obj.(*corev1.ConfigMap); ok && operation == "get" {
						return wantErr
					}
					return cl.Get(ctx, key, obj, opts...)
				},
				Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					if _, ok := obj.(*corev1.ConfigMap); ok && operation == "patch" {
						return wantErr
					}
					return cl.Patch(ctx, obj, patch, opts...)
				},
				Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
					if _, ok := obj.(*corev1.ConfigMap); ok && operation == "delete" {
						return wantErr
					}
					return cl.Delete(ctx, obj, opts...)
				},
			}
			cl := k8stools.GetTestClientWithObjectsAndInterceptors([]runtime.Object{cm}, fns)
			require.ErrorIs(t, deleteOrphaned(ctx, cl, cr, config.MustGetBaseConfig()), wantErr)
		})
	}

	f("get")
	f("patch")
	f("delete")
}
