package v1beta1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVMRule_sanityCheck(t *testing.T) {
	type fields struct {
		TypeMeta   metav1.TypeMeta
		ObjectMeta metav1.ObjectMeta
		Spec       VMRuleSpec
		Status     VMRuleStatus
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "simple check",
			fields: fields{
				Spec: VMRuleSpec{
					Groups: []RuleGroup{
						{
							Name: "group name",
							Rules: []Rule{
								{
									Alert: "hosts down",
									Expr:  "up == 0",
								},
							},
						},
					},
				},
			},
		},
		{
			name: "duplicate groups",
			fields: fields{
				Spec: VMRuleSpec{
					Groups: []RuleGroup{
						{
							Name: "group name",
							Rules: []Rule{
								{
									Alert: "hosts down",
									Expr:  "up == 0",
								},
							},
						},
						{
							Name: "group name",
							Rules: []Rule{
								{
									Alert: "hosts down",
									Expr:  "up == 0",
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "incorrect expr",
			fields: fields{
				Spec: VMRuleSpec{
					Groups: []RuleGroup{
						{
							Name: "group name",
							Rules: []Rule{
								{
									Alert: "hosts down",
									Expr:  "asf124qaf(fa!@$qrfz",
								},
							},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "correct annotation with query",
			fields: fields{
				Spec: VMRuleSpec{
					Groups: []RuleGroup{
						{
							Name: "group name",
							Rules: []Rule{
								{
									Alert: "hosts down",
									Expr:  "down == 1",
									Annotations: map[string]string{
										"name": "{{ range printf \"alertmanager_config_hash{namespace=\\\"%s\\\",service=\\\"%s\\\"}\" $labels.namespace $labels.service | query }}\n            Configuration hash for pod {{ .Labels.pod }} is \"{{ printf \"%.f\" .Value }}\"\n            {{ end }}",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &VMRule{
				TypeMeta:   tt.fields.TypeMeta,
				ObjectMeta: tt.fields.ObjectMeta,
				Spec:       tt.fields.Spec,
				Status:     tt.fields.Status,
			}
			if err := r.sanityCheck(); (err != nil) != tt.wantErr {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}
