package vmsingle

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
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMSingle
		want              *appsv1.Deployment
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		assert.NoError(t, CreateOrUpdate(context.TODO(), o.cr, fclient))
	}

	// base-vmsingle-gen
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmsingle-base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1))},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vmsingle-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmsingle", "app.kubernetes.io/instance": "vmsingle-base", "managed-by": "vm-operator"}},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vmsingle-vmsingle-base", "default"),
		},
		want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vmsingle-vmsingle-base", Namespace: "default"}},
	})

	// base-vmsingle-with-ports
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmsingle-base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				InsertPorts: &vmv1beta1.InsertPorts{
					InfluxPort:       "8051",
					OpenTSDBHTTPPort: "8052",
					GraphitePort:     "8053",
					OpenTSDBPort:     "8054",
				},
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1))},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vmsingle-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmsingle", "app.kubernetes.io/instance": "vmsingle-base", "managed-by": "vm-operator"}},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vmsingle-vmsingle-base", "default"),
		},
		want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vmsingle-vmsingle-base", Namespace: "default"}},
	})
}

func TestCreateOrUpdateService(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VMSingle
		want              *corev1.Service
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		assert.NoError(t, createOrUpdateService(ctx, fclient, o.cr, nil))
		svc := build.Service(o.cr, o.cr.Spec.Port, nil)
		var got corev1.Service
		nsn := types.NamespacedName{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		}
		assert.NoError(t, fclient.Get(ctx, nsn, &got))
		assert.Equal(t, got.Name, o.want.Name)
		assert.ElementsMatch(t, got.Spec.Ports, o.want.Spec.Ports)
	}

	// base service test
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "single-1",
				Namespace: "default",
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmsingle-single-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:       "http",
						Protocol:   "TCP",
						TargetPort: intstr.FromString(""),
					}, {
						Name:       "http-alias",
						Port:       8428,
						Protocol:   "TCP",
						TargetPort: intstr.FromString(""),
					},
				},
			},
		},
	})

	// base service test-with ports
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "single-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				InsertPorts: &vmv1beta1.InsertPorts{
					InfluxPort:       "8051",
					OpenTSDBHTTPPort: "8052",
					GraphitePort:     "8053",
					OpenTSDBPort:     "8054",
				},
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmsingle-single-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:       "http",
						Protocol:   "TCP",
						TargetPort: intstr.FromString(""),
					}, {
						Name:       "http-alias",
						Protocol:   "TCP",
						Port:       8428,
						TargetPort: intstr.FromString(""),
					}, {
						Name:       "graphite-tcp",
						Protocol:   "TCP",
						Port:       8053,
						TargetPort: intstr.FromInt(8053),
					}, {
						Name:       "graphite-udp",
						Protocol:   "UDP",
						Port:       8053,
						TargetPort: intstr.FromInt(8053),
					}, {
						Name:       "influx-tcp",
						Protocol:   "TCP",
						Port:       8051,
						TargetPort: intstr.FromInt(8051),
					}, {
						Name:       "influx-udp",
						Protocol:   "UDP",
						Port:       8051,
						TargetPort: intstr.FromInt(8051),
					}, {
						Name:       "opentsdb-tcp",
						Protocol:   "TCP",
						Port:       8054,
						TargetPort: intstr.FromInt(8054),
					}, {
						Name:       "opentsdb-udp",
						Protocol:   "UDP",
						Port:       8054,
						TargetPort: intstr.FromInt(8054),
					}, {
						Name:       "opentsdb-http",
						Protocol:   "TCP",
						Port:       8052,
						TargetPort: intstr.FromInt(8052),
					},
				},
			},
		},
	})

	// with extra service nodePort
	f(opts{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "single-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMSingleSpec{
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "additional-service"},
					Spec: corev1.ServiceSpec{
						Type: corev1.ServiceTypeNodePort,
					},
				},
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmsingle-single-1",
				Namespace: "default",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{
						Name:       "http",
						Protocol:   "TCP",
						TargetPort: intstr.FromString(""),
					}, {
						Name:       "http-alias",
						Port:       8428,
						Protocol:   "TCP",
						TargetPort: intstr.FromString(""),
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-svc",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vmsingle",
						"app.kubernetes.io/instance":  "single-1",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
				Spec: corev1.ServiceSpec{},
			},
		},
	})
}
