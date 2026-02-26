package vmcluster

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

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr                *vmv1beta1.VMCluster
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
		_ = vmv1beta1.AddToScheme(s)
		build.AddDefaults(s)
		s.Default(args.cr)

		var actions []k8stools.ClientAction
		objInterceptors := k8stools.GetInterceptorsWithObjects()
		actionInterceptor := k8stools.NewActionRecordingInterceptor(&actions, &objInterceptors)

		fclient := fake.NewClientBuilder().
			WithScheme(s).
			WithStatusSubresource(&vmv1beta1.VMCluster{}).
			WithRuntimeObjects(args.predefinedObjects...).
			WithInterceptorFuncs(actionInterceptor).
			Build()

		ctx := context.TODO()
		err := CreateOrUpdate(ctx, args.cr, fclient)
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
	saName := types.NamespacedName{Namespace: namespace, Name: "vmcluster-" + name}
	vmstorageName := types.NamespacedName{Namespace: namespace, Name: "vmstorage-" + name}
	vmselectName := types.NamespacedName{Namespace: namespace, Name: "vmselect-" + name}
	vminsertName := types.NamespacedName{Namespace: namespace, Name: "vminsert-" + name}

	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}
	vmstorageMeta := metav1.ObjectMeta{Name: vmstorageName.Name, Namespace: namespace}
	vmselectMeta := metav1.ObjectMeta{Name: vmselectName.Name, Namespace: namespace}
	vminsertMeta := metav1.ObjectMeta{Name: vminsertName.Name, Namespace: namespace}

	// create vmcluster with all components
	f(args{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod: "1",
				VMStorage: &vmv1beta1.VMStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
				VMInsert: &vmv1beta1.VMInsert{
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

				// VMStorage
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vmstorageName},
				{Verb: "Create", Kind: "Service", Resource: vmstorageName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmstorageName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmstorageName},

				// VMSelect
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vmselectName},
				{Verb: "Create", Kind: "Service", Resource: vmselectName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmselectName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmselectName},

				// VMInsert
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Create", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName}, // wait for ready
				{Verb: "Get", Kind: "Service", Resource: vminsertName},
				{Verb: "Create", Kind: "Service", Resource: vminsertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vminsertName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vminsertName},
			},
		})

	// update vmcluster with changes
	f(args{
		cr: &vmv1beta1.VMCluster{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod: "1",
				VMStorage: &vmv1beta1.VMStorage{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
					RollingUpdateStrategy: appsv1.RollingUpdateStatefulSetStrategyType,
				},
				VMSelect: &vmv1beta1.VMSelect{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
					RollingUpdateStrategy: appsv1.RollingUpdateStatefulSetStrategyType,
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
						ReplicaCount: ptr.To(int32(1)),
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: saName.Name, Namespace: namespace}},
			// VMStorage resources
			&appsv1.StatefulSet{
				ObjectMeta: vmstorageMeta,
				Spec: appsv1.StatefulSetSpec{
					ServiceName: vmstorageName.Name,
					Replicas:    ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":      "vmstorage",
							"app.kubernetes.io/instance":  name,
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
					Replicas:           1,
					ReadyReplicas:      1,
					UpdatedReplicas:    1,
					ObservedGeneration: 1,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vmstorageName.Name + "-0",
					Namespace: namespace,
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmstorage",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
						"controller-revision-hash":    "v1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "StatefulSet",
							Name:       vmstorageName.Name,
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
				ObjectMeta: vmstorageMeta,
				Spec: corev1.ServiceSpec{
					ClusterIP: "None",
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 8482, TargetPort: intstr.FromInt(8482), Protocol: "TCP"},
						{Name: "vminsert", Port: 8400, TargetPort: intstr.FromInt(8400), Protocol: "TCP"},
						{Name: "vmselect", Port: 8401, TargetPort: intstr.FromInt(8401), Protocol: "TCP"},
					},
					Selector: map[string]string{
						"app.kubernetes.io/name":      "vmstorage",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: vmstorageMeta},
			// VMSelect resources
			&appsv1.StatefulSet{
				ObjectMeta: vmselectMeta,
				Spec: appsv1.StatefulSetSpec{
					ServiceName: vmselectName.Name,
					Replicas:    ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":      "vmselect",
							"app.kubernetes.io/instance":  name,
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
				},
				Status: appsv1.StatefulSetStatus{
					Replicas:           1,
					ReadyReplicas:      1,
					UpdatedReplicas:    1,
					ObservedGeneration: 1,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vmselectName.Name + "-0",
					Namespace: namespace,
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmselect",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
						"controller-revision-hash":    "v1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "StatefulSet",
							Name:       vmselectName.Name,
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
				ObjectMeta: vmselectMeta,
				Spec: corev1.ServiceSpec{
					ClusterIP: "None",
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 8481, TargetPort: intstr.FromInt(8481), Protocol: "TCP"},
					},
					Selector: map[string]string{
						"app.kubernetes.io/name":      "vmselect",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: vmselectMeta},
			// VMInsert resources
			&appsv1.Deployment{
				ObjectMeta: vminsertMeta,
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":      "vminsert",
							"app.kubernetes.io/instance":  name,
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
				},
				Status: appsv1.DeploymentStatus{
					Replicas:           1,
					ReadyReplicas:      1,
					UpdatedReplicas:    1,
					ObservedGeneration: 1,
				},
			},
			&corev1.Service{
				ObjectMeta: vminsertMeta,
				Spec: corev1.ServiceSpec{
					ClusterIP: "None",
					Ports: []corev1.ServicePort{
						{Name: "http", Port: 8480, TargetPort: intstr.FromInt(8480), Protocol: "TCP"},
					},
					Selector: map[string]string{
						"app.kubernetes.io/name":      "vminsert",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: vminsertMeta},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				// ServiceAccount
				{Verb: "Get", Kind: "ServiceAccount", Resource: saName},
				{Verb: "Update", Kind: "ServiceAccount", Resource: saName},

				// VMStorage
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Delete", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmstorageName},
				{Verb: "Get", Kind: "Service", Resource: vmstorageName},
				{Verb: "Delete", Kind: "Service", Resource: vmstorageName},
				{Verb: "Create", Kind: "Service", Resource: vmstorageName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmstorageName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vmstorageName},

				// VMSelect
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Delete", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmselectName},
				{Verb: "Get", Kind: "Service", Resource: vmselectName},
				{Verb: "Delete", Kind: "Service", Resource: vmselectName},
				{Verb: "Create", Kind: "Service", Resource: vmselectName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmselectName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vmselectName},

				// VMInsert
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Update", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Get", Kind: "Deployment", Resource: vminsertName},
				{Verb: "Get", Kind: "Service", Resource: vminsertName},
				{Verb: "Delete", Kind: "Service", Resource: vminsertName},
				{Verb: "Create", Kind: "Service", Resource: vminsertName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vminsertName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vminsertName},
			},
		})
}
