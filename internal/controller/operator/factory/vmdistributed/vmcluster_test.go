package vmdistributed

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stypes "k8s.io/apimachinery/pkg/types"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestSetOwnerRefIfNeeded(t *testing.T) {
	cr := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vdc",
			Namespace: "default",
			UID:       k8stypes.UID("owner-uid"),
		},
	}
	vmc := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1beta1.GroupVersion.String(),
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc",
			Namespace: "default",
		},
	}
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr, vmc})
	modified, err := setOwnerRefIfNeeded(cr, vmc, rclient.Scheme())
	assert.NoError(t, err)
	assert.True(t, modified)
	assert.Len(t, vmc.OwnerReferences, 1)
	assert.Equal(t, "vdc", vmc.OwnerReferences[0].Name)

	// second call should detect owner ref already set
	modified2, err := setOwnerRefIfNeeded(cr, vmc, rclient.Scheme())
	assert.NoError(t, err)
	assert.False(t, modified2)
}

func TestEnsureNoVMClusterOwners(t *testing.T) {
	cr := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vdc",
			Namespace: "default",
			UID:       k8stypes.UID("owner-uid"),
		},
	}
	otherCR := &vmv1alpha1.VMDistributed{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1alpha1.GroupVersion.String(),
			Kind:       "VMDistributed",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "other",
			Namespace: "default",
			UID:       k8stypes.UID("other-uid"),
		},
	}

	vmc := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc",
			Namespace: "default",
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: otherCR.APIVersion,
					Kind:       otherCR.Kind,
					Name:       otherCR.Name,
					UID:        otherCR.UID,
				},
			},
		},
	}

	err := cr.Owns(vmc)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is owned by other distributed resource")
}

func TestWaitForVMClusterReady_Success(t *testing.T) {
	vmc := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1beta1.GroupVersion.String(),
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc",
			Namespace: "default",
		},
		Status: vmv1beta1.VMClusterStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusOperational,
			},
		},
	}
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{vmc})
	ctx := context.Background()
	err := waitForVMClusterReady(ctx, rclient, vmc.DeepCopy(), 500*time.Millisecond)
	assert.NoError(t, err)
}

func TestWaitForVMClusterReady_Timeout(t *testing.T) {
	vmc := &vmv1beta1.VMCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: vmv1beta1.GroupVersion.String(),
			Kind:       "VMCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmc-stuck",
			Namespace: "default",
		},
		Status: vmv1beta1.VMClusterStatus{
			StatusMetadata: vmv1beta1.StatusMetadata{
				UpdateStatus: vmv1beta1.UpdateStatusExpanding,
			},
		},
	}
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{vmc})
	ctx := context.Background()
	err := waitForVMClusterReady(ctx, rclient, vmc.DeepCopy(), 100*time.Millisecond)
	assert.Error(t, err)
}

func TestFetchVMClusters_InlineAndRef(t *testing.T) {
	refCluster := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ref",
			Namespace: "ns",
		},
	}
	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{refCluster})

	inlineSpec := vmv1beta1.VMClusterSpec{
		ClusterVersion: "v0.1.0",
	}
	cr := &vmv1alpha1.VMDistributed{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			Zones: []vmv1alpha1.VMDistributedZone{
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Ref: &corev1.LocalObjectReference{Name: "ref"},
					},
				},
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Name: "inline",
						Spec: &inlineSpec,
					},
				},
			},
		},
	}

	got, err := fetchVMClusters(context.Background(), rclient, cr)
	assert.NoError(t, err)
	assert.Len(t, got, 2)
	assert.Equal(t, "ref", got[0].Name)
	assert.Equal(t, "inline", got[1].Name)
	assert.Equal(t, inlineSpec.ClusterVersion, got[1].Spec.ClusterVersion)
}

func TestFetchVMClusters_SortedByGeneration(t *testing.T) {
	c1 := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-1", Namespace: "ns"},
		Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{ObservedGeneration: 1}},
	}
	c2 := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-2", Namespace: "ns"},
		Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{ObservedGeneration: 3}},
	}
	c3 := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster-3", Namespace: "ns"},
		Status:     vmv1beta1.VMClusterStatus{StatusMetadata: vmv1beta1.StatusMetadata{ObservedGeneration: 2}},
	}

	rclient := k8stools.GetTestClientWithObjects([]runtime.Object{c1, c2, c3})

	cr := &vmv1alpha1.VMDistributed{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns",
		},
		Spec: vmv1alpha1.VMDistributedSpec{
			Zones: []vmv1alpha1.VMDistributedZone{
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Ref: &corev1.LocalObjectReference{Name: "cluster-1"},
					},
				},
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Ref: &corev1.LocalObjectReference{Name: "cluster-2"},
					},
				},
				{
					VMCluster: &vmv1alpha1.VMClusterObjOrRef{
						Ref: &corev1.LocalObjectReference{Name: "cluster-3"},
					},
				},
			},
		},
	}
	got, err := fetchVMClusters(context.Background(), rclient, cr)
	assert.NoError(t, err)
	assert.Len(t, got, 3)

	// Expected order: descending by ObservedGeneration
	assert.Equal(t, "cluster-2", got[0].Name)
	assert.Equal(t, int64(3), got[0].Status.ObservedGeneration)
	assert.Equal(t, "cluster-3", got[1].Name)
	assert.Equal(t, int64(2), got[1].Status.ObservedGeneration)
	assert.Equal(t, "cluster-1", got[2].Name)
	assert.Equal(t, int64(1), got[2].Status.ObservedGeneration)
}
