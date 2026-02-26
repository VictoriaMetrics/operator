package vmalertmanager

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
		cr                *vmv1beta1.VMAlertmanager
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
			WithStatusSubresource(&vmv1beta1.VMAlertmanager{}).
			WithRuntimeObjects(args.predefinedObjects...).
			WithInterceptorFuncs(actionInterceptor).
			Build()

		ctx := context.TODO()
		err := CreateOrUpdateAlertManager(ctx, args.cr, fclient)
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

	name := "vmalertmanager"
	namespace := "default"
	vmalertmanagerName := types.NamespacedName{Namespace: namespace, Name: "vmalertmanager-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}
	childObjectMeta := metav1.ObjectMeta{Name: vmalertmanagerName.Name, Namespace: namespace}

	// create vmalertmanager with default config
	f(args{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: objectMeta,
			Spec:       vmv1beta1.VMAlertmanagerSpec{},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Role", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "Role", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "RoleBinding", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "RoleBinding", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Service", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "Service", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
			},
		})

	// update vmalertmanager
	f(args{
		cr: &vmv1beta1.VMAlertmanager{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMAlertmanagerSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				RollingUpdateStrategy: appsv1.RollingUpdateStatefulSetStrategyType,
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ServiceAccount{ObjectMeta: childObjectMeta},
			&rbacv1.Role{ObjectMeta: childObjectMeta},
			&rbacv1.RoleBinding{ObjectMeta: childObjectMeta},
			&corev1.Service{
				ObjectMeta: childObjectMeta,
				Spec: corev1.ServiceSpec{
					ClusterIP:                "None",
					PublishNotReadyAddresses: true,
					Selector: map[string]string{
						"app.kubernetes.io/name":      "vmalertmanager",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "web",
							Protocol:   "TCP",
							Port:       9093,
							TargetPort: intstr.FromInt(9093),
						},
						{
							Name:       "tcp-mesh",
							Port:       9094,
							TargetPort: intstr.FromInt(9094),
							Protocol:   corev1.ProtocolTCP,
						},
						{
							Name:       "udp-mesh",
							Port:       9094,
							TargetPort: intstr.FromInt(9094),
							Protocol:   corev1.ProtocolUDP,
						},
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: childObjectMeta},
			&appsv1.StatefulSet{
				ObjectMeta: childObjectMeta,
				Spec: appsv1.StatefulSetSpec{
					ServiceName:         vmalertmanagerName.Name,
					Replicas:            ptr.To(int32(1)),
					PodManagementPolicy: appsv1.ParallelPodManagement,
					UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
						Type: appsv1.OnDeleteStatefulSetStrategyType,
					},
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":      "vmalertmanager",
							"app.kubernetes.io/instance":  name,
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":      "vmalertmanager",
								"app.kubernetes.io/instance":  name,
								"app.kubernetes.io/component": "monitoring",
								"managed-by":                  "vm-operator",
							},
						},
						Spec: corev1.PodSpec{
							Volumes: []corev1.Volume{
								{
									Name: "alertmanager-db",
									VolumeSource: corev1.VolumeSource{
										EmptyDir: &corev1.EmptyDirVolumeSource{},
									},
								},
							},
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
					Name:      vmalertmanagerName.Name + "-0",
					Namespace: namespace,
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmalertmanager",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
						"controller-revision-hash":    "v1",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "StatefulSet",
							Name:       vmalertmanagerName.Name,
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
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmalertmanagerName},
				{Verb: "Update", Kind: "ServiceAccount", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Role", Resource: vmalertmanagerName},
				{Verb: "Update", Kind: "Role", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "RoleBinding", Resource: vmalertmanagerName},
				{Verb: "Update", Kind: "RoleBinding", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "Service", Resource: vmalertmanagerName},
				{Verb: "Delete", Kind: "Service", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "Service", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmalertmanagerName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Delete", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Create", Kind: "StatefulSet", Resource: vmalertmanagerName},
				{Verb: "Get", Kind: "StatefulSet", Resource: vmalertmanagerName},
			},
		})
}
