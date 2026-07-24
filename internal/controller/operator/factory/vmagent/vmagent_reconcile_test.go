package vmagent

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	vpav1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/reconcile"
)

func Test_CreateOrUpdate_Actions(t *testing.T) {
	type args struct {
		cr     *vmv1beta1.VMAgent
		preRun func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAgent)
	}
	type want struct {
		actions []k8stools.ClientAction
		err     error
	}

	vmagentName := types.NamespacedName{Namespace: "default", Name: "vmagent-vmagent"}
	clusterRoleName := types.NamespacedName{Name: "monitoring:default:vmagent-vmagent"}
	roleName := types.NamespacedName{Namespace: "default", Name: "monitoring:default:vmagent-vmagent"}
	tlsAssetsName := types.NamespacedName{Namespace: "default", Name: "tls-assets-vmagent-vmagent"}

	defaultCR := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmagent",
			Namespace: "default",
			UID:       "123",
		},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{URL: "http://remote-write"},
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
			},
		},
		Status: vmv1beta1.VMAgentStatus{
			LastAppliedSpec: &vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To(int32(1)),
				},
			},
		},
	}

	crWithoutStatus := defaultCR.DeepCopy()
	crWithoutStatus.Status = vmv1beta1.VMAgentStatus{}

	setupReadyVMAgent := func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAgent) {
		// Create objects first
		assert.NoError(t, CreateOrUpdate(ctx, cr, c))
		// clear actions
		c.Actions = nil
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

	// create vmagent with default config (Deployment mode)
	f(args{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Get", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Create", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Get", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Create", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Get", Kind: "Role", Resource: roleName},
				{Verb: "Create", Kind: "Role", Resource: roleName},
				{Verb: "Get", Kind: "RoleBinding", Resource: roleName},
				{Verb: "Create", Kind: "RoleBinding", Resource: roleName},
				{Verb: "Get", Kind: "Service", Resource: vmagentName},
				{Verb: "Create", Kind: "Service", Resource: vmagentName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmagentName},
				{Verb: "Create", Kind: "VMServiceScrape", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: vmagentName},
				{Verb: "Create", Kind: "Secret", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Create", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
				{Verb: "Create", Kind: "Deployment", Resource: vmagentName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
			},
		})

	// update vmagent (Deployment mode)
	f(args{
		cr:     crWithoutStatus,
		preRun: setupReadyVMAgent,
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Get", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Get", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Get", Kind: "Role", Resource: roleName},
				{Verb: "Get", Kind: "RoleBinding", Resource: roleName},
				{Verb: "Get", Kind: "Service", Resource: vmagentName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
			},
		})

	// no update on status change
	f(args{
		cr: defaultCR.DeepCopy(),
		preRun: func(ctx context.Context, c *k8stools.ClientWithActions, cr *vmv1beta1.VMAgent) {
			setupReadyVMAgent(ctx, c, cr)
			// Change status
			cr.Status.Replicas = 1
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "DaemonSet", Resource: vmagentName},
				{Verb: "Get", Kind: "HorizontalPodAutoscaler", Resource: vmagentName},
				{Verb: "Get", Kind: "NetworkPolicy", Resource: vmagentName},
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Get", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Get", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Get", Kind: "Role", Resource: roleName},
				{Verb: "Get", Kind: "RoleBinding", Resource: roleName},
				{Verb: "Get", Kind: "Service", Resource: vmagentName},
				{Verb: "Get", Kind: "VMServiceScrape", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
				{Verb: "Get", Kind: "Deployment", Resource: vmagentName},
			},
		})

	// daemonset mode
	f(args{
		cr: &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: vmv1beta1.VMAgentSpec{
				DaemonSetMode: true,
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://remote-write"},
				},
			},
		},
	},
		want{
			actions: []k8stools.ClientAction{
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Create", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Get", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Create", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Get", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Create", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
				{Verb: "Get", Kind: "Role", Resource: roleName},
				{Verb: "Create", Kind: "Role", Resource: roleName},
				{Verb: "Get", Kind: "RoleBinding", Resource: roleName},
				{Verb: "Create", Kind: "RoleBinding", Resource: roleName},
				{Verb: "Get", Kind: "Service", Resource: vmagentName},
				{Verb: "Create", Kind: "Service", Resource: vmagentName},
				{Verb: "Get", Kind: "VMPodScrape", Resource: vmagentName},
				{Verb: "Create", Kind: "VMPodScrape", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: vmagentName},
				{Verb: "Create", Kind: "Secret", Resource: vmagentName},
				{Verb: "Get", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Create", Kind: "Secret", Resource: tlsAssetsName},
				{Verb: "Get", Kind: "DaemonSet", Resource: vmagentName},
				{Verb: "Create", Kind: "DaemonSet", Resource: vmagentName},
				{Verb: "Get", Kind: "DaemonSet", Resource: vmagentName},
			},
		})
}

