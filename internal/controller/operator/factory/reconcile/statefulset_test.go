package reconcile

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_waitForPodReady(t *testing.T) {
	type opts struct {
		nsn               types.NamespacedName
		desiredVersion    string
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		synctest.Test(t, func(t *testing.T) {
			err := waitForPodReady(ctx, fclient, o.nsn, o.desiredVersion, 0)
			if o.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}

	// testing pod with unready status
	f(opts{
		nsn: types.NamespacedName{
			Namespace: "default",
			Name:      "vmselect-example-0",
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-example-0",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
					Phase:      corev1.PodPending,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-example-1",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
					Phase:      corev1.PodPending,
				},
			},
		},
		wantErr: true,
	})

	// testing pod with ready status
	f(opts{
		nsn: types.NamespacedName{
			Namespace: "default",
			Name:      "vmselect-example-0",
		},
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-example-0",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Status: "True", Type: corev1.PodReady},
					},
					Phase: corev1.PodRunning,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-example-1",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
					Phase:      corev1.PodPending,
				},
			},
		},
	})

	// with desiredVersion
	f(opts{
		nsn: types.NamespacedName{
			Namespace: "default",
			Name:      "vmselect-example-0",
		},
		desiredVersion: "some-version",
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-example-0",
					Namespace: "default",
					Labels: map[string]string{
						podRevisionLabel: "some-version",
					},
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Status: "True", Type: corev1.PodReady},
					},
					Phase: corev1.PodRunning,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-example-1",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
					Phase:      corev1.PodPending,
				},
			},
		},
	})

	// with missing desiredVersion
	f(opts{
		nsn: types.NamespacedName{
			Namespace: "default",
			Name:      "vmselect-example-0",
		},
		desiredVersion: "some-version",
		predefinedObjects: []runtime.Object{
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-example-0",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{
						{Status: "True", Type: corev1.PodReady},
					},
					Phase: corev1.PodRunning,
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-example-1",
					Namespace: "default",
				},
				Status: corev1.PodStatus{
					Conditions: []corev1.PodCondition{},
					Phase:      corev1.PodPending,
				},
			},
		},
		wantErr: true,
	})
}

func Test_podIsReady(t *testing.T) {
	type opts struct {
		pod             corev1.Pod
		minReadySeconds int32
		want            bool
	}
	f := func(o opts) {
		t.Helper()
		got := PodIsReady(&o.pod, o.minReadySeconds)
		assert.Equal(t, o.want, got)
	}

	// pod is ready
	f(opts{
		pod: corev1.Pod{
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodInitialized,
						Status: "False",
					},
					{
						Type:   corev1.PodReady,
						Status: "True",
					},
				},
				Phase: corev1.PodRunning,
			},
		},
		want: true,
	})

	// pod is unready
	f(opts{
		pod: corev1.Pod{
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodInitialized,
						Status: "False",
					},
					{
						Type:   corev1.PodReady,
						Status: "True",
					},
				},
				Phase: corev1.PodPending,
			},
		},
	})

	// pod is deleted
	f(opts{
		pod: corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodInitialized,
						Status: "False",
					},
					{
						Type:   corev1.PodReady,
						Status: "True",
					},
				},
				Phase: corev1.PodSucceeded,
			},
		},
	})
	// pod is not min ready
	f(opts{
		pod: corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{},
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodInitialized,
						Status: "False",
					},
					{
						Type:               corev1.PodReady,
						Status:             "True",
						LastTransitionTime: metav1.Time{Time: time.Now().Add(time.Hour)},
					},
				},
				Phase: corev1.PodSucceeded,
			},
		},
		minReadySeconds: 45,
	})
}

