package vmsingle

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestDeleteOrphaned_UsesReconcilerConfig(t *testing.T) {
	ctx := context.Background()
	cr := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "vmsingle", Namespace: "single-ns"},
		Spec:       vmv1beta1.VMSingleSpec{ServiceAccountName: "external"},
	}
	rbacName := cr.GetRBACName()
	owner := cr.AsOwner()
	foreignNamespace := "other-reconciler-ns"
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{
		cr,
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: cr.Namespace, OwnerReferences: []metav1.OwnerReference{owner}}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: cr.Namespace, OwnerReferences: []metav1.OwnerReference{owner}}},
		&rbacv1.Role{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: foreignNamespace, OwnerReferences: []metav1.OwnerReference{owner}}},
		&rbacv1.RoleBinding{ObjectMeta: metav1.ObjectMeta{Name: rbacName, Namespace: foreignNamespace, OwnerReferences: []metav1.OwnerReference{owner}}},
		&vpav1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: cr.PrefixedName(), Namespace: cr.Namespace, OwnerReferences: []metav1.OwnerReference{owner}}},
	})

	assert.NoError(t, deleteOrphaned(ctx, fclient, cr, &config.BaseOperatorConf{WatchNamespaces: []string{cr.Namespace}, VPAAPIEnabled: true}))
	for _, obj := range []client.Object{&rbacv1.Role{}, &rbacv1.RoleBinding{}} {
		err := fclient.Get(ctx, types.NamespacedName{Name: rbacName, Namespace: cr.Namespace}, obj)
		assert.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))
		assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Name: rbacName, Namespace: foreignNamespace}, obj))
	}
	assert.True(t, k8serrors.IsNotFound(fclient.Get(ctx, types.NamespacedName{Name: cr.PrefixedName(), Namespace: cr.Namespace}, &vpav1.VerticalPodAutoscaler{})))
}

// In namespaced mode a Role and RoleBinding are created at every watched namespace,
// so that service discovery works there, see createK8sAPIAccess.
// Setting spec.serviceAccountName makes all of them unnecessary, but the ones outside
// cr.Namespace cannot reference cr as their owner, so deleteOrphaned has to delete
// them instead of relying on garbage collection.
func TestDeleteOrphaned_RemovesCrossNamespaceRBAC(t *testing.T) {
	ctx := context.Background()
	cr := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "vmsingle", Namespace: "single-ns"},
		// an externally managed ServiceAccount, operator owned RBAC is no longer needed
		Spec: vmv1beta1.VMSingleSpec{ServiceAccountName: "external"},
	}
	watchedNamespace := "watched-ns"
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{
		cr,
		buildRole(cr, cr.Namespace), buildRB(cr, cr.Namespace),
		buildRole(cr, watchedNamespace), buildRB(cr, watchedNamespace),
	})

	cfg := &config.BaseOperatorConf{WatchNamespaces: []string{cr.Namespace, watchedNamespace}}
	assert.NoError(t, deleteOrphaned(ctx, fclient, cr, cfg))
	for _, ns := range cfg.WatchNamespaces {
		for _, obj := range []client.Object{&rbacv1.Role{}, &rbacv1.RoleBinding{}} {
			err := fclient.Get(ctx, types.NamespacedName{Name: cr.GetRBACName(), Namespace: ns}, obj)
			assert.True(t, k8serrors.IsNotFound(err), "%T at %s must be removed, got %v", obj, ns, err)
		}
	}
}

