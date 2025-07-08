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
	f := func(ns string, ss *corev1.SecretKeySelector, want string, wantErr bool, predefinedObjects []runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		cfg := map[ResourceKind]*ResourceCfg{
			TLSAssetsResourceKind: {
				MountDir:   "/test",
				SecretName: "tls-volume",
			},
		}
		cache := NewAssetsCache(context.TODO(), fclient, cfg)
		got, err := cache.LoadKeyFromSecret(ns, ss)
		if (err != nil) != wantErr {
			t.Errorf("LoadKeyFromSecret() error = %v, wantErr %v", err, wantErr)
			return
		}
		if got != want {
			t.Errorf("LoadKeyFromSecret() got = %q, want %q", got, want)
		}
	}

	// extract tls key data from secret
	f("default", &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "tls-secret",
		},
		Key: "key.pem",
	}, "tls-key-data", false, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{"ca.crt": []byte(`ca-data`), "key.pem": []byte(`tls-key-data`)},
		},
	})

	// extract basic auth password with leading space and new line
	f("default", &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "basic-auth",
		},
		Key: "password",
	}, " password-value", false, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "basic-auth",
				Namespace: "default",
			},
			Data: map[string][]byte{"password": []byte(" password-value\n")},
		},
	})

	// fail extract missing tls cert data from secret
	f("default", &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "tls-secret",
		},
		Key: "cert.pem",
	}, "", true, []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "tls-secret",
				Namespace: "default",
			},
			Data: map[string][]byte{"ca.crt": []byte(`ca-data`), "key.pem": []byte(`tls-key-data`)},
		},
	})
}

func Test_LoadKeyFromConfigMap(t *testing.T) {
	f := func(ns string, cs *corev1.ConfigMapKeySelector, want string, wantErr bool, predefinedObjects []runtime.Object) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		cfg := map[ResourceKind]*ResourceCfg{
			TLSAssetsResourceKind: {
				MountDir:   "/test",
				SecretName: "tls-volume",
			},
		}
		cache := NewAssetsCache(context.TODO(), fclient, cfg)
		got, err := cache.LoadKeyFromConfigMap(ns, cs)
		if (err != nil) != wantErr {
			t.Errorf("LoadKeyFromConfigMap() error = %v, wantErr %v", err, wantErr)
			return
		}
		if got != want {
			t.Errorf("LoadKeyFromConfigMap() got = %v, want %v", got, want)
		}
	}

	// extract key from cm
	f("default", &corev1.ConfigMapKeySelector{
		Key: "tls-conf",
		LocalObjectReference: corev1.LocalObjectReference{
			Name: "tls-cm",
		},
	}, "secret-data", false, []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "tls-cm", Namespace: "default"},
			Data:       map[string]string{"tls-conf": "secret-data"},
		},
	})
}
