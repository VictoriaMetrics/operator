package k8stools_test

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestRenderPlaceholders(t *testing.T) {
	type opts struct {
		resource     *corev1.ConfigMap
		placeholders map[string]string
		want         *corev1.ConfigMap
		wantErr      bool
	}
	f := func(opts opts) {
		t.Helper()
		got, err := k8stools.RenderPlaceholders(opts.resource, opts.placeholders)
		if (err != nil) != opts.wantErr {
			t.Errorf("RenderPlaceholders() error = %v, wantErr = %v", err, opts.wantErr)
			return
		}
		if !reflect.DeepEqual(got, opts.want) {
			t.Errorf("%v != %v", got, opts.want)
		}
	}

	// render without placeholders
	o := opts{
		resource: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "value_1",
				"key_2": "value_2",
			},
		},
		want: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "value_1",
				"key_2": "value_2",
			},
		},
	}
	f(o)

	// render without placeholders, but with specified values
	o = opts{
		resource: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "value_1",
				"key_2": "value_2",
			},
		},
		placeholders: map[string]string{
			"%PLACEHOLDER_1%": "new_value_1",
			"%PLACEHOLDER_2%": "new_value_2",
		},
		want: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "value_1",
				"key_2": "value_2",
			},
		},
	}
	f(o)

	// render with placeholders and specified values
	o = opts{
		resource: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "%PLACEHOLDER_1%",
				"key_2": "%PLACEHOLDER_2%",
				"key_3": "%PLACEHOLDER_3%",
				"key_4": "value_4",
			},
		},
		placeholders: map[string]string{
			"%PLACEHOLDER_1%": "new_value_1",
			"%PLACEHOLDER_2%": "new_value_2",
			"%PLACEHOLDER_4%": "new_value_4",
		},
		want: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "new_value_1",
				"key_2": "new_value_2",
				"key_3": "%PLACEHOLDER_3%",
				"key_4": "value_4",
			},
		},
	}
	f(o)

	// render without combined placeholders in different places of resource
	o = opts{
		resource: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-%PLACEHOLDER_3%-%PLACEHOLDER_4%-configmap",
			},
			Data: map[string]string{
				"%PLACEHOLDER_1%_%PLACEHOLDER_2%": "bla_%PLACEHOLDER_4%_bla",
				"%PLACEHOLDER_3%":                 "from %PLACEHOLDER_1% to %PLACEHOLDER_4%",
				"key":                             "value",
			},
		},
		placeholders: map[string]string{
			"%PLACEHOLDER_1%": "new-value-1",
			"%PLACEHOLDER_2%": "new-value-2",
			"%PLACEHOLDER_3%": "new-value-3",
			"%PLACEHOLDER_4%": "new-value-4",
		},
		want: &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-new-value-3-new-value-4-configmap",
			},
			Data: map[string]string{
				"new-value-1_new-value-2": "bla_new-value-4_bla",
				"new-value-3":             "from new-value-1 to new-value-4",
				"key":                     "value",
			},
		},
	}
	f(o)

	// placeholder with % in value
	o = opts{
		resource: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "%PLACEHOLDER_1%",
				"key_2": "%PLACEHOLDER_2%",
			},
		},
		placeholders: map[string]string{
			"%PLACEHOLDER_1%": "%PLACEHOLDER_1%",
			"%PLACEHOLDER_2%": "%PLACEHOLDER_2%",
		},
		want: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "%PLACEHOLDER_1%",
				"key_2": "%PLACEHOLDER_2%",
			},
		},
	}
	f(o)

	// placeholder with incorrect name 1
	o = opts{
		resource: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "%PLACEHOLDER_1%",
			},
		},
		placeholders: map[string]string{
			"PLACEHOLDER_1": "value_1",
		},
		wantErr: true,
	}
	f(o)

	// placeholder with incorrect name 2
	o = opts{
		resource: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "%PLACEHOLDER_1%",
			},
		},
		placeholders: map[string]string{
			"%PLACEHOLDER_1": "value_1",
		},
		wantErr: true,
	}
	f(o)

	// placeholder with incorrect name 3
	o = opts{
		resource: &corev1.ConfigMap{
			Data: map[string]string{
				"key_1": "%PLACEHOLDER_1%",
			},
		},
		placeholders: map[string]string{
			"PLACEHOLDER_1%": "value_1",
		},
		wantErr: true,
	}
	f(o)
}
