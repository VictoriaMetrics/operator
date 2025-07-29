package v1beta1

import (
	"testing"
)

func TestVMAgent_Validate(t *testing.T) {
	type opts struct {
		cr      *VMAgent
		wantErr bool
	}
	f := func(opts opts) {
		t.Helper()
		if err := opts.cr.Validate(); (err != nil) != opts.wantErr {
			t.Errorf("Validate() error = %v, wantErr %v", err, opts.wantErr)
		}
	}

	// wo remotewrite
	o := opts{
		cr:      &VMAgent{},
		wantErr: true,
	}
	f(o)

	// rw empty url
	o = opts{
		cr: &VMAgent{
			Spec: VMAgentSpec{
				RemoteWrite: []VMAgentRemoteWriteSpec{
					{},
				},
			},
		},
		wantErr: true,
	}
	f(o)

	// bad inline cfg
	o = opts{
		cr: &VMAgent{
			Spec: VMAgentSpec{
				RemoteWrite:        []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
				InlineScrapeConfig: "some; none yaml formatted string",
			},
		},
		wantErr: true,
	}
	f(o)

	// valid inline cfg
	o = opts{
		cr: &VMAgent{
			Spec: VMAgentSpec{
				RemoteWrite:        []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
				InlineScrapeConfig: `key: value`,
			},
		},
	}
	f(o)

	// valid relabeling
	o = opts{
		cr: &VMAgent{
			Spec: VMAgentSpec{
				RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
				InlineRelabelConfig: []*RelabelConfig{
					{
						Action:       "drop",
						SourceLabels: []string{"src_id"},
					},
				},
			},
		},
	}
	f(o)

	// relabeling with if array
	o = opts{
		cr: &VMAgent{
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
		},
	}
	f(o)
}
