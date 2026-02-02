package vlsingle

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdateVLogs(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VLogs
		c                 *config.BaseOperatorConf
		want              *appsv1.Deployment
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		err := CreateOrUpdateVLogs(context.Background(), fclient, o.cr)
		if (err != nil) != o.wantErr {
			t.Errorf("CreateOrUpdateVLogs() error = %v, wantErr %v", err, o.wantErr)
			return
		}
	}

	// base-vlogs-gen
	f(opts{
		c: config.MustGetBaseConfig(),
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
		want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vlogs-vlogs-base", Namespace: "default"}},
	})

	// base-vlogs-with-ports
	f(opts{
		c: config.MustGetBaseConfig(),
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
		want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vlogs-vlogs-base", Namespace: "default"}},
	})
}

func TestCreateOrUpdateVLogsService(t *testing.T) {
	type opts struct {
		cr                *vmv1beta1.VLogs
		c                 *config.BaseOperatorConf
		want              *corev1.Service
		wantErr           bool
		wantPortsLen      int
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		if err := createOrUpdateVLogsService(ctx, fclient, o.cr, nil); (err != nil) != o.wantErr {
			t.Errorf("CreateOrUpdateVLogsService() error = %v, wantErr %v", err, o.wantErr)
			return
		}
		svc := build.Service(o.cr, o.cr.Spec.Port, nil)
		var got corev1.Service
		nsn := types.NamespacedName{
			Name:      svc.Name,
			Namespace: svc.Namespace,
		}
		assert.NoError(t, fclient.Get(ctx, nsn, &got))
		assert.Equal(t, got.Name, o.want.Name)
		assert.Len(t, got.Spec.Ports, o.wantPortsLen)
	}

	// base service test
	f(opts{
		c: config.MustGetBaseConfig(),
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
	})

	// with extra service nodePort
	f(opts{
		c: config.MustGetBaseConfig(),
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
	})
}
