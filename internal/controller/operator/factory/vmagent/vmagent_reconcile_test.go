package vmagent

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
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr                *vmv1beta1.VMAgent
		predefinedObjects []runtime.Object
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	f := func(args args, want want) {
		t.Helper()
		// Use shared helper instead of custom interceptors
		fclient := k8stools.GetTestClientWithActionsAndObjects(args.predefinedObjects)
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(args.cr)
		err := CreateOrUpdate(ctx, args.cr, fclient)
		if want.err != nil {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}

		if !assert.Equal(t, len(want.actions), len(fclient.Actions)) {
			for i, action := range fclient.Actions {
				t.Logf("Action %d: %s %s %s", i, action.Verb, action.Kind, action.Resource)
			}
		}

		for i, action := range want.actions {
			if i >= len(fclient.Actions) {
				break
			}
			assert.Equal(t, action.Verb, fclient.Actions[i].Verb, "idx %d verb", i)
			assert.Equal(t, action.Kind, fclient.Actions[i].Kind, "idx %d kind", i)
			assert.Equal(t, action.Resource, fclient.Actions[i].Resource, "idx %d resource", i)
		}
	}

	vmagentName := types.NamespacedName{Namespace: "default", Name: "vmagent-vmagent"}
	clusterRoleName := types.NamespacedName{Name: "monitoring:default:vmagent-vmagent"}
	tlsAssetsName := types.NamespacedName{Namespace: "default", Name: "tls-assets-vmagent-vmagent"}

	// create vmagent with default config (Deployment mode)
	f(args{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Get", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Create", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Get", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Create", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Get", Kind: "Service", Resource: vmagentName},
				{Verb: "Create", Kind: "Service", Resource: vmagentName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmagentName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: vmagentName},
				{Verb: "Create", Kind: "Secret", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Create", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
				{Verb: "Create", Kind: "Deployment", Resource: vmagentName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
			},
		})

	// update vmagent (Deployment mode)
	f(args{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: vmagentName.Name, Namespace: vmagentName.Namespace}},
			&rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName.Name}},
			&rbacv1.ClusterRoleBinding{ObjectMeta: metav1.ObjectMeta{Name: clusterRoleName.Name}},
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: vmagentName.Name, Namespace: vmagentName.Namespace},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.1",
					Selector: map[string]string{
						"app.kubernetes.io/name":      "vmagent",
						"app.kubernetes.io/instance":  "vmagent",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   "TCP",
							Port:       8429,
							TargetPort: intstr.Parse("8429"),
						},
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: metav1.ObjectMeta{Name: vmagentName.Name, Namespace: vmagentName.Namespace}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: vmagentName.Name, Namespace: vmagentName.Namespace}},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: tlsAssetsName.Name, Namespace: tlsAssetsName.Namespace}},
			&appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: vmagentName.Name, Namespace: vmagentName.Namespace},
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":      "vmagent",
							"app.kubernetes.io/instance":  "vmagent",
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":      "vmagent",
								"app.kubernetes.io/instance":  "vmagent",
								"app.kubernetes.io/component": "monitoring",
								"managed-by":                  "vm-operator",
							},
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
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Update", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Get", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Update", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Get", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Update", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Get", Kind: "Service", Resource: vmagentName},
				{Verb: "Update", Kind: "Service", Resource: vmagentName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmagentName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: vmagentName},
				{Verb: "Update", Kind: "Secret", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Update", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
				{Verb: "Update", Kind: "Deployment", Resource: vmagentName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
			},
		})
}
