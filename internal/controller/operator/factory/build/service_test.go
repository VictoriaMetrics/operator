package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestAddOTLPGRPCPortToService(t *testing.T) {
	svc := &corev1.Service{}
	AddOTLPGRPCPortToService(svc, nil)
	assert.Empty(t, svc.Spec.Ports)

	AddOTLPGRPCPortToService(svc, &vmv1.OTLPGRPCSpec{ListenPort: 4317})
	assert.Equal(t, []corev1.ServicePort{
		{Name: "otlp-grpc", Protocol: corev1.ProtocolTCP, Port: 4317, TargetPort: intstr.FromInt32(4317)},
	}, svc.Spec.Ports)
}

func Test_mergeServiceSpec(t *testing.T) {
	type opts struct {
		svc      *corev1.Service
		svcSpec  *vmv1beta1.AdditionalServiceSpec
		validate func(svc *corev1.Service)
	}

	f := func(o opts) {
		t.Helper()
		additionalSvc := AdditionalServiceFromDefault(o.svc, o.svcSpec)
		o.validate(additionalSvc)
	}

	// override ports
	f(opts{
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "web"},
				},
			},
		},
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "metrics"},
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, "some-name-additional-service", svc.Name)
			assert.Len(t, svc.Spec.Ports, 1)
			assert.Equal(t, "metrics", svc.Spec.Ports[0].Name)
		},
	})

	// change clusterIP ports
	f(opts{
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "metrics"},
				},
			},
		},
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, corev1.ServiceTypeNodePort, svc.Spec.Type)
			assert.Len(t, svc.Spec.Ports, 1)
			assert.Equal(t, "metrics", svc.Spec.Ports[0].Name)
		},
	})

	// change selector
	f(opts{
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					{Name: "metrics"},
				},
				Selector: map[string]string{
					"app": "value",
				},
			},
		},
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Selector: map[string]string{
					"app-2": "value-3",
				},
			},
		},
		validate: func(svc *corev1.Service) {
			assert.Equal(t, map[string]string{"app-2": "value-3"}, svc.Spec.Selector)
			assert.Len(t, svc.Spec.Ports, 1)
			assert.Equal(t, "metrics", svc.Spec.Ports[0].Name)
		},
	})
}

func TestServiceUseAsDefault(t *testing.T) {
	type opts struct {
		name             string
		svcSpec          *vmv1beta1.AdditionalServiceSpec
		headless         bool
		allowNonHeadless bool
		validate         func(t *testing.T, svc *corev1.Service)
	}

	// builds the default service for a component with a headless or regular default
	f := func(o opts) {
		t.Helper()
		cr := &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec: vmv1beta1.VMClusterSpec{
				VMStorage: &vmv1beta1.VMStorage{
					Port: "8482", VMInsertPort: "8400", VMSelectPort: "8401",
					ServiceSpec: o.svcSpec,
				},
			},
		}
		b := NewChildBuilder(cr, vmv1beta1.ClusterComponentStorage)
		var svcOpts []ServiceOption
		if o.allowNonHeadless {
			svcOpts = append(svcOpts, AllowNonHeadlessDefault())
		}
		svc := Service(b, cr.Spec.VMStorage.Port, func(svc *corev1.Service) {
			if o.headless {
				svc.Spec.ClusterIP = corev1.ClusterIPNone
			}
		}, svcOpts...)
		o.validate(t, svc)
	}

	// no override keeps the headless default
	f(opts{
		name:     "no override",
		headless: true,
		validate: func(t *testing.T, svc *corev1.Service) {
			assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
		},
	})

	// no type defined, only patches the default service and keeps it headless
	f(opts{
		name:     "useAsDefault without type",
		headless: true,
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			UseAsDefault: true,
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{{Name: "extra", Port: 9999}},
			},
		},
		validate: func(t *testing.T, svc *corev1.Service) {
			assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
			assert.Len(t, svc.Spec.Ports, 2, "default ports must be merged")
		},
	})

	// explicit type defines the service shape, the headless clusterIP must not be inherited
	f(opts{
		name:             "useAsDefault with type=ClusterIP",
		headless:         true,
		allowNonHeadless: true,
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			UseAsDefault: true,
			Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
		},
		validate: func(t *testing.T, svc *corev1.Service) {
			assert.Empty(t, svc.Spec.ClusterIP, "clusterIP must be assigned by kubernetes")
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
		},
	})

	// an explicitly headless override stays headless
	f(opts{
		name:     "useAsDefault with explicit clusterIP=None",
		headless: true,
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			UseAsDefault: true,
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: corev1.ClusterIPNone,
			},
		},
		validate: func(t *testing.T, svc *corev1.Service) {
			assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
		},
	})

	// a pinned clusterIP is kept as given
	f(opts{
		name:     "useAsDefault with pinned clusterIP",
		headless: true,
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			UseAsDefault: true,
			Spec: corev1.ServiceSpec{
				Type:      corev1.ServiceTypeClusterIP,
				ClusterIP: "10.96.0.42",
			},
		},
		validate: func(t *testing.T, svc *corev1.Service) {
			assert.Equal(t, "10.96.0.42", svc.Spec.ClusterIP)
		},
	})

	// a different type was always allowed to drop the headless clusterIP
	f(opts{
		name:             "useAsDefault with type=LoadBalancer",
		headless:         true,
		allowNonHeadless: true,
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			UseAsDefault: true,
			Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		},
		validate: func(t *testing.T, svc *corev1.Service) {
			assert.Empty(t, svc.Spec.ClusterIP)
			assert.Equal(t, corev1.ServiceTypeLoadBalancer, svc.Spec.Type)
		},
	})

	// without the option the headless clusterIP is inherited, as before
	f(opts{
		name:     "useAsDefault with type=ClusterIP, not allowed to be non-headless",
		headless: true,
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			UseAsDefault: true,
			Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
		},
		validate: func(t *testing.T, svc *corev1.Service) {
			assert.Equal(t, corev1.ClusterIPNone, svc.Spec.ClusterIP)
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
		},
	})

	// a different type was always allowed to drop the headless clusterIP
	f(opts{
		name:     "useAsDefault with type=LoadBalancer, not allowed to be non-headless",
		headless: true,
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			UseAsDefault: true,
			Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		},
		validate: func(t *testing.T, svc *corev1.Service) {
			assert.Empty(t, svc.Spec.ClusterIP)
			assert.Equal(t, corev1.ServiceTypeLoadBalancer, svc.Spec.Type)
		},
	})

	// components with a regular default service are not affected
	f(opts{
		name: "useAsDefault with type=ClusterIP and non-headless default",
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			UseAsDefault: true,
			Spec:         corev1.ServiceSpec{Type: corev1.ServiceTypeClusterIP},
		},
		validate: func(t *testing.T, svc *corev1.Service) {
			assert.Empty(t, svc.Spec.ClusterIP)
			assert.Equal(t, corev1.ServiceTypeClusterIP, svc.Spec.Type)
		},
	})
}
