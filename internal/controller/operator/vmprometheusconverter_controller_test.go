package operator

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func Test_mergeLabelsWithStrategy(t *testing.T) {
	type opts struct {
		old           map[string]string
		new           map[string]string
		mergeStrategy string
		want          map[string]string
	}
	f := func(o opts) {
		t.Helper()
		got := mergeLabelsWithStrategy(o.old, o.new, o.mergeStrategy)
		assert.Equal(t, got, o.want)
	}

	// delete not existing label
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
		new:           map[string]string{"label1": "value1", "label2": "value4"},
		mergeStrategy: MetaPreferProm,
		want:          map[string]string{"label1": "value1", "label2": "value4"},
	})

	// add new label
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value2", "missinglabel": "value3"},
		new:           map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
		mergeStrategy: MetaPreferProm,
		want:          map[string]string{"label1": "value1", "label2": "value4", "label5": "value10"},
	})

	// add new label with VM priority
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
		new:           map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
		mergeStrategy: MetaPreferVM,
		want:          map[string]string{"label1": "value1", "label2": "value2", "label5": "value3"},
	})

	// remove all labels
	f(opts{
		old:           nil,
		new:           map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
		mergeStrategy: MetaPreferVM,
		want:          nil,
	})

	// remove keep old labels
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value4"},
		new:           nil,
		mergeStrategy: MetaPreferVM,
		want:          map[string]string{"label1": "value1", "label2": "value4"},
	})

	// merge all labels with VMPriority
	f(opts{
		old:           map[string]string{"label1": "value1", "label2": "value4"},
		new:           map[string]string{"label1": "value2", "label2": "value4", "missinglabel": "value10"},
		mergeStrategy: MetaMergeLabelsVMPriority,
		want:          map[string]string{"label1": "value1", "label2": "value4", "missinglabel": "value10"},
	})
}

// newTestSharedAPIDiscoverer creates a sharedAPIDiscoverer with a fast poll interval
// and a fake kubernetes client pre-configured with the given API resources.
func newTestSharedAPIDiscoverer(resources []*metav1.APIResourceList) *sharedAPIDiscoverer {
	fakeClient := fake.NewSimpleClientset()
	fakeClient.Fake.Resources = resources
	return &sharedAPIDiscoverer{
		baseClient:       fakeClient.Discovery(),
		pollInterval:     10 * time.Millisecond,
		kindReadyByGroup: map[string]map[string]chan struct{}{},
	}
}

// Test_sharedAPIDiscoverer_allSubscribersNotified verifies that all subscribers for a
// group are notified when the API becomes available.
func Test_sharedAPIDiscoverer_allSubscribersNotified(t *testing.T) {
	const group = "monitoring.coreos.com/v1"
	sd := newTestSharedAPIDiscoverer([]*metav1.APIResourceList{
		{
			GroupVersion: group,
			APIResources: []metav1.APIResource{
				{Kind: "ServiceMonitor"},
				{Kind: "PodMonitor"},
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	chSvc := sd.subscribeForGroupKind(ctx, group, "ServiceMonitor")
	chPod := sd.subscribeForGroupKind(ctx, group, "PodMonitor")

	for _, ch := range []chan struct{}{chSvc, chPod} {
		select {
		case <-ch:
		case <-ctx.Done():
			t.Fatal("timed out waiting for subscriber notification")
		}
	}
}

// Test_sharedAPIDiscoverer_lateSubscriberGetsNotified verifies the fix for the
// late-subscriber race: if a goroutine subscribes after the polling goroutine has
// already exited (because all prior subscribers were notified), it must still be
// notified via a new polling goroutine.
//
// Before the fix, startPollFor did not delete the group entry from kindReadyByGroup
// when it exited. A late subscriber would add itself to the existing (orphaned) group
// map, but no polling goroutine would ever send to its channel.
func Test_sharedAPIDiscoverer_lateSubscriberGetsNotified(t *testing.T) {
	const group = "monitoring.coreos.com/v1"
	sd := newTestSharedAPIDiscoverer([]*metav1.APIResourceList{
		{
			GroupVersion: group,
			APIResources: []metav1.APIResource{
				{Kind: "ServiceMonitor"},
				{Kind: "PodMonitor"},
			},
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First subscriber: triggers the initial polling goroutine.
	chSvc := sd.subscribeForGroupKind(ctx, group, "ServiceMonitor")
	select {
	case <-chSvc:
	case <-ctx.Done():
		t.Fatal("timed out waiting for ServiceMonitor notification")
	}

	// After chSvc was notified, the polling goroutine exited and (with the fix)
	// deleted the group entry. Give it a moment to complete the cleanup.
	time.Sleep(20 * time.Millisecond)

	// Verify the group was removed from the map, so a late subscriber starts fresh.
	sd.mu.Lock()
	_, groupExists := sd.kindReadyByGroup[group]
	sd.mu.Unlock()
	assert.False(t, groupExists, "group entry should be deleted after all subscribers notified")

	// Late subscriber: the group is gone, so subscribeForGroupKind must recreate
	// the entry and start a new polling goroutine.
	chPod := sd.subscribeForGroupKind(ctx, group, "PodMonitor")
	select {
	case <-chPod:
	case <-ctx.Done():
		t.Fatal("timed out waiting for PodMonitor notification (late-subscriber bug)")
	}
}
