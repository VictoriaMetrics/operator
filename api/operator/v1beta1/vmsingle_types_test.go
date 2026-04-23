package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/config"
)

func TestVMSingle_FinalLabels(t *testing.T) {
	type opts struct {
		cr           *VMSingle
		commonLabels map[string]string
		want         map[string]string
	}

	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonLabels = o.commonLabels
		got := o.cr.FinalLabels()
		assert.Equal(t, o.want, got)
	}

	// default labels
	f(opts{
		cr: &VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmsingle",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
		},
	})

	// common labels added
	f(opts{
		cr:           &VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"team": "platform", "env": "prod"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmsingle",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "platform",
			"env":                         "prod",
		},
	})

	// common labels cannot override existing selector labels
	f(opts{
		cr:           &VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonLabels: map[string]string{"managed-by": "intruder", "team": "platform"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmsingle",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "platform",
		},
	})

	// common labels cannot override managedMetadata labels
	f(opts{
		cr: &VMSingle{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec: VMSingleSpec{
				ManagedMetadata: &ManagedObjectsMetadata{
					Labels: map[string]string{"team": "backend"},
				},
			},
		},
		commonLabels: map[string]string{"team": "intruder", "env": "prod"},
		want: map[string]string{
			"app.kubernetes.io/name":      "vmsingle",
			"app.kubernetes.io/instance":  "test",
			"app.kubernetes.io/component": "monitoring",
			"managed-by":                  "vm-operator",
			"team":                        "backend",
			"env":                         "prod",
		},
	})
}

func TestVMSingle_FinalAnnotations(t *testing.T) {
	type opts struct {
		cr                *VMSingle
		commonAnnotations map[string]string
		want              map[string]string
	}

	f := func(o opts) {
		t.Helper()
		cfg := config.MustGetBaseConfig()
		orig := *cfg
		defer func() { *cfg = orig }()
		cfg.CommonAnnotations = o.commonAnnotations
		got := o.cr.FinalAnnotations()
		assert.Equal(t, o.want, got)
	}

	// no common annotations, no managed metadata → nil
	f(opts{
		cr:   &VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		want: nil,
	})

	// common annotations added
	f(opts{
		cr:                &VMSingle{ObjectMeta: metav1.ObjectMeta{Name: "test"}},
		commonAnnotations: map[string]string{"note": "managed-by-gitops"},
		want:              map[string]string{"note": "managed-by-gitops"},
	})

	// common annotations cannot override managedMetadata annotations
	f(opts{
		cr: &VMSingle{
			ObjectMeta: metav1.ObjectMeta{Name: "test"},
			Spec: VMSingleSpec{
				ManagedMetadata: &ManagedObjectsMetadata{
					Annotations: map[string]string{"note": "from-spec"},
				},
			},
		},
		commonAnnotations: map[string]string{"note": "intruder", "extra": "value"},
		want:              map[string]string{"note": "from-spec", "extra": "value"},
	})
}

func TestVMSingle_Validate(t *testing.T) {
	f := func(spec VMSingleSpec, wantErr bool) {
		t.Helper()
		r := &VMSingle{
			Spec: spec,
		}
		if wantErr {
			assert.Error(t, r.Validate())
		} else {
			assert.NoError(t, r.Validate())
		}
	}

	// no scrape classes
	f(VMSingleSpec{}, false)

	// single default scrape class
	f(VMSingleSpec{
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "default", Default: ptr.To(true)},
				{Name: "other"},
			},
		},
	}, false)

	// multiple default scrape classes
	f(VMSingleSpec{
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "default", Default: ptr.To(true)},
				{Name: "other", Default: ptr.To(true)},
			},
		},
	}, true)

	// duplicated scrape class names
	f(VMSingleSpec{
		CommonScrapeParams: CommonScrapeParams{
			ScrapeClasses: []ScrapeClass{
				{Name: "cls"},
				{Name: "cls"},
			},
		},
	}, true)
}
