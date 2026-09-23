package operator

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

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
		watchNamespaces   []string
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
		matches, err := isSelectorsMatchesTargetCRD(context.Background(), fclient, o.sourceCRD, o.targetCRD, opts, o.watchNamespaces)
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

	// match: namespace selector labels are ignored in multi-namespace mode
	f(opts{
		selectAll:       true,
		watchNamespaces: []string{"n1"},
		sourceCRD: &vmv1beta1.VMRule{
			ObjectMeta: metav1.ObjectMeta{Name: "rule", Namespace: "n1"},
		},
		targetCRD: &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{Name: "vmalert", Namespace: "n2"},
		},
		namespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"team": "ops"}},
		isMatch:           true,
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
		err: newParsingError("bad field value"),
		object: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		},
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{},
		wantErr:    newParsingError("bad field value"),
		wantStatus: vmv1beta1.UpdateStatusFailed,
	})

	// context.Canceled sets RequeueAfter, no status update
	f(opts{
		err: context.Canceled,
		object: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		},
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{RequeueAfter: time.Second * 5},
		wantErr:    nil,
	})

	// transient error
	f(opts{
		err: fmt.Errorf("some transient error"),
		object: &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-cluster",
				Namespace: "default",
			},
		},
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{},
		wantErr:    fmt.Errorf("some transient error"),
	})

	// object without identity (no namespace): result/error pass through unchanged, no event/status update attempted
	f(opts{
		err:        fmt.Errorf("some transient error"),
		object:     &vmv1beta1.VMCluster{},
		origin:     ctrl.Result{RequeueAfter: 10},
		wantResult: ctrl.Result{RequeueAfter: 10},
		wantErr:    fmt.Errorf("some transient error"),
	})
}

