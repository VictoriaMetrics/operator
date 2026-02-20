package build

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
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

	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		cfg := map[ResourceKind]*ResourceCfg{
			TLSAssetsResourceKind: {
				MountDir:   "/test",
				SecretName: "tls-volume",
			},
		}
		cache := NewAssetsCache(context.TODO(), fclient, cfg)
		got, err := cache.LoadKeyFromSecret(o.ns, o.ss)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		assert.Equal(t, o.want, got)
	}

	// extract tls key data from secret
	f(opts{
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
	})

	// extract basic auth password with leading space and new line
	f(opts{
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
	})

	// fail extract missing tls cert data from secret
	f(opts{
		ns: "default",
		ss: &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{
				Name: "tls-secret",
			},
			Key: "cert.pem",
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
	})
}

func Test_LoadKeyFromConfigMap(t *testing.T) {
	type opts struct {
		ns                string
		cs                *corev1.ConfigMapKeySelector
		want              string
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		cfg := map[ResourceKind]*ResourceCfg{
			TLSAssetsResourceKind: {
				MountDir:   "/test",
				SecretName: "tls-volume",
			},
		}
		cache := NewAssetsCache(context.TODO(), fclient, cfg)
		got, err := cache.LoadKeyFromConfigMap(o.ns, o.cs)
		assert.NoError(t, err)
		assert.Equal(t, o.want, got)
	}

	// extract key from cm
	f(opts{
		ns: "default",
		cs: &corev1.ConfigMapKeySelector{Key: "tls-conf", LocalObjectReference: corev1.LocalObjectReference{Name: "tls-cm"}},
		predefinedObjects: []runtime.Object{
			&corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "tls-cm", Namespace: "default"},
				Data:       map[string]string{"tls-conf": "secret-data"},
			},
		},
		want: "secret-data",
	})
}
