package build

import (
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestPodTemplateParams(t *testing.T) {
	f := func(params *vmv1beta1.CommonAppsParams, want corev1.PodSpec) {
		t.Helper()
		var got corev1.PodTemplateSpec
		PodTemplateAddCommonParams(&got, params)
		assert.Equal(t, want, got.Spec)
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
	f(
		&vmv1beta1.CommonAppsParams{
			Affinity:    affinity,
			Tolerations: tolerations,
		},
		corev1.PodSpec{
			Affinity:    affinity,
			Tolerations: tolerations,
		},
	)

	// HostAliases set
	f(
		&vmv1beta1.CommonAppsParams{
			HostAliases: []corev1.HostAlias{
				{IP: "1.2.3.4", Hostnames: []string{"my.host"}},
			},
		},
		corev1.PodSpec{
			HostAliases: []corev1.HostAlias{
				{IP: "1.2.3.4", Hostnames: []string{"my.host"}},
			},
		},
	)

	// HostAliasesUnderScore takes precedence over HostAliases
	f(
		&vmv1beta1.CommonAppsParams{
			HostAliases: []corev1.HostAlias{
				{IP: "1.2.3.4", Hostnames: []string{"old.host"}},
			},
			HostAliasesUnderScore: []corev1.HostAlias{
				{IP: "5.6.7.8", Hostnames: []string{"new.host"}},
			},
		},
		corev1.PodSpec{
			HostAliases: []corev1.HostAlias{
				{IP: "5.6.7.8", Hostnames: []string{"new.host"}},
			},
		},
	)

	// DisableAutomountServiceAccountToken=true
	f(
		&vmv1beta1.CommonAppsParams{
			DisableAutomountServiceAccountToken: true,
		},
		corev1.PodSpec{
			AutomountServiceAccountToken: ptr.To(false),
		},
	)

	// DisableAutomountServiceAccountToken=false
	f(
		&vmv1beta1.CommonAppsParams{
			DisableAutomountServiceAccountToken: false,
		},
		corev1.PodSpec{},
	)

	// scheduler, runtime class, priority class, node selector
	f(
		&vmv1beta1.CommonAppsParams{
			SchedulerName:     "custom-scheduler",
			RuntimeClassName:  ptr.To("gvisor"),
			PriorityClassName: "high-priority",
			NodeSelector:      map[string]string{"disktype": "ssd"},
		},
		corev1.PodSpec{
			SchedulerName:     "custom-scheduler",
			RuntimeClassName:  ptr.To("gvisor"),
			PriorityClassName: "high-priority",
			NodeSelector:      map[string]string{"disktype": "ssd"},
		},
	)

	// DNS settings
	f(
		&vmv1beta1.CommonAppsParams{
			DNSPolicy: corev1.DNSClusterFirstWithHostNet,
			DNSConfig: &corev1.PodDNSConfig{
				Nameservers: []string{"8.8.8.8"},
			},
		},
		corev1.PodSpec{
			DNSPolicy: corev1.DNSClusterFirstWithHostNet,
			DNSConfig: &corev1.PodDNSConfig{
				Nameservers: []string{"8.8.8.8"},
			},
		},
	)

	// image pull secrets and readiness gates
	f(
		&vmv1beta1.CommonAppsParams{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry-secret"}},
			ReadinessGates:   []corev1.PodReadinessGate{{ConditionType: "example.com/ready"}},
		},
		corev1.PodSpec{
			ImagePullSecrets: []corev1.LocalObjectReference{{Name: "registry-secret"}},
			ReadinessGates:   []corev1.PodReadinessGate{{ConditionType: "example.com/ready"}},
		},
	)

	// topology spread constraints
	f(
		&vmv1beta1.CommonAppsParams{
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				{MaxSkew: 1, TopologyKey: "zone", WhenUnsatisfiable: corev1.DoNotSchedule},
			},
		},
		corev1.PodSpec{
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				{MaxSkew: 1, TopologyKey: "zone", WhenUnsatisfiable: corev1.DoNotSchedule},
			},
		},
	)

	// termination grace period
	f(
		&vmv1beta1.CommonAppsParams{
			TerminationGracePeriodSeconds: ptr.To(int64(30)),
		},
		corev1.PodSpec{
			TerminationGracePeriodSeconds: ptr.To(int64(30)),
		},
	)

	// host network
	f(
		&vmv1beta1.CommonAppsParams{
			HostNetwork: true,
		},
		corev1.PodSpec{
			HostNetwork: true,
		},
	)
}

func TestDeploymentAddCommonParams(t *testing.T) {
	f := func(params *vmv1beta1.CommonAppsParams, want appsv1.DeploymentSpec) {
		t.Helper()
		var got appsv1.Deployment
		DeploymentAddCommonParams(&got, params)
		assert.Equal(t, want, got.Spec)
	}

	// replica count, min ready seconds, revision history
	f(
		&vmv1beta1.CommonAppsParams{
			ReplicaCount:              ptr.To(int32(3)),
			MinReadySeconds:           10,
			RevisionHistoryLimitCount: ptr.To(int32(5)),
		},
		appsv1.DeploymentSpec{
			Replicas:             ptr.To(int32(3)),
			MinReadySeconds:      10,
			RevisionHistoryLimit: ptr.To(int32(5)),
		},
	)

	// pod-level fields are propagated
	f(
		&vmv1beta1.CommonAppsParams{
			NodeSelector: map[string]string{"env": "prod"},
			ReplicaCount: ptr.To(int32(1)),
		},
		appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{"env": "prod"},
				},
			},
		},
	)
}

