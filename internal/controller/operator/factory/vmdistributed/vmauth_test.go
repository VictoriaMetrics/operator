package vmdistributed

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestVMClusterTargetRefZoneOrder(t *testing.T) {
	now := metav1.Now()
	owner := metav1.OwnerReference{
		APIVersion: "operator.victoriametrics.com/v1alpha1",
		Kind:       "VMDistributed",
		Name:       "test-dist",
	}
	makeCluster := func(name string, gen int64) *vmv1beta1.VMCluster {
		return &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         "ns",
				CreationTimestamp: now,
				OwnerReferences:   []metav1.OwnerReference{owner},
			},
			Status: vmv1beta1.VMClusterStatus{
				StatusMetadata: vmv1beta1.StatusMetadata{
					UpdateStatus:       vmv1beta1.UpdateStatusOperational,
					ObservedGeneration: gen,
				},
			},
		}
	}

	type opts struct {
		clusters   []*vmv1beta1.VMCluster
		excludeIds []int
		wantNames  []string
	}

	f := func(o opts) {
		t.Helper()
		ref := vmClusterTargetRef(o.clusters, &owner, o.excludeIds...)
		var gotNames []string
		for _, nsn := range ref.CRD.Objects {
			gotNames = append(gotNames, nsn.Name)
		}
		assert.Equal(t, o.wantNames, gotNames)
	}

	// sort by ObservedGeneration (newest first)
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			makeCluster("zone-gen5", 5),
			makeCluster("zone-gen3", 3),
			makeCluster("zone-gen1", 1),
		},
		wantNames: []string{"zone-gen1", "zone-gen3", "zone-gen5"},
	})

	// skip excluded zone indexes
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			makeCluster("zone-gen5", 5),
			makeCluster("zone-gen3", 3),
			makeCluster("zone-gen1", 1),
		},
		excludeIds: []int{0},
		wantNames:  []string{"zone-gen1", "zone-gen3"},
	})

	// clusters with zero CreationTimestamp (not yet created) are skipped
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
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
		clusters: []*vmv1beta1.VMCluster{
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

	cr := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "operator.victoriametrics.com/v1alpha1",
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-dist",
			Namespace: "ns",
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			VMAuth: vmv1alpha1.VMDistributedAuth{
				Enabled: ptr.To(true),
			},
		},
	}
	owner := cr.AsOwner()

	makeCluster := func(name string, gen int64) *vmv1beta1.VMCluster {
		return &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         "ns",
				CreationTimestamp: now,
				OwnerReferences:   []metav1.OwnerReference{owner},
			},
			Status: vmv1beta1.VMClusterStatus{
				StatusMetadata: vmv1beta1.StatusMetadata{
					ObservedGeneration: gen,
				},
			},
		}
	}
	makeAgent := func(name string) *vmv1beta1.VMAgent {
		return &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:              name,
				Namespace:         "ns",
				CreationTimestamp: now,
				OwnerReferences:   []metav1.OwnerReference{owner},
			},
		}
	}

	type opts struct {
		clusters         []*vmv1beta1.VMCluster
		agents           []*vmv1beta1.VMAgent
		excludeIds       []int
		wantClusterNames []string
	}

	f := func(o opts) {
		t.Helper()
		vmAuth := buildVMAuthLB(cr, o.agents, o.clusters, o.excludeIds...)
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
		clusters: []*vmv1beta1.VMCluster{
			makeCluster("zone-gen5", 5),
			makeCluster("zone-gen3", 3),
			makeCluster("zone-gen1", 1),
		},
		agents: []*vmv1beta1.VMAgent{
			makeAgent("zone-gen5"),
			makeAgent("zone-gen3"),
			makeAgent("zone-gen1"),
		},
		wantClusterNames: []string{"zone-gen1", "zone-gen3", "zone-gen5"},
	})

	// exclude a zone
	f(opts{
		clusters: []*vmv1beta1.VMCluster{
			makeCluster("zone-gen5", 5),
			makeCluster("zone-gen3", 3),
			makeCluster("zone-gen1", 1),
		},
		agents: []*vmv1beta1.VMAgent{
			makeAgent("zone-gen5"),
			makeAgent("zone-gen3"),
			makeAgent("zone-gen1"),
		},
		excludeIds:       []int{1},
		wantClusterNames: []string{"zone-gen1", "zone-gen5"},
	})
}
