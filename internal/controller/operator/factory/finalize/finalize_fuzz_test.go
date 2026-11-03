package finalize

import (
	"context"
	"encoding/json"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/VictoriaMetrics/operator/api/operator/fuzztest"
	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

const (
	fuzzName      = "n"
	fuzzNamespace = "ns"
)

// target kinds, used as targets index and in seeds
const (
	vmAgentKind uint8 = iota
	vmAlertKind
	vmAlertmanagerKind
	vmAuthKind
	vmSingleKind
	vmUserKind
	vLogsKind
	vmAnomalyKind
	vlAgentKind
	vlSingleKind
	vtAgentKind
	vtSingleKind
	vlClusterKind
	vtClusterKind
	vmClusterKind
	vmDistributedKind
	vlDistributedKind
)

// deleteTarget fills CR from input, puts it into fake client and runs its delete hook.
type deleteTarget func(ctx context.Context, data []byte) error

func newDeleteTarget[T client.Object](newCR func() T, del func(context.Context, client.Client, T) error) deleteTarget {
	return func(ctx context.Context, data []byte) error {
		cr := newCR()
		if err := json.Unmarshal(data, cr); err != nil {
			fuzztest.New(data).Fill(cr)
		}
		cr.SetName(fuzzName)
		cr.SetNamespace(fuzzNamespace)
		cr.SetResourceVersion("")
		cr.SetDeletionTimestamp(nil)
		cr.SetOwnerReferences(nil)
		cr.SetManagedFields(nil)
		return del(ctx, k8stools.GetTestClientWithObjects([]runtime.Object{cr.DeepCopyObject()}), cr)
	}
}

func FuzzOnDelete(f *testing.F) {
	targets := [...]deleteTarget{
		vmAgentKind:        newDeleteTarget(func() *vmv1beta1.VMAgent { return &vmv1beta1.VMAgent{} }, OnVMAgentDelete),
		vmAlertKind:        newDeleteTarget(func() *vmv1beta1.VMAlert { return &vmv1beta1.VMAlert{} }, OnVMAlertDelete),
		vmAlertmanagerKind: newDeleteTarget(func() *vmv1beta1.VMAlertmanager { return &vmv1beta1.VMAlertmanager{} }, OnVMAlertManagerDelete),
		vmAuthKind:         newDeleteTarget(func() *vmv1beta1.VMAuth { return &vmv1beta1.VMAuth{} }, OnVMAuthDelete),
		vmSingleKind:       newDeleteTarget(func() *vmv1beta1.VMSingle { return &vmv1beta1.VMSingle{} }, OnVMSingleDelete),
		vmUserKind:         newDeleteTarget(func() *vmv1beta1.VMUser { return &vmv1beta1.VMUser{} }, OnVMUserDelete),
		vLogsKind:          newDeleteTarget(func() *vmv1beta1.VLogs { return &vmv1beta1.VLogs{} }, OnVLogsDelete),
		vmAnomalyKind:      newDeleteTarget(func() *vmv1.VMAnomaly { return &vmv1.VMAnomaly{} }, OnVMAnomalyDelete),
		vlAgentKind:        newDeleteTarget(func() *vmv1.VLAgent { return &vmv1.VLAgent{} }, OnVLAgentDelete),
		vlSingleKind:       newDeleteTarget(func() *vmv1.VLSingle { return &vmv1.VLSingle{} }, OnVLSingleDelete),
		vtAgentKind:        newDeleteTarget(func() *vmv1.VTAgent { return &vmv1.VTAgent{} }, OnVTAgentDelete),
		vtSingleKind:       newDeleteTarget(func() *vmv1.VTSingle { return &vmv1.VTSingle{} }, OnVTSingleDelete),
		vlClusterKind: newDeleteTarget(func() *vmv1.VLCluster { return &vmv1.VLCluster{} }, func(ctx context.Context, c client.Client, cr *vmv1.VLCluster) error {
			return OnClusterDelete(ctx, c, cr)
		}),
		vtClusterKind: newDeleteTarget(func() *vmv1.VTCluster { return &vmv1.VTCluster{} }, func(ctx context.Context, c client.Client, cr *vmv1.VTCluster) error {
			return OnClusterDelete(ctx, c, cr)
		}),
		vmClusterKind: newDeleteTarget(func() *vmv1beta1.VMCluster { return &vmv1beta1.VMCluster{} }, func(ctx context.Context, c client.Client, cr *vmv1beta1.VMCluster) error {
			return OnClusterDelete(ctx, c, cr)
		}),
		vmDistributedKind: newDeleteTarget(func() *vmv1alpha1.VMDistributed { return &vmv1alpha1.VMDistributed{} }, OnVMDistributedDelete),
		vlDistributedKind: newDeleteTarget(func() *vmv1alpha1.VLDistributed { return &vmv1alpha1.VLDistributed{} }, OnVLDistributedDelete),
	}
	for kind := range targets {
		f.Add(uint8(kind), false, []byte(`{}`))
		f.Add(uint8(kind), true, []byte(`{}`))
	}
	f.Add(vmAgentKind, true, []byte(`{"spec":{"serviceAccountName":"sa","podDisruptionBudget":{"minAvailable":1},"statefulMode":true}}`))
	f.Add(vmDistributedKind, false, []byte(`{"spec":{"retain":true,"zones":[{"name":"a"},{"name":"b"}]}}`))
	f.Fuzz(func(t *testing.T, kind uint8, namespaced bool, data []byte) {
		cfg := config.MustGetBaseConfig()
		prev := *cfg
		t.Cleanup(func() { *cfg = prev })
		if namespaced {
			cfg.WatchNamespaces = []string{fuzzNamespace, "other"}
		}
		// error is expected for malformed CRs, only panics matter
		_ = targets[int(kind)%len(targets)](t.Context(), data)
	})
}

func FuzzRemoveOrphaned(f *testing.F) {
	f.Add(`{}`, "", false)
	f.Add(`{"metadata":{"labels":{"a":"b"}}}`, "keep", true)
	f.Add(`{"metadata":{"name":"keep","namespace":"ns","finalizers":["f"]}}`, "keep", true)
	f.Fuzz(func(t *testing.T, data string, keep string, shouldRemove bool) {
		cr := &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{Name: fuzzName, Namespace: fuzzNamespace}}
		owner := cr.AsOwner()
		var objMeta metav1.ObjectMeta
		if err := json.Unmarshal([]byte(data), &struct {
			Metadata *metav1.ObjectMeta `json:"metadata"`
		}{&objMeta}); err != nil {
			fuzztest.New([]byte(data)).Fill(&objMeta)
		}
		objMeta.ResourceVersion = ""
		objMeta.DeletionTimestamp = nil
		objMeta.ManagedFields = nil
		objMeta.Namespace = fuzzNamespace
		if objMeta.Name == "" {
			objMeta.Name = "obj"
		}
		owned := objMeta.DeepCopy()
		owned.Labels = cr.SelectorLabels()
		owned.OwnerReferences = []metav1.OwnerReference{owner}
		cl := k8stools.GetTestClientWithObjects([]runtime.Object{
			cr,
			&appsv1.Deployment{ObjectMeta: *owned.DeepCopy()},
			&appsv1.StatefulSet{ObjectMeta: *owned.DeepCopy()},
			&corev1.ConfigMap{ObjectMeta: *objMeta.DeepCopy()},
			&corev1.Service{ObjectMeta: *owned.DeepCopy()},
		})
		keepNames := sets.New(keep)
		ctx := t.Context()

		// errors are expected for malformed objects, only panics matter
		_ = RemoveOrphanedDeployments(ctx, cl, cr, keepNames, shouldRemove)
		_ = RemoveOrphanedSTSs(ctx, cl, cr, keepNames, shouldRemove)
		_ = RemoveOrphanedConfigMaps(ctx, cl, cr, keepNames, shouldRemove)
		_ = RemoveOrphanedPDBs(ctx, cl, cr, keepNames, shouldRemove)
		_ = RemoveOrphanedServices(ctx, cl, cr, keepNames, shouldRemove)
		_ = RemoveOrphanedHPAs(ctx, cl, cr, keepNames, shouldRemove)
		_ = RemoveOrphanedVPAs(ctx, cl, cr, keepNames, shouldRemove)
		_ = RemoveOrphanedNetworkPolicies(ctx, cl, cr, keepNames, shouldRemove)
		_ = RemoveOrphanedVMServiceScrapes(ctx, cl, cr, keepNames, shouldRemove)
		_ = RemoveOrphanedVMPodScrapes(ctx, cl, cr, keepNames, shouldRemove)
	})
}

