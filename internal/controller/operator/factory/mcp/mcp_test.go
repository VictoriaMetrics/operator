package mcp

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/build"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func secretKey(name, key string) *corev1.SecretKeySelector {
	return &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: name},
		Key:                  key,
	}
}

// reconcileMCP applies defaults to the given MCP server CR and reconciles it with fake client
func reconcileMCP(t *testing.T, rclient client.Client, cr client.Object) error {
	t.Helper()
	rclient.Scheme().Default(cr)
	ctx := context.TODO()
	var err error
	synctest.Test(t, func(t *testing.T) {
		switch cr := cr.(type) {
		case *vmv1.VMMCP:
			err = CreateOrUpdateVMMCP(ctx, rclient, cr)
		default:
			t.Fatalf("unexpected object type %T", cr)
		}
	})
	return err
}

func newTestClient() client.Client {
	fclient := k8stools.GetTestClientWithObjects(nil)
	build.AddDefaults(fclient.Scheme())
	return fclient
}

func getDeployment(t *testing.T, rclient client.Client, name string) *appsv1.Deployment {
	t.Helper()
	var dep appsv1.Deployment
	require.NoError(t, rclient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: name}, &dep))
	return &dep
}

func TestCreateOrUpdateVMMCP(t *testing.T) {
	rclient := newTestClient()
	cr := &vmv1.VMMCP{
		ObjectMeta: metav1.ObjectMeta{Name: "base", Namespace: "default"},
		Spec: vmv1.VMMCPSpec{
			Entrypoint: "http://vmsingle-example.default.svc:8428",
		},
	}
	require.NoError(t, reconcileMCP(t, rclient, cr))
	ctx := context.TODO()
	nsn := types.NamespacedName{Namespace: "default", Name: "vmmcp-base"}

	dep := getDeployment(t, rclient, nsn.Name)
	assert.Equal(t, map[string]string{
		"app.kubernetes.io/name":      "vmmcp",
		"app.kubernetes.io/instance":  "base",
		"app.kubernetes.io/component": "monitoring",
		"managed-by":                  "vm-operator",
	}, dep.Spec.Selector.MatchLabels)
	assert.Equal(t, appsv1.RollingUpdateDeploymentStrategyType, dep.Spec.Strategy.Type)
	assert.Equal(t, "vmmcp-base", dep.Spec.Template.Spec.ServiceAccountName)
	require.Len(t, dep.Spec.Template.Spec.Containers, 1)
	c := dep.Spec.Template.Spec.Containers[0]
	assert.Equal(t, "vmmcp", c.Name)
	assert.Equal(t, "ghcr.io/victoriametrics/mcp-victoriametrics:"+config.MustGetBaseConfig().VMMCP.Version, c.Image)
	assert.Empty(t, c.Args)
	assert.Equal(t, []corev1.ContainerPort{{Name: "http", Protocol: corev1.ProtocolTCP, ContainerPort: 8080}}, c.Ports)
	assert.Equal(t, []corev1.EnvVar{
		{Name: "MCP_LISTEN_ADDR", Value: ":8080"},
		{Name: "MCP_SERVER_MODE", Value: "http"},
		{Name: "VM_INSTANCE_ENTRYPOINT", Value: "http://vmsingle-example.default.svc:8428"},
		{Name: "VM_INSTANCE_TYPE", Value: "single"},
	}, c.Env)

	// probes
	require.NotNil(t, c.ReadinessProbe)
	assert.Equal(t, "/health/readiness", c.ReadinessProbe.HTTPGet.Path)
	assert.Equal(t, intstr.Parse("8080"), c.ReadinessProbe.HTTPGet.Port)
	require.NotNil(t, c.LivenessProbe)
	assert.Equal(t, "/health/liveness", c.LivenessProbe.HTTPGet.Path)
	assert.Equal(t, intstr.Parse("8080"), c.LivenessProbe.HTTPGet.Port)
	require.NotNil(t, c.StartupProbe)
	require.NotNil(t, c.StartupProbe.TCPSocket)
	assert.Equal(t, intstr.Parse("8080"), c.StartupProbe.TCPSocket.Port)
	assert.Equal(t, int32(60), c.StartupProbe.FailureThreshold)

	// default resources account for the documentation index
	memLimit, memRequest := c.Resources.Limits[corev1.ResourceMemory], c.Resources.Requests[corev1.ResourceMemory]
	assert.True(t, resource.MustParse("1024Mi").Equal(memLimit), "memory limit: %s", memLimit.String())
	assert.True(t, resource.MustParse("768Mi").Equal(memRequest), "memory request: %s", memRequest.String())

	var svc corev1.Service
	require.NoError(t, rclient.Get(ctx, nsn, &svc))
	require.Len(t, svc.Spec.Ports, 1)
	assert.Equal(t, int32(8080), svc.Spec.Ports[0].Port)
	assert.Equal(t, "http", svc.Spec.Ports[0].Name)

	var sa corev1.ServiceAccount
	require.NoError(t, rclient.Get(ctx, nsn, &sa))

	var vmss vmv1beta1.VMServiceScrape
	require.NoError(t, rclient.Get(ctx, nsn, &vmss))
	require.Len(t, vmss.Spec.Endpoints, 1)
	assert.Equal(t, "/metrics", vmss.Spec.Endpoints[0].Path)
	assert.Equal(t, "http", vmss.Spec.Endpoints[0].Port)

	var pdb policyv1.PodDisruptionBudget
	assert.True(t, k8serrors.IsNotFound(rclient.Get(ctx, nsn, &pdb)), "pdb must not be created by default")
}