func Test_performRollingUpdateOnSts(t *testing.T) {
	type opts struct {
		sts               *appsv1.StatefulSet
		opts              rollingUpdateOpts
		wantErr           bool
		predefinedObjects []runtime.Object
		actions           map[string][]string
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		var mu sync.Mutex
		actions := make(map[string][]string)
		fclient := k8stools.GetTestClientWithObjectsAndInterceptors(o.predefinedObjects, interceptor.Funcs{
			SubResourceCreate: func(ctx context.Context, cl client.Client, _ string, obj client.Object, subResource client.Object, _ ...client.SubResourceCreateOption) error {
				pod, podOk := obj.(*corev1.Pod)
				_, evictionOk := subResource.(*policyv1.Eviction)
				if evictionOk && podOk {
					mu.Lock()
					actions[pod.Name] = append(actions[pod.Name], "Evict")
					mu.Unlock()
					pod.Labels[podRevisionLabel] = o.sts.Status.UpdateRevision
					return cl.Update(ctx, pod)
				}
				return nil
			},
			Delete: func(ctx context.Context, cl client.WithWatch, obj client.Object, opts ...client.DeleteOption) error {
				assert.NoError(t, cl.Delete(ctx, obj, opts...))
				if pod, ok := obj.(*corev1.Pod); ok {
					mu.Lock()
					actions[pod.Name] = append(actions[pod.Name], "Delete")
					mu.Unlock()
					pod.Labels[podRevisionLabel] = o.sts.Status.UpdateRevision
					pod.ResourceVersion = ""
					return cl.Create(ctx, pod)
				}
				return nil
			},
			Get: func(ctx context.Context, cl client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
				if _, ok := obj.(*corev1.Pod); ok {
					mu.Lock()
					actions[key.Name] = append(actions[key.Name], "Get")
					mu.Unlock()
				}
				return cl.Get(ctx, key, obj)
			},
		})
		err := performRollingUpdateOnSts(ctx, fclient, o.sts, o.opts)
		if o.wantErr {
			assert.Error(t, err)
			return
		} else {
			assert.NoError(t, err)
		}
		assert.Equal(t, actions, o.actions)
	}

	// rolling update is not needed
	f(opts{
		sts: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect-sts",
				Namespace: "default",
			},
			Status: appsv1.StatefulSetStatus{
				CurrentRevision: "rev1",
				UpdateRevision:  "rev1",
			},
		},
		opts: rollingUpdateOpts{
			selector:       map[string]string{"app": "vmselect"},
			maxUnavailable: 1,
		},
		predefinedObjects: []runtime.Object{
			&appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect"},
				},
				Status: appsv1.StatefulSetStatus{
					CurrentRevision: "rev1",
					UpdateRevision:  "rev1",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-0",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev1"},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: "True",
						},
					},
				},
			},
		},
		actions: make(map[string][]string),
	})

	// evict pods
	f(opts{
		sts: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect-sts",
				Namespace: "default",
			},
			Status: appsv1.StatefulSetStatus{
				CurrentRevision: "rev1",
				UpdateRevision:  "rev1",
			},
		},
		opts: rollingUpdateOpts{
			selector:       map[string]string{"app": "vmselect"},
			maxUnavailable: 4,
		},
		predefinedObjects: []runtime.Object{
			&appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect"},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(int32(4)),
				},
				Status: appsv1.StatefulSetStatus{
					CurrentRevision: "rev1",
					UpdateRevision:  "rev1",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-0",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev0"},
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "StatefulSet",
					}},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: "True",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev0"},
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "StatefulSet",
					}},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: "True",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-2",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev0"},
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "StatefulSet",
					}},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: "True",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-3",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev1"},
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "StatefulSet",
					}},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: "True",
						},
					},
				},
			},
		},
		actions: map[string][]string{
			"vmselect-sts-0": {"Evict", "Get"},
			"vmselect-sts-1": {"Evict", "Get"},
			"vmselect-sts-2": {"Evict", "Get"},
		},
	})

	// recreating pods
	f(opts{
		sts: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect-sts",
				Namespace: "default",
			},
			Status: appsv1.StatefulSetStatus{
				CurrentRevision: "rev1",
				UpdateRevision:  "rev1",
			},
		},
		opts: rollingUpdateOpts{
			selector:       map[string]string{"app": "vmselect"},
			maxUnavailable: 4,
			delete:         true,
		},
		predefinedObjects: []runtime.Object{
			&appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect"},
				},
				Spec: appsv1.StatefulSetSpec{
					Replicas: ptr.To(int32(4)),
				},
				Status: appsv1.StatefulSetStatus{
					CurrentRevision: "rev1",
					UpdateRevision:  "rev1",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-0",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev0"},
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "StatefulSet",
					}},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: "True",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev0"},
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "StatefulSet",
					}},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: "True",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-2",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev0"},
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "StatefulSet",
					}},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: "True",
						},
					},
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-3",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev1"},
					OwnerReferences: []metav1.OwnerReference{{
						Kind: "StatefulSet",
					}},
				},
				Status: corev1.PodStatus{
					Phase: corev1.PodRunning,
					Conditions: []corev1.PodCondition{
						{
							Type:   corev1.PodReady,
							Status: "True",
						},
					},
				},
			},
		},
		actions: map[string][]string{
			"vmselect-sts-0": {"Delete", "Get"},
			"vmselect-sts-1": {"Delete", "Get"},
			"vmselect-sts-2": {"Delete", "Get"},
		},
	})

	// rolling update is timeout
	f(opts{
		sts: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect-sts",
				Namespace: "default",
			},
			Status: appsv1.StatefulSetStatus{
				CurrentRevision: "rev1",
				UpdateRevision:  "rev1",
			},
		},
		opts: rollingUpdateOpts{
			selector:       map[string]string{"app": "vmselect"},
			maxUnavailable: 1,
		},
		predefinedObjects: []runtime.Object{
			&appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect"},
				},
				Spec: appsv1.StatefulSetSpec{Replicas: ptr.To(int32(4))},
				Status: appsv1.StatefulSetStatus{
					CurrentRevision: "rev1",
					UpdateRevision:  "rev2",
				},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-0",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev1"},
				},
				Status: corev1.PodStatus{},
			},
			&corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-sts-1",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect", podRevisionLabel: "rev2"},
				},
				Status: corev1.PodStatus{},
			},
		},
		wantErr: true,
	})
}