// In cluster-wide mode a Role and RoleBinding are still created at cr.Namespace,
// to keep the secrets access namespace-scoped, see createK8sAPIAccess.
func TestDeleteOrphaned_RemovesClusterWideRBAC(t *testing.T) {
	ctx := context.Background()
	cr := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{Name: "vmsingle", Namespace: "single-ns"},
		Spec:       vmv1beta1.VMSingleSpec{ServiceAccountName: "external"},
	}
	rbacName := cr.GetRBACName()
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{
		cr,
		buildRole(cr, cr.Namespace), buildRB(cr, cr.Namespace),
		buildCR(cr), buildCRB(cr),
	})

	assert.NoError(t, deleteOrphaned(ctx, fclient, cr, &config.BaseOperatorConf{}))
	for _, obj := range []client.Object{&rbacv1.Role{}, &rbacv1.RoleBinding{}} {
		err := fclient.Get(ctx, types.NamespacedName{Name: rbacName, Namespace: cr.Namespace}, obj)
		assert.True(t, k8serrors.IsNotFound(err), "%T at %s must be removed, got %v", obj, cr.Namespace, err)
	}
	for _, obj := range []client.Object{&rbacv1.ClusterRole{}, &rbacv1.ClusterRoleBinding{}} {
		err := fclient.Get(ctx, types.NamespacedName{Name: rbacName}, obj)
		assert.True(t, k8serrors.IsNotFound(err), "%T must be removed, got %v", obj, err)
	}
}

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr     *vmv1beta1.VMSingle
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMSingle)
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	f := func(args args, want want) {
		t.Helper()

		fclient := k8stools.GetTestClientWithActionsAndObjects(nil)
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(args.cr)

		if args.preRun != nil {
			args.preRun(ctx, fclient, args.cr)
		}

		synctest.Test(t, func(t *testing.T) {
			err := CreateOrUpdate(ctx, args.cr, fclient)
			if want.err != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if !assert.Equal(t, len(want.actions), len(fclient.Actions)) {
				for i, action := range fclient.Actions {
					t.Logf("Action %d: %s %s %s", i, action.Verb, action.Kind, action.Resource)
				}
			}

			for i, action := range want.actions {
				if i >= len(fclient.Actions) {
					break
				}
				assert.Equal(t, action.Verb, fclient.Actions[i].Verb, "idx %d verb", i)
				assert.Equal(t, action.Kind, fclient.Actions[i].Kind, "idx %d kind", i)
				assert.Equal(t, action.Resource, fclient.Actions[i].Resource, "idx %d resource", i)
			}
		})
	}

	name := "example-single"
	namespace := "default"
	vmsingleName := types.NamespacedName{Namespace: namespace, Name: "vmsingle-" + name}
	objectMeta := metav1.ObjectMeta{Name: name, Namespace: namespace}

	setupReadyVMSingle := func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMSingle) {
		// Create objects first
		assert.NoError(t, CreateOrUpdate(ctx, cr.DeepCopy(), c))

		// clear actions
		c.Actions = nil
	}

	// create vmsingle with default config
	f(args{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: objectMeta,
			Spec:       vmv1beta1.VMSingleSpec{},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Get", Kind: "Service", Resource: vmsingleName},
				{Verb: "Create", Kind: "Service", Resource: vmsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmsingleName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmsingleName},
				// Deployment
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Create", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
			},
		})

	// update vmsingle with no changes
	f(args{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: setupReadyVMSingle,
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Get", Kind: "Service", Resource: vmsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmsingleName},
				// Deployment
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
			},
		})

	// no update on status change
	f(args{
		cr: &vmv1beta1.VMSingle{
			ObjectMeta: objectMeta,
			Spec: vmv1beta1.VMSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMSingle) {
			setupReadyVMSingle(ctx, c, cr)

			// Update status to simulate consistency
			cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "NetworkPolicy", Resource: vmsingleName},
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmsingleName},
				{Verb: "Get", Kind: "Service", Resource: vmsingleName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
				{Verb: "Get", Kind: "Deployment", Resource: vmsingleName},
			},
		})
}

func TestCreateOrUpdate_Paused(t *testing.T) {
	// Create a paused VMSingle CR and test that it is not reconciled
	cr := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-single",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMSingleSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
				Paused:       true,
			},
		},
	}
	nsn := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
	ctx := context.TODO()
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	synctest.Test(t, func(t *testing.T) {
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		var dep appsv1.Deployment
		err := fclient.Get(ctx, nsn, &dep)
		assert.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))

		// unpause and verify reconciliation
		cr.Spec.Paused = false
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
		err = fclient.Get(ctx, nsn, &dep)
		assert.NoError(t, err)

		// pause and update replica count
		cr.Spec.Paused = true
		cr.Spec.ReplicaCount = ptr.To(int32(2))
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		// check that replicas count is not updated
		err = fclient.Get(ctx, nsn, &dep)
		assert.NoError(t, err)
		assert.Equal(t, int32(1), *dep.Spec.Replicas)
	})
}

func TestCreateOrUpdate_RemovesOrphanedFeatureConfigMaps(t *testing.T) {
	ctx := context.TODO()
	cr := &vmv1beta1.VMSingle{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-single",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMSingleSpec{
			CommonScrapeParams: vmv1beta1.CommonScrapeParams{
				IngestOnlyMode: ptr.To(true),
			},
		},
	}
	cr.Status.LastAppliedSpec = &vmv1beta1.VMSingleSpec{
		CommonRelabelParams: vmv1beta1.CommonRelabelParams{
			InlineRelabelConfig: []*vmv1beta1.RelabelConfig{{
				Action:       "drop",
				SourceLabels: []string{"instance"},
			}},
		},
		StreamAggrConfig: &vmv1beta1.StreamAggrConfig{
			Rules: []vmv1beta1.StreamAggrRule{{
				Match:    vmv1beta1.StringOrArray{"foo"},
				Interval: "1m",
				Outputs:  []string{"count_samples"},
			}},
		},
	}

	relabelName := types.NamespacedName{
		Name:      build.ResourceName(build.RelabelConfigResourceKind, cr),
		Namespace: cr.Namespace,
	}
	streamAggrName := types.NamespacedName{
		Name:      build.ResourceName(build.StreamAggrConfigResourceKind, cr),
		Namespace: cr.Namespace,
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: build.ResourceMeta(build.RelabelConfigResourceKind, cr),
		},
		&corev1.ConfigMap{
			ObjectMeta: build.ResourceMeta(build.StreamAggrConfigResourceKind, cr),
		},
	})
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	synctest.Test(t, func(t *testing.T) {
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		var relabelCM corev1.ConfigMap
		err := fclient.Get(ctx, relabelName, &relabelCM)
		assert.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))

		var streamAggrCM corev1.ConfigMap
		err = fclient.Get(ctx, streamAggrName, &streamAggrCM)
		assert.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))
	})
}
