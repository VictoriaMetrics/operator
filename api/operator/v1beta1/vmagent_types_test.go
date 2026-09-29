package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

func TestVMAgent_Validate(t *testing.T) {
	f := func(spec VMAgentSpec, wantErr bool) {
		t.Helper()
		r := &VMAgent{
			Spec: spec,
		}
		if wantErr {
			assert.Error(t, r.Validate())
		} else {
			assert.NoError(t, r.Validate())
		}
	}

	// wo remotewrite
	f(VMAgentSpec{}, true)

	// rw empty url
	f(VMAgentSpec{RemoteWrite: []VMAgentRemoteWriteSpec{{}}}, true)

	// bad inline cfg
	f(VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonScrapeParams: CommonScrapeParams{
			InlineScrapeConfig: "some; none yaml formatted string",
		},
	}, true)

	// valid inline cfg
	f(VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonScrapeParams: CommonScrapeParams{
			InlineScrapeConfig: `key: value`,
		},
	}, false)

	// valid relabeling
	f(VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonRelabelParams: CommonRelabelParams{
			InlineRelabelConfig: []*RelabelConfig{
				{
					Action:       "drop",
					SourceLabels: []string{"src_id"},
				},
			},
		},
	}, false)

	// duplicate scrape class names
	f(VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "class-a"},
				{Name: "class-a"},
			},
		},
	}, true)

	// multiple default scrape classes
	f(VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "class-a", Default: ptr.To(true)},
				{Name: "class-b", Default: ptr.To(true)},
			},
		},
	}, true)

	// relabeling with if array
	f(VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonRelabelParams: CommonRelabelParams{
			InlineRelabelConfig: []*RelabelConfig{
				{
					Action: "drop_metrics",
					If: []string{
						"{job=~\"aaa.*\"}",
						"{job=~\"bbb.*\"}",
					},
				},
			},
		},
	}, false)

	// single default scrape class - valid
	f(VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "default", Default: ptr.To(true)},
				{Name: "other"},
			},
		},
	}, false)

	// multiple default scrape classes - invalid
	f(VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "default", Default: ptr.To(true)},
				{Name: "other", Default: ptr.To(true)},
			},
		},
	}, true)

	// duplicated scrape class names - invalid
	f(VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "cls"},
				{Name: "cls"},
			},
		},
	}, true)
}

func TestVMAgent_DefaultStatusFields(t *testing.T) {
	f := func(cr *VMAgent, wantShards, wantReplicas int32) {
		t.Helper()
		var status VMAgentStatus
		cr.DefaultStatusFields(&status)
		assert.Equal(t, wantShards, status.Shards, "shards")
		assert.Equal(t, wantReplicas, status.Replicas, "replicas")
	}

	// no shardCount set: must report 1 so VPA scale subresource gets a non-zero statusReplicasPath
	f(&VMAgent{Spec: VMAgentSpec{}}, 1, 0)

	// shardCount explicitly set
	f(&VMAgent{Spec: VMAgentSpec{ShardCount: ptr.To(int32(3))}}, 3, 0)

	// shardCount=1 explicitly
	f(&VMAgent{Spec: VMAgentSpec{ShardCount: ptr.To(int32(1))}}, 1, 0)

	// daemonset mode disables sharding, should still report 1
	f(&VMAgent{Spec: VMAgentSpec{ShardCount: ptr.To(int32(3)), DaemonSetMode: true}}, 1, 0)

	// replicaCount is tracked independently
	f(&VMAgent{Spec: VMAgentSpec{CommonAppsParams: CommonAppsParams{ReplicaCount: ptr.To(int32(2))}}}, 1, 2)
}

func TestVMAgent_PrefixedName(t *testing.T) {
	f := func(name string, omit bool, want string) {
		t.Helper()
		cr := &VMAgent{Spec: VMAgentSpec{UseLegacyNaming: omit}}
		cr.Name = name
		assert.Equal(t, want, cr.PrefixedName())
	}

	f("myapp", false, "vmagent-myapp")
	f("myapp", true, "myapp")
}

// TestVMAgent_IsUnmanaged is the VMAgent counterpart of TestVMAlert_IsUnmanaged.
func TestVMAgent_IsUnmanaged(t *testing.T) {
	f := func(cr VMAgent, want bool) {
		t.Helper()
		assert.Equal(t, want, cr.IsUnmanaged(nil))
	}

	f(VMAgent{Spec: VMAgentSpec{CommonScrapeParams: CommonScrapeParams{SelectAllByDefault: true}}}, false)
	f(VMAgent{}, true)
	f(VMAgent{
		Status: VMAgentStatus{ParsingSpecError: `json: unknown field "foo"`},
		Spec:   VMAgentSpec{CommonScrapeParams: CommonScrapeParams{SelectAllByDefault: true}},
	}, false)
	f(VMAgent{
		Status: VMAgentStatus{ParsingSpecError: "some other unrelated parse failure"},
		Spec:   VMAgentSpec{CommonScrapeParams: CommonScrapeParams{SelectAllByDefault: true}},
	}, true)
}