func TestCreateOrUpdateVMMCP_Envs(t *testing.T) {
	f := func(spec vmv1.VMMCPSpec, want []corev1.EnvVar) {
		t.Helper()
		rclient := newTestClient()
		cr := &vmv1.VMMCP{
			ObjectMeta: metav1.ObjectMeta{Name: "envs", Namespace: "default"},
			Spec:       spec,
		}
		require.NoError(t, reconcileMCP(t, rclient, cr))
		dep := getDeployment(t, rclient, "vmmcp-envs")
		assert.Equal(t, want, dep.Spec.Template.Spec.Containers[0].Env)
	}

	// all MCP server params
	f(vmv1.VMMCPSpec{
		Entrypoint:         "http://vmselect-example.default.svc:8481",
		InstanceType:       vmv1.VMMCPInstanceTypeCluster,
		BearerToken:        secretKey("vm-auth", "token"),
		Headers:            []string{"X-Scope=team-a", "X-Env=prod"},
		DefaultTenantID:    "1:2",
		PassthroughHeaders: []string{"X-Token", "X-Access-Key"},
		DisableResources:   true,
		CommonMCPSpec: vmv1.CommonMCPSpec{
			ServerMode:        vmv1.MCPServerModeSSE,
			DisabledTools:     []string{"export", "flags"},
			HeartbeatInterval: "1m",
			LogLevel:          "debug",
			LogFormat:         "json",
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				Port: "8090",
			},
		},
	}, []corev1.EnvVar{
		{Name: "MCP_DISABLED_TOOLS", Value: "export,flags"},
		{Name: "MCP_DISABLE_RESOURCES", Value: "true"},
		{Name: "MCP_HEARTBEAT_INTERVAL", Value: "1m"},
		{Name: "MCP_LISTEN_ADDR", Value: ":8090"},
		{Name: "MCP_LOG_FORMAT", Value: "json"},
		{Name: "MCP_LOG_LEVEL", Value: "debug"},
		{Name: "MCP_PASSTHROUGH_HEADERS", Value: "X-Token,X-Access-Key"},
		{Name: "MCP_SERVER_MODE", Value: "sse"},
		{Name: "VM_DEFAULT_TENANT_ID", Value: "1:2"},
		{Name: "VM_INSTANCE_BEARER_TOKEN", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: secretKey("vm-auth", "token")}},
		{Name: "VM_INSTANCE_ENTRYPOINT", Value: "http://vmselect-example.default.svc:8481"},
		{Name: "VM_INSTANCE_HEADERS", Value: "X-Scope=team-a,X-Env=prod"},
		{Name: "VM_INSTANCE_TYPE", Value: "cluster"},
	})

	// VictoriaMetrics Cloud
	f(vmv1.VMMCPSpec{
		CloudAPIKey: secretKey("vmc", "api-key"),
	}, []corev1.EnvVar{
		{Name: "MCP_LISTEN_ADDR", Value: ":8080"},
		{Name: "MCP_SERVER_MODE", Value: "http"},
		{Name: "VMC_API_KEY", ValueFrom: &corev1.EnvVarSource{SecretKeyRef: secretKey("vmc", "api-key")}},
	})

	// extra envs override env vars set by operator and keep their order
	f(vmv1.VMMCPSpec{
		Entrypoint: "http://vmsingle-example.default.svc:8428",
		CommonMCPSpec: vmv1.CommonMCPSpec{
			LogLevel: "info",
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ExtraEnvs: []corev1.EnvVar{
					{Name: "MCP_LOG_LEVEL", Value: "debug"},
					{Name: "MCP_DISABLED_TOOLS", Value: ""},
				},
			},
		},
	}, []corev1.EnvVar{
		{Name: "MCP_LISTEN_ADDR", Value: ":8080"},
		{Name: "MCP_SERVER_MODE", Value: "http"},
		{Name: "VM_INSTANCE_ENTRYPOINT", Value: "http://vmsingle-example.default.svc:8428"},
		{Name: "VM_INSTANCE_TYPE", Value: "single"},
		{Name: "MCP_LOG_LEVEL", Value: "debug"},
		{Name: "MCP_DISABLED_TOOLS", Value: ""},
	})
}

