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
