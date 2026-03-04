package v1beta1

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
)

func Test_buildPathWithPrefixFlag(t *testing.T) {
	type opts struct {
		flags       map[string]string
		defaultPath string
		want        string
	}
	f := func(o opts) {
		t.Helper()
		assert.Equal(t, BuildPathWithPrefixFlag(o.flags, o.defaultPath), o.want)
	}

	// default path
	f(opts{
		defaultPath: healthPath,
		want:        healthPath,
	})

	// with some prefix
	f(opts{
		defaultPath: healthPath,
		flags:       map[string]string{"some.flag": "some-value", httpPathPrefixFlag: "/prefix/path/"},
		want:        fmt.Sprintf("/prefix/path%s", healthPath),
	})

	// with bad path
	f(opts{
		defaultPath: healthPath,
		flags:       map[string]string{"some.flag": "some-value", httpPathPrefixFlag: "badpath/badvalue"},
		want:        fmt.Sprintf("badpath/badvalue%s", healthPath),
	})
}

func TestParsingMatch(t *testing.T) {
	type opts struct {
		data    string
		match   StringOrArray
		wantErr bool
	}
	f := func(o opts) {
		t.Helper()
		var match StringOrArray
		err := yaml.Unmarshal([]byte(o.data), &match)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		assert.Equal(t, match, o.match)
	}

	// old string match
	f(opts{
		data:  `http_requests_total`,
		match: StringOrArray{"http_requests_total"},
	})

	// new list match
	f(opts{
		data: `
- \{__name__=~"count1"\}
- \{__name__=~"count2"\}
`,
		match: StringOrArray{"\\{__name__=~\"count1\"\\}", "\\{__name__=~\"count2\"\\}"},
	})

	// wrong type of match
	f(opts{
		data:    `{__name__=~"count1"}`,
		wantErr: true,
	})
}

func TestStringOrArrayMarshal(t *testing.T) {
	f := func(src *StringOrArray, marshalF func(any) ([]byte, error), expected string) {
		t.Helper()
		got, err := marshalF(src)
		assert.NoError(t, err)
		assert.Equal(t, expected, string(got))
	}

	f(&StringOrArray{"1", "2", "3"}, json.Marshal, `["1","2","3"]`)
	f(&StringOrArray{"1"}, json.Marshal, `"1"`)
	f(&StringOrArray{}, json.Marshal, `""`)
	f(&StringOrArray{"1", "2", "3"}, yaml.Marshal, `- "1"
- "2"
- "3"
`)
	f(&StringOrArray{"1"}, yaml.Marshal, `"1"
`)
	f(&StringOrArray{}, yaml.Marshal, `""
`)

}

func TestStringOrArrayUnMarshal(t *testing.T) {
	f := func(src string, unmarshalF func([]byte, any) error, expected StringOrArray) {
		t.Helper()
		var got StringOrArray
		assert.NoError(t, unmarshalF([]byte(src), &got))
		assert.Equal(t, expected, got)
	}
	f(`["1","2","3"]`, json.Unmarshal, StringOrArray{"1", "2", "3"})
	f(`"1"`, json.Unmarshal, StringOrArray{"1"})
	f(`""`, json.Unmarshal, StringOrArray{""})
	f(`- "1"
- "2"
- "3"
`, yaml.Unmarshal, StringOrArray{"1", "2", "3"})

	f(`"1"
`, yaml.Unmarshal, StringOrArray{"1"})
	f(`""
`, yaml.Unmarshal, StringOrArray{""})

}

func TestEmbeddedVPAValidation(t *testing.T) {
	type opts struct {
		vpa     *EmbeddedVPA
		wantErr bool
	}
	updateModeRecreate := vpav1.UpdateModeRecreate
	f := func(o opts) {
		t.Helper()
		err := o.vpa.Validate()
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}

	// empty VPA should fail
	f(opts{
		vpa:     &EmbeddedVPA{},
		wantErr: true,
	})

	// VPA with empty updatePolicy should fail
	f(opts{
		vpa: &EmbeddedVPA{
			UpdatePolicy: &vpav1.PodUpdatePolicy{},
		},
		wantErr: true,
	})

	// VPA with updateMode only should fail
	f(opts{
		vpa: &EmbeddedVPA{
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &updateModeRecreate,
			},
		},
		wantErr: true,
	})

	// VPA with empty resourcePolicy should fail
	f(opts{
		vpa: &EmbeddedVPA{
			ResourcePolicy: &vpav1.PodResourcePolicy{},
		},
		wantErr: true,
	})

	// VPA with containerPolicies only should fail
	f(opts{
		vpa: &EmbeddedVPA{
			ResourcePolicy: &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{ContainerName: "test"},
				},
			},
		},
		wantErr: true,
	})

	// VPA with recommenders only should fail
	f(opts{
		vpa: &EmbeddedVPA{
			Recommenders: []*vpav1.VerticalPodAutoscalerRecommenderSelector{
				{Name: "test"},
			},
		},
		wantErr: true,
	})

	// VPA with updateMode and recommenders should fail
	f(opts{
		vpa: &EmbeddedVPA{
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &updateModeRecreate,
			},
			Recommenders: []*vpav1.VerticalPodAutoscalerRecommenderSelector{
				{Name: "test"},
			},
		},
		wantErr: true,
	})

	// VPA with containerPolicies and recommenders should fail
	f(opts{
		vpa: &EmbeddedVPA{
			ResourcePolicy: &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{ContainerName: "test"},
				},
			},
			Recommenders: []*vpav1.VerticalPodAutoscalerRecommenderSelector{
				{Name: "test"},
			},
		},
		wantErr: true,
	})

	// VPA with updateMode and containerPolicies should pass
	f(opts{
		vpa: &EmbeddedVPA{
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &updateModeRecreate,
			},
			ResourcePolicy: &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{ContainerName: "test"},
				},
			},
		},
		wantErr: false,
	})

	// VPA with all configs should pass
	f(opts{
		vpa: &EmbeddedVPA{
			UpdatePolicy: &vpav1.PodUpdatePolicy{
				UpdateMode: &updateModeRecreate,
			},
			ResourcePolicy: &vpav1.PodResourcePolicy{
				ContainerPolicies: []vpav1.ContainerResourcePolicy{
					{ContainerName: "test"},
				},
			},
			Recommenders: []*vpav1.VerticalPodAutoscalerRecommenderSelector{
				{Name: "test"},
			},
		},
		wantErr: false,
	})
}