func TestCreateOrUpdate_Deployment(t *testing.T) {
	rclient := newTestClient()
	cr := &vmv1.VMMCP{
		ObjectMeta: metav1.ObjectMeta{Name: "deploy", Namespace: "default"},
		Spec: vmv1.VMMCPSpec{
			Entrypoint: "http://vmsingle-example.default.svc:8428",
			CommonMCPSpec: vmv1.CommonMCPSpec{
				UpdateStrategy: ptr.To(appsv1.RecreateDeploymentStrategyType),
				// must be ignored for Recreate strategy, since it's rejected by Kubernetes
				RollingUpdate: &appsv1.RollingUpdateDeployment{MaxSurge: ptr.To(intstr.FromInt32(2))},
				PodMetadata: &vmv1beta1.EmbeddedObjectMetadata{
					Labels:      map[string]string{"team": "a"},
					Annotations: map[string]string{"note": "mcp"},
				},
				ServiceAccountName: "custom-sa",
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](2),
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{{
						MaxSkew:           1,
						TopologyKey:       "kubernetes.io/hostname",
						WhenUnsatisfiable: corev1.ScheduleAnyway,
					}},
					Secrets:    []string{"tls"},
					ConfigMaps: []string{"extra"},
				},
			},
		},
	}
	require.NoError(t, reconcileMCP(t, rclient, cr))
	dep := getDeployment(t, rclient, "vmmcp-deploy")
	assert.Equal(t, appsv1.RecreateDeploymentStrategyType, dep.Spec.Strategy.Type)
	assert.Nil(t, dep.Spec.Strategy.RollingUpdate)
	assert.Equal(t, ptr.To[int32](2), dep.Spec.Replicas)
	pod := dep.Spec.Template
	assert.Equal(t, "a", pod.Labels["team"])
	assert.Equal(t, "mcp", pod.Annotations["note"])
	assert.Equal(t, "custom-sa", pod.Spec.ServiceAccountName)
	require.Len(t, pod.Spec.TopologySpreadConstraints, 1)
	assert.Equal(t, cr.SelectorLabels(), pod.Spec.TopologySpreadConstraints[0].LabelSelector.MatchLabels)
	assert.Equal(t, []corev1.VolumeMount{
		{Name: "secret-tls", ReadOnly: true, MountPath: "/etc/vm/secrets/tls"},
		{Name: "configmap-extra", ReadOnly: true, MountPath: "/etc/vm/configs/extra"},
	}, pod.Spec.Containers[0].VolumeMounts)

	// the service account isn't created for the explicitly set serviceAccountName
	var sa corev1.ServiceAccount
	err := rclient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "vmmcp-deploy"}, &sa)
	assert.True(t, k8serrors.IsNotFound(err))
}

