package v1beta1

import (
	"testing"
)

func TestVMAlert_sanityCheck(t *testing.T) {
	tests := []struct {
		name    string
		spec    VMAlertSpec
		wantErr bool
	}{
		{
			name: "wo datasource",
			spec: VMAlertSpec{
				Notifier: &VMAlertNotifierSpec{
					URL: "some-url",
				},
			},
			wantErr: true,
		},
		{
			name:    "wo notifiers",
			spec:    VMAlertSpec{Datasource: VMAlertDatasourceSpec{URL: "http://some-url"}},
			wantErr: false,
		},
		{
			name: "wo notifier url",
			spec: VMAlertSpec{
				Datasource: VMAlertDatasourceSpec{URL: "some-url"},
				Notifier:   &VMAlertNotifierSpec{},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &VMAlert{
				Spec: tt.spec,
			}
			if err := r.sanityCheck(); (err != nil) != tt.wantErr {
				t.Errorf("sanityCheck() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
