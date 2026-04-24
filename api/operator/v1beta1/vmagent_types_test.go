package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
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
}
