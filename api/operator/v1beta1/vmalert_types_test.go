package v1beta1

import (
	"testing"
)

func TestVMAlert_Validate(t *testing.T) {
	type opts struct {
		cr      *VMAlert
		wantErr bool
	}
	f := func(opts opts) {
		t.Helper()
		if err := opts.cr.Validate(); (err != nil) != opts.wantErr {
			t.Errorf("Validate() error = %v, wantErr %v", err, opts.wantErr)
		}
	}

	// wo datasource
	o := opts{
		cr: &VMAlert{
			Spec: VMAlertSpec{
				Notifier: &VMAlertNotifierSpec{
					URL: "some-url",
				},
			},
		},
		wantErr: true,
	}
	f(o)

	// with notifier blackhole
	o = opts{
		cr: &VMAlert{
			Spec: VMAlertSpec{
				Datasource: VMAlertDatasourceSpec{URL: "http://some-url"},
				CommonApplicationDeploymentParams: CommonApplicationDeploymentParams{
					ExtraArgs: map[string]string{"notifier.blackhole": "true"},
				},
			},
		},
	}
	f(o)

	// wo notifier url
	o = opts{
		cr: &VMAlert{
			Spec: VMAlertSpec{
				Datasource: VMAlertDatasourceSpec{URL: "some-url"},
				Notifier:   &VMAlertNotifierSpec{},
			},
		},
		wantErr: true,
	}
	f(o)
}