func TestStatefulSetAddCommonParams(t *testing.T) {
	f := func(params *vmv1beta1.CommonAppsParams, want appsv1.StatefulSetSpec) {
		t.Helper()
		var got appsv1.StatefulSet
		StatefulSetAddCommonParams(&got, params)
		assert.Equal(t, want, got.Spec)
	}

	// replica count, min ready seconds, revision history
	f(
		&vmv1beta1.CommonAppsParams{
			ReplicaCount:              ptr.To(int32(2)),
			MinReadySeconds:           5,
			RevisionHistoryLimitCount: ptr.To(int32(3)),
		},
		appsv1.StatefulSetSpec{
			Replicas:             ptr.To(int32(2)),
			MinReadySeconds:      5,
			RevisionHistoryLimit: ptr.To(int32(3)),
		},
	)

	// pod-level fields are propagated
	f(
		&vmv1beta1.CommonAppsParams{
			SchedulerName: "my-scheduler",
			ReplicaCount:  ptr.To(int32(1)),
		},
		appsv1.StatefulSetSpec{
			Replicas: ptr.To(int32(1)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					SchedulerName: "my-scheduler",
				},
			},
		},
	)
}

func TestDaemonSetAddCommonParams(t *testing.T) {
	f := func(params *vmv1beta1.CommonAppsParams, want appsv1.DaemonSetSpec) {
		t.Helper()
		var got appsv1.DaemonSet
		DaemonSetAddCommonParams(&got, params)
		assert.Equal(t, want, got.Spec)
	}

	// min ready seconds and revision history
	f(
		&vmv1beta1.CommonAppsParams{
			MinReadySeconds:           15,
			RevisionHistoryLimitCount: ptr.To(int32(2)),
		},
		appsv1.DaemonSetSpec{
			MinReadySeconds:      15,
			RevisionHistoryLimit: ptr.To(int32(2)),
		},
	)

	// pod-level fields propagated
	f(
		&vmv1beta1.CommonAppsParams{
			NodeSelector: map[string]string{"role": "worker"},
		},
		appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					NodeSelector: map[string]string{"role": "worker"},
				},
			},
		},
	)

	// root-aware security context
	f(
		&vmv1beta1.CommonAppsParams{
			UseStrictSecurity: ptr.To(true),
		},
		appsv1.DaemonSetSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{},
				Spec: corev1.PodSpec{
					SecurityContext: addStrictSecuritySettingsWithRootToPod(&vmv1beta1.CommonAppsParams{
						UseStrictSecurity: ptr.To(true),
					}),
				},
			},
		},
	)
}

func TestPodTemplateAddCommonParams_MergesCommonLabels(t *testing.T) {
	cfg := config.MustGetBaseConfig()
	defaultCfg := *cfg
	defer func() { *config.MustGetBaseConfig() = defaultCfg }()

	f := func(commonLabels map[string]string, initialLabels map[string]string, wantLabels map[string]string) {
		t.Helper()
		cfg.CommonAnnotations = nil
		cfg.CommonLabels = commonLabels

		got := corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: initialLabels,
			},
		}

		PodTemplateAddCommonParams(&got, &vmv1beta1.CommonAppsParams{})

		if got.Labels == nil {
			got.Labels = map[string]string{}
		}

		assert.Equal(t, wantLabels, got.Labels)
	}

	// existing + commons
	f(
		map[string]string{
			"common-label":   "common-value",
			"existing-label": "should-not-overwrite",
		},
		map[string]string{
			"existing-label": "existing-value",
		},
		map[string]string{
			"common-label":   "common-value",
			"existing-label": "existing-value",
		},
	)

	// don't let common rewrite selector-like labels
	f(
		map[string]string{
			"app.kubernetes.io/name": "common-app",
			"app":                    "common",
		},
		map[string]string{
			"app.kubernetes.io/name": "my-app",
			"app":                    "my-app",
		},
		map[string]string{
			"app.kubernetes.io/name": "my-app",
			"app":                    "my-app",
		},
	)

	// common selector-like labels added when missing
	f(
		map[string]string{
			"app.kubernetes.io/name": "common-app",
			"app":                    "common",
		},
		map[string]string{},
		map[string]string{
			"app.kubernetes.io/name": "common-app",
			"app":                    "common",
		},
	)

	// mix of selector and regular labels
	f(
		map[string]string{
			"app.kubernetes.io/name": "common-app",
			"common-label":           "common-value",
		},
		map[string]string{
			"common-label": "existing-value",
		},
		map[string]string{
			"app.kubernetes.io/name": "common-app",
			"common-label":           "existing-value",
		},
	)
}

func TestPodTemplateAddCommonParams_MergesCommonAnnotations(t *testing.T) {
	cfg := config.MustGetBaseConfig()
	defaultCfg := *cfg
	defer func() { *config.MustGetBaseConfig() = defaultCfg }()

	f := func(commonAnnotations map[string]string, initialAnnotations map[string]string, wantAnnotations map[string]string) {
		t.Helper()
		cfg.CommonLabels = nil
		cfg.CommonAnnotations = commonAnnotations

		got := corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: initialAnnotations,
			},
		}

		PodTemplateAddCommonParams(&got, &vmv1beta1.CommonAppsParams{})

		if got.Annotations == nil {
			got.Annotations = map[string]string{}
		}

		assert.Equal(t, wantAnnotations, got.Annotations)
	}

	// existing + commons
	f(
		map[string]string{
			"common-annotation":   "common-value",
			"existing-annotation": "should-not-overwrite",
		},
		map[string]string{
			"existing-annotation": "existing-value",
		},
		map[string]string{
			"common-annotation":   "common-value",
			"existing-annotation": "existing-value",
		},
	)

	// add annotations when missing
	f(
		map[string]string{
			"common-annotation": "common-value",
		},
		map[string]string{},
		map[string]string{
			"common-annotation": "common-value",
		},
	)
}
