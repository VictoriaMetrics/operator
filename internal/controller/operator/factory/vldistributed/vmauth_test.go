package vldistributed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func newTestLBZones(vlAgents []*vmv1.VLAgent, vlClusters []*vmv1.VLCluster, vlSingles []*vmv1.VLSingle) *zones {
	zs := &zones{vlagents: vlAgents}
	for _, c := range vlClusters {
		zs.backends = append(zs.backends, vlBackend{obj: c})
	}
	for _, s := range vlSingles {
		zs.backends = append(zs.backends, vlBackend{obj: s})
	}
	return zs
}

func TestVLBackendTargetRef(t *testing.T) {
	now := metav1.Now()
	owner := metav1.OwnerReference{
		APIVersion: "operator.victoriametrics.com/v1alpha1",
		Kind:       "VLDistributed",
		Name:       "test-dist",
	}
	makeCluster := func(name string, gen int64) *vmv1.VLCluster {
		return &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         "ns",
				CreationTimestamp: now,
				OwnerReferences:   []metav1.OwnerReference{owner},
			},
			Status: vmv1.VLClusterStatus{
				StatusMetadata: vmv1beta1.StatusMetadata{
					UpdateStatus:       vmv1beta1.UpdateStatusOperational,
					ObservedGeneration: gen,
				},
			},
		}
	}

	type opts struct {
		clusters   []*vmv1.VLCluster
		excludeIds []int
		wantNames  []string
	}

	f := func(o opts) {
		t.Helper()
		backends := make([]vlBackend, len(o.clusters))
		for i, c := range o.clusters {
			backends[i] = vlBackend{obj: c}
		}
		ref := vlBackendTargetRef(backends, "VLCluster/vlselect", []string{"/select/.+", "/flags", "/metrics"}, &owner, o.excludeIds...)
		var gotNames []string
		for _, nsn := range ref.CRD.Objects {
			gotNames = append(gotNames, nsn.Name)
		}
		assert.Equal(t, o.wantNames, gotNames)
	}

	// sort by ObservedGeneration (newest first)
	f(opts{
		clusters: []*vmv1.VLCluster{
			makeCluster("zone-gen5", 5),
			makeCluster("zone-gen3", 3),
			makeCluster("zone-gen1", 1),
		},
		wantNames: []string{"zone-gen1", "zone-gen3", "zone-gen5"},
	})

	// skip excluded zone indexes
	f(opts{
		clusters: []*vmv1.VLCluster{
			makeCluster("zone-gen5", 5),
			makeCluster("zone-gen3", 3),
			makeCluster("zone-gen1", 1),
		},
		excludeIds: []int{0},
		wantNames:  []string{"zone-gen1", "zone-gen3"},
	})

	// clusters with zero CreationTimestamp (not yet created) are skipped
	f(opts{
		clusters: []*vmv1.VLCluster{
			makeCluster("zone-gen5", 5),
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "zone-new",
					Namespace:       "ns",
					OwnerReferences: []metav1.OwnerReference{owner},
				},
			},
			makeCluster("zone-gen1", 1),
		},
		wantNames: []string{"zone-gen1", "zone-gen5"},
	})

	// clusters without owner reference are skipped
	f(opts{
		clusters: []*vmv1.VLCluster{
			makeCluster("zone-gen5", 5),
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "zone-no-owner",
					Namespace:         "ns",
					CreationTimestamp: now,
				},
			},
			makeCluster("zone-gen1", 1),
		},
		wantNames: []string{"zone-gen1", "zone-gen5"},
	})
}