func TestVMAgent_Validate_RemoteWriteSettings(t *testing.T) {
	f := func(rw VMAgentRemoteWriteSpec, wantErr bool) {
		t.Helper()
		rw.URL = "http://some-rw"
		r := &VMAgent{Spec: VMAgentSpec{RemoteWrite: []VMAgentRemoteWriteSpec{rw}}}
		if wantErr {
			assert.Error(t, r.Validate())
		} else {
			assert.NoError(t, r.Validate())
		}
	}

	// forcePromProto and forceVMProto are mutually exclusive, vmagent exits with a fatal error
	f(VMAgentRemoteWriteSpec{ForcePromProto: ptr.To(true), ForceVMProto: true}, true)
	f(VMAgentRemoteWriteSpec{ForcePromProto: ptr.To(true)}, false)
	f(VMAgentRemoteWriteSpec{ForceVMProto: true}, false)

	// retry intervals
	f(VMAgentRemoteWriteSpec{RetryMinInterval: ptr.To("5s"), RetryMaxInterval: ptr.To("30s")}, false)
	f(VMAgentRemoteWriteSpec{RetryMinInterval: ptr.To("30s"), RetryMaxInterval: ptr.To("5s")}, true)
	// a lone retryMinInterval is compared against the 1m default of retryMaxInterval
	f(VMAgentRemoteWriteSpec{RetryMinInterval: ptr.To("30s")}, false)
	f(VMAgentRemoteWriteSpec{RetryMinInterval: ptr.To("5m")}, true)
	f(VMAgentRemoteWriteSpec{RetryMinInterval: ptr.To("not-a-duration")}, true)

	// obfuscateLabels encoding
	f(VMAgentRemoteWriteSpec{ObfuscateLabels: []string{"instance", "job"}}, false)
	f(VMAgentRemoteWriteSpec{ObfuscateLabels: []string{""}}, true)
	f(VMAgentRemoteWriteSpec{ObfuscateLabels: []string{"a^^b"}}, true)
	f(VMAgentRemoteWriteSpec{ObfuscateLabels: []string{"a,b"}}, true)

	// aws static credentials
	validSel := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "aws-creds"},
		Key:                  "key",
	}
	f(VMAgentRemoteWriteSpec{AWS: &AWS{UseSigv4: true, AccessKeySecret: validSel, SecretKeySecret: validSel}}, false)
	// must be set together
	f(VMAgentRemoteWriteSpec{AWS: &AWS{UseSigv4: true, AccessKeySecret: validSel}}, true)
	f(VMAgentRemoteWriteSpec{AWS: &AWS{UseSigv4: true, SecretKeySecret: validSel}}, true)
	// silently ignored by vmagent without useSigv4
	f(VMAgentRemoteWriteSpec{AWS: &AWS{AccessKeySecret: validSel, SecretKeySecret: validSel}}, true)
	// incomplete selector
	f(VMAgentRemoteWriteSpec{AWS: &AWS{
		UseSigv4:        true,
		AccessKeySecret: &corev1.SecretKeySelector{Key: "key"},
		SecretKeySecret: validSel,
	}}, true)
	f(VMAgentRemoteWriteSpec{AWS: &AWS{
		UseSigv4:        true,
		AccessKeySecret: validSel,
		SecretKeySecret: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "aws-creds"}},
	}}, true)
	// the pre-existing aws fields keep working on their own
	f(VMAgentRemoteWriteSpec{AWS: &AWS{UseSigv4: true, Region: "us-east-1"}}, false)

	// oauth2 endpoint params are ignored by vmagent without a client secret
	f(VMAgentRemoteWriteSpec{OAuth2: &OAuth2{
		EndpointParams: map[string]string{"audience": "x"},
	}}, true)
	f(VMAgentRemoteWriteSpec{OAuth2: &OAuth2{
		ClientSecretFile: "/etc/secret",
		EndpointParams:   map[string]string{"audience": "x"},
	}}, false)
	// a single quote would break the quoting of the flag value
	f(VMAgentRemoteWriteSpec{OAuth2: &OAuth2{
		ClientSecretFile: "/etc/secret",
		EndpointParams:   map[string]string{"audience": "it's"},
	}}, true)

	// mutually exclusive secret sources, they would otherwise corrupt the flag array
	f(VMAgentRemoteWriteSpec{BasicAuth: &BasicAuth{
		Password:     corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "s"}, Key: "p"},
		PasswordFile: "/etc/pass",
	}}, true)
	f(VMAgentRemoteWriteSpec{OAuth2: &OAuth2{
		ClientSecret:     &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "s"}, Key: "p"},
		ClientSecretFile: "/etc/secret",
	}}, true)

	// extraEnvs must not collide with the operator-managed prefix
	r := &VMAgent{Spec: VMAgentSpec{
		RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
		CommonAppsParams: CommonAppsParams{
			ExtraEnvs: []corev1.EnvVar{{Name: RemoteWriteEnvPrefix + "0_AWS_ACCESS_KEY", Value: "x"}},
		},
	}}
	assert.Error(t, r.Validate())
}