func TestHandleConfigReconcileErrWithStatus(t *testing.T) {
	type opts struct {
		ctx        context.Context
		err        error
		origin     ctrl.Result
		object     *vmv1beta1.VMRule
		wantResult ctrl.Result
		wantErr    error
		wantStatus vmv1beta1.UpdateStatus
		wantReason string
	}

	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		if o.ctx != nil {
			ctx = o.ctx
		}
		fclient := k8stools.GetTestClientWithObjects([]runtime.Object{o.object})
		got, err := handleConfigReconcileErrWithStatus(ctx, fclient, o.object, o.origin, o.err)
		assert.Equal(t, o.wantErr, err)
		assert.Equal(t, o.wantResult, got)
		updated := &vmv1beta1.VMRule{}
		assert.NoError(t, fclient.Get(context.Background(), client.ObjectKeyFromObject(o.object), updated))
		assert.Equal(t, o.wantStatus, updated.Status.UpdateStatus)
		assert.Equal(t, o.wantReason, updated.Status.Reason)
	}

	newRule := func(modify ...func(*vmv1beta1.VMRule)) *vmv1beta1.VMRule {
		r := &vmv1beta1.VMRule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-rule",
				Namespace: "default",
			},
		}
		for _, m := range modify {
			m(r)
		}
		return r
	}

	// successful reconcile marks the object operational, even though no parent selected it
	f(opts{
		object:     newRule(),
		origin:     ctrl.Result{RequeueAfter: 10},
		wantResult: ctrl.Result{RequeueAfter: 10},
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	// the operator cannot decode the spec: the object itself is broken, not a parent
	f(opts{
		err:        newParsingError("bad field value"),
		object:     newRule(),
		wantErr:    newParsingError("bad field value"),
		wantStatus: vmv1beta1.UpdateStatusFailed,
		wantReason: newParsingError("bad field value").Error(),
	})

	// once the spec parses again, the failed status and its reason are cleared
	f(opts{
		object: newRule(func(r *vmv1beta1.VMRule) {
			r.Status.UpdateStatus = vmv1beta1.UpdateStatusFailed
			r.Status.Reason = "bad field value"
		}),
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	// a transient reconcile error belongs to the parent, the object itself stays operational
	f(opts{
		err:        fmt.Errorf("some transient error"),
		object:     newRule(),
		wantErr:    fmt.Errorf("some transient error"),
		wantStatus: vmv1beta1.UpdateStatusOperational,
	})

	// the object could not be fetched, so its state is unknown and must not be reported
	f(opts{
		err:     newGetError(fmt.Errorf("api server is unavailable")),
		object:  newRule(),
		wantErr: newGetError(fmt.Errorf("api server is unavailable")),
	})

	// a NotFound get error is swallowed by handleReconcileErr, so the skip must rely on the
	// original error; the object is kept in the client so that a status write would show up
	f(opts{
		err: newGetError(k8serrors.NewNotFound(
			schema.GroupResource{Group: "operator.victoriametrics.com", Resource: "vmrules"}, "test-rule")),
		object: newRule(),
	})

	// the operator is shutting down, writing a status can only fail
	f(opts{
		ctx:    canceledCtx(),
		object: newRule(),
	})

	// the object is being deleted, its status is not worth writing
	f(opts{
		object: newRule(func(r *vmv1beta1.VMRule) {
			ts := metav1.Now()
			r.DeletionTimestamp = &ts
			r.Finalizers = []string{vmv1beta1.FinalizerName}
		}),
	})
}

func TestHandleConfigReconcileErrWithStatus_PropagatesStatusWriteFailure(t *testing.T) {
	ctx := context.Background()
	newRule := func() *vmv1beta1.VMRule {
		return &vmv1beta1.VMRule{ObjectMeta: metav1.ObjectMeta{Name: "test-rule", Namespace: "default"}}
	}
	fns := interceptor.Funcs{
		SubResourceUpdate: func(_ context.Context, _ client.Client, _ string, _ client.Object, _ ...client.SubResourceUpdateOption) error {
			return errors.New("api server is unavailable")
		},
	}
	rule := newRule()
	fclient := k8stools.GetTestClientWithObjectsAndInterceptors([]runtime.Object{rule}, fns)

	// the reconcile succeeded, so only a returned error can requeue the object and give
	// it another chance to get its status
	_, err := handleConfigReconcileErrWithStatus(ctx, fclient, rule, ctrl.Result{}, nil)
	if assert.Error(t, err, "a failed status write must be surfaced to the controller") {
		assert.Contains(t, err.Error(), "api server is unavailable")
	}

	// an existing reconcile error already requeues the object and must not be masked
	reconcileErr := fmt.Errorf("some transient error")
	_, err = handleConfigReconcileErrWithStatus(ctx, fclient, rule, ctrl.Result{}, reconcileErr)
	assert.Equal(t, reconcileErr, err)
}

func canceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestHandleReconcileErr(t *testing.T) {
	type opts struct {
		ctx        context.Context
		err        error
		origin     ctrl.Result
		object     client.Object
		wantResult ctrl.Result
		wantErr    error
	}

	f := func(o opts) {
		t.Helper()
		if o.ctx == nil {
			o.ctx = context.Background()
		}
		object := o.object
		if object == nil {
			object = (*vmv1beta1.VMCluster)(nil)
		}
		fclient := k8stools.GetTestClientWithObjects(nil)
		got, err := handleReconcileErr(o.ctx, fclient, object, o.origin, o.err)
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

	// object without identity (nil): result/error pass through unchanged, no event/metrics side effects attempted
	f(opts{
		err:        context.Canceled,
		origin:     ctrl.Result{RequeueAfter: 10},
		wantResult: ctrl.Result{RequeueAfter: 10},
		wantErr:    context.Canceled,
	})

	identified := &vmv1beta1.VMCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	// context canceled
	f(opts{
		err:        context.Canceled,
		object:     identified,
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
		object:     identified,
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{},
		wantErr:    nil,
	})

	gr := schema.GroupResource{Group: "operator.victoriametrics.com", Resource: "vmclusters"}

	// getError wrapping NotFound: routine object deletion, swallowed silently, result passed through
	notFoundErr := newGetError(k8serrors.NewNotFound(gr, "test-cluster"))
	f(opts{
		err:        notFoundErr,
		object:     identified,
		origin:     ctrl.Result{RequeueAfter: 10},
		wantResult: ctrl.Result{RequeueAfter: 10},
		wantErr:    nil,
	})

	// getError wrapping a real (non-NotFound) API error: propagates as-is
	forbiddenErr := newGetError(k8serrors.NewForbidden(gr, "test-cluster", fmt.Errorf("no access")))
	f(opts{
		err:        forbiddenErr,
		object:     identified,
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{},
		wantErr:    forbiddenErr,
	})

	// propagate cancel context if it's wrapped into getError
	getCanceledErr := newGetError(fmt.Errorf("Get %q: %w", "https://example.com", context.Canceled))
	f(opts{
		err:        getCanceledErr,
		object:     identified,
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{},
		wantErr:    getCanceledErr,
	})

	// conflict: swallowed, requeued after 5s
	conflictErr := k8serrors.NewConflict(gr, "test-cluster", fmt.Errorf("stale resourceVersion"))
	f(opts{
		err:        conflictErr,
		object:     identified,
		origin:     ctrl.Result{},
		wantResult: ctrl.Result{RequeueAfter: time.Second * 5},
		wantErr:    nil,
	})
}
