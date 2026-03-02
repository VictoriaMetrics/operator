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
		preRun            func(c *k8stools.ClientWithActions, cr *vmv1beta1.VMAgent)
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	vmagentName := types.NamespacedName{Namespace: "default", Name: "vmagent-vmagent"}
	clusterRoleName := types.NamespacedName{Name: "monitoring:default:vmagent-vmagent"}
	tlsAssetsName := types.NamespacedName{Namespace: "default", Name: "tls-assets-vmagent-vmagent"}

	vmagentCommonLabels := map[string]string{
		"app.kubernetes.io/name":      "vmagent",
		"app.kubernetes.io/instance":  "vmagent",
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}

	ownerRef := metav1.OwnerReference{
		APIVersion:         vmv1beta1.GroupVersion.String(),
		Kind:               "VMAgent",
		Name:               vmagentName.Name,
		UID:                "123",
		Controller:         ptr.To(true),
		BlockOwnerDeletion: ptr.To(true),
	}

	defaultCR := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmagent",
			Namespace: "default",
			UID:       "123",
		},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{URL: "http://remote-write"},
			},
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(1)),
			},
		},
		Status: vmv1beta1.VMAgentStatus{
			LastAppliedSpec: &vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:            vmagentName.Name,
			Namespace:       vmagentName.Namespace,
			Labels:          vmagentCommonLabels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
	}
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:            clusterRoleName.Name,
			Labels:          vmagentCommonLabels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
	}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:            clusterRoleName.Name,
			Labels:          vmagentCommonLabels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:            vmagentName.Name,
			Namespace:       vmagentName.Namespace,
			Labels:          vmagentCommonLabels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.1",
			Selector:  vmagentCommonLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       "http",
					Protocol:   "TCP",
					Port:       8429,
					TargetPort: intstr.Parse("8429"),
				},
			},
		},
	}
	vmServiceScrape := &vmv1beta1.VMServiceScrape{
		ObjectMeta: metav1.ObjectMeta{
			Name:            vmagentName.Name,
			Namespace:       vmagentName.Namespace,
			Labels:          vmagentCommonLabels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: vmv1beta1.VMServiceScrapeSpec{
			Selector: metav1.LabelSelector{MatchLabels: vmagentCommonLabels},
			Endpoints: []vmv1beta1.Endpoint{
				{
					Port: "http",
				},
			},
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            vmagentName.Name,
			Namespace:       vmagentName.Namespace,
			Labels:          vmagentCommonLabels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
	}
	tlsSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:            tlsAssetsName.Name,
			Namespace:       tlsAssetsName.Namespace,
			Labels:          vmagentCommonLabels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
	}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:            vmagentName.Name,
			Namespace:       vmagentName.Namespace,
			Labels:          vmagentCommonLabels,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{
				MatchLabels: vmagentCommonLabels,
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: vmagentCommonLabels,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: vmagentName.Name,
					Containers: []corev1.Container{
						{
							Name:  "vmagent",
							Image: "victoriametrics/vmagent:v1.101.0",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8429,
								},
							},
							Args: []string{
								"-promscrape.config=/etc/vmagent/config/scrape_config.yaml",
								"-remoteWrite.url=http://remote-write",
								"-remoteWrite.tmpDataPath=/tmp/vmagent-remotewrite-data",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/etc/vmagent/config",
								},
								{
									Name:      "tls-assets",
									MountPath: "/etc/vmagent/tls",
									ReadOnly:  true,
								},
							},
							Resources: corev1.ResourceRequirements{},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: vmagentName.Name,
								},
							},
						},
						{
							Name: "tls-assets",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: tlsAssetsName.Name,
								},
							},
						},
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
	}

	predefinedObjects := []runtime.Object{
		sa, cr, crb, svc, vmServiceScrape, secret, tlsSecret, deployment,
	}

	crWithoutStatus := defaultCR.DeepCopy()
	crWithoutStatus.Status = vmv1beta1.VMAgentStatus{}

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

	// update vmagent
	f(args{
		cr:                crWithoutStatus,
		predefinedObjects: predefinedObjects,
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

	// daemonset mode
	f(args{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				DaemonSetMode: true,
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
				{Verb: "Get", Kind: "VMPodScrape", Resource: vmagentName},
				{Verb: "Create", Kind: "VMPodScrape", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: vmagentName},
				{Verb: "Create", Kind: "Secret", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Create", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Get", Kind: "DaemonSet", Resource: vmagentName},
				{Verb: "Create", Kind: "DaemonSet", Resource: vmagentName},
				{Verb: "Get", Kind: "DaemonSet", Resource: vmagentName},
			},
		})

	// no update on status change
	f(args{
		cr: defaultCR.DeepCopy(),
		preRun: func(c *k8stools.ClientWithActions, cr *vmv1beta1.VMAgent) {
			ctx := context.TODO()
			// Create the object first
			_ = CreateOrUpdate(ctx, cr, c)
			// Clear actions
			c.Actions = nil
			// Change status
			cr.Status.Replicas = 1
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "DaemonSet", Resource: vmagentName},
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Get", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Get", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Get", Kind: "Service", Resource: vmagentName},
				// TODO: bug
				{Verb: "Update", Kind: "Service", Resource: vmagentName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
			},
		})

}
