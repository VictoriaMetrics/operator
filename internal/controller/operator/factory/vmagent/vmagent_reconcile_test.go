package vmagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
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
		ParsedLastAppliedSpec: &vmv1beta1.VMAgentSpec{
			RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
				{URL: "http://remote-write"},
			},
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ReplicaCount: ptr.To(int32(1)),
			},
		},
	}

	crWithoutStatus := defaultCR.DeepCopy()
	crWithoutStatus.ParsedLastAppliedSpec = nil

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
				{Verb: "Get", Kind: "ServiceAccount", Resource: vmagentName},
				{Verb: "Get", Kind: "ClusterRole", Resource: clusterRoleName},
				{Verb: "Get", Kind: "ClusterRoleBinding", Resource: clusterRoleName},
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
