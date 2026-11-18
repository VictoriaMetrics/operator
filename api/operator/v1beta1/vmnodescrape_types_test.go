package v1beta1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestVMNodeScrapeValidateSelector(t *testing.T) {
	cr := &VMNodeScrape{
		Spec: VMNodeScrapeSpec{Selector: metav1.LabelSelector{MatchLabels: map[string]string{"": ""}}},
	}
	if err := cr.Validate(); err == nil {
		t.Fatalf("expected selector validation error")
	}
}
