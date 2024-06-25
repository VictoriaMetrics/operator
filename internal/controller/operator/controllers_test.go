package controller

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	victoriametricsv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestIsSelectorsMatchesTargetCRD(t *testing.T) {
	tests := []struct {
		name              string
		sourceCRD         client.Object
		targetCRD         client.Object
		selector          *metav1.LabelSelector
		namespaceSelector *metav1.LabelSelector
		predefinedObjects []runtime.Object
		isMatch           bool
	}{
		{
			name: "matches cause under same namespace",
			sourceCRD: &victoriametricsv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"app": "target-app",
					},
				},
			},
			targetCRD: &victoriametricsv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: victoriametricsv1beta1.VMAlertSpec{
					RuleSelector:          &metav1.LabelSelector{},
					RuleNamespaceSelector: &metav1.LabelSelector{},
				},
			},
			selector:          nil,
			namespaceSelector: nil,
			isMatch:           true,
		},
		{
			name: "not match",
			sourceCRD: &victoriametricsv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"app": "target-app",
					},
				},
			},
			targetCRD: &victoriametricsv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "vm-stack",
				},
				Spec: victoriametricsv1beta1.VMAlertSpec{
					RuleSelector:          &metav1.LabelSelector{},
					RuleNamespaceSelector: &metav1.LabelSelector{},
				},
			},
			selector:          nil,
			namespaceSelector: nil,
			isMatch:           false,
		},
		{
			name: "selector matches",
			sourceCRD: &victoriametricsv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "prod",
						"a":       "b",
					},
				},
			},
			targetCRD: &victoriametricsv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: victoriametricsv1beta1.VMAlertSpec{
					RuleSelector: &metav1.LabelSelector{},
				},
			},
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "cluster",
						Operator: metav1.LabelSelectorOpNotIn,
						Values:   []string{"poc"},
					},
					{
						Key:      "a",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"b"},
					},
				},
			},
			namespaceSelector: nil,
			isMatch:           true,
		},
		{
			name: "selector not match",
			sourceCRD: &victoriametricsv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "poc",
						"a":       "b",
					},
				},
			},
			targetCRD: &victoriametricsv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: victoriametricsv1beta1.VMAlertSpec{
					RuleSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "cluster",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"poc"},
							},
							{
								Key:      "a",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"b"},
							},
						},
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "cluster",
						Operator: metav1.LabelSelectorOpNotIn,
						Values:   []string{"poc"},
					},
					{
						Key:      "a",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"b"},
					},
				},
			},
			namespaceSelector: nil,
			isMatch:           false,
		},
		{
			name: "namespaceselector matches",
			sourceCRD: &victoriametricsv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "prod",
						"a":       "b",
					},
				},
			},
			targetCRD: &victoriametricsv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: victoriametricsv1beta1.VMAlertSpec{
					RuleNamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "default",
						},
					},
				},
			},
			selector: nil,
			namespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": "default",
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"kubernetes.io/metadata.name": "default"}},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-stack", Labels: map[string]string{"kubernetes.io/metadata.name": "vm-stack"}},
				},
			},
			isMatch: true,
		},
		{
			name: "namespaceselector not matches",
			sourceCRD: &victoriametricsv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "prod",
						"a":       "b",
					},
				},
			},
			targetCRD: &victoriametricsv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: victoriametricsv1beta1.VMAlertSpec{
					RuleNamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "default",
						},
					},
				},
			},
			selector: nil,
			namespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": "vm-stack",
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"kubernetes.io/metadata.name": "default"}},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-stack", Labels: map[string]string{"kubernetes.io/metadata.name": "vm-stack"}},
				},
			},
			isMatch: false,
		},
		{
			name: "selector+namespaceSelector match",
			sourceCRD: &victoriametricsv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "prod",
					},
				},
			},
			targetCRD: &victoriametricsv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: victoriametricsv1beta1.VMAlertSpec{
					RuleSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "cluster",
								Operator: metav1.LabelSelectorOpNotIn,
								Values:   []string{"poc"},
							},
						},
					},
					RuleNamespaceSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"kubernetes.io/metadata.name": "default",
						},
					},
				},
			},
			selector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "cluster",
						Operator: metav1.LabelSelectorOpNotIn,
						Values:   []string{"poc"},
					},
				},
			},
			namespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": "default",
				},
			},
			predefinedObjects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"kubernetes.io/metadata.name": "default"}},
				},
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "vm-stack", Labels: map[string]string{"kubernetes.io/metadata.name": "vm-stack"}},
				},
			},
			isMatch: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			matches, err := isSelectorsMatchesTargetCRD(context.Background(), fclient, tt.sourceCRD, tt.targetCRD, tt.selector, tt.namespaceSelector)
			if err != nil {
				t.Fatal(err)
			}
			if matches != tt.isMatch {
				t.Fatalf("BUG: %s: expect %t, got %t", tt.name, tt.isMatch, matches)
			}
		})
	}
}
