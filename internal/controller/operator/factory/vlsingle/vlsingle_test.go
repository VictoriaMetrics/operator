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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdateVLSingle(t *testing.T) {
	type opts struct {
		cr                *vmv1.VLSingle
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		err := CreateOrUpdate(context.TODO(), fclient, opts.cr)
		if err != nil {
			t.Errorf("CreateOrUpdate() error = %v", err)
			return
		}
	}

	// base gen
	o := opts{
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VLSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vlsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vlsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vlsingle-base", "default"),
		},
	}
	f(o)

	// with syslog tls config
	o = opts{
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VLSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Port: "8435",
				},
				SyslogSpec: &vmv1.SyslogServerSpec{
					TCPListeners: []*vmv1.SyslogTCPListener{
						{
							ListenPort:       3001,
							DecolorizeFields: vmv1.FieldsListString(`["log","level"]`),
						},
						{
							ListenPort:   3002,
							StreamFields: vmv1.FieldsListString(`["job","instance"]`),
							IgnoreFields: vmv1.FieldsListString(`["ip"]`),
							TLSConfig: &vmv1.TLSServerConfig{
								KeySecret: &corev1.SecretKeySelector{
									Key: "tls-key",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "syslog-tls-server",
									},
								},
								CertSecret: &corev1.SecretKeySelector{
									Key: "tls-cert",
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "syslog-tls-server",
									},
								},
							},
						},
					},
					UDPListeners: []*vmv1.SyslogUDPListener{
						{
							ListenPort:     3001,
							CompressMethod: "zstd",
							StreamFields:   vmv1.FieldsListString(`["job","instance"]`),
							IgnoreFields:   vmv1.FieldsListString(`["ip"]`),
						},
					},
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vlsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vlsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vlsingle-base", "default"),
		},
	}
	f(o)

	o = opts{
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "base",
				Namespace: "default",
			},
			Spec: vmv1.VLSingleSpec{
				CommonApplicationDeploymentParams: vmv1beta1.CommonApplicationDeploymentParams{
					ReplicaCount: ptr.To(int32(1)),
				},
				CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
					Port: "8435",
				},
			},
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "default",
					Name:      "vlsingle-0",
					Labels: map[string]string{
						"app.kubernetes.io/component": "monitoring",
						"app.kubernetes.io/name":      "vlsingle",
						"app.kubernetes.io/instance":  "base",
						"managed-by":                  "vm-operator",
					},
				},
				Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: "True"}}},
			},
			k8stools.NewReadyDeployment("vlsingle-base", "default"),
		},
	}
	f(o)
}

func TestCreateOrUpdateVLSingleService(t *testing.T) {
	type opts struct {
		cr                *vmv1.VLSingle
		want              *corev1.Service
		wantPortsLen      int
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		got, err := createOrUpdateService(context.TODO(), fclient, opts.cr, nil)
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
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "logs-1",
				Namespace: "default",
			},
		},
		want: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vlsingle-logs-1",
				Namespace: "default",
			},
		},
		wantPortsLen: 1,
	}
	f(o)

	// with extra service nodePort
	o = opts{
		cr: &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "logs-1",
				Namespace: "default",
			},
			Spec: vmv1.VLSingleSpec{
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
				Name:      "vlsingle-logs-1",
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
						"app.kubernetes.io/name":      "vlsingle",
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