func TestCreateOrUpdate_RemoveOptionalObjects(t *testing.T) {
	rclient := newTestClient()
	ctx := context.TODO()
	nsn := types.NamespacedName{Namespace: "default", Name: "vmmcp-optional"}
	cr := &vmv1.VMMCP{
		ObjectMeta: metav1.ObjectMeta{Name: "optional", Namespace: "default"},
		Spec: vmv1.VMMCPSpec{
			Entrypoint: "http://vmsingle-example.default.svc:8428",
			CommonMCPSpec: vmv1.CommonMCPSpec{
				PodDisruptionBudget: &vmv1beta1.EmbeddedPodDisruptionBudgetSpec{
					MinAvailable: ptr.To(intstr.FromInt32(1)),
				},
				ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
					Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort},
				},
				NetworkPolicy: &vmv1beta1.EmbeddedNetworkPolicy{},
			},
		},
	}
	require.NoError(t, reconcileMCP(t, rclient, cr))
	var pdb policyv1.PodDisruptionBudget
	require.NoError(t, rclient.Get(ctx, nsn, &pdb))
	assert.Equal(t, ptr.To(intstr.FromInt32(1)), pdb.Spec.MinAvailable)
	var svc corev1.Service
	require.NoError(t, rclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "vmmcp-optional-additional-service"}, &svc))
	assert.Equal(t, corev1.ServiceTypeNodePort, svc.Spec.Type)
	var np networkingv1.NetworkPolicy
	require.NoError(t, rclient.Get(ctx, nsn, &np))

	// remove optional objects from spec
	cr.Status.LastAppliedSpec = cr.Spec.DeepCopy()
	cr.Spec.PodDisruptionBudget = nil
	cr.Spec.ServiceSpec = nil
	cr.Spec.NetworkPolicy = nil
	require.NoError(t, reconcileMCP(t, rclient, cr))
	assert.True(t, k8serrors.IsNotFound(rclient.Get(ctx, nsn, &pdb)), "pdb must be removed")
	err := rclient.Get(ctx, types.NamespacedName{Namespace: "default", Name: "vmmcp-optional-additional-service"}, &svc)
	assert.True(t, k8serrors.IsNotFound(err), "additional service must be removed")
	assert.True(t, k8serrors.IsNotFound(rclient.Get(ctx, nsn, &np)), "network policy must be removed")
}

func TestCreateOrUpdate_Errors(t *testing.T) {
	// vpa requires VPA API to be enabled
	rclient := newTestClient()
	cr := &vmv1.VMMCP{
		ObjectMeta: metav1.ObjectMeta{Name: "vpa", Namespace: "default"},
		Spec: vmv1.VMMCPSpec{
			Entrypoint: "http://vmsingle-example.default.svc:8428",
			CommonMCPSpec: vmv1.CommonMCPSpec{
				VPA: &vmv1beta1.EmbeddedVPA{},
			},
		},
	}
	assert.ErrorContains(t, reconcileMCP(t, rclient, cr), "VM_VPA_API_ENABLED=true")

	// paused CR isn't reconciled
	rclient = newTestClient()
	paused := &vmv1.VMMCP{
		ObjectMeta: metav1.ObjectMeta{Name: "paused", Namespace: "default"},
		Spec: vmv1.VMMCPSpec{
			Entrypoint: "http://vmsingle-example.default.svc:8428",
			CommonMCPSpec: vmv1.CommonMCPSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{Paused: true},
			},
		},
	}
	require.NoError(t, reconcileMCP(t, rclient, paused))
	var dep appsv1.Deployment
	err := rclient.Get(context.TODO(), types.NamespacedName{Namespace: "default", Name: "vmmcp-paused"}, &dep)
	assert.True(t, k8serrors.IsNotFound(err))
}
