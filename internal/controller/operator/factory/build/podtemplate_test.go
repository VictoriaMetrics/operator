package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
)

func TestPodTemplateParams(t *testing.T) {
	type opts struct {
		params *vmv1beta1.CommonAppsParams
		want   corev1.PodSpec
	}

	f := func(o opts) {
		t.Helper()
		var got corev1.PodTemplateSpec
		PodTemplateAddCommonParams(&got, o.params)
		assert.Equal(t, o.want, got.Spec)
	}

	affinity := &corev1.Affinity{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					{MatchExpressions: []corev1.NodeSelectorRequirement{{Key: "zone", Operator: corev1.NodeSelectorOpIn, Values: []string{"us-east"}}}},
				},
			},
		},
	}
	tolerations := []corev1.Toleration{{Key: "dedicated", Value: "monitoring", Effect: corev1.TaintEffectNoSchedule}}

	// affinity and tolerations
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			Affinity:    affinity,
			Tolerations: tolerations,
		},
		want: corev1.PodSpec{
			Affinity:    affinity,
			Tolerations: tolerations,
		},
	})

	// HostAliases set
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			HostAliases: []corev1.HostAlias{
				{IP: "1.2.3.4", Hostnames: []string{"my.host"}},
			},
		},
		want: corev1.PodSpec{
			HostAliases: []corev1.HostAlias{
				{IP: "1.2.3.4", Hostnames: []string{"my.host"}},
			},
		},
	})

	// HostAliasesUnderScore takes precedence over HostAliases
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			HostAliases: []corev1.HostAlias{
				{IP: "1.2.3.4", Hostnames: []string{"old.host"}},
			},
			HostAliasesUnderScore: []corev1.HostAlias{
				{IP: "5.6.7.8", Hostnames: []string{"new.host"}},
			},
		},
		want: corev1.PodSpec{
			HostAliases: []corev1.HostAlias{
				{IP: "5.6.7.8", Hostnames: []string{"new.host"}},
			},
		},
	})

	// DisableAutomountServiceAccountToken=true
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			DisableAutomountServiceAccountToken: true,
		},
		want: corev1.PodSpec{
			AutomountServiceAccountToken: ptr.To(false),
		},
	})

	// DisableAutomountServiceAccountToken=false
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			DisableAutomountServiceAccountToken: false,
		},
		want: corev1.PodSpec{},
	})

	// scheduler, runtime class, priority class, node selector
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			SchedulerName:     "custom-scheduler",
			RuntimeClassName:  ptr.To("gvisor"),
			PriorityClassName: "high-priority",
			NodeSelector:      map[string]string{"disktype": "ssd"},
		},
		want: corev1.PodSpec{
			SchedulerName:     "custom-scheduler",
			RuntimeClassName:  ptr.To("gvisor"),
			PriorityClassName: "high-priority",
			NodeSelector:      map[string]string{"disktype": "ssd"},
		},
	})

	// DNS settings
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			DNSPolicy: corev1.DNSClusterFirstWithHostNet,
			DNSConfig: &corev1.PodDNSConfig{
				Nameservers: []string{"8.8.8.8"},
			},
		},
		want: corev1.PodSpec{
			DNSPolicy: corev1.DNSClusterFirstWithHostNet,
			DNSConfig: &corev1.PodDNSConfig{
				Nameservers: []string{"8.8.8.8"},
			},
		},
	})

	// image pull secrets and readiness gates
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry-secret"}},
			ReadinessGates:   []corev1.PodReadinessGate{{ConditionType: "example.com/ready"}},
		},
		want: corev1.PodSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry-secret"}},
			ReadinessGates:   []corev1.PodReadinessGate{{ConditionType: "example.com/ready"}},
		},
	})

	// topology spread constraints
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				{MaxSkew: 1, TopologyKey: "zone", WhenUnsatisfiable: corev1.DoNotSchedule},
			},
		},
		want: corev1.PodSpec{
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				{MaxSkew: 1, TopologyKey: "zone", WhenUnsatisfiable: corev1.DoNotSchedule},
			},
		},
	})

	// termination grace period
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			TerminationGracePeriodSeconds: ptr.To(int64(30)),
		},
		want: corev1.PodSpec{
			TerminationGracePeriodSeconds: ptr.To(int64(30)),
		},
	})

	// host network
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			HostNetwork: true,
		},
		want: corev1.PodSpec{
			HostNetwork: true,
		},
	})
}

