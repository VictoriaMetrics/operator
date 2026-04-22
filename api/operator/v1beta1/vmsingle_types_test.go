package v1beta1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"
)

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