func TestCreateOrUpdate_StatefulSetWithHPA(t *testing.T) {
	cr := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			StatefulMode:                  true,
			StatefulRollingUpdateStrategy: appsv1.RollingUpdateStatefulSetStrategyType,
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{URL: "http://remote-write"},
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
			},
			HPA: &vmv1beta1.EmbeddedHPA{
				MinReplicas: ptr.To(int32(1)),
				MaxReplicas: 5,
			},
		},
	}
	stsNSN := types.NamespacedName{Namespace: cr.Namespace, Name: cr.PrefixedName()}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
	ctx := context.TODO()
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	synctest.Test(t, func(t *testing.T) {
		// initial creation: StatefulSet created with 1 replica
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		var sts appsv1.StatefulSet
		assert.NoError(t, fclient.Get(ctx, stsNSN, &sts))
		assert.Equal(t, int32(1), *sts.Spec.Replicas)

		// HPA now targets the VMAgent CR scale subresource (spec.shardCount), not the StatefulSet
		// directly. The operator must always reconcile pod replicas from spec.replicaCount.
		cr.Spec.ReplicaCount = ptr.To(int32(2))
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

		assert.NoError(t, fclient.Get(ctx, stsNSN, &sts))
		assert.Equal(t, int32(2), *sts.Spec.Replicas)
	})
}

func TestCreateOrUpdateVPA_TargetsCR(t *testing.T) {
	cfg := config.MustGetBaseConfig()
	defaultCfg := *cfg
	cfg.VPAAPIEnabled = true
	defer func() { *cfg = defaultCfg }()

	f := func(cr *vmv1beta1.VMAgent) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)

		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))

			var vpa vpav1.VerticalPodAutoscaler
			assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}, &vpa))
			assert.Equal(t, &autoscalingv1.CrossVersionObjectReference{
				Name:       cr.Name,
				Kind:       "VMAgent",
				APIVersion: "operator.victoriametrics.com/v1beta1",
			}, vpa.Spec.TargetRef)
		})
	}

	// non-sharded
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "vpa-vmagent", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote-write"}},
			VPA:         &vmv1beta1.EmbeddedVPA{},
		},
	})

	// sharded
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "vpa-vmagent-sharded", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote-write"}},
			ShardCount:  ptr.To(int32(3)),
			VPA:         &vmv1beta1.EmbeddedVPA{},
		},
	})
}

func TestCreateOrUpdateVPA_RemovedOnDisable(t *testing.T) {
	cfg := config.MustGetBaseConfig()
	defaultCfg := *cfg
	cfg.VPAAPIEnabled = true
	defer func() { *cfg = defaultCfg }()

	cr := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "vpa-disable-vmagent", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote-write"}},
			VPA:         &vmv1beta1.EmbeddedVPA{},
		},
	}
	fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
	ctx := context.TODO()
	build.AddDefaults(fclient.Scheme())
	fclient.Scheme().Default(cr)

	synctest.Test(t, func(t *testing.T) {
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
		vpaNSN := types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}
		var vpa vpav1.VerticalPodAutoscaler
		assert.NoError(t, fclient.Get(ctx, vpaNSN, &vpa))

		// disabling VPA must delete the VPA created under cr.Name, not one under cr.PrefixedName()
		cr.Spec.VPA = nil
		assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
		err := fclient.Get(ctx, vpaNSN, &vpa)
		assert.Error(t, err)
		assert.True(t, k8serrors.IsNotFound(err))
	})
}

func TestCreateOrUpdate_StatusShards(t *testing.T) {
	f := func(cr *vmv1beta1.VMAgent, wantShards int32) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{cr})
		ctx := context.TODO()
		build.AddDefaults(fclient.Scheme())
		fclient.Scheme().Default(cr)

		synctest.Test(t, func(t *testing.T) {
			assert.NoError(t, CreateOrUpdate(ctx, cr, fclient))
			assert.NoError(t, reconcile.UpdateObjectStatus(ctx, fclient, cr, vmv1beta1.UpdateStatusOperational, nil))

			var updated vmv1beta1.VMAgent
			assert.NoError(t, fclient.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: cr.Name}, &updated))
			assert.Equal(t, wantShards, updated.Status.Shards)
		})
	}

	// non-sharded: status.shards must be 1 so VPA/HPA scale subresource is non-zero
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "no-shards", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote-write"}},
		},
	}, 1)

	// explicit shardCount=3
	f(&vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{Name: "three-shards", Namespace: "default"},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{{URL: "http://remote-write"}},
			ShardCount:  ptr.To(int32(3)),
		},
	}, 3)
}

func TestCreateOrUpdate_Paused(t *testing.T) {
	cr := &vmv1beta1.VMAgent{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "example-vmagent",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{URL: "http://remote-write"},
			},
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
