package k8stools_test

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestRenderPlaceholders(t *testing.T) {
	f := func(resource *corev1.ConfigMap, placeholders map[string]string, want *corev1.ConfigMap, wantErr bool) {
		t.Helper()
		got, err := k8stools.RenderPlaceholders(resource, placeholders)
		if (err != nil) != wantErr {
			t.Errorf("RenderPlaceholders() error = %v, wantErr = %v", err, wantErr)
			return
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%v != %v", got, want)
		}
	}

	// render without placeholders
	f(&corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "value_1",
			"key_2": "value_2",
		},
	}, nil, &corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "value_1",
			"key_2": "value_2",
		},
	}, false)

	// render without placeholders, but with specified values
	f(&corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "value_1",
			"key_2": "value_2",
		},
	}, map[string]string{
		"%PLACEHOLDER_1%": "new_value_1",
		"%PLACEHOLDER_2%": "new_value_2",
	}, &corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "value_1",
			"key_2": "value_2",
		},
	}, false)

	// render with placeholders and specified values
	f(&corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "%PLACEHOLDER_1%",
			"key_2": "%PLACEHOLDER_2%",
			"key_3": "%PLACEHOLDER_3%",
			"key_4": "value_4",
		},
	}, map[string]string{
		"%PLACEHOLDER_1%": "new_value_1",
		"%PLACEHOLDER_2%": "new_value_2",
		"%PLACEHOLDER_4%": "new_value_4",
	}, &corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "new_value_1",
			"key_2": "new_value_2",
			"key_3": "%PLACEHOLDER_3%",
			"key_4": "value_4",
		},
	}, false)

	// render without combined placeholders in different places of resource
	f(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-%PLACEHOLDER_3%-%PLACEHOLDER_4%-configmap",
		},
		Data: map[string]string{
			"%PLACEHOLDER_1%_%PLACEHOLDER_2%": "bla_%PLACEHOLDER_4%_bla",
			"%PLACEHOLDER_3%":                 "from %PLACEHOLDER_1% to %PLACEHOLDER_4%",
			"key":                             "value",
		},
	}, map[string]string{
		"%PLACEHOLDER_1%": "new-value-1",
		"%PLACEHOLDER_2%": "new-value-2",
		"%PLACEHOLDER_3%": "new-value-3",
		"%PLACEHOLDER_4%": "new-value-4",
	}, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-new-value-3-new-value-4-configmap",
		},
		Data: map[string]string{
			"new-value-1_new-value-2": "bla_new-value-4_bla",
			"new-value-3":             "from new-value-1 to new-value-4",
			"key":                     "value",
		},
	}, false)

	// placeholder with % in value
	f(&corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "%PLACEHOLDER_1%",
			"key_2": "%PLACEHOLDER_2%",
		},
	}, map[string]string{
		"%PLACEHOLDER_1%": "%PLACEHOLDER_1%",
		"%PLACEHOLDER_2%": "%PLACEHOLDER_2%",
	}, &corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "%PLACEHOLDER_1%",
			"key_2": "%PLACEHOLDER_2%",
		},
	}, false)

	// placeholder with incorrect name 1
	f(&corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "%PLACEHOLDER_1%",
		},
	}, map[string]string{
		"PLACEHOLDER_1": "value_1",
	}, nil, true)

	// placeholder with incorrect name 2
	f(&corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "%PLACEHOLDER_1%",
		},
	}, map[string]string{
		"%PLACEHOLDER_1": "value_1",
	}, nil, true)

	// placeholder with incorrect name 3
	f(&corev1.ConfigMap{
		Data: map[string]string{
			"key_1": "%PLACEHOLDER_1%",
		},
	}, map[string]string{
		"PLACEHOLDER_1%": "value_1",
	}, nil, true)
}