func TestSortPodsByID(t *testing.T) {
	f := func(unorderedPods []corev1.Pod, expectedOrder []corev1.Pod) {
		t.Helper()
		assert.NoError(t, sortStsPodsByID(unorderedPods))
		for idx, pod := range expectedOrder {
			assert.Equal(t, pod.Name, unorderedPods[idx].Name)
		}
	}
	podsFromNames := func(podNames []string) []corev1.Pod {
		dst := make([]corev1.Pod, 0, len(podNames))
		for _, name := range podNames {
			dst = append(dst, corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
		return dst
	}
	f(
		podsFromNames([]string{"sts-id-15", "sts-id-13", "sts-id-1", "sts-id-0", "sts-id-2", "sts-id-25"}),
		podsFromNames([]string{"sts-id-0", "sts-id-1", "sts-id-2", "sts-id-13", "sts-id-15", "sts-id-25"}),
	)
	f(
		podsFromNames([]string{"pod-1", "pod-0"}),
		podsFromNames([]string{"pod-0", "pod-1"}),
	)
}

func TestStatefulsetReconcile(t *testing.T) {
	type opts struct {
		new, prev         *appsv1.StatefulSet
		predefinedObjects []runtime.Object
		validate          func(*appsv1.StatefulSet)
		actions           []k8stools.ClientAction
		wantErr           bool
	}
	getSts := func(fns ...func(s *appsv1.StatefulSet)) *appsv1.StatefulSet {
		s := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-1",
				Namespace: "default",
			},
			Spec: appsv1.StatefulSetSpec{
				Replicas: ptr.To[int32](4),
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"label": "value",
					},
				},
				PodManagementPolicy: appsv1.OrderedReadyPodManagement,
				UpdateStrategy: appsv1.StatefulSetUpdateStrategy{
					Type: appsv1.RollingUpdateStatefulSetStrategyType,
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{"label": "value"},
					},
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name:            "vmalertmanager",
								ImagePullPolicy: "IfNowPresent",
								Image:           "some-image:tag",
							},
						},
					},
				},
			},
			Status: appsv1.StatefulSetStatus{
				ReadyReplicas:   4,
				UpdatedReplicas: 4,
			},
		}
		for _, fn := range fns {
			fn(s)
		}
		return s
	}
	f := func(o opts) {
		t.Helper()
		ctx := context.Background()
		cl := k8stools.GetTestClientWithActionsAndObjects(o.predefinedObjects)
		var emptyOpts STSOptions
		err := StatefulSet(ctx, cl, emptyOpts, o.new, o.prev, nil)
		if o.wantErr {
			assert.Error(t, err)
			return
		} else {
			assert.NoError(t, err)
		}
		nsn := types.NamespacedName{
			Name:      o.new.Name,
			Namespace: o.new.Namespace,
		}
		assert.Equal(t, o.actions, cl.Actions)
		if o.validate != nil {
			var got appsv1.StatefulSet
			assert.NoError(t, cl.Get(ctx, nsn, &got))
			o.validate(&got)
		}
	}

	nn := types.NamespacedName{Name: "test-1", Namespace: "default"}

	// create statefulset
	f(opts{
		new: getSts(),
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
			{Verb: "Create", Kind: "StatefulSet", Resource: nn},
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
		},
	})

	// no updates
	f(opts{
		new:  getSts(),
		prev: getSts(),
		predefinedObjects: []runtime.Object{
			getSts(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
		},
	})

	// add annotations
	f(opts{
		new: getSts(func(s *appsv1.StatefulSet) {
			s.Spec.Template.Annotations = map[string]string{"new-annotation": "value"}
		}),
		prev: getSts(),
		predefinedObjects: []runtime.Object{
			getSts(),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
			{Verb: "Update", Kind: "StatefulSet", Resource: nn},
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
		},
		validate: func(s *appsv1.StatefulSet) {
			assert.Equal(t, "value", s.Spec.Template.Annotations["new-annotation"])
		},
	})

	// no update on status change
	f(opts{
		new:  getSts(),
		prev: getSts(),
		predefinedObjects: []runtime.Object{
			getSts(func(s *appsv1.StatefulSet) {
				s.Status.Replicas = 1
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
		},
	})

	// recreate on ServiceName change
	f(opts{
		new: getSts(func(s *appsv1.StatefulSet) {
			s.Spec.ServiceName = "new-service-name"
		}),
		prev: getSts(),
		predefinedObjects: []runtime.Object{
			getSts(func(s *appsv1.StatefulSet) {
				s.Finalizers = []string{vmv1beta1.FinalizerName}
				s.Spec.ServiceName = "old-service-name"
			}),
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
			{Verb: "Patch", Kind: "StatefulSet", Resource: nn},
			{Verb: "Delete", Kind: "StatefulSet", Resource: nn},
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
			{Verb: "Create", Kind: "StatefulSet", Resource: nn},
			{Verb: "Get", Kind: "StatefulSet", Resource: nn},
		},
	})

	// context deadline error
	f(opts{
		new: getSts(func(s *appsv1.StatefulSet) {
			s.Status.ReadyReplicas = 3
		}),
		prev: getSts(),
		predefinedObjects: []runtime.Object{
			getSts(func(s *appsv1.StatefulSet) {
				s.Status.ReadyReplicas = 3
			}),
		},
		wantErr: true,
	})
}

func TestValidateStatefulSetFail(t *testing.T) {
	f := func(sts appsv1.StatefulSet) {
		t.Helper()
		assert.Error(t, validateStatefulSet(&sts))
	}
	// missing volume name
	f(appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{},
					Containers: []corev1.Container{
						{
							Name: "vmbackup",
							VolumeMounts: []corev1.VolumeMount{
								{Name: "configmap-access"},
							},
						},
					},
				},
			},
		},
	})
	// duplicate volume names
	f(appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "data"}},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "secret-access",
						},
						{
							Name: "data",
						},
					},
					Containers: []corev1.Container{
						{
							Name: "storage",
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data"},
							},
						},
						{
							Name: "vmbackuper",
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data"},
								{Name: "secret-access"},
							},
						},
					},
				},
			},
		},
	})
	// duplicate volumes
	f(appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "data"}},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "data",
						},
						{
							Name: "data",
						},
					},
				},
			},
		},
	})
	// duplicate claim templates
	f(appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "data"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "data"}},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{},
			},
		},
	})

}

func TestValidateStatefulSetOk(t *testing.T) {
	f := func(sts appsv1.StatefulSet) {
		t.Helper()
		assert.NoError(t, validateStatefulSet(&sts))
	}
	// empty case
	f(appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{},
				},
			},
		},
	})
	// reference for claims and volumes
	f(appsv1.StatefulSet{
		Spec: appsv1.StatefulSetSpec{
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "data"}},
			},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "secret-access",
						},
					},
					Containers: []corev1.Container{
						{
							Name: "storage",
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data"},
							},
						},
						{
							Name: "vmbackuper",
							VolumeMounts: []corev1.VolumeMount{
								{Name: "data"},
								{Name: "secret-access"},
							},
						},
					},
				},
			},
		},
	})
}
