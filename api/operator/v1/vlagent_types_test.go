package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestVLAgent_FinalLabels(t *testing.T) {
	type opts struct {
		cr           *VLAgent
		commonLabels map[string]string
		want         map[string]string
	}
	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonLabels = o.commonLabels
		assert.Equal(t, o.want, o.cr.FinalLabels())
	}

	// no common labels
	f(opts{
		cr: &VLAgent{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		want: map[string]string{
			"app.kubernetes.io/name":      "vlagent",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
		},
	})
	// common labels added
	f(opts{
		cr:           &VLAgent{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vlagent",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "platform",
		},
	})
	// common labels cannot override existing
	f(opts{
		cr:           &VLAgent{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"managed-by": "intruder", "team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vlagent",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "platform",
		},
	})
	// common labels cannot override managedMetadata
	f(opts{
		cr: &VLAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VLAgentSpec{ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{Labels: map[string]string{"team": "backend"}}},
		},
		commonLabels: map[string]string{"team": "intruder", "env": "prod"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vlagent",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "backend",
			"env":                         "prod",
		},
	})
}

func TestVLAgent_FinalAnnotations(t *testing.T) {
	type opts struct {
		cr                *VLAgent
		commonAnnotations map[string]string
		want              map[string]string
	}
	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonAnnotations = o.commonAnnotations
		assert.Equal(t, o.want, o.cr.FinalAnnotations())
	}

	// no annotations
	f(opts{cr: &VLAgent{ObjectMeta: metav1.ObjectMeta{Name: "test"}}, want: nil})
	// common annotations added
	f(opts{
		cr:                &VLAgent{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonAnnotations: map[string]string{"note": "managed-by-gitops"},
		want:              map[string]string{"note": "managed-by-gitops"},
	})
	// common annotations cannot override managedMetadata
	f(opts{
		cr: &VLAgent{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec:       VLAgentSpec{ManagedMetadata: &vmv1beta1.ManagedObjectsMetadata{Annotations: map[string]string{"note": "from-spec"}}},
		},
		commonAnnotations: map[string]string{"note": "intruder", "extra": "value"},
		want:              map[string]string{"note": "from-spec", "extra": "value"},
	})
}
