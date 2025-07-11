package v1beta1

import (
	"testing"
)

func TestVMAgent_Validate(t *testing.T) {
	f := func(cr *VMAgent, wantErr bool) {
		t.Helper()
		if err := cr.Validate(); (err != nil) != wantErr {
			t.Errorf("Validate() error = %v, wantErr %v", err, wantErr)
		}
	}

	// wo remotewrite
	f(&VMAgent{}, true)

	// rw empty url
	f(&VMAgent{
		Spec: VMAgentSpec{
			RemoteWrite: []VMAgentRemoteWriteSpec{
				{},
			},
		},
	}, true)

	// bad inline cfg
	f(&VMAgent{
		Spec: VMAgentSpec{
			RemoteWrite:        []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
			InlineScrapeConfig: "some; none yaml formatted string",
		},
	}, true)

	// valid inline cfg
	f(&VMAgent{
		Spec: VMAgentSpec{
			RemoteWrite:        []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
			InlineScrapeConfig: `key: value`,
		},
	}, false)

	// valid relabeling
	f(&VMAgent{
		Spec: VMAgentSpec{
			RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
			InlineRelabelConfig: []*RelabelConfig{
				{
					Action:       "drop",
					SourceLabels: []string{"src_id"},
				},
			},
		},
	}, false)

	// relabeling with if array
	f(&VMAgent{
		Spec: VMAgentSpec{
			RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
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
