package vmauth

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
		cr                *vmv1beta1.VMAuth
		predefinedObjects []runtime.Object
		preRun            func(c *k8stools.ClientWithActions, cr *vmv1beta1.VMAuth)
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	f := func(args args, want want) {
		t.Helper()

		fclient := k8stools.GetTestClientWithActionsAndObjects(args.predefinedObjects)
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(args.cr)

		if args.preRun != nil {
			args.preRun(fclient, args.cr)
		}

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

	name := "example"
	namespace := "default"
	vmauthName := types.NamespacedName{Namespace: namespace, Name: "vmauth-" + name}
	configSecretName := types.NamespacedName{Namespace: namespace, Name: "vmauth-config-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}
	childObjectMeta := metav1.ObjectMeta{Name: vmauthName.Name, Namespace: namespace}

	// create vmauth with default config
	f(args{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: objectMeta,
			Spec:       vmv1beta1.VMAuthSpec{},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmauthName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmauthName},
				{Verb: "Get", Kind: "Role", Resource: vmauthName},
				{Verb: "Create", Kind: "Role", Resource: vmauthName},
				{Verb: "Get", Kind: "RoleBinding", Resource: vmauthName},
				{Verb: "Create", Kind: "RoleBinding", Resource: vmauthName},
				{Verb: "Get", Kind: "Service", Resource: vmauthName},
				{Verb: "Create", Kind: "Service", Resource: vmauthName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmauthName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmauthName},
				// Config secret
				{Verb: "Get", Kind: "Secret", Resource: configSecretName},
				{Verb: "Create", Kind: "Secret", Resource: configSecretName},
				// Deployment
				{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
				{Verb: "Create", Kind: "Deployment", Resource: vmauthName},
				{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
			},
		})

	// update vmauth
	f(args{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMAuthSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.ServiceAccount{ObjectMeta: childObjectMeta},
			&rbacv1.Role{ObjectMeta: childObjectMeta},
			&rbacv1.RoleBinding{ObjectMeta: childObjectMeta},
			&corev1.Service{
				ObjectMeta: childObjectMeta,
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "10.0.0.1",
					Selector: map[string]string{
						"app.kubernetes.io/name":      "vmauth",
						"app.kubernetes.io/instance":  name,
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Protocol:   "TCP",
							Port:       8427,
							TargetPort: intstr.Parse("8427"),
						},
					},
				},
			},
			&vmv1beta1.VMServiceScrape{ObjectMeta: childObjectMeta},
			&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: configSecretName.Name, Namespace: namespace}},
			&appsv1.Deployment{
				ObjectMeta: childObjectMeta,
				Spec: appsv1.DeploymentSpec{
					Replicas: ptr.To(int32(1)),
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app.kubernetes.io/name":      "vmauth",
							"app.kubernetes.io/instance":  name,
							"app.kubernetes.io/component": "monitoring",
							"managed-by":                  "vm-operator",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app.kubernetes.io/name":      "vmauth",
								"app.kubernetes.io/instance":  name,
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
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmauthName},
				{Verb: "Update", Kind: "ServiceAccount", Resource: vmauthName},
				{Verb: "Get", Kind: "Role", Resource: vmauthName},
				{Verb: "Update", Kind: "Role", Resource: vmauthName},
				{Verb: "Get", Kind: "RoleBinding", Resource: vmauthName},
				{Verb: "Update", Kind: "RoleBinding", Resource: vmauthName},
				{Verb: "Get", Kind: "Service", Resource: vmauthName},
				{Verb: "Update", Kind: "Service", Resource: vmauthName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmauthName},
				{Verb: "Update", Kind: "VMServiceScrape", Resource: vmauthName},
				{Verb: "Get", Kind: "Secret", Resource: configSecretName},
				{Verb: "Update", Kind: "Secret", Resource: configSecretName},
				{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
				{Verb: "Update", Kind: "Deployment", Resource: vmauthName},
				{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
			},
		})

	// no update on status change
	f(args{
		cr: &vmv1beta1.VMAuth{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMAuthSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(c *k8stools.ClientWithActions, cr *vmv1beta1.VMAuth) {
			ctx := context.TODO()
			// Create objects first
			_ = CreateOrUpdate(ctx, cr, c)

			// clear actions
			c.Actions = nil

			// Update status to simulate consistency
			cr.Status.LastAppliedSpec = &cr.Spec
		},
	}, want{
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "ServiceAccount", Resource: vmauthName},
			{Verb: "Get", Kind: "Role", Resource: vmauthName},
			{Verb: "Get", Kind: "RoleBinding", Resource: vmauthName},
			{Verb: "Get", Kind: "Service", Resource: vmauthName},
			// TODO: bug
			{Verb: "Update", Kind: "Service", Resource: vmauthName},
			{Verb: "Get", Kind: "VMServiceScrape", Resource: vmauthName},
			{Verb: "Get", Kind: "Secret", Resource: configSecretName},
			{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
			{Verb: "Get", Kind: "Deployment", Resource: vmauthName},
			{Verb: "Get", Kind: "PodDisruptionBudget", Resource: vmauthName},
			{Verb: "Get", Kind: "Ingress", Resource: vmauthName},
			{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: vmauthName},
		},
	})
}
