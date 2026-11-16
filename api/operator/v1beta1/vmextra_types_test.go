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

func TestHTTPListenersValidate(t *testing.T) {
	f := func(p CommonAppsParams, wantErr string) {
		t.Helper()
		err := p.Validate()
		if wantErr == "" {
			assert.NoError(t, err)
			return
		}
		assert.ErrorContains(t, err, wantErr)
	}
	tlsListener := HTTPListener{Name: "https", Port: 8443, TLS: &HTTPListenerTLS{CertFile: "/tls/crt", KeyFile: "/tls/key"}}

	// valid listeners
	f(CommonAppsParams{Port: "8428", HTTPListeners: []HTTPListener{
		{Name: "plain", Port: 8429, UseProxyProtocol: true},
		tlsListener,
		{Name: "mtls", Port: 8444, TLS: &HTTPListenerTLS{CertFile: "/tls/crt", KeyFile: "/tls/key", MTLS: true, MTLSCAFile: "/tls/ca"}},
	}}, "")

	// listeners in both httpListeners and extraArgs
	f(CommonAppsParams{
		ExtraArgs:     map[string]string{"httpListenAddr": ":8428,:8429"},
		HTTPListeners: []HTTPListener{tlsListener},
	}, "cannot be combined with listeners defined in extraArgs.httpListenAddr")
	f(CommonAppsParams{
		ExtraArgs:     map[string]string{"tls": "true,false"},
		HTTPListeners: []HTTPListener{tlsListener},
	}, "cannot be combined with listeners defined in extraArgs.tls")

	// single primary listener value in extraArgs is allowed unless its port conflicts
	f(CommonAppsParams{
		ExtraArgs:     map[string]string{"tls": "true"},
		HTTPListeners: []HTTPListener{tlsListener},
	}, "")
	f(CommonAppsParams{
		ExtraArgs:     map[string]string{"httpListenAddr": ":8429"},
		HTTPListeners: []HTTPListener{{Name: "plain", Port: 8429}},
	}, "port=8429 is already in use")
	f(CommonAppsParams{
		ExtraArgs:     map[string]string{"httpListenAddr": "[::1]:8443"},
		HTTPListeners: []HTTPListener{tlsListener},
	}, "port=8443 is already in use")
	f(CommonAppsParams{
		Port:          "8428",
		ExtraArgs:     map[string]string{"httpListenAddr": ":8428"},
		HTTPListeners: []HTTPListener{{Name: "plain", Port: 8429}},
	}, "")
	f(CommonAppsParams{
		Port:          "8428",
		ExtraArgs:     map[string]string{"httpListenAddr": ":9000"},
		HTTPListeners: []HTTPListener{{Name: "plain", Port: 8429}},
	}, "extraArgs.httpListenAddr port=9000 must match port=8428")

	// reserved name
	f(CommonAppsParams{HTTPListeners: []HTTPListener{{Name: "8080", Port: 8429}}}, "is invalid")
	f(CommonAppsParams{HTTPListeners: []HTTPListener{{Name: "a--b", Port: 8429}}}, "is invalid")
	f(CommonAppsParams{HTTPListeners: []HTTPListener{{Name: "http", Port: 8429}}}, "is reserved")
	f(CommonAppsParams{HTTPListeners: []HTTPListener{{Name: "syslog-tcp-0", Port: 8429}}}, "is reserved")

	// duplicated name
	f(CommonAppsParams{HTTPListeners: []HTTPListener{tlsListener, tlsListener}}, "is duplicated")

	// port conflicts with primary port
	f(CommonAppsParams{Port: "8443", HTTPListeners: []HTTPListener{tlsListener}}, "port=8443 is already in use")
}

func TestConfigReloaderPort(t *testing.T) {
	f := func(addr, want string) {
		t.Helper()
		p := CommonConfigReloaderParams{}
		if addr != "" {
			p.ConfigReloaderExtraArgs = map[string]string{"http.listenAddr": addr}
		}
		assert.Equal(t, want, p.ConfigReloaderPort())
	}

	f("", "8435")
	f(":8436", "8436")
	f("[::1]:8437", "8437")
	f("hostname-only", "8435")
	f("127.0.0.1:0", "8435")
	f("127.0.0.1:70000", "8435")
}

func TestHTTPListenersSidecarPortConflicts(t *testing.T) {
	f := func(cr interface{ Validate() error }, wantErr string) {
		t.Helper()
		err := cr.Validate()
		if wantErr == "" {
			assert.NoError(t, err)
			return
		}
		assert.ErrorContains(t, err, wantErr)
	}
	listeners := func(port int32) CommonAppsParams {
		return CommonAppsParams{Port: "8427", HTTPListeners: []HTTPListener{{Name: "extra", Port: port}}}
	}

	// config-reloader default and custom ports
	f(&VMAuth{Spec: VMAuthSpec{CommonAppsParams: listeners(8435)}}, `port=8435 conflicts with config-reloader port`)
	f(&VMAuth{Spec: VMAuthSpec{
		CommonAppsParams:           listeners(8435),
		CommonConfigReloaderParams: CommonConfigReloaderParams{ConfigReloaderExtraArgs: map[string]string{"http.listenAddr": ":8436"}},
	}}, "")
	f(&VMAuth{Spec: VMAuthSpec{
		CommonAppsParams:           listeners(8436),
		CommonConfigReloaderParams: CommonConfigReloaderParams{ConfigReloaderExtraArgs: map[string]string{"http.listenAddr": ":8436"}},
	}}, `port=8436 conflicts with config-reloader port`)

	// alertmanager cluster mesh port
	f(&VMAlertmanager{Spec: VMAlertmanagerSpec{CommonAppsParams: listeners(9094)}}, `port=9094 conflicts with cluster mesh port`)
	f(&VMAlertmanager{Spec: VMAlertmanagerSpec{CommonAppsParams: listeners(8435)}}, `port=8435 conflicts with config-reloader port`)

	// vmstorage vmbackupmanager sidecar
	f(&VMCluster{Spec: VMClusterSpec{RetentionPeriod: "1", VMStorage: &VMStorage{
		CommonAppsParams: listeners(8300),
		VMBackup:         &VMBackup{Port: "8300", AcceptEULA: true},
	}}}, `vmstorage: httpListeners name="extra" port=8300 conflicts with vmBackup.port`)
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
