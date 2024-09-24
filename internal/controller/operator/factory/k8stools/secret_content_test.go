package k8stools

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func Test_getCredFromSecret(t *testing.T) {
	type args struct {
		ns       string
		sel      corev1.SecretKeySelector
		cacheKey string
		cache    map[string]*corev1.Secret
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
				sel: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "tls-secret",
					},
					Key: "key.pem",
				},
				cacheKey: "tls-secret",
				cache:    map[string]*corev1.Secret{},
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
				sel: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "basic-auth",
					},
					Key: "password",
				},
				cacheKey: "tls-secret",
				cache:    map[string]*corev1.Secret{},
			},
			want: "password-value",
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
				sel: corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: "tls-secret",
					},
					Key: "cert.pem",
				},
				cacheKey: "tls-secret",
				cache:    map[string]*corev1.Secret{},
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
			fclient := GetTestClientWithObjects(tt.predefinedObjects)

			got, err := GetCredFromSecret(context.TODO(), fclient, tt.args.ns, &tt.args.sel, tt.args.cacheKey, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCredFromSecret() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getCredFromSecret() got = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_getCredFromConfigMap(t *testing.T) {
	type args struct {
		ns       string
		sel      corev1.ConfigMapKeySelector
		cacheKey string
		cache    map[string]*corev1.ConfigMap
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
				ns:    "default",
				sel:   corev1.ConfigMapKeySelector{Key: "tls-conf", LocalObjectReference: corev1.LocalObjectReference{Name: "tls-cm"}},
				cache: map[string]*corev1.ConfigMap{},
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
			fclient := GetTestClientWithObjects(tt.predefinedObjects)

			got, err := GetCredFromConfigMap(context.TODO(), fclient, tt.args.ns, tt.args.sel, tt.args.cacheKey, tt.args.cache)
			if (err != nil) != tt.wantErr {
				t.Errorf("getCredFromConfigMap() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("getCredFromConfigMap() got = %v, want %v", got, tt.want)
			}
		})
	}
}
