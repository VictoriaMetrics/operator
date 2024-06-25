package k8stools_test

import (
	"testing"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRenderPlaceholders(t *testing.T) {
	type args struct {
		resource     any
		placeholders map[string]string
	}
	tests := []struct {
		name    string
		args    args
		want    any
		wantErr bool
	}{
		{
			name: "render without placeholders",
			args: args{
				resource: &v1.ConfigMap{
					Data: map[string]string{
						"key_1": "value_1",
						"key_2": "value_2",
					},
				},
			},
			want: &v1.ConfigMap{
				Data: map[string]string{
					"key_1": "value_1",
					"key_2": "value_2",
				},
			},
		},
		{
			name: "render without placeholders, but with specified values",
			args: args{
				resource: &v1.ConfigMap{
					Data: map[string]string{
						"key_1": "value_1",
						"key_2": "value_2",
					},
				},
				placeholders: map[string]string{
					"%PLACEHOLDER_1%": "new_value_1",
					"%PLACEHOLDER_2%": "new_value_2",
				},
			},
			want: &v1.ConfigMap{
				Data: map[string]string{
					"key_1": "value_1",
					"key_2": "value_2",
				},
			},
		},
		{
			name: "render with placeholders and specified values",
			args: args{
				resource: &v1.ConfigMap{
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
			},
			want: &v1.ConfigMap{
				Data: map[string]string{
					"key_1": "new_value_1",
					"key_2": "new_value_2",
					"key_3": "%PLACEHOLDER_3%",
					"key_4": "value_4",
				},
			},
		},
		{
			name: "render without combined placeholders in different places of resource",
			args: args{
				resource: &v1.ConfigMap{
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
			},
			want: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-new-value-3-new-value-4-configmap",
				},
				Data: map[string]string{
					"new-value-1_new-value-2": "bla_new-value-4_bla",
					"new-value-3":             "from new-value-1 to new-value-4",
					"key":                     "value",
				},
			},
		},
		{
			name: "placeholder with % in value",
			args: args{
				resource: &v1.ConfigMap{
					Data: map[string]string{
						"key_1": "%PLACEHOLDER_1%",
						"key_2": "%PLACEHOLDER_2%",
					},
				},
				placeholders: map[string]string{
					"%PLACEHOLDER_1%": "%PLACEHOLDER_1%",
					"%PLACEHOLDER_2%": "%PLACEHOLDER_2%",
				},
			},
			want: &v1.ConfigMap{
				Data: map[string]string{
					"key_1": "%PLACEHOLDER_1%",
					"key_2": "%PLACEHOLDER_2%",
				},
			},
		},
		{
			name: "placeholder with incorrect name 1",
			args: args{
				resource: &v1.ConfigMap{
					Data: map[string]string{
						"key_1": "%PLACEHOLDER_1%",
					},
				},
				placeholders: map[string]string{
					"PLACEHOLDER_1": "value_1",
				},
			},
			wantErr: true,
		},
		{
			name: "placeholder with incorrect name 2",
			args: args{
				resource: &v1.ConfigMap{
					Data: map[string]string{
						"key_1": "%PLACEHOLDER_1%",
					},
				},
				placeholders: map[string]string{
					"%PLACEHOLDER_1": "value_1",
				},
			},
			wantErr: true,
		},
		{
			name: "placeholder with incorrect name 3",
			args: args{
				resource: &v1.ConfigMap{
					Data: map[string]string{
						"key_1": "%PLACEHOLDER_1%",
					},
				},
				placeholders: map[string]string{
					"PLACEHOLDER_1%": "value_1",
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resource := tt.args.resource.(*v1.ConfigMap)
			_, err := k8stools.RenderPlaceholders(resource, tt.args.placeholders)
			if (err != nil) != tt.wantErr {
				t.Errorf("RenderPlaceholders() error = %v, wantErr = %v", err, tt.wantErr)
				return
			}
		})
	}
}
