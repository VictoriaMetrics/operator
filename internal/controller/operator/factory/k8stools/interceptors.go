package k8stools

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

// GetInterceptorsWithObjects returns interceptors for objects
func GetInterceptorsWithObjects() interceptor.Funcs {
	return interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			switch v := obj.(type) {
			case *appsv1.StatefulSet:
				v.Status.ObservedGeneration = v.Generation
				v.Status.ReadyReplicas = ptr.Deref(v.Spec.Replicas, 0)
				v.Status.UpdatedReplicas = ptr.Deref(v.Spec.Replicas, 0)
				v.Status.CurrentReplicas = ptr.Deref(v.Spec.Replicas, 0)
				v.Status.UpdateRevision = "v1"
				v.Status.CurrentRevision = "v1"
			case *appsv1.Deployment:
				v.Status.ObservedGeneration = v.Generation
				v.Status.Conditions = append(v.Status.Conditions, appsv1.DeploymentCondition{
					Type:   appsv1.DeploymentProgressing,
					Reason: "NewReplicaSetAvailable",
					Status: "True",
				})
				v.Status.UpdatedReplicas = ptr.Deref(v.Spec.Replicas, 0)
				v.Status.ReadyReplicas = ptr.Deref(v.Spec.Replicas, 0)
			}
			if err := cl.Create(ctx, obj, opts...); err != nil {
				return err
			}
			switch v := obj.(type) {
			case *vmv1beta1.VMAgent:
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
				return cl.Status().Update(ctx, v)
			case *vmv1beta1.VMCluster:
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
				return cl.Status().Update(ctx, v)
			case *vmv1beta1.VMAuth:
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
				return cl.Status().Update(ctx, v)
			}
			return nil
		},
		Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			switch v := obj.(type) {
			case *appsv1.StatefulSet:
				v.Status.ObservedGeneration = v.Generation
				v.Status.ReadyReplicas = ptr.Deref(v.Spec.Replicas, 0)
				v.Status.UpdatedReplicas = ptr.Deref(v.Spec.Replicas, 0)
				v.Status.CurrentReplicas = ptr.Deref(v.Spec.Replicas, 0)
				v.Status.UpdateRevision = "v1"
				v.Status.CurrentRevision = "v1"
			case *appsv1.Deployment:
				v.Status.ObservedGeneration = v.Generation
				v.Status.UpdatedReplicas = ptr.Deref(v.Spec.Replicas, 0)
				v.Status.ReadyReplicas = ptr.Deref(v.Spec.Replicas, 0)
				v.Status.Replicas = ptr.Deref(v.Spec.Replicas, 0)
			}
			if err := cl.Update(ctx, obj, opts...); err != nil {
				return err
			}
			switch v := obj.(type) {
			case *vmv1beta1.VMAgent:
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
				return cl.Status().Update(ctx, v)
			case *vmv1beta1.VMCluster:
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
				return cl.Status().Update(ctx, v)
			case *vmv1beta1.VMAuth:
				v.Status.UpdateStatus = vmv1beta1.UpdateStatusOperational
				v.Status.ObservedGeneration = v.Generation
				return cl.Status().Update(ctx, v)
			}
			return nil
		},
	}
}

// NewActionRecordingInterceptor returns an interceptor that records actions to the provided slice pointer.
// It wraps an existing interceptorFuncs if provided.
func NewActionRecordingInterceptor(actions *[]ClientAction, wrapped *interceptor.Funcs) interceptor.Funcs {
	return interceptor.Funcs{
		Create: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.CreateOption) error {
			gvk, _ := apiutil.GVKForObject(obj, cl.Scheme())
			*actions = append(*actions, ClientAction{Verb: "Create", Kind: gvk.Kind, Resource: client.ObjectKeyFromObject(obj)})
			if wrapped != nil && wrapped.Create != nil {
				return wrapped.Create(ctx, cl, obj, opts...)
			}
			return cl.Create(ctx, obj, opts...)
		},
		Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
			gvk, _ := apiutil.GVKForObject(obj, cl.Scheme())
			*actions = append(*actions, ClientAction{Verb: "Get", Kind: gvk.Kind, Resource: key})
			if wrapped != nil && wrapped.Get != nil {
				return wrapped.Get(ctx, cl, key, obj, opts...)
			}
			return cl.Get(ctx, key, obj, opts...)
		},
		Update: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.UpdateOption) error {
			gvk, _ := apiutil.GVKForObject(obj, cl.Scheme())
			*actions = append(*actions, ClientAction{Verb: "Update", Kind: gvk.Kind, Resource: client.ObjectKeyFromObject(obj)})
			if wrapped != nil && wrapped.Update != nil {
				return wrapped.Update(ctx, cl, obj, opts...)
			}
			return cl.Update(ctx, obj, opts...)
		},
		Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
			gvk, _ := apiutil.GVKForObject(obj, cl.Scheme())
			*actions = append(*actions, ClientAction{Verb: "Delete", Kind: gvk.Kind, Resource: client.ObjectKeyFromObject(obj)})
			if wrapped != nil && wrapped.Delete != nil {
				return wrapped.Delete(ctx, cl, obj, opts...)
			}
			return cl.Delete(ctx, obj, opts...)
		},
		Patch: func(ctx context.Context, cl client.WithWatch, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
			gvk, _ := apiutil.GVKForObject(obj, cl.Scheme())
			*actions = append(*actions, ClientAction{Verb: "Patch", Kind: gvk.Kind, Resource: client.ObjectKeyFromObject(obj)})
			if wrapped != nil && wrapped.Patch != nil {
				return wrapped.Patch(ctx, cl, obj, patch, opts...)
			}
			return cl.Patch(ctx, obj, patch, opts...)
		},
	}
}
