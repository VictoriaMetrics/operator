package vtsingle

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdate(t *testing.T) {
	type opts struct {
		cr                *vmv1.VTSingle
		c                 *config.BaseOperatorConf
		wantErr           bool
		predefinedObjects []runtime.Object
	}

	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		err := CreateOrUpdate(context.TODO(), fclient, opts.cr)
		if (err != nil) != opts.wantErr {
			t.Errorf("CreateOrUpdate() error = %v, wantErr %v", err, opts.wantErr)
			return
		}
	}

	// base gen
	o := opts{
		c: config.MustGetBaseConfig(),
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vtsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vtsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vtsingle-base", "default"),
		},
	}
	f(o)

	// base with specific port
	o = opts{
		c: config.MustGetBaseConfig(),
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Port: "10435",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vtsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vtsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vtsingle-base", "default"),
		},
	}
	f(o)

	// with syslog tls config
	o = opts{
		c: config.MustGetBaseConfig(),
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Port: "10435",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vtsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vtsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vtsingle-base", "default"),
		},
	}
	f(o)
}

func TestCreateOrUpdateService(t *testing.T) {
	type opts struct {
		cr                *vmv1.VTSingle
		c                 *config.BaseOperatorConf
		want              *corev1.Service
		wantErr           bool
		wantPortsLen      int
		predefinedObjects []runtime.Object
	}

	f := func(opts opts) {
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		got, err := createOrUpdateService(context.TODO(), fclient, opts.cr, nil)
		if (err != nil) != opts.wantErr {
			t.Errorf("CreateOrUpdateService() error = %v, wantErr %v", err, opts.wantErr)
			return
		}

		if !reflect.DeepEqual(got.Name, opts.want.Name) {
			t.Errorf("CreateOrUpdateService(): %s", cmp.Diff(got, opts.want))
		}
		if len(got.Spec.Ports) != opts.wantPortsLen {
			t.Fatalf("unexpected number of ports: %d, want: %d", len(got.Spec.Ports), opts.wantPortsLen)
		}
	}

	// base service test
	o := opts{
		c: config.MustGetBaseConfig(),
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "traces-1",
				Namespace: "default",
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vtsingle-traces-1",
				Namespace: "default",
			},
		},
		wantPortsLen: 1,
	}
	f(o)

	// with extra service nodePort
	o = opts{
		c: config.MustGetBaseConfig(),
		cr: &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "traces-1",
				Namespace: "default",
			},
			Spec: vmv1.VTSingleSpec{
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
				Name:      "vtsingle-traces-1",
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
						"app.kubernetes.io/name":      "vtsingle",
						"app.kubernetes.io/instance":  "traces-1",
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
