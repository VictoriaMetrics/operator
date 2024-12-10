package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_waitForPodReady(t *testing.T) {
	type args struct {
		ns      string
		podName string
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "testing pod with unready status",
			args: args{
				ns:      "default",
				podName: "vmselect-example-0",
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
		},
		{
			name: "testing pod with ready status",
			args: args{
				ns:      "default",
				podName: "vmselect-example-0",
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
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			if err := waitForPodReady(context.Background(), fclient, tt.args.ns, tt.args.podName, 0); (err != nil) != tt.wantErr {
				t.Errorf("waitForPodReady() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_podIsReady(t *testing.T) {
	type args struct {
		pod             corev1.Pod
		minReadySeconds int32
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "pod is ready",
			args: args{
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
			},
			want: true,
		},
		{
			name: "pod is unready",
			args: args{
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
			},
			want: false,
		},
		{
			name: "pod is deleted",
			args: args{
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
			},
			want: false,
		},
		{
			name: "pod is not min ready",
			args: args{
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
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PodIsReady(&tt.args.pod, tt.args.minReadySeconds); got != tt.want {
				t.Errorf("PodIsReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_performRollingUpdateOnSts(t *testing.T) {
	type args struct {
		stsName   string
		ns        string
		podLabels map[string]string
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects []runtime.Object
		neededPodRev      string
	}{
		{
			name: "rolling update is not needed",
			args: args{
				stsName:   "vmselect-sts",
				ns:        "default",
				podLabels: map[string]string{"app": "vmselect"},
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
		},
		{
			name: "rolling update is timeout",
			args: args{
				stsName:   "vmselect-sts",
				ns:        "default",
				podLabels: map[string]string{"app": "vmselect"},
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := k8stools.GetTestClientWithObjects(tt.predefinedObjects)

			if err := performRollingUpdateOnSts(context.Background(), false, fclient, tt.args.stsName, tt.args.ns, tt.args.podLabels); (err != nil) != tt.wantErr {
				t.Errorf("performRollingUpdateOnSts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSortPodsByID(t *testing.T) {
	f := func(unorderedPods []corev1.Pod, expectedOrder []corev1.Pod) {
		t.Helper()
		if err := sortStsPodsByID(unorderedPods); err != nil {
			t.Fatalf("unexpected error during pod sorting: %s", err)
		}
		for idx, pod := range expectedOrder {
			if pod.Name != unorderedPods[idx].Name {
				t.Fatalf("order mismatch want pod: %s at idx: %d, got: %s", pod.Name, idx, unorderedPods[idx].Name)
			}
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
		podsFromNames([]string{"sts-id-0", "sts-id-1", "sts-id-2", "sts-id-13", "sts-id-15", "sts-id-25"}))
	f(
		podsFromNames([]string{"pod-1", "pod-0"}),
		podsFromNames([]string{"pod-0", "pod-1"}))
}

func TestStatefulsetReconcileOk(t *testing.T) {
	f := func(sts *appsv1.StatefulSet) {
		//	t.Helper()
		ctx := context.Background()
		rclient := k8stools.GetTestClientWithObjects(nil)
		clientStats := rclient.(*k8stools.TestClientWithStatsTrack)

		waitTimeout := 5 * time.Second
		prevSts := sts.DeepCopy()
		createErr := make(chan error)
		var emptyOpts STSOptions
		go func() {
			err := HandleSTSUpdate(ctx, rclient, emptyOpts, sts, nil)
			select {
			case createErr <- err:
			default:
			}
		}()
		reloadSts := func() {
			t.Helper()
			if err := rclient.Get(ctx, types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace}, sts); err != nil {
				t.Fatalf("cannot reload created statefulset: %s", err)
			}
		}

		err := wait.PollUntilContextTimeout(ctx, time.Millisecond*50,
			waitTimeout, false, func(ctx context.Context) (done bool, err error) {
				var createdSts appsv1.StatefulSet
				if err := rclient.Get(ctx, types.NamespacedName{Name: sts.Name, Namespace: sts.Namespace}, &createdSts); err != nil {
					if errors.IsNotFound(err) {
						return false, nil
					}
					return false, err
				}
				createdSts.Status.ReadyReplicas = *sts.Spec.Replicas
				createdSts.Status.UpdatedReplicas = *sts.Spec.Replicas
				if err := rclient.Status().Update(ctx, &createdSts); err != nil {
					return false, err
				}
				return true, nil
			})
		assert.NoErrorf(t, err, "failed to wait sts to be created")

		err = <-createErr
		assert.NoErrorf(t, err, "failed to create sts")

		// expect 1 create
		assert.Equal(t, int64(1), clientStats.CreateCalls.Load())
		// expect 0 update
		assert.NoErrorf(t, HandleSTSUpdate(ctx, rclient, emptyOpts, sts, prevSts), "expect 0 update")

		assert.Equal(t, int64(1), clientStats.CreateCalls.Load())
		assert.Equal(t, int64(0), clientStats.UpdateCalls.Load())

		// expect 1 UpdateCalls
		reloadSts()
		sts.Spec.Template.ObjectMeta.Annotations = map[string]string{"new-annotation": "value"}

		assert.NoErrorf(t, HandleSTSUpdate(ctx, rclient, emptyOpts, sts, prevSts), "expect 1 update")

		assert.Equal(t, int64(1), clientStats.CreateCalls.Load())
		assert.Equal(t, int64(1), clientStats.UpdateCalls.Load())

		// expected still same 1 update
		reloadSts()

		assert.NoErrorf(t, HandleSTSUpdate(ctx, rclient, emptyOpts, sts, prevSts), "expect still 1 update")
		assert.Equal(t, int64(1), clientStats.CreateCalls.Load())
		assert.Equal(t, int64(1), clientStats.UpdateCalls.Load())

		// expected 2 updates
		prevSts.Spec.Template.ObjectMeta.Annotations = sts.Spec.Template.ObjectMeta.Annotations
		sts.Spec.Template.ObjectMeta.Annotations = nil

		assert.NoErrorf(t, HandleSTSUpdate(ctx, rclient, emptyOpts, sts, prevSts), "expect 2 updates")
		assert.Equal(t, int64(1), clientStats.CreateCalls.Load())
		assert.Equal(t, int64(2), clientStats.UpdateCalls.Load())
	}

	f(&appsv1.StatefulSet{
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
	})
}
