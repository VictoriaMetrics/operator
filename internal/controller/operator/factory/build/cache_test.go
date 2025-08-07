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
	type opts struct {
		ns                string
		ss                *corev1.SecretKeySelector
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		cfg := map[ResourceKind]*ResourceCfg{
			TLSAssetsResourceKind: {
				MountDir:   "/test",
				SecretName: "tls-volume",
			},
		}
		cache := NewAssetsCache(context.TODO(), fclient, cfg)
		got, err := cache.LoadKeyFromSecret(opts.ns, opts.ss)
		if (err != nil) != opts.wantErr {
			t.Errorf("LoadKeyFromSecret() error = %v, wantErr %v", err, opts.wantErr)
			return
		}
		if got != opts.want {
			t.Errorf("LoadKeyFromSecret() got = %q, want %q", got, opts.want)
		}
	}

	// extract tls key data from secret
	o := opts{
		ns: "default",
		ss: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "tls-secret",
			},
			Key: "key.pem",
		},
		want: "tls-key-data",
		predefinedObjects: []runtime.Object{
			&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "tls-secret",
					Namespace: "default",
				},
				Data: map[string][]byte{"ca.crt": []byte(`ca-data`), "key.pem": []byte(`tls-key-data`)},
			},
		},
	}
	f(o)

	// extract basic auth password with leading space and new line
	o = opts{
		ns: "default",
		ss: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "basic-auth",
			},
			Key: "password",
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
	}
	f(o)

	// fail extract missing tls cert data from secret
	o = opts{
		ns: "default",
		ss: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "tls-secret",
			},
			Key: "cert.pem",
		},
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
	}
	f(o)
}

func Test_LoadKeyFromConfigMap(t *testing.T) {
	type opts struct {
		ns                string
		cs                *corev1.ConfigMapKeySelector
		want              string
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		cfg := map[ResourceKind]*ResourceCfg{
			TLSAssetsResourceKind: {
				MountDir:   "/test",
				SecretName: "tls-volume",
			},
		}
		cache := NewAssetsCache(context.TODO(), fclient, cfg)
		got, err := cache.LoadKeyFromConfigMap(opts.ns, opts.cs)
		if (err != nil) != opts.wantErr {
			t.Errorf("LoadKeyFromConfigMap() error = %v, wantErr %v", err, opts.wantErr)
			return
		}
		if got != opts.want {
			t.Errorf("LoadKeyFromConfigMap() got = %v, want %v", got, opts.want)
		}
	}

	// extract key from cm
	o := opts{
		ns: "default",
		cs: &corev1.ConfigMapKeySelector{
			Key: "tls-conf",
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "tls-cm",
			},
		},
		want: "secret-data",
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "tls-cm", Namespace: "default"},
				Data:       map[string]string{"tls-conf": "secret-data"},
			},
		},
	}
	f(o)
}
