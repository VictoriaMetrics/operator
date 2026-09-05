package v1beta1

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v2"
	corev1 "k8s.io/api/core/v1"
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

func getStandardAppsParams(listeners []HTTPListener, args map[string]string) *StandardAppsParams {
	return &StandardAppsParams{
		CommonAppsParams: CommonAppsParams{ExtraArgs: args},
		HTTPListeners:    listeners,
	}
}

func TestUseProxyProtocol(t *testing.T) {
	type opts struct {
		listeners []HTTPListener
		args      map[string]string
		expected  bool
	}
	f := func(o opts) {
		t.Helper()
		assert.Equal(t, o.expected, getStandardAppsParams(o.listeners, o.args).UseProxyProtocol())
	}

	// no args set
	f(opts{})

	// no proxy protocol flag
	f(opts{
		args: map[string]string{
			"test": "test",
		},
	})

	// proxy protocol set to false via ExtraArgs
	f(opts{
		args: map[string]string{
			httpUseProxyProtocolFlag: "false",
		},
	})

	// first proxy protocol value is false via ExtraArgs
	f(opts{
		args: map[string]string{
			httpUseProxyProtocolFlag: "false,true,true",
		},
	})

	// proxy protocol is true via ExtraArgs
	f(opts{
		args: map[string]string{
			httpUseProxyProtocolFlag: "true",
		},
		expected: true,
	})

	// only first ExtraArgs value is true
	f(opts{
		args: map[string]string{
			httpUseProxyProtocolFlag: "true,false,false",
		},
		expected: true,
	})

	// primary listener enables proxy protocol
	f(opts{
		listeners: []HTTPListener{
			{Addr: ":8428", Primary: true, UseProxyProtocol: ptr.To(true)},
		},
		expected: true,
	})

	// primary listener disables proxy protocol, ExtraArgs says true — listener wins
	f(opts{
		listeners: []HTTPListener{
			{Addr: ":8428", Primary: true, UseProxyProtocol: ptr.To(false)},
		},
		args:     map[string]string{httpUseProxyProtocolFlag: "true"},
		expected: false,
	})

	// listener without explicit UseProxyProtocol falls through to ExtraArgs
	f(opts{
		listeners: []HTTPListener{
			{Addr: ":8428"},
		},
		args:     map[string]string{httpUseProxyProtocolFlag: "true"},
		expected: true,
	})

	// first listener is implicitly primary when none has Primary:true
	f(opts{
		listeners: []HTTPListener{
			{Addr: ":8428", UseProxyProtocol: ptr.To(true)},
			{Addr: ":8429", UseProxyProtocol: ptr.To(false)},
		},
		expected: true,
	})
}

func TestPrimaryListener(t *testing.T) {
	assert.Nil(t, getStandardAppsParams(nil, nil).Primary())
	assert.Nil(t, getStandardAppsParams([]HTTPListener{}, nil).Primary())

	got := getStandardAppsParams([]HTTPListener{
		{Addr: ":8428", Name: "first"},
		{Addr: ":8429", Name: "second", Primary: true},
	}, nil).Primary()
	assert.Equal(t, "second", got.Name)

	// no Primary flag set — returns first
	got2 := getStandardAppsParams([]HTTPListener{
		{Addr: ":8428", Name: "a"},
		{Addr: ":8429", Name: "b"},
	}, nil).Primary()
	assert.Equal(t, "a", got2.Name)
}

func TestHTTPProto(t *testing.T) {
	// no listeners, no ExtraArgs
	assert.Equal(t, "http", getStandardAppsParams(nil, nil).Proto())

	// ExtraArgs TLS enabled
	assert.Equal(t, "https", getStandardAppsParams(nil, map[string]string{tlsFlag: "true"}).Proto())

	// primary listener with TLS=true overrides ExtraArgs
	assert.Equal(t, "https", getStandardAppsParams([]HTTPListener{{Addr: ":8428", Primary: true, TLS: ptr.To(true)}}, nil).Proto())

	// primary listener with TLS=false overrides ExtraArgs tls flag
	assert.Equal(t, "http", getStandardAppsParams([]HTTPListener{{Addr: ":8428", Primary: true, TLS: ptr.To(false)}}, map[string]string{tlsFlag: "true"}).Proto())

	// listener without TLS set falls through to ExtraArgs
	assert.Equal(t, "https", getStandardAppsParams([]HTTPListener{{Addr: ":8428"}}, map[string]string{tlsFlag: "true"}).Proto())
}

