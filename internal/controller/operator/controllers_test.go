package operator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func TestIsSelectorsMatchesTargetCRD(t *testing.T) {
	type opts struct {
		selectAll         bool
		sourceCRD         client.Object
		targetCRD         client.Object
		selector          *metav1.LabelSelector
		namespaceSelector *metav1.LabelSelector
		predefinedObjects []runtime.Object
		isMatch           bool
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		opts := &k8stools.SelectorOpts{
			SelectAll:         o.selectAll,
			NamespaceSelector: o.namespaceSelector,
			ObjectSelector:    o.selector,
		}
		matches, err := isSelectorsMatchesTargetCRD(context.Background(), fclient, o.sourceCRD, o.targetCRD, opts)
		assert.NoError(t, err)
		assert.Equal(t, matches, o.isMatch)
	}

	// match: selectors are nil, selectAll=true
	f(opts{
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
		isMatch: true,
	})

	// not match: selectors are nil, selectAll=false
	f(opts{
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
	})

	// match: selector matches, selectAll=any
	f(opts{
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
		isMatch: true,
	})

	// not match: selector not match, selectAll=any
	f(opts{
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
	})

	// match: namespaceselector matches, selectAll=any
	f(opts{
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
	})

	// not match: namespaceselector not matches, selectAll=any
	f(opts{
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
	})

	// match: selector+namespaceSelector match, selectAll=any
	f(opts{
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
	})
}

func TestHandleReconcileErrWithStatus(t *testing.T) {
	type opts struct {
		ctx        context.Context
		err        error
		origin     ctrl.Result
		object     *vmv1beta1.VMCluster
		wantResult ctrl.Result
		wantErr    error
		wantStatus vmv1beta1.UpdateStatus
	}

	f := func(o opts) {
		t.Helper()
		if o.ctx == nil {
			o.ctx = context.Background()
		}
		var predefined []runtime.Object
		if o.object != nil {
			predefined = append(predefined, o.object)
		}
		fclient := k8stools.GetTestClientWithObjects(predefined)
		got, err := handleReconcileErrWithStatus(o.ctx, fclient, o.object, o.origin, o.err)
		assert.Equal(t, o.wantErr, err)
		assert.Equal(t, o.wantResult, got)
		if o.wantStatus != "" && o.object != nil {
			updated := &vmv1beta1.VMCluster{}
			assert.NoError(t, fclient.Get(o.ctx, client.ObjectKeyFromObject(o.object), updated))
			assert.Equal(t, o.wantStatus, updated.Status.UpdateStatus)
		}
	}

	// nil error
	f(opts{
		err:        nil,
		object:     &vmv1beta1.VMCluster{},
		origin:     ctrl.Result{RequeueAfter: 10},
		wantResult: ctrl.Result{RequeueAfter: 10},
		wantErr:    nil,
	})

	// parsingError
	f(opts{
		err: &parsingError{origin: "bad field value", controller: "vmcluster"},
		object: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		},
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{},
		wantErr:    &parsingError{origin: "bad field value", controller: "vmcluster"},
		wantStatus: vmv1beta1.UpdateStatusFailed,
	})

	// context.Canceled sets RequeueAfter, no status update
	f(opts{
		err:        context.Canceled,
		object:     &vmv1beta1.VMCluster{},
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{RequeueAfter: time.Second * 5},
		wantErr:    nil,
	})

	// transient error
	f(opts{
		err:        fmt.Errorf("some transient error"),
		object:     &vmv1beta1.VMCluster{},
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{},
		wantErr:    fmt.Errorf("some transient error"),
	})
}

func TestHandleReconcileErr(t *testing.T) {
	type opts struct {
		ctx        context.Context
		err        error
		origin     ctrl.Result
		wantResult ctrl.Result
		wantErr    error
	}

	f := func(o opts) {
		t.Helper()
		if o.ctx == nil {
			o.ctx = context.Background()
		}
		got, err := handleReconcileErr(o.ctx, nil, (*vmv1beta1.VMCluster)(nil), o.origin, o.err)
		assert.Equal(t, o.wantErr, err)
		assert.Equal(t, o.wantResult, got)
	}

	// no error
	f(opts{
		err:        nil,
		origin:     ctrl.Result{RequeueAfter: 10},
		wantResult: ctrl.Result{RequeueAfter: 10},
		wantErr:    nil,
	})

	// context canceled
	f(opts{
		err:        context.Canceled,
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{RequeueAfter: time.Second * 5},
		wantErr:    nil,
	})

	// context canceled with ErrShutdown
	shutdownCtx, shutdownCancel := context.WithCancelCause(context.Background())
	shutdownCancel(ErrShutdown)
	f(opts{
		ctx:        shutdownCtx,
		err:        fmt.Errorf("wrapped: %w", errors.Join(context.Canceled, ErrShutdown)),
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{},
		wantErr:    nil,
	})
}
