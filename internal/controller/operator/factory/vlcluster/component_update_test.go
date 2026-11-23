package vlcluster

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestAddComponent(t *testing.T) {
	for _, component := range []string{"vlinsert", "vlselect", "vlstorage"} {
		for _, resource := range []string{"pdb", "hpa"} {
			t.Run(component+"/"+resource, func(t *testing.T) {
				cr := &vmv1.VLCluster{
					ObjectMeta: metav1.ObjectMeta{Name: "add-component", Namespace: "default"},
					Spec: vmv1.VLClusterSpec{
						VLInsert:  &vmv1.VLInsert{},
						VLSelect:  &vmv1.VLSelect{},
						VLStorage: &vmv1.VLStorage{},
					},
				}
				cr.Spec.VLInsert.ReplicaCount = ptr.To(int32(0))
				cr.Spec.VLSelect.ReplicaCount = ptr.To(int32(0))
				cr.Spec.VLStorage.ReplicaCount = ptr.To(int32(0))
				cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()

				var pdb **vmv1beta1.EmbeddedPodDisruptionBudgetSpec
				var hpa **vmv1beta1.EmbeddedHPA
				targetKind := "Deployment"
				switch component {
				case "vlinsert":
					cr.Status.LastAppliedSpec.VLInsert = nil
					pdb, hpa = &cr.Spec.VLInsert.PodDisruptionBudget, &cr.Spec.VLInsert.HPA
				case "vlselect":
					cr.Status.LastAppliedSpec.VLSelect = nil
					pdb, hpa = &cr.Spec.VLSelect.PodDisruptionBudget, &cr.Spec.VLSelect.HPA
				case "vlstorage":
					cr.Status.LastAppliedSpec.VLStorage = nil
					pdb, hpa = &cr.Spec.VLStorage.PodDisruptionBudget, &cr.Spec.VLStorage.HPA
					targetKind = "StatefulSet"
				}
				if resource == "pdb" {
					*pdb = &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{MaxUnavailable: ptr.To(intstr.FromInt(1))}
				} else {
					*hpa = &vmv1beta1.EmbeddedHPA{MinReplicas: ptr.To(int32(1)), MaxReplicas: 3}
				}

				fclient := k8stools.GetTestClientWithObjects(nil)
				build.AddDefaults(fclient.Scheme())
				fclient.Scheme().Default(cr)

				// LastAppliedSpec exists, but newly enabled component has no previous spec.
				synctest.Test(t, func(t *testing.T) {
					ctx := t.Context()
					require.NoError(t, CreateOrUpdate(ctx, fclient, cr))
					key := types.NamespacedName{Name: component + "-" + cr.Name, Namespace: cr.Namespace}
					if resource == "pdb" {
						var actual policyv1.PodDisruptionBudget
						require.NoError(t, fclient.Get(ctx, key, &actual))
						assert.Equal(t, (*pdb).MaxUnavailable, actual.Spec.MaxUnavailable)
						assert.Equal(t, []metav1.OwnerReference{cr.AsOwner()}, actual.OwnerReferences)
						return
					}

					var actual autoscalingv2.HorizontalPodAutoscaler
					require.NoError(t, fclient.Get(ctx, key, &actual))
					assert.Equal(t, (*hpa).MinReplicas, actual.Spec.MinReplicas)
					assert.Equal(t, (*hpa).MaxReplicas, actual.Spec.MaxReplicas)
					assert.Equal(t, autoscalingv2.CrossVersionObjectReference{
						Name: key.Name, Kind: targetKind, APIVersion: "apps/v1",
					}, actual.Spec.ScaleTargetRef)
					assert.Equal(t, []metav1.OwnerReference{cr.AsOwner()}, actual.OwnerReferences)
				})
			})
		}
	}
}
