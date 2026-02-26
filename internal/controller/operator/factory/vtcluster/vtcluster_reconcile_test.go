package vtcluster

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr                *vmv1.VTCluster
		predefinedObjects []runtime.Object
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	f := func(args args, want want) {
		t.Helper()

		// Use local scheme to avoid global scheme pollution
		s := runtime.NewScheme()
		_ = scheme.AddToScheme(s)
		_ = vmv1.AddToScheme(s)
		_ = vmv1beta1.AddToScheme(s)
		build.AddDefaults(s)
		s.Default(args.cr)

		var actions []k8stools.ClientAction
		objInterceptors := k8stools.GetInterceptorsWithObjects()
		actionInterceptor := k8stools.NewActionRecordingInterceptor(&actions, &objInterceptors)

		fclient := fake.NewClientBuilder().
			WithScheme(s).
			WithStatusSubresource(&vmv1.VTCluster{}).
			WithRuntimeObjects(args.predefinedObjects...).
			WithInterceptorFuncs(actionInterceptor).
			Build()

		ctx := context.TODO()
		err := CreateOrUpdate(ctx, fclient, args.cr)
		if want.err != nil {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}

		if !assert.Equal(t, len(want.actions), len(actions)) {
			for i, action := range actions {
				t.Logf("Action %d: %s %s %s", i, action.Verb, action.Kind, action.Resource)
			}
		}

		for i, action := range want.actions {
			if i >= len(actions) {
				break
			}
			assert.Equal(t, action.Verb, actions[i].Verb, "idx %d verb", i)
			assert.Equal(t, action.Kind, actions[i].Kind, "idx %d kind", i)
			assert.Equal(t, action.Resource, actions[i].Resource, "idx %d resource", i)
		}
	}

	name := "example-cluster"
	namespace := "default"
	saName := types.NamespacedName{Namespace: namespace, Name: "vtcluster-" + name}
	vtstorageName := types.NamespacedName{Namespace: namespace, Name: "vtstorage-" + name}
	vtselectName := types.NamespacedName{Namespace: namespace, Name: "vtselect-" + name}
	vtinsertName := types.NamespacedName{Namespace: namespace, Name: "vtinsert-" + name}

	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}
	vtstorageMeta := metav1.ObjectMeta{Name: vtstorageName.Name, Namespace: namespace}
	vtselectMeta := metav1.ObjectMeta{Name: vtselectName.Name, Namespace: namespace}
	vtinsertMeta := metav1.ObjectMeta{Name: vtinsertName.Name, Namespace: namespace}

	// create vtcluster with all components
	f(args{
		cr: &vmv1.VTCluster{
			ObjectMeta: objectMeta,
			Spec: vmv1.VTClusterSpec{
				Storage: &vmv1.VTStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				Select: &vmv1.VTSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				Insert: &vmv1.VTInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				// ServiceAccount
				{Verb: "Get", Kind: "ServiceAccount", Resource: saName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: saName},

				// VTStorage
				{Verb: "Get", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vtstorageName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vtstorageName},
				{Verb: "Create", Kind: "Service", Resource: vtstorageName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtstorageName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vtstorageName},

				// VTSelect
				{Verb: "Get", Kind: "Service", Resource: vtselectName},
				{Verb: "Create", Kind: "Service", Resource: vtselectName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtselectName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vtselectName},
				{Verb: "Get", Kind: "Deployment", Resource: vtselectName},
				{Verb: "Create", Kind: "Deployment", Resource: vtselectName},
				{Verb: "Get", Kind: "Deployment", Resource: vtselectName}, // wait for ready

				// VTInsert
				{Verb: "Get", Kind: "Deployment", Resource: vtinsertName},
				{Verb: "Create", Kind: "Deployment", Resource: vtinsertName},
				{Verb: "Get", Kind: "Deployment", Resource: vtinsertName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vtinsertName},
				{Verb: "Create", Kind: "Service", Resource: vtinsertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtinsertName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vtinsertName},
			},
		})

	// update vtcluster with changes
	f(args{
		cr: &vmv1.VTCluster{
			ObjectMeta: objectMeta,
			Spec: vmv1.VTClusterSpec{
				Storage: &vmv1.VTStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
					RollingUpdateStrategy: appsv1.RollingUpdateStatefulSetStrategyType,
				},
				Select: &vmv1.VTSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
					UpdateStrategy: ptr.To(appsv1.RollingUpdateDeploymentStrategyType),
				},
				Insert: &vmv1.VTInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
					UpdateStrategy: ptr.To(appsv1.RollingUpdateDeploymentStrategyType),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saName.Name, Namespace: namespace}},
			// VTStorage resources
			&appsv1.StatefulSet{
				ObjectMeta: vtstorageMeta,
				Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				Status: appsv1.StatefulSetStatus{
					Replicas:           1,
					ReadyReplicas:      1,
					UpdatedReplicas:    1,
					ObservedGeneration: 1,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vtstorageName.Name + "-0",
					Namespace: namespace,
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vtstorage",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
						"controller-revision-hash":    "v1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "StatefulSet",
							Name:       vtstorageName.Name,
							Controller: ptr.To(true),
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			&corev1.Service{
				ObjectMeta: vtstorageMeta,
				Spec: corev1.ServiceSpec{
					ClusterIP: "None",
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 8482, TargetPort: intstr.FromInt(8482), Protocol: "TCP"},
						{Name: "vminsert", Port: 8400, TargetPort: intstr.FromInt(8400), Protocol: "TCP"},
						{Name: "vmselect", Port: 8401, TargetPort: intstr.FromInt(8401), Protocol: "TCP"},
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: vtstorageMeta},
			// VTSelect resources
			&appsv1.StatefulSet{
				ObjectMeta: vtselectMeta,
				Spec:       appsv1.StatefulSetSpec{Replicas: ptr.To(int32(1))},
				Status: appsv1.StatefulSetStatus{
					Replicas:           1,
					ReadyReplicas:      1,
					UpdatedReplicas:    1,
					ObservedGeneration: 1,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vtselectName.Name + "-0",
					Namespace: namespace,
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vtselect",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
						"controller-revision-hash":    "v1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "StatefulSet",
							Name:       vtselectName.Name,
							Controller: ptr.To(true),
						},
					},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{Type: corev1.PodReady, Status: corev1.ConditionTrue},
					},
				},
			},
			&corev1.Service{
				ObjectMeta: vtselectMeta,
				Spec: corev1.ServiceSpec{
					ClusterIP: "None",
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 8481, TargetPort: intstr.FromInt(8481), Protocol: "TCP"},
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: vtselectMeta},
			// VTInsert resources
			&appsv1.Deployment{
				ObjectMeta: vtinsertMeta,
				Spec:       appsv1.DeploymentSpec{Replicas: ptr.To(int32(1))},
				Status: appsv1.DeploymentStatus{
					Replicas:           1,
					ReadyReplicas:      1,
					UpdatedReplicas:    1,
					ObservedGeneration: 1,
				},
			},
			&corev1.Service{
				ObjectMeta: vtinsertMeta,
				Spec: corev1.ServiceSpec{
					ClusterIP: "None",
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 8480, TargetPort: intstr.FromInt(8480), Protocol: "TCP"},
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: vtinsertMeta},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				// ServiceAccount
				{Verb: "Get", Kind: "ServiceAccount", Resource: saName},
				{Verb: "Update", Kind: "ServiceAccount", Resource: saName},

				// VTStorage
				{Verb: "Get", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Delete", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vtstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vtstorageName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vtstorageName},
				{Verb: "Delete", Kind: "Service", Resource: vtstorageName},
				{Verb: "Create", Kind: "Service", Resource: vtstorageName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtstorageName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vtstorageName},

				// VTSelect
				{Verb: "Get", Kind: "Service", Resource: vtselectName},
				{Verb: "Delete", Kind: "Service", Resource: vtselectName},
				{Verb: "Create", Kind: "Service", Resource: vtselectName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtselectName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vtselectName},
				{Verb: "Get", Kind: "Deployment", Resource: vtselectName},
				{Verb: "Create", Kind: "Deployment", Resource: vtselectName},
				{Verb: "Get", Kind: "Deployment", Resource: vtselectName}, // wait for ready

				// VTInsert
				{Verb: "Get", Kind: "Deployment", Resource: vtinsertName},
				{Verb: "Update", Kind: "Deployment", Resource: vtinsertName},
				{Verb: "Get", Kind: "Deployment", Resource: vtinsertName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vtinsertName},
				{Verb: "Delete", Kind: "Service", Resource: vtinsertName},
				{Verb: "Create", Kind: "Service", Resource: vtinsertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vtinsertName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vtinsertName},
			},
		})
}
