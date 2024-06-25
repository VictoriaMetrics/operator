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

func TestParsingEmailConfig(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		headers EmailConfigHeaders
		wantErr bool
	}{
		{
			name: "without headers",
			spec: `to: 'myAwesomeEmail@localhost.de'
from: 'myOtherAwesomeEmail@localhost.de'
`,
		},
		{
			name: "headers as map",
			spec: `to: 'myAwesomeEmail@localhost.de'
from: 'myOtherAwesomeEmail@localhost.de'
headers:
  X-My-Header: 'myValue'
`,
			headers: map[string]string{"X-My-Header": "myValue"},
		},
		{
			name: "headers as map 2",
			spec: `to: 'myAwesomeEmail@localhost.de'
from: 'myOtherAwesomeEmail@localhost.de'
headers:
  key1: value1
  key2: value2
`,
			headers: map[string]string{"key1": "value1", "key2": "value2"},
		},
		{
			name: "headers as array",
			spec: `to: 'myAwesomeEmail@localhost.de'
from: 'myOtherAwesomeEmail@localhost.de'
headers:
  - key: X-My-Header
    value: myValue
`,
			headers: map[string]string{"X-My-Header": "myValue"},
		},
		{
			name: "headers as array 2",
			spec: `to: 'myAwesomeEmail@localhost.de'
from: 'myOtherAwesomeEmail@localhost.de'
headers:
  - key: key1
    value: value1
  - key: key2
    value: value2
`,
			headers: map[string]string{"key1": "value1", "key2": "value2"},
		},
		{
			name: "wrong type in headers",
			spec: `to: 'myAwesomeEmail@localhost.de'
from: 'myOtherAwesomeEmail@localhost.de'
headers: 1
`,
			wantErr: true,
		},
		{
			name: "wrong type in headers 2",
			spec: `to: 'myAwesomeEmail@localhost.de'
from: 'myOtherAwesomeEmail@localhost.de'
headers: "headers"
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config EmailConfig
			err := yaml.Unmarshal([]byte(tt.spec), &config)
			if (err != nil) != tt.wantErr {
				t.Errorf("EmailConfigHeaders.UnmarshalYAML() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(config.Headers, tt.headers) {
				t.Fatalf("EmailConfigHeaders.UnmarshalYAML() wrong enmarshalled headers, got: %v, want: %v", config.Headers, tt.headers)
			}
		})
	}
}
