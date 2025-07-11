package vmsingle

import (
	"context"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	f := func(cr *vmv1beta1.VMSingle, predefinedObjects []runtime.Object) {
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		err := CreateOrUpdate(context.TODO(), cr, fclient)
		if err != nil {
			t.Errorf("CreateOrUpdate() error = %v", err)
			return
		}
	}

	// base vmsingle gen
	f(&vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmsingle-base",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMSingleSpec{
			CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
				ReplicaCount: ptr.To(int32(1))},
		},
	}, []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vmsingle-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmsingle", "app.kubernetes.io/instance": "vmsingle-base", "managed-by": "vm-operator"}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
		},
		k8stools.NewReadyDeployment("vmsingle-vmsingle-base", "default"),
	})

	// base vmsingle with ports
	f(&vmv1beta1.VMSingle{
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
	}, []runtime.Object{
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vmsingle-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vmsingle", "app.kubernetes.io/instance": "vmsingle-base", "managed-by": "vm-operator"}},
			Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
		},
		k8stools.NewReadyDeployment("vmsingle-vmsingle-base", "default"),
	})
}

func TestCreateOrUpdateService(t *testing.T) {
	f := func(cr *vmv1beta1.VMSingle, want *corev1.Service, wantPortsLen int, predefinedObjects []runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		got, err := createOrUpdateService(context.TODO(), fclient, cr, nil)
		if err != nil {
			t.Errorf("createOrUpdateService() error = %v", err)
			return
		}
		if !reflect.DeepEqual(got.Name, want.Name) {
			t.Errorf("createOrUpdateService() got = %v, want %v", got, want)
		}
		if len(got.Spec.Ports) != wantPortsLen {
			t.Fatalf("unexpected number of ports: %d, want: %d", len(got.Spec.Ports), wantPortsLen)
		}
	}

	// base service test
	f(&vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "single-1",
			Namespace: "default",
		},
	}, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmsingle-single-1",
			Namespace: "default",
		},
	}, 2, nil)

	// base service test-with ports
	f(&vmv1beta1.VMSingle{
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
	}, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmsingle-single-1",
			Namespace: "default",
		},
	}, 9, nil)

	// with extra service nodePort
	f(&vmv1beta1.VMSingle{
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
	}, &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmsingle-single-1",
			Namespace: "default",
		},
	}, 2, []runtime.Object{
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
	})
}
