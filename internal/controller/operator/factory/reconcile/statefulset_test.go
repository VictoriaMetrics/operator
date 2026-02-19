package reconcile

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_waitForPodReady(t *testing.T) {
	type opts struct {
		ns                string
		podName           string
		desiredVersion    string
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		nsn := types.NamespacedName{Namespace: o.ns, Name: o.podName}
		if err := waitForPodReady(context.Background(), fclient, nsn, o.desiredVersion, 0); (err != nil) != o.wantErr {
			t.Errorf("waitForPodReady() error = %v, wantErr %v", err, o.wantErr)
		}
	}

	// testing pod with unready status
	f(opts{
		ns:      "default",
		podName: "vmselect-example-0",
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
		ns:      "default",
		podName: "vmselect-example-0",
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
		ns:             "default",
		podName:        "vmselect-example-0",
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
		ns:             "default",
		podName:        "vmselect-example-0",
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
		if got := PodIsReady(&o.pod, o.minReadySeconds); got != o.want {
			t.Errorf("PodIsReady() = %v, want %v", got, o.want)
		}
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
		stsName           string
		ns                string
		podLabels         map[string]string
		podMaxUnavailable int
		wantErr           bool
		predefinedObjects []runtime.Object
	}
	f := func(o opts) {
		t.Helper()
		fclient := k8stools.GetTestClientWithObjects(o.predefinedObjects)

		if err := performRollingUpdateOnSts(context.Background(), false, fclient, o.stsName, o.ns, o.podLabels, o.podMaxUnavailable); (err != nil) != o.wantErr {
			t.Errorf("performRollingUpdateOnSts() error = %v, wantErr %v", err, o.wantErr)
		}
	}

	// rolling update is not needed
	f(opts{
		stsName:           "vmselect-sts",
		ns:                "default",
		podLabels:         map[string]string{"app": "vmselect"},
		podMaxUnavailable: 1,
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
	})

	// rolling update is timeout
	f(opts{
		stsName:           "vmselect-sts",
		ns:                "default",
		podLabels:         map[string]string{"app": "vmselect"},
		podMaxUnavailable: 1,
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
		actions           []string
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
		cl := getTestClient(o.new, o.predefinedObjects)
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
		assert.Equal(t, o.actions, cl.actions)
		if o.validate != nil {
			var got appsv1.StatefulSet
			assert.NoError(t, cl.Get(ctx, nsn, &got))
			o.validate(&got)
		}
	}

	// create statefulset
	f(opts{
		new:     getSts(),
		actions: []string{"Get", "Create", "Get"},
	})

	// no updates
	f(opts{
		new:  getSts(),
		prev: getSts(),
		predefinedObjects: []runtime.Object{
			getSts(func(s *appsv1.StatefulSet) {
				s.Finalizers = []string{vmv1beta1.FinalizerName}
			}),
		},
		actions: []string{"Get", "Get"},
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
		actions: []string{"Get", "Update", "Get"},
		validate: func(s *appsv1.StatefulSet) {
			assert.Equal(t, "value", s.Spec.Template.Annotations["new-annotation"])
		},
	})

	// delete annotations
	f(opts{
		new: getSts(),
		prev: getSts(func(s *appsv1.StatefulSet) {
			s.Spec.Template.Annotations = map[string]string{"new-annotation": "value"}
		}),
		predefinedObjects: []runtime.Object{
			getSts(func(s *appsv1.StatefulSet) {
				s.Spec.Template.Annotations = map[string]string{"new-annotation": "value"}
			}),
		},
		actions: []string{"Get", "Update", "Get"},
		validate: func(s *appsv1.StatefulSet) {
			assert.Empty(t, s.Spec.Template.Annotations["new-annotation"])
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
