package reconcile

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_reconcileServiceForCRD(t *testing.T) {

	type opts struct {
		newService        *corev1.Service
		prevService       *corev1.Service
		predefinedObjects []runtime.Object
		validate          func(svc *corev1.Service)
	}

	f := func(opts opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		ctx := context.Background()
		assert.NoError(t, Service(ctx, cl, opts.newService, opts.prevService, nil))
		var gotSvc corev1.Service
		assert.NoError(t, cl.Get(ctx, types.NamespacedName{Namespace: opts.newService.Namespace, Name: opts.newService.Name}, &gotSvc))
		opts.validate(&gotSvc)
	}

	f(opts{
		newService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, svc.Name, "prefixed-1")
		},
	})

	// update loadbalancer class
	f(opts{
		newService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type:              corev1.ServiceTypeLoadBalancer,
				LoadBalancerClass: ptr.To("some-class"),
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeLoadBalancer,
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, *svc.Spec.LoadBalancerClass, "some-class")
		},
	})

	// do not recreate loadbalancer class
	f(opts{
		newService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
				Labels:    map[string]string{"custom": "label"},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
			},
		},
		prevService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeLoadBalancer,
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
					Labels:    map[string]string{"custom": "label"},
				},
				Spec: corev1.ServiceSpec{
					Type:              corev1.ServiceTypeLoadBalancer,
					LoadBalancerClass: ptr.To("some-class"),
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, *svc.Spec.LoadBalancerClass, "some-class")
			l, ok := svc.Labels["custom"]
			assert.True(t, ok)
			assert.Equal(t, l, "label")
		},
	})

	// update svc from headless to clusterIP
	f(opts{
		newService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "None",
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, svc.Name, "prefixed-1")
			assert.Empty(t, svc.Spec.ClusterIP)
		},
	})

	// update svc from clusterIP to headless
	f(opts{
		newService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "None",
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "192.168.1.5",
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, svc.Name, "prefixed-1")
			assert.Equal(t, svc.Spec.ClusterIP, "None")
		},
	})

	// update svc clusterIP value
	f(opts{
		newService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "192.168.1.5",
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeClusterIP,
					ClusterIP: "192.168.1.4",
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, svc.Name, "prefixed-1")
			assert.Equal(t, svc.Spec.ClusterIP, "192.168.1.5")
		},
	})

	// update svc from nodePort to clusterIP with value
	f(opts{
		newService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "192.168.1.5",
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeNodePort,
					ClusterIP: "192.168.1.1",
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, svc.Name, "prefixed-1")
			assert.Equal(t, svc.Spec.Type, corev1.ServiceTypeClusterIP)
			assert.Equal(t, svc.Spec.ClusterIP, "192.168.1.5")
		},
	})

	// keep node port
	f(opts{
		newService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeNodePort,
				ClusterIP: "192.168.1.5",
				Ports: []corev1.ServicePort{
					{
						Name:     "web",
						Protocol: "TCP",
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
				},
				Spec: corev1.ServiceSpec{
					Type:      corev1.ServiceTypeNodePort,
					ClusterIP: "192.168.1.5",
					Ports: []corev1.ServicePort{
						{
							Name:     "web",
							Protocol: "TCP",
							NodePort: 331,
						},
					},
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, svc.Name, "prefixed-1")
			assert.Equal(t, svc.Spec.Type, corev1.ServiceTypeNodePort)
			assert.Equal(t, svc.Spec.ClusterIP, "192.168.1.5")
			assert.Equal(t, svc.Spec.Ports[0].NodePort, int32(331))
		},
	})

	// keep custom labels on svc
	f(opts{
		newService: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "prefixed-1",
				Namespace: "default",
				Labels:    map[string]string{"custom": "label"},
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeClusterIP,
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "prefixed-1",
					Namespace: "default",
					Labels:    map[string]string{"custom": "label"},
				},
				Spec: corev1.ServiceSpec{
					Type: corev1.ServiceTypeClusterIP,
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, svc.Name, "prefixed-1")
			l, ok := svc.Labels["custom"]
			assert.True(t, ok)
			assert.Equal(t, l, "label")
		},
	})
}