func TestDeploymentAddCommonParams(t *testing.T) {
	type opts struct {
		params *vmv1beta1.CommonAppsParams
		want   appsv1.DeploymentSpec
	}

	f := func(o opts) {
		t.Helper()
		var got appsv1.Deployment
		DeploymentAddCommonParams(&got, o.params)
		assert.Equal(t, o.want, got.Spec)
	}

	// replica count, min ready seconds, revision history
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			ReplicaCount:              ptr.To(int32(3)),
			MinReadySeconds:           10,
			RevisionHistoryLimitCount: ptr.To(int32(5)),
		},
		want: appsv1.DeploymentSpec{
			Replicas:             ptr.To(int32(3)),
			MinReadySeconds:      10,
			RevisionHistoryLimit: ptr.To(int32(5)),
		},
	})

	// pod-level fields are propagated
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			NodeSelector: map[string]string{"env": "prod"},
			ReplicaCount: ptr.To(int32(1)),
		},
		want: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{"env": "prod"},
				},
			},
		},
	})
}

func TestStatefulSetAddCommonParams(t *testing.T) {
	type opts struct {
		params *vmv1beta1.CommonAppsParams
		want   appsv1.StatefulSetSpec
	}

	f := func(o opts) {
		t.Helper()
		var got appsv1.StatefulSet
		StatefulSetAddCommonParams(&got, o.params)
		assert.Equal(t, o.want, got.Spec)
	}

	// replica count, min ready seconds, revision history
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			ReplicaCount:              ptr.To(int32(2)),
			MinReadySeconds:           5,
			RevisionHistoryLimitCount: ptr.To(int32(3)),
		},
		want: appsv1.StatefulSetSpec{
			Replicas:             ptr.To(int32(2)),
			MinReadySeconds:      5,
			RevisionHistoryLimit: ptr.To(int32(3)),
		},
	})

	// pod-level fields are propagated
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			SchedulerName: "my-scheduler",
			ReplicaCount:  ptr.To(int32(1)),
		},
		want: appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SchedulerName: "my-scheduler",
				},
			},
		},
	})
}

func TestDaemonSetAddCommonParams(t *testing.T) {
	type opts struct {
		params *vmv1beta1.CommonAppsParams
		want   appsv1.DaemonSetSpec
	}

	f := func(o opts) {
		t.Helper()
		var got appsv1.DaemonSet
		DaemonSetAddCommonParams(&got, o.params)
		assert.Equal(t, o.want, got.Spec)
	}

	// min ready seconds and revision history (no Replicas field)
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			MinReadySeconds:           15,
			RevisionHistoryLimitCount: ptr.To(int32(2)),
		},
		want: appsv1.DaemonSetSpec{
			MinReadySeconds:      15,
			RevisionHistoryLimit: ptr.To(int32(2)),
		},
	})

	// pod-level fields propagated
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			NodeSelector: map[string]string{"role": "worker"},
		},
		want: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{"role": "worker"},
				},
			},
		},
	})

	// DaemonSet uses root-aware security context
	f(opts{
		params: &vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
		},
		want: appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: corev1.PodSpec{
					SecurityContext: addStrictSecuritySettingsWithRootToPod(&vmv1beta1.CommonAppsParams{
						UseStrictSecurity: ptr.To(true),
					}),
				},
			},
		},
	})
}