func TestBuildVMAuthLBZoneOrder(t *testing.T) {
	now := metav1.Now()

	cr := &vmv1alpha1.VLDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1alpha1",
			Kind:       "VLDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dist",
			Namespace: "ns",
		},
		Spec: vmv1alpha1.VLDistributedSpec{
			VMAuth: vmv1alpha1.VLDistributedAuth{
				Enabled: ptr.To(true),
			},
		},
	}
	owner := cr.AsOwner()

	makeCluster := func(name string, gen int64) *vmv1.VLCluster {
		return &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         "ns",
				CreationTimestamp: now,
				OwnerReferences:   []metav1.OwnerReference{owner},
			},
			Status: vmv1.VLClusterStatus{
				StatusMetadata: vmv1beta1.StatusMetadata{
					ObservedGeneration: gen,
				},
			},
		}
	}
	makeAgent := func(name string) *vmv1.VLAgent {
		return &vmv1.VLAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         "ns",
				CreationTimestamp: now,
				OwnerReferences:   []metav1.OwnerReference{owner},
			},
		}
	}

	type opts struct {
		clusters         []*vmv1.VLCluster
		agents           []*vmv1.VLAgent
		excludeIds       []int
		wantClusterNames []string
	}

	f := func(o opts) {
		t.Helper()
		vmAuth := buildVMAuthLB(cr, newTestLBZones(o.agents, o.clusters, nil), o.excludeIds...)
		assert.NotNil(t, vmAuth)
		assert.Len(t, vmAuth.Spec.DefaultTargetRefs, 2)

		readRef := vmAuth.Spec.DefaultTargetRefs[1]

		var gotClusterNames []string
		for _, nsn := range readRef.CRD.Objects {
			gotClusterNames = append(gotClusterNames, nsn.Name)
		}
		assert.Equal(t, o.wantClusterNames, gotClusterNames)
	}

	// clusters: oldest generations first
	f(opts{
		clusters: []*vmv1.VLCluster{
			makeCluster("zone-gen5", 5),
			makeCluster("zone-gen3", 3),
			makeCluster("zone-gen1", 1),
		},
		agents: []*vmv1.VLAgent{
			makeAgent("zone-gen5"),
			makeAgent("zone-gen3"),
			makeAgent("zone-gen1"),
		},
		wantClusterNames: []string{"zone-gen1", "zone-gen3", "zone-gen5"},
	})

	// exclude a zone
	f(opts{
		clusters: []*vmv1.VLCluster{
			makeCluster("zone-gen5", 5),
			makeCluster("zone-gen3", 3),
			makeCluster("zone-gen1", 1),
		},
		agents: []*vmv1.VLAgent{
			makeAgent("zone-gen5"),
			makeAgent("zone-gen3"),
			makeAgent("zone-gen1"),
		},
		excludeIds:       []int{1},
		wantClusterNames: []string{"zone-gen1", "zone-gen5"},
	})
}

func TestBuildVMAuthLBVLSingleBackend(t *testing.T) {
	now := metav1.Now()
	cr := &vmv1alpha1.VLDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1alpha1",
			Kind:       "VLDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dist",
			Namespace: "ns",
		},
		Spec: vmv1alpha1.VLDistributedSpec{
			BackendType: vmv1alpha1.VLDistributedBackendTypeVLSingle,
		},
	}
	owner := cr.AsOwner()
	vmAuth := buildVMAuthLB(cr, newTestLBZones(
		[]*vmv1.VLAgent{
			{ObjectMeta: metav1.ObjectMeta{Name: "agent-a", Namespace: "ns", CreationTimestamp: now, OwnerReferences: []metav1.OwnerReference{owner}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "agent-b", Namespace: "ns", CreationTimestamp: now, OwnerReferences: []metav1.OwnerReference{owner}}},
		},
		nil,
		[]*vmv1.VLSingle{
			{ObjectMeta: metav1.ObjectMeta{Name: "single-a", Namespace: "ns", CreationTimestamp: now, OwnerReferences: []metav1.OwnerReference{owner}}},
			{ObjectMeta: metav1.ObjectMeta{Name: "single-b", Namespace: "ns", CreationTimestamp: now, OwnerReferences: []metav1.OwnerReference{owner}}},
		},
	))

	assert.NotNil(t, vmAuth)
	assert.Len(t, vmAuth.Spec.DefaultTargetRefs, 2)
	assert.Equal(t, "write", vmAuth.Spec.DefaultTargetRefs[0].Name)
	assert.Equal(t, "VLAgent", vmAuth.Spec.DefaultTargetRefs[0].CRD.Kind)
	assert.Equal(t, "read", vmAuth.Spec.DefaultTargetRefs[1].Name)
	assert.Equal(t, "VLSingle", vmAuth.Spec.DefaultTargetRefs[1].CRD.Kind)
	assert.Equal(t, []vmv1beta1.NamespacedName{
		{Name: "single-b", Namespace: "ns"},
		{Name: "single-a", Namespace: "ns"},
	}, vmAuth.Spec.DefaultTargetRefs[1].CRD.Objects)
}
