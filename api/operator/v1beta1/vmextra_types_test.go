package v1beta1

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
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
	got, err := json.Marshal(struct {
		Match StringOrArray `json:"match"`
	}{
		Match: StringOrArray{"1"},
	})
	assert.NoError(t, err)
	assert.Equal(t, `{"match":"1"}`, string(got))

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

func TestUseProxyProtocol(t *testing.T) {
	type opts struct {
		args     map[string]string
		expected bool
	}
	f := func(o opts) {
		assert.Equal(t, o.expected, UseProxyProtocol(o.args))
	}

	// no args set
	f(opts{})

	// no proxy protocol flag
	f(opts{
		args: map[string]string{
			"test": "test",
		},
	})

	// proxy protocol set to false
	f(opts{
		args: map[string]string{
			httpUseProxyProtocolFlag: "false",
		},
	})

	// first proxy protocol value is false
	f(opts{
		args: map[string]string{
			httpUseProxyProtocolFlag: "false,true,true",
		},
	})

	// proxy protocol is true
	f(opts{
		args: map[string]string{
			httpUseProxyProtocolFlag: "true",
		},
		expected: true,
	})

	// only first value is true
	f(opts{
		args: map[string]string{
			httpUseProxyProtocolFlag: "true,false,false",
		},
		expected: true,
	})

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

func TestCommonAppsParamsValidate(t *testing.T) {
	f := func(p CommonAppsParams, wantErr bool) {
		t.Helper()
		err := p.Validate()
		if wantErr {
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		} else {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
	}
	// both nil — ok
	f(CommonAppsParams{}, false)
	// only preStop set — ok (grace period unknown, no constraint)
	f(CommonAppsParams{PreStopSleepSeconds: ptr.To[int32](15)}, false)
	// only grace period set — ok
	f(CommonAppsParams{TerminationGracePeriodSeconds: ptr.To[int64](30)}, false)
	// preStop < grace period — ok
	f(CommonAppsParams{
		PreStopSleepSeconds:           ptr.To[int32](15),
		TerminationGracePeriodSeconds: ptr.To[int64](30),
	}, false)
	// preStop == grace period — error
	f(CommonAppsParams{
		PreStopSleepSeconds:           ptr.To[int32](15),
		TerminationGracePeriodSeconds: ptr.To[int64](15),
	}, true)
	// preStop > grace period — error
	f(CommonAppsParams{
		PreStopSleepSeconds:           ptr.To[int32](30),
		TerminationGracePeriodSeconds: ptr.To[int64](15),
	}, true)
}

func TestVLogs_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VLogs{Spec: VLogsSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	f("myapp", false, "vlogs-myapp")
	f("myapp", true, "myapp")
}
