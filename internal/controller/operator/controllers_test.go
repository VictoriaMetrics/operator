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
	type opts struct {
		opts              *k8stools.SelectorOpts
		sourceCRD         client.Object
		targetCRD         client.Object
		predefinedObjects []runtime.Object
		isMatch           bool
	}
	f := func(opts opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		matches, err := isSelectorsMatchesTargetCRD(context.Background(), fclient, opts.sourceCRD, opts.targetCRD, opts.opts)
		if err != nil {
			t.Fatal(err)
		}
		if matches != opts.isMatch {
			t.Fatalf("isSelectorsMatchesTargetCRD(): expect %t, got %t", opts.isMatch, matches)
		}
	}

	// match: selectors are nil, selectAll=true
	o := opts{
		opts: &k8stools.SelectorOpts{
			SelectAll: true,
		},

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
		isMatch: true,
	}
	f(o)

	// not match: selectors are nil, selectAll=false
	o = opts{
		opts: &k8stools.SelectorOpts{},
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
	}
	f(o)

	// match: selector matches, selectAll=any
	o = opts{
		opts: &k8stools.SelectorOpts{
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
		},
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
		isMatch: true,
	}
	f(o)

	// not match: selector not match, selectAll=any
	o = opts{
		opts: &k8stools.SelectorOpts{
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
		},
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
	}
	f(o)

	// match: namespaceselector matches, selectAll=any
	o = opts{
		opts: &k8stools.SelectorOpts{
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": "default",
				},
			},
		},
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
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"kubernetes.io/metadata.name": "default"}},
			},
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "vm-stack", Labels: map[string]string{"kubernetes.io/metadata.name": "vm-stack"}},
			},
		},
		isMatch: true,
	}
	f(o)

	// not match: namespaceselector not matches, selectAll=any
	o = opts{
		opts: &k8stools.SelectorOpts{
			SelectAll: true,
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"kubernetes.io/metadata.name": "vm-stack",
				},
			},
		},
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
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"kubernetes.io/metadata.name": "default"}},
			},
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "vm-stack", Labels: map[string]string{"kubernetes.io/metadata.name": "vm-stack"}},
			},
		},
	}
	f(o)

	// match: selector+namespaceSelector match, selectAll=any
	o = opts{
		opts: &k8stools.SelectorOpts{
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
		},
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
		predefinedObjects: []runtime.Object{
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "default", Labels: map[string]string{"kubernetes.io/metadata.name": "default"}},
			},
			&corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "vm-stack", Labels: map[string]string{"kubernetes.io/metadata.name": "vm-stack"}},
			},
		},
		isMatch: true,
	}
	f(o)
}
