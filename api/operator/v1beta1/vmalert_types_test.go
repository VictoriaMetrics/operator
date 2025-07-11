package v1beta1

import (
	"testing"
)

func TestVMAlert_Validate(t *testing.T) {
	f := func(cr *VMAlert, wantErr bool) {
		t.Helper()
		if err := cr.Validate(); (err != nil) != wantErr {
			t.Errorf("Validate() error = %v, wantErr %v", err, wantErr)
		}
	}

	// wo datasource
	f(&VMAlert{
		Spec: VMAlertSpec{
			Notifier: &VMAlertNotifierSpec{
				URL: "some-url",
			},
		},
	}, true)

	// with notifier blackhole
	f(&VMAlert{
		Spec: VMAlertSpec{
			Datasource: VMAlertDatasourceSpec{URL: "http://some-url"},
			CommonApplicationDeploymentParams: CommonApplicationDeploymentParams{
				ExtraArgs: map[string]string{"notifier.blackhole": "true"},
			},
		},
	}, false)

	// wo notifier url
	f(&VMAlert{
		Spec: VMAlertSpec{
			Datasource: VMAlertDatasourceSpec{URL: "some-url"},
			Notifier:   &VMAlertNotifierSpec{},
		},
	}, true)
}
