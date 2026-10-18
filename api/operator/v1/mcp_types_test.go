package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestVMMCP_Validate(t *testing.T) {
	f := func(spec VMMCPSpec, wantErr string) {
		t.Helper()
		cr := &VMMCP{
			ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "default"},
			Spec:       spec,
		}
		err := cr.Validate()
		if wantErr == "" {
			assert.NoError(t, err)
		} else {
			assert.ErrorContains(t, err, wantErr)
		}
	}
	secret := &corev1.SecretKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "vm-auth"},
		Key:                  "token",
	}

	// entrypoint
	f(VMMCPSpec{Entrypoint: "http://vmsingle-example:8428"}, "")

	// cloud API key
	f(VMMCPSpec{CloudAPIKey: secret}, "")

	// neither entrypoint nor cloud API key
	f(VMMCPSpec{}, "either spec.entrypoint or spec.cloudAPIKey must be set")

	// both entrypoint and cloud API key
	f(VMMCPSpec{
		Entrypoint:  "http://vmsingle-example:8428",
		CloudAPIKey: secret,
	}, "cannot be set at the same time")

	// all supported params
	f(VMMCPSpec{
		Entrypoint:         "http://vmselect-example:8481",
		InstanceType:       VMMCPInstanceTypeCluster,
		BearerToken:        secret,
		Headers:            []string{"X-Scope=team-a", "X-Env=prod"},
		DefaultTenantID:    "1:2",
		PassthroughHeaders: []string{"X-Token"},
		DisableResources:   true,
		CommonMCPSpec: CommonMCPSpec{
			ServerMode:        MCPServerModeSSE,
			DisabledTools:     []string{"export", "flags"},
			HeartbeatInterval: "1m30s",
			LogLevel:          "debug",
			LogFormat:         "json",
		},
	}, "")

	// header without value
	f(VMMCPSpec{
		Entrypoint: "http://vmsingle-example:8428",
		Headers:    []string{"X-Scope"},
	}, "must be in key=value format")

	// header with comma, which is used as headers separator
	f(VMMCPSpec{
		Entrypoint: "http://vmsingle-example:8428",
		Headers:    []string{"X-Scope=a,b"},
	}, "cannot contain commas")

	// empty passthrough header
	f(VMMCPSpec{
		Entrypoint:         "http://vmsingle-example:8428",
		PassthroughHeaders: []string{" "},
	}, "spec.passthroughHeaders[0] cannot be empty")

	// disabled tool with comma, which is used as tools separator
	f(VMMCPSpec{
		Entrypoint: "http://vmsingle-example:8428",
		CommonMCPSpec: CommonMCPSpec{
			DisabledTools: []string{"export,flags"},
		},
	}, "cannot contain commas")

	// incorrect heartbeat interval
	f(VMMCPSpec{
		Entrypoint: "http://vmsingle-example:8428",
		CommonMCPSpec: CommonMCPSpec{
			HeartbeatInterval: "10sec",
		},
	}, "cannot parse spec.heartbeatInterval")

	// extra args aren't supported by the MCP server
	f(VMMCPSpec{
		Entrypoint: "http://vmsingle-example:8428",
		CommonMCPSpec: CommonMCPSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				ExtraArgs: map[string]string{"loggerLevel": "INFO"},
			},
		},
	}, "spec.extraArgs is not supported")

	// additional service name is equal to prefixed name
	f(VMMCPSpec{
		Entrypoint: "http://vmsingle-example:8428",
		CommonMCPSpec: CommonMCPSpec{
			ServiceSpec: &vmv1beta1.AdditionalServiceSpec{
				EmbeddedObjectMetadata: vmv1beta1.EmbeddedObjectMetadata{Name: "vmmcp-test"},
			},
		},
	}, "spec.serviceSpec.Name cannot be equal to prefixed name")

	// preStop sleep exceeds termination grace period
	f(VMMCPSpec{
		Entrypoint: "http://vmsingle-example:8428",
		CommonMCPSpec: CommonMCPSpec{
			CommonAppsParams: vmv1beta1.CommonAppsParams{
				PreStopSleepSeconds:           ptr.To[int32](30),
				TerminationGracePeriodSeconds: ptr.To[int64](10),
			},
		},
	}, "preStopSleepSeconds (30) must be less than terminationGracePeriodSeconds (10)")
}

func TestMCP_Naming(t *testing.T) {
	meta := metav1.ObjectMeta{Name: "example", Namespace: "monitoring"}
	vm := &VMMCP{ObjectMeta: meta}
	vm.Spec.Port = "8080"

	assert.Equal(t, "vmmcp-example", vm.PrefixedName())
	assert.Equal(t, "vmmcp", vm.SelectorLabels()["app.kubernetes.io/name"])
	assert.Equal(t, "http://vmmcp-example.monitoring.svc:8080", vm.AsURL(false))

	// URL of the object without defaults, which is read from Kubernetes API, uses the default port
	assert.Equal(t, "http://vmmcp-example.monitoring.svc:8080", (&VMMCP{ObjectMeta: meta}).AsURL(false))
	custom := &VMMCP{ObjectMeta: meta}
	custom.Spec.Port = "8090"
	assert.Equal(t, "http://vmmcp-example.monitoring.svc:8090", custom.AsURL(false))
}
