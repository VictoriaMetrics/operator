package v1beta1

import (
	"testing"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVMAuth_sanityCheck(t *testing.T) {
	type fields struct {
		TypeMeta   v1.TypeMeta
		ObjectMeta v1.ObjectMeta
		Spec       VMAuthSpec
		Status     VMAuthStatus
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		{
			name: "invalid ingress",
			fields: fields{
				Spec: VMAuthSpec{
					Ingress: &EmbeddedIngress{
						TlsHosts: []string{"host-1", "host-2"},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "valid cfg",
			fields: fields{
				Spec: VMAuthSpec{
					Ingress: &EmbeddedIngress{
						TlsHosts:      []string{"host1"},
						TlsSecretName: "secret-1",
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cr := &VMAuth{
				TypeMeta:   tt.fields.TypeMeta,
				ObjectMeta: tt.fields.ObjectMeta,
				Spec:       tt.fields.Spec,
				Status:     tt.fields.Status,
			}
			if err := cr.sanityCheck(); (err != nil) != tt.wantErr {
				t.Errorf("sanityCheck() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
