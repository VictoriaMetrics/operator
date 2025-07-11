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
	f := func(opts *k8stools.SelectorOpts, sourceCRD, targetCRD client.Object, predefinedObjects []runtime.Object, isMatch bool) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(predefinedObjects)
		matches, err := isSelectorsMatchesTargetCRD(context.Background(), fclient, sourceCRD, targetCRD, opts)
		if err != nil {
			t.Fatal(err)
		}
		if matches != isMatch {
			t.Fatalf("isSelectorsMatchesTargetCRD(): expect %t, got %t", isMatch, matches)
		}
	}

	// match: selectors are nil, selectAll=true
	f(&k8stools.SelectorOpts{
		SelectAll: true,
	}, &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rule",
			Namespace: "n1",
			Labels: map[string]string{
				"app": "target-app",
			},
		},
	}, &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vmalert",
			Namespace: "n2",
		},
		Spec: vmv1beta1.VMAlertSpec{
			RuleSelector:          &metav1.LabelSelector{},
			RuleNamespaceSelector: &metav1.LabelSelector{},
		},
	}, nil, true)

	// not match: selectors are nil, selectAll=false
	f(&k8stools.SelectorOpts{}, &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rule",
			Namespace: "default",
			Labels: map[string]string{
				"app": "target-app",
			},
		},
	}, &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vmalert",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAlertSpec{
			RuleSelector:          &metav1.LabelSelector{},
			RuleNamespaceSelector: &metav1.LabelSelector{},
		},
	}, nil, false)

	// match: selector matches, selectAll=any
	f(&k8stools.SelectorOpts{
		ObjectSelector: &metav1.LabelSelector{
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
	}, &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rule",
			Namespace: "default",
			Labels: map[string]string{
				"cluster": "prod",
				"a":       "b",
			},
		},
	}, &vmv1beta1.VMAlert{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-vmalert",
			Namespace: "default",
		},
		Spec: vmv1beta1.VMAlertSpec{
			RuleSelector: &metav1.LabelSelector{},
		},
	}, nil, true)

	// not match: selector not match, selectAll=any
	f(&k8stools.SelectorOpts{
		SelectAll: true,
		ObjectSelector: &metav1.LabelSelector{
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
	}, &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rule",
			Namespace: "default",
			Labels: map[string]string{
				"cluster": "poc",
				"a":       "b",
			},
		},
	}, &vmv1beta1.VMAlert{
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
	}, nil, false)

	// match: namespaceselector matches, selectAll=any
	f(&k8stools.SelectorOpts{
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": "default",
			},
		},
	}, &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rule",
			Namespace: "default",
			Labels: map[string]string{
				"cluster": "prod",
				"a":       "b",
			},
		},
	}, &vmv1beta1.VMAlert{
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
	}, []runtime.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"kubernetes.io/metadata.name": "default"}},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-stack", Labels: map[string]string{"kubernetes.io/metadata.name": "vm-stack"}},
		},
	}, true)

	// not match: namespaceselector not matches, selectAll=any
	f(&k8stools.SelectorOpts{
		SelectAll: true,
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": "vm-stack",
			},
		},
	}, &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rule",
			Namespace: "default",
			Labels: map[string]string{
				"cluster": "prod",
				"a":       "b",
			},
		},
	}, &vmv1beta1.VMAlert{
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
	}, []runtime.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"kubernetes.io/metadata.name": "default"}},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-stack", Labels: map[string]string{"kubernetes.io/metadata.name": "vm-stack"}},
		},
	}, false)

	// match: selector+namespaceSelector match, selectAll=any
	f(&k8stools.SelectorOpts{
		ObjectSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "cluster",
					Operator: metav1.LabelSelectorOpNotIn,
					Values:   []string{"poc"},
				},
			},
		},
		NamespaceSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{
				"kubernetes.io/metadata.name": "default",
			},
		},
	}, &vmv1beta1.VMRule{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rule",
			Namespace: "default",
			Labels: map[string]string{
				"cluster": "prod",
			},
		},
	}, &vmv1beta1.VMAlert{
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
	}, []runtime.Object{
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"kubernetes.io/metadata.name": "default"}},
		},
		&corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: "vm-stack", Labels: map[string]string{"kubernetes.io/metadata.name": "vm-stack"}},
		},
	}, true)
}
