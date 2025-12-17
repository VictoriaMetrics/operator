package v1beta1

import (
	"testing"
)

func TestVMAgent_Validate(t *testing.T) {
	f := func(spec VMAgentSpec, wantErr bool) {
		t.Helper()
		r := &VMAgent{
			Spec: spec,
		}
		if err := r.Validate(); (err != nil) != wantErr {
			t.Errorf("Validate() error = %v, wantErr %v", err, wantErr)
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
}
