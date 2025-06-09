package operator

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestIsSelectorsMatchesTargetCRD(t *testing.T) {
	tests := []struct {
		name              string
		selectAll         bool
		sourceCRD         client.Object
		targetCRD         client.Object
		selector          *metav1.LabelSelector
		namespaceSelector *metav1.LabelSelector
		predefinedObjects []runtime.Object
		isMatch           bool
	}{
		{
			name:      "match: selectors are nil, selectAll=true",
			selectAll: true,
			sourceCRD: &vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "n1",
					Labels: map[string]string{
						"app": "target-app",
					},
				},
			},
			targetCRD: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "n2",
				},
				Spec: vmv1beta1.VMAlertSpec{
					RuleSelector:          &metav1.LabelSelector{},
					RuleNamespaceSelector: &metav1.LabelSelector{},
				},
			},
			selector:          nil,
			namespaceSelector: nil,
			isMatch:           true,
		},
		{
			name:      "not match: selectors are nil, selectAll=false",
			selectAll: false,
			sourceCRD: &vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"app": "target-app",
					},
				},
			},
			targetCRD: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertSpec{
					RuleSelector:          &metav1.LabelSelector{},
					RuleNamespaceSelector: &metav1.LabelSelector{},
				},
			},
			selector:          nil,
			namespaceSelector: nil,
			isMatch:           false,
		},
		{
			name:      "match: selector matches, selectAll=any",
			selectAll: false,
			sourceCRD: &vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "prod",
						"a":       "b",
					},
				},
			},
			targetCRD: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertSpec{
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
			name:      "not match: selector not match, selectAll=any",
			selectAll: true,
			sourceCRD: &vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "poc",
						"a":       "b",
					},
				},
			},
			targetCRD: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertSpec{
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
			name:      "match: namespaceselector matches, selectAll=any",
			selectAll: false,
			sourceCRD: &vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "prod",
						"a":       "b",
					},
				},
			},
			targetCRD: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertSpec{
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
			name:      "not match: namespaceselector not matches, selectAll=any",
			selectAll: true,
			sourceCRD: &vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "prod",
						"a":       "b",
					},
				},
			},
			targetCRD: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertSpec{
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
			name:      "match: selector+namespaceSelector match, selectAll=any",
			selectAll: false,
			sourceCRD: &vmv1beta1.VMRule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "rule",
					Namespace: "default",
					Labels: map[string]string{
						"cluster": "prod",
					},
				},
			},
			targetCRD: &vmv1beta1.VMAlert{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmalert",
					Namespace: "default",
				},
				Spec: vmv1beta1.VMAlertSpec{
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
			opts := &k8stools.SelectorOpts{
				SelectAll:         tt.selectAll,
				NamespaceSelector: tt.namespaceSelector,
				ObjectSelector:    tt.selector,
			}
			matches, err := isSelectorsMatchesTargetCRD(context.Background(), fclient, tt.sourceCRD, tt.targetCRD, opts)
			if err != nil {
				t.Fatal(err)
			}
			if matches != tt.isMatch {
				t.Fatalf("BUG: %s: expect %t, got %t", tt.name, tt.isMatch, matches)
			}
		})
	}
}