func FuzzFinalizers(f *testing.F) {
	f.Add(`{}`, uint8(0))
	f.Add(`{"metadata":{"name":"cm","namespace":"ns","finalizers":["operator.victoriametrics.com/finalizer"]}}`, uint8(1))
	f.Add(`{"metadata":{"name":"cm","namespace":"ns","ownerReferences":[{"apiVersion":"v1","kind":"VMAgent","name":"n","uid":"u"}]}}`, uint8(2))
	f.Fuzz(func(t *testing.T, data string, mode uint8) {
		var input struct {
			Metadata metav1.ObjectMeta `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(data), &input); err != nil {
			fuzztest.New([]byte(data)).Fill(&input)
		}
		input.Metadata.ResourceVersion = ""
		input.Metadata.DeletionTimestamp = nil
		input.Metadata.ManagedFields = nil
		cm := &corev1.ConfigMap{ObjectMeta: input.Metadata}
		cr := &vmv1beta1.VMAgent{ObjectMeta: metav1.ObjectMeta{Name: fuzzName, Namespace: fuzzNamespace}}
		cl := k8stools.GetTestClientWithObjects([]runtime.Object{cr, cm.DeepCopy()})
		ctx := t.Context()
		objs := []client.Object{cm}

		// errors are expected for malformed objects, only panics matter
		switch mode % 6 {
		case 0:
			_ = AddFinalizer(ctx, cl, cm)
		case 1:
			_ = RemoveFinalizer(ctx, cl, cm)
		case 2:
			_ = SafeDelete(ctx, cl, cm)
		case 3:
			_ = SafeDeleteWithFinalizer(ctx, cl, objs, cr)
		case 4:
			_ = SafeDeleteCrossNamespaceWithFinalizer(ctx, cl, objs, cr)
		case 5:
			_ = removeFinalizers(ctx, cl, objs, []bool{true}, cr)
		}
	})
}
