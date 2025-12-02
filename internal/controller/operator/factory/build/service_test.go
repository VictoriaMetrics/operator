package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func Test_mergeServiceSpec(t *testing.T) {
	type opts struct {
		svc      *corev1.Service
		svcSpec  *vmv1beta1.AdditionalServiceSpec
		validate func(svc *corev1.Service) error
	}

	f := func(o opts) {
		t.Helper()
		additionalSvc := AdditionalServiceFromDefault(o.svc, o.svcSpec)
		assert.NoError(t, o.validate(additionalSvc))
	}

	// override ports
	f(opts{
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "web"},
				},
			},
		},
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "metrics"},
				},
			},
		},
		validate: func(svc *corev1.Service) error {
			assert.Equal(t, "some-name-additional-service", svc.Name)
			assert.Len(t, svc.Spec.Ports, 1)
			assert.Equal(t, "metrics", svc.Spec.Ports[0].Name)
			return nil
		},
	})

	// change clusterIP ports
	f(opts{
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
			Spec: corev1.ServiceSpec{
				Ports: []corev1.ServicePort{
					{Name: "metrics"},
				},
			},
		},
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
			},
		},
		validate: func(svc *corev1.Service) error {
			assert.Equal(t, corev1.ServiceTypeNodePort, svc.Spec.Type)
			assert.Len(t, svc.Spec.Ports, 1)
			assert.Equal(t, "metrics", svc.Spec.Ports[0].Name)
			return nil
		},
	})

	// change selector
	f(opts{
		svc: &corev1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name: "some-name",
			},
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Ports: []corev1.ServicePort{
					{Name: "metrics"},
				},
				Selector: map[string]string{
					"app": "value",
				},
			},
		},
		svcSpec: &vmv1beta1.AdditionalServiceSpec{
			Spec: corev1.ServiceSpec{
				Type: corev1.ServiceTypeNodePort,
				Selector: map[string]string{
					"app-2": "value-3",
				},
			},
		},
		validate: func(svc *corev1.Service) error {
			assert.Equal(t, map[string]string{"app-2": "value-3"}, svc.Spec.Selector)
			assert.Len(t, svc.Spec.Ports, 1)
			assert.Equal(t, "metrics", svc.Spec.Ports[0].Name)
			return nil
		},
	})
}
