package k8stools

import (
	"context"
	"testing"
	"time"

	"github.com/VictoriaMetrics/operator/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
)

func Test_waitForPodReady(t *testing.T) {

	type args struct {
		ns      string
		podName string
		c       *config.BaseOperatorConf
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
				c:       &config.BaseOperatorConf{PodWaitReadyIntervalCheck: time.Second * 1, PodWaitReadyInitDelay: time.Second, PodWaitReadyTimeout: time.Second * 4},
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
				c:       &config.BaseOperatorConf{PodWaitReadyIntervalCheck: time.Second * 1, PodWaitReadyInitDelay: time.Second, PodWaitReadyTimeout: time.Second * 4},
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
			fclient := GetTestClientWithObjects(tt.predefinedObjects)

			if err := waitForPodReady(context.Background(), fclient, tt.args.ns, tt.args.podName, tt.args.c, nil); (err != nil) != tt.wantErr {
				t.Errorf("waitForPodReady() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func Test_podIsReady(t *testing.T) {
	type args struct {
		pod corev1.Pod
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PodIsReady(tt.args.pod); got != tt.want {
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
		c         *config.BaseOperatorConf
	}
	tests := []struct {
		name                string
		args                args
		wantErr             bool
		predefinedObjects   []runtime.Object
		updatePodRevByIndex *int32
		neededPodRev        string
	}{
		{
			name: "rolling update is not needed",
			args: args{
				stsName:   "vmselect-sts",
				ns:        "default",
				c:         &config.BaseOperatorConf{},
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
				stsName: "vmselect-sts",
				ns:      "default",
				c: &config.BaseOperatorConf{
					PodWaitReadyTimeout:       time.Second * 2,
					PodWaitReadyInitDelay:     time.Millisecond,
					PodWaitReadyIntervalCheck: time.Second,
				},
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
		{
			name: "rolling update is needed with update",
			args: args{
				stsName: "vmselect-sts",
				ns:      "default",
				c: &config.BaseOperatorConf{
					PodWaitReadyTimeout:       time.Second * 2,
					PodWaitReadyInitDelay:     time.Millisecond,
					PodWaitReadyIntervalCheck: time.Second,
				},
				podLabels: map[string]string{"app": "vmselect"},
			},

			predefinedObjects: []runtime.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-sts",
						Namespace: "default",
						Labels:    map[string]string{"app": "vmselect"},
					},
					Spec: appsv1.StatefulSetSpec{Replicas: pointer.Int32Ptr(2)},
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
			updatePodRevByIndex: pointer.Int32Ptr(1),
			neededPodRev:        "rev2",
			wantErr:             false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fclient := GetTestClientWithObjects(tt.predefinedObjects)
			if tt.updatePodRevByIndex != nil {
				podInd := tt.predefinedObjects[int(*tt.updatePodRevByIndex)]
				pod := podInd.(*corev1.Pod)
				go func(pod *corev1.Pod, rev string) {
					time.Sleep(time.Millisecond * 1200)
					pod.ObjectMeta.Labels[podRevisionLabel] = rev
					pod.Status.Phase = corev1.PodRunning
					pod.Status.Conditions = []corev1.PodCondition{
						{
							Status: "True",
							Type:   corev1.PodReady,
						},
					}
					err := fclient.Update(context.Background(), pod)
					if err != nil {
						t.Errorf("cannot update pod for rolling update check: %s", err)
					}
					if err := fclient.Status().Update(context.Background(), pod); err != nil {
						t.Errorf("cannot update pod status for update check: %s", err)
					}
				}(pod, tt.neededPodRev)
			}

			if err := performRollingUpdateOnSts(context.Background(), false, fclient, tt.args.stsName, tt.args.ns, tt.args.podLabels, tt.args.c); (err != nil) != tt.wantErr {
				t.Errorf("performRollingUpdateOnSts() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
