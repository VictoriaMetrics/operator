package v1beta1

import (
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/utils/pointer"
	"reflect"
	"testing"
)

func TestParsingPagerDutyConfig(t *testing.T) {
	f := func(srcYAML string, wantConfig PagerDutyConfig) {
		t.Helper()
		var gotPD PagerDutyConfig
		if err := yaml.Unmarshal([]byte(srcYAML), &gotPD); err != nil {
			t.Fatalf("cannot parse from yaml: %s", err)
		}
		if !reflect.DeepEqual(gotPD, wantConfig) {
			t.Fatalf("different configs, got: \n%v\nwant: \n%v", gotPD, wantConfig)
		}
	}
	// prometheus operator format
	f(`
sendresolved: true
servicekey:
  localobjectreference:
    name: pagerduty-config
  key: mn
severity: labels.severity
details:
- key: firing
  value: '{{ template \"pagerduty.text\" . }}'
- key: num_firing
  value: '{{ .Alerts.Firing | len }}'
`, PagerDutyConfig{
		SendResolved: pointer.Bool(true),
		ServiceKey: &v1.SecretKeySelector{
			Key: "mn",
			LocalObjectReference: v1.LocalObjectReference{
				Name: "pagerduty-config"},
		},
		Severity: "labels.severity",
		Details: map[string]string{
			"firing":     "{{ template \\\"pagerduty.text\\\" . }}",
			"num_firing": "{{ .Alerts.Firing | len }}",
		},
	})
	// normal format
	f(`
sendresolved: true
servicekey:
  localobjectreference:
    name: pagerduty-config
  key: mn
severity: labels.severity
details:
  firing: '{{ template \"pagerduty.text\" . }}'
  num_firing: '{{ .Alerts.Firing | len }}'

`, PagerDutyConfig{
		SendResolved: pointer.Bool(true),
		ServiceKey: &v1.SecretKeySelector{
			Key: "mn",
			LocalObjectReference: v1.LocalObjectReference{
				Name: "pagerduty-config"},
		},
		Severity: "labels.severity",
		Details: map[string]string{
			"firing":     "{{ template \\\"pagerduty.text\\\" . }}",
			"num_firing": "{{ .Alerts.Firing | len }}",
		},
	})

	// w/o details
	f(`
sendresolved: true
servicekey:
  localobjectreference:
    name: pagerduty-config
  key: mn
severity: labels.severity

`, PagerDutyConfig{
		SendResolved: pointer.Bool(true),
		ServiceKey: &v1.SecretKeySelector{
			Key: "mn",
			LocalObjectReference: v1.LocalObjectReference{
				Name: "pagerduty-config"},
		},
		Severity: "labels.severity",
	})
}
