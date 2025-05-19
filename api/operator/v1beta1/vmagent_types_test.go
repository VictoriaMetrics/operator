package v1beta1

import (
	"testing"
)

func TestVMAgent_Validate(t *testing.T) {

	tests := []struct {
		name    string
		spec    VMAgentSpec
		wantErr bool
	}{
		{
			name:    "wo remotewrite",
			spec:    VMAgentSpec{},
			wantErr: true,
		},
		{
			name: "rw empty url",
			spec: VMAgentSpec{RemoteWrite: []VMAgentRemoteWriteSpec{
				{},
			}},
			wantErr: true,
		},
		{
			name: "bad inline cfg",
			spec: VMAgentSpec{
				RemoteWrite:        []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
				InlineScrapeConfig: "some; none yaml formatted string",
			},
			wantErr: true,
		},
		{
			name: "valid inline cfg",
			spec: VMAgentSpec{
				RemoteWrite:        []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
				InlineScrapeConfig: `key: value`,
			},
		},
		{
			name: "valid relabeling",
			spec: VMAgentSpec{
				RemoteWrite: []VMAgentRemoteWriteSpec{{URL: "http://some-rw"}},
				InlineRelabelConfig: []*RelabelConfig{
					{
						Action:       "drop",
						SourceLabels: []string{"src_id"},
					},
				},
			},
		},
		{
			name: "relabeling with if array",
			spec: VMAgentSpec{
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
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &VMAgent{
				Spec: tt.spec,
			}
			if err := r.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
