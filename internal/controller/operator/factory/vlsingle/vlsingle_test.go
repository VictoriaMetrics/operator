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

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestCreateOrUpdateVLSingle(t *testing.T) {
	type opts struct {
		cr                *vmv1.VLSingle
		c                 *config.BaseOperatorConf
		want              *appsv1.Deployment
		wantErr           bool
		predefinedObjects []runtime.Object
	}

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		err := CreateOrUpdate(context.TODO(), fclient, o.cr)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}

	// base gen
	f(opts{
		c: config.MustGetBaseConfig(),
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
		want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vlsingle-base", Namespace: "default"}},
	})

	// base with specific port
	f(opts{
		c: config.MustGetBaseConfig(),
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
		want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vlsingle-base", Namespace: "default"}},
	})

	// with syslog tls config
	f(opts{
		c: config.MustGetBaseConfig(),
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

		want: &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "vlsingle-base", Namespace: "default"}},
	})
}

func TestCreateOrUpdateVLSingleService(t *testing.T) {
	type opts struct {
		cr                *vmv1.VLSingle
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
		err := createOrUpdateService(ctx, fclient, o.cr, nil)
		if o.wantErr {
			assert.Error(t, err)
			return
		}
		assert.NoError(t, err)
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
	})

	// with extra service nodePort
	f(opts{
		c: config.MustGetBaseConfig(),
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
	})
}
