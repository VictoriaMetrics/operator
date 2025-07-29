package vlsingle

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdateVLogs(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VLogs
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		err := CreateOrUpdateVLogs(context.TODO(), fclient, opts.cr)
		if err != nil {
			t.Errorf("CreateOrUpdateVLogs() error = %v", err)
			return
		}
	}

	// base vlogs gen
	o := opts{
		cr: &vmv1beta1.VLogs{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vlogs-base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VLogsSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vlogs-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vlogs", "app.kubernetes.io/instance": "vlogs-base", "managed-by": "vm-operator"}},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vlogs-vlogs-base", "default"),
		},
	}
	f(o)

	// base vlogs with ports
	o = opts{
		cr: &vmv1beta1.VLogs{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vlogs-base",
				Namespace: "default",
			},
			Spec: vmv1beta1.VLogsSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "vlogs-0", Labels: map[string]string{"app.kubernetes.io/component": "monitoring", "app.kubernetes.io/name": "vlogs", "app.kubernetes.io/instance": "vlogs-base", "managed-by": "vm-operator"}},
				Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vlogs-vlogs-base", "default"),
		},
	}
	f(o)
}

func TestCreateOrUpdateVLogsService(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VLogs
		want              *corev1.Service
		wantPortsLen      int
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		got, err := createOrUpdateVLogsService(context.TODO(), fclient, opts.cr, nil)
		if err != nil {
			t.Errorf("createOrUpdateService() error = %v", err)
			return
		}

		if !reflect.DeepEqual(got.Name, opts.want.Name) {
			t.Errorf("createOrUpdateService(): %s", cmp.Diff(got, opts.want))
		}
		if len(got.Spec.Ports) != opts.wantPortsLen {
			t.Fatalf("unexpected number of ports: %d, want: %d", len(got.Spec.Ports), opts.wantPortsLen)
		}
	}

	// base service test
	o := opts{
		cr: &vmv1beta1.VLogs{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "logs-1",
				Namespace: "default",
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vlogs-logs-1",
				Namespace: "default",
			},
		},
		wantPortsLen: 1,
	}
	f(o)

	// with extra service nodePort
	o = opts{
		cr: &vmv1beta1.VLogs{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "logs-1",
				Namespace: "default",
			},
			Spec: vmv1beta1.VLogsSpec{
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
				Name:      "vlogs-logs-1",
				Namespace: "default",
			},
		},
		wantPortsLen: 1,
		predefinedObjects: []runtime.Object{
			&corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-svc",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/name":      "vlogs",
						"app.kubernetes.io/instance":  "logs-1",
						"app.kubernetes.io/component": "monitoring",
						"managed-by":                  "vm-operator",
					},
				},
				Spec: corev1.ServiceSpec{},
			},
		},
	}
	f(o)
}
