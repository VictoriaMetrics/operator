package reconcile

import (
	"context"
	"fmt"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_reCreateSTS(t *testing.T) {
	f := func(newSTS, existingSTS *appsv1.StatefulSet, validate func(sts *appsv1.StatefulSet) error, stsRecreated, mustRecreatePod bool) {
		t.Helper()
		ctx := context.TODO()
		cl := k8stools.GetTestClientWithObjects([]runtime.Object{existingSTS})
		actualStsRecreated, actualMustRecreatePod, err := recreateSTSIfNeed(ctx, cl, newSTS, existingSTS)
		if err != nil {
			t.Errorf("recreateSTSIfNeed() error = %v", err)
			return
		}
		var updatedSts appsv1.StatefulSet
		if err := cl.Get(ctx, types.NamespacedName{Namespace: newSTS.Namespace, Name: newSTS.Name}, &updatedSts); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := validate(&updatedSts); err != nil {
			t.Fatalf("sts validation failed: %v", err)
		}
		if actualStsRecreated != stsRecreated {
			t.Fatalf("expect `stsRecreated`: %v, got: %v", stsRecreated, actualStsRecreated)
		}
		if actualMustRecreatePod != mustRecreatePod {
			t.Fatalf("expect `mustRecreatePod`: %v, got: %v", mustRecreatePod, actualMustRecreatePod)
		}
	}

	// add claim to sts
	f(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{},
				},
			},
		}},
	}, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect",
			Namespace: "default",
		},
	}, func(sts *appsv1.StatefulSet) error {
		if len(sts.Spec.VolumeClaimTemplates) != 1 {
			return fmt.Errorf("unexpected configuration for volumeclaim at sts: %v, want at least one, got: %v", sts.Name, sts.Spec.VolumeClaimTemplates)
		}
		return nil
	}, true, true)

	// resize claim at sts
	f(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
				Spec: corev1.PersistentVolumeClaimSpec{Resources: corev1.VolumeResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("15Gi"),
					},
				}},
			},
		}},
	}, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
				Spec: corev1.PersistentVolumeClaimSpec{Resources: corev1.VolumeResourceRequirements{
					Requests: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				}},
			},
		}},
	}, func(sts *appsv1.StatefulSet) error {
		if len(sts.Spec.VolumeClaimTemplates) != 1 {
			return fmt.Errorf("unexpected configuration for volumeclaim at sts: %v, want at least one, got: %v", sts.Name, sts.Spec.VolumeClaimTemplates)
		}
		sz := sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String()
		if sz != "15Gi" {
			return fmt.Errorf("unexpected sts size, got: %v, want: %v", sz, "15Gi")
		}
		return nil
	}, true, false)

	// change claim storageClass name
	f(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
					StorageClassName: ptr.To("new-sc"),
				},
			},
		}},
	}, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
					StorageClassName: ptr.To("old-sc"),
				},
			},
		}},
	}, func(sts *appsv1.StatefulSet) error {
		if len(sts.Spec.VolumeClaimTemplates) != 1 {
			return fmt.Errorf("unexpected configuration for volumeclaim at sts: %v, want at least one, got: %v", sts.Name, sts.Spec.VolumeClaimTemplates)
		}
		name := *sts.Spec.VolumeClaimTemplates[0].Spec.StorageClassName
		if name != "new-sc" {
			return fmt.Errorf("unexpected sts storageClass name, got: %v, want: %v", name, "new-sc")
		}
		return nil
	}, true, false)

	// change serviceName
	f(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmagent",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "new-service",
		},
	}, &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmagent",
			Namespace: "default",
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: "old-service",
		},
	}, func(sts *appsv1.StatefulSet) error {
		if sts.Spec.ServiceName != "new-service" {
			return fmt.Errorf("unexpected serviceName at sts: %s, want: %s", sts.Spec.ServiceName, "new-service")
		}
		return nil
	}, true, true)
}

func Test_growSTSPVC(t *testing.T) {
	f := func(sts *appsv1.StatefulSet, predefinedObjects []runtime.Object) {
		t.Helper()
		ctx := context.TODO()
		cl := k8stools.GetTestClientWithObjects(predefinedObjects)
		if err := growSTSPVC(ctx, cl, sts); err != nil {
			t.Errorf("growSTSPVC() error = %v", err)
		}
	}

	// no need to expand
	f(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect",
			Namespace: "default",
			Labels: map[string]string{
				"app": "vmselect",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "vmselect",
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmselect-cachedir",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			},
		},
	}, []runtime.Object{
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect-cachedir-vmselect-insight-victoria-metrics-k8s-stack-0",
				Namespace: "default",
				Labels: map[string]string{
					"app": "vmselect",
				},
				Annotations: map[string]string{"operator.victoriametrics.com/pvc/allow-volume-expansion": "true"},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				}},
			},
		},
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "standard",
				Annotations: map[string]string{
					"volume.beta.kubernetes.io/storage-class": "true",
				},
			},
		},
	})

	// expand successfully
	f(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect",
			Namespace: "default",
			Labels: map[string]string{
				"app": "vmselect",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "vmselect",
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmselect-cachedir",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("15Gi"),
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("5Gi"),
							},
						},
					},
				},
			},
		},
	}, []runtime.Object{
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect-cachedir-vmselect-insight-victoria-metrics-k8s-stack-0",
				Namespace: "default",
				Labels: map[string]string{
					"app": "vmselect",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				}},
			},
		},
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-vmselect-insight-victoria-metrics-k8s-stack-0",
				Namespace: "default",
				Labels: map[string]string{
					"app": "vmselect",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("3Gi"),
				}},
			},
		},
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "standard",
				Annotations: map[string]string{
					"storageclass.kubernetes.io/is-default-class": "true",
				},
			},
			AllowVolumeExpansion: func() *bool { b := true; return &b }(),
		},
	})

	// failed with non-expandable sc
	f(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect",
			Namespace: "default",
			Labels: map[string]string{
				"app": "vmselect",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "vmselect",
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmselect-cachedir",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("15Gi"),
							},
						},
					},
				},
			},
		},
	}, []runtime.Object{
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect-cachedir-vmselect-insight-victoria-metrics-k8s-stack-0",
				Namespace: "default",
				Labels: map[string]string{
					"app": "vmselect",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				}},
			},
		},
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "standard",
				Annotations: map[string]string{
					"storageclass.kubernetes.io/is-default-class": "true",
				},
			},
		},
	})

	// expand with named class
	f(&appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "vmselect-cachedir",
			Namespace: "default",
			Labels: map[string]string{
				"app": "vmselect",
			},
		},
		Spec: appsv1.StatefulSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "vmselect",
				},
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmselect",
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						StorageClassName: ptr.To("ssd"),
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("15Gi"),
							},
						},
					},
				},
			},
		},
	}, []runtime.Object{
		&corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect-cachedir-vmselect-insight-victoria-metrics-k8s-stack-0",
				Namespace: "default",
				Labels: map[string]string{
					"app": "vmselect",
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				Resources: corev1.VolumeResourceRequirements{Requests: map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse("10Gi"),
				}},
			},
		},
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "standard",
				Annotations: map[string]string{
					"storageclass.kubernetes.io/is-default-class": "true",
				},
			},
			AllowVolumeExpansion: func() *bool { b := true; return &b }(),
		},
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ssd",
				Annotations: map[string]string{
					"storageclass.kubernetes.io/is-default-class": "false",
				},
			},
			AllowVolumeExpansion: func() *bool { b := true; return &b }(),
		},
	})
}
