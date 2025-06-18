package build

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_LoadKeyFromSecret(t *testing.T) {
	type args struct {
		ns string
		ss *corev1.SecretKeySelector
	}
	tests := []struct {
		name              string
		args              args
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "extract tls key data from secret",
			args: args{
				ns: "default",
				ss: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "tls-secret",
					},
					Key: "key.pem",
				},
			},
			want:    "tls-key-data",
			wantErr: false,
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"ca.crt": []byte(`ca-data`), "key.pem": []byte(`tls-key-data`)},
				},
			},
		},
		{
			name: "extract basic auth password with leading space and new line",
			args: args{
				ns: "default",
				ss: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "basic-auth",
					},
					Key: "password",
				},
			},
			want: " password-value",
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "basic-auth",
						Namespace: "default",
					},
					Data: map[string][]byte{"password": []byte(" password-value\n")},
				},
			},
		},
		{
			name: "fail extract missing tls cert data from secret",
			args: args{
				ns: "default",
				ss: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "tls-secret",
					},
					Key: "cert.pem",
				},
			},
			want:    "",
			wantErr: true,
			predefinedObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "tls-secret",
						Namespace: "default",
					},
					Data: map[string][]byte{"ca.crt": []byte(`ca-data`), "key.pem": []byte(`tls-key-data`)},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			cfg := map[ResourceKind]*ResourceCfg{
				TLSAssetsResourceKind: {
					MountDir:   "/test",
					SecretName: "tls-volume",
				},
			}
			cache := NewAssetsCache(context.TODO(), fclient, cfg)
			got, err := cache.LoadKeyFromSecret(tt.args.ns, tt.args.ss)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadKeyFromSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("LoadKeyFromSecret() got = %q, want %q", got, tt.want)
			}
		})
	}
}

func Test_LoadKeyFromConfigMap(t *testing.T) {
	type args struct {
		ns string
		cs *corev1.ConfigMapKeySelector
	}
	tests := []struct {
		name              string
		args              args
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "extract key from cm",
			args: args{
				ns: "default",
				cs: &corev1.ConfigMapKeySelector{Key: "tls-conf", LocalObjectReference: corev1.LocalObjectReference{Name: "tls-cm"}},
			},
			predefinedObjects: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{Name: "tls-cm", Namespace: "default"},
					Data:       map[string]string{"tls-conf": "secret-data"},
				},
			},
			want:    "secret-data",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			cfg := map[ResourceKind]*ResourceCfg{
				TLSAssetsResourceKind: {
					MountDir:   "/test",
					SecretName: "tls-volume",
				},
			}
			cache := NewAssetsCache(context.TODO(), fclient, cfg)
			got, err := cache.LoadKeyFromConfigMap(tt.args.ns, tt.args.cs)
			if (err != nil) != tt.wantErr {
				t.Errorf("LoadKeyFromConfigMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("LoadKeyFromConfigMap() got = %v, want %v", got, tt.want)
			}
		})
	}
}