func TestListenerAddrPort(t *testing.T) {
	cases := []struct {
		addr string
		want string
	}{
		{":8428", "8428"},
		{"0.0.0.0:9090", "9090"},
		{"[::]:8080", "8080"},
	}
	for _, c := range cases {
		l := &HTTPListener{Addr: c.addr}
		assert.Equal(t, c.want, l.AddrPort(), "addr=%s", c.addr)
	}
}

func TestListenerByName(t *testing.T) {
	p := getStandardAppsParams([]HTTPListener{
		{Name: "a", Addr: ":8428"},
		{Name: "b", Addr: ":8429"},
	}, nil)
	got := p.ByName("b")
	assert.NotNil(t, got)
	assert.Equal(t, ":8429", got.Addr)

	assert.Nil(t, p.ByName("missing"))
	assert.Nil(t, getStandardAppsParams(nil, nil).ByName("a"))
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

func TestHTTPListenerValidate(t *testing.T) {
	f := func(l HTTPListener, wantErr bool) {
		t.Helper()
		err := l.Validate()
		if wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
	f(HTTPListener{Name: "http", Addr: ":8428"}, false)
	f(HTTPListener{Name: "http", Addr: "bad-addr"}, true)
	f(HTTPListener{Name: "http", Addr: ":not-a-number"}, true)
	f(HTTPListener{Name: "http", Addr: ":0"}, true)
	f(HTTPListener{Name: "http", Addr: ":65536"}, true)
	f(HTTPListener{Name: "http", Addr: ":65535"}, false)
	f(HTTPListener{Addr: ":8428"}, true)
	f(HTTPListener{Name: "not valid!", Addr: ":8428"}, true)
	f(HTTPListener{Name: "http", Addr: ":8428", TLSCertFile: "/tls.crt", TLSCertSecret: &corev1.SecretKeySelector{}}, true)
	f(HTTPListener{Name: "http", Addr: ":8428", TLSKeyFile: "/tls.key", TLSKeySecret: &corev1.SecretKeySelector{}}, true)
	f(HTTPListener{Name: "http", Addr: ":8428", MTLSCAFile: "/ca.crt", MTLSCASecret: &corev1.SecretKeySelector{}}, true)
}

func TestStandardAppsParamsValidate_HTTPListenersConflict(t *testing.T) {
	f := func(p StandardAppsParams, wantErr bool) {
		t.Helper()
		err := p.Validate()
		if wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
	}
	// no listeners, no conflict
	f(StandardAppsParams{}, false)
	// listeners without extraArgs override
	f(StandardAppsParams{HTTPListeners: []HTTPListener{{Name: "http", Addr: ":8428"}}}, false)
	// httpListenAddr override without listeners
	f(StandardAppsParams{CommonAppsParams: CommonAppsParams{ExtraArgs: map[string]string{httpListenAddrFlag: ":8429"}}}, false)
	// both set - conflict
	f(StandardAppsParams{
		HTTPListeners: []HTTPListener{{Name: "http", Addr: ":8428"}},
		CommonAppsParams: CommonAppsParams{
			ExtraArgs: map[string]string{httpListenAddrFlag: ":8429"},
		},
	}, true)
	// single listener synthesized by defaulting to mirror the override - no conflict
	f(StandardAppsParams{
		HTTPListeners: []HTTPListener{{Name: "http", Addr: ":8429"}},
		CommonAppsParams: CommonAppsParams{
			ExtraArgs: map[string]string{httpListenAddrFlag: ":8429"},
		},
	}, false)
	// duplicate listener names
	f(StandardAppsParams{
		HTTPListeners: []HTTPListener{
			{Name: "http", Addr: ":8428"},
			{Name: "http", Addr: ":8429"},
		},
	}, true)
	// more than one listener marked primary
	f(StandardAppsParams{
		HTTPListeners: []HTTPListener{
			{Name: "http", Addr: ":8428", Primary: true},
			{Name: "mtls", Addr: ":8429", Primary: true},
		},
	}, true)
	// listener's explicit TLS conflicts with extraArgs tls
	f(StandardAppsParams{
		HTTPListeners: []HTTPListener{{Name: "http", Addr: ":8428", TLS: ptr.To(false)}},
		CommonAppsParams: CommonAppsParams{
			ExtraArgs: map[string]string{tlsFlag: "true"},
		},
	}, true)
	// listener's explicit UseProxyProtocol conflicts with extraArgs
	f(StandardAppsParams{
		HTTPListeners: []HTTPListener{{Name: "http", Addr: ":8428", UseProxyProtocol: ptr.To(false)}},
		CommonAppsParams: CommonAppsParams{
			ExtraArgs: map[string]string{httpUseProxyProtocolFlag: "true"},
		},
	}, true)
	// extraArgs tls with no explicit per-listener TLS - no conflict (legacy fallback)
	f(StandardAppsParams{
		HTTPListeners: []HTTPListener{{Name: "http", Addr: ":8428"}},
		CommonAppsParams: CommonAppsParams{
			ExtraArgs: map[string]string{tlsFlag: "true"},
		},
	}, false)
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

func TestImage_Reference(t *testing.T) {
	f := func(img Image, want string) {
		t.Helper()
		assert.Equal(t, want, img.Reference())
	}

	// regular tag joined with ":"
	f(Image{Repository: "victoriametrics/vmsingle", Tag: "v1.96.0"},
		"victoriametrics/vmsingle:v1.96.0")

	// sha256 digest tag joined with "@"
	f(Image{Repository: "victoriametrics/vmsingle", Tag: "sha256:abc123def4567890abc123def4567890"},
		"victoriametrics/vmsingle@sha256:abc123def4567890abc123def4567890")

	// non-sha256 algorithm digest also joined with "@"
	f(Image{Repository: "victoriametrics/vmsingle", Tag: "sha512:abc123def4567890abc123def4567890"},
		"victoriametrics/vmsingle@sha512:abc123def4567890abc123def4567890")

	// combined tag + digest pin (Docker "repository:tag@digest" form):
	// the whole value is appended after ":", yielding a valid reference
	f(Image{Repository: "victoriametrics/vmsingle", Tag: "v1.96.0@sha256:abc123def4567890abc123def4567890"},
		"victoriametrics/vmsingle:v1.96.0@sha256:abc123def4567890abc123def4567890")

	// regular tag with no colon joined with ":"
	f(Image{Repository: "victoriametrics/vmsingle", Tag: "v1.96.0-scratch"},
		"victoriametrics/vmsingle:v1.96.0-scratch")

	// tag containing a colon whose suffix isn't hex isn't a digest, joins with ":"
	f(Image{Repository: "victoriametrics/vmsingle", Tag: "v1.96.0-scratch:amd64"},
		"victoriametrics/vmsingle:v1.96.0-scratch:amd64")

	// colon-delimited value whose hex suffix is too short (<32) isn't a digest, joins with ":"
	f(Image{Repository: "victoriametrics/vmsingle", Tag: "sha256:abc123"},
		"victoriametrics/vmsingle:sha256:abc123")

	// digest algorithm with no hex after the colon isn't a digest, joins with ":"
	f(Image{Repository: "victoriametrics/vmsingle", Tag: "sha256:"},
		"victoriametrics/vmsingle:sha256:")

	// empty tag joins with ":"
	f(Image{Repository: "victoriametrics/vmsingle", Tag: ""},
		"victoriametrics/vmsingle:")
}
