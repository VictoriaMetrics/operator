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
	type opts struct {
		newSTS          *appsv1.StatefulSet
		existingSTS     *appsv1.StatefulSet
		validate        func(sts *appsv1.StatefulSet) error
		recreatedSTS    bool
		mustRecreatePod bool
	}
	f := func(opts opts) {
		t.Helper()
		ctx := context.TODO()
		cl := k8stools.GetTestClientWithObjects([]runtime.Object{opts.existingSTS})
		actualRecreatedSTS, actualMustRecreatePod, err := recreateSTSIfNeed(ctx, cl, opts.newSTS, opts.existingSTS)
		if err != nil {
			t.Errorf("recreateSTSIfNeed() error = %v", err)
			return
		}
		var updatedSts appsv1.StatefulSet
		if err := cl.Get(ctx, types.NamespacedName{Namespace: opts.newSTS.Namespace, Name: opts.newSTS.Name}, &updatedSts); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if err := opts.validate(&updatedSts); err != nil {
			t.Fatalf("sts validation failed: %v", err)
		}
		if actualRecreatedSTS != opts.recreatedSTS {
			t.Fatalf("expect `stsRecreated`: %v, got: %v", opts.recreatedSTS, actualRecreatedSTS)
		}
		if actualMustRecreatePod != opts.mustRecreatePod {
			t.Fatalf("expect `mustRecreatePod`: %v, got: %v", opts.mustRecreatePod, actualMustRecreatePod)
		}
	}

	// add claim to sts
	o := opts{
		newSTS: &appsv1.StatefulSet{
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
		},
		existingSTS: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect",
				Namespace: "default",
			},
		},
		validate: func(sts *appsv1.StatefulSet) error {
			if len(sts.Spec.VolumeClaimTemplates) != 1 {
				return fmt.Errorf("unexpected configuration for volumeclaim at sts: %v, want at least one, got: %v", sts.Name, sts.Spec.VolumeClaimTemplates)
			}
			return nil
		},
		recreatedSTS:    true,
		mustRecreatePod: true,
	}
	f(o)

	// resize claim at sts
	o = opts{
		newSTS: &appsv1.StatefulSet{
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
		},
		existingSTS: &appsv1.StatefulSet{
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
		},
		validate: func(sts *appsv1.StatefulSet) error {
			if len(sts.Spec.VolumeClaimTemplates) != 1 {
				return fmt.Errorf("unexpected configuration for volumeclaim at sts: %v, want at least one, got: %v", sts.Name, sts.Spec.VolumeClaimTemplates)
			}
			sz := sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String()
			if sz != "15Gi" {
				return fmt.Errorf("unexpected sts size, got: %v, want: %v", sz, "15Gi")
			}
			return nil
		},
		recreatedSTS: true,
	}
	f(o)

	// change claim storageClass name
	o = opts{
		newSTS: &appsv1.StatefulSet{
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
		},
		existingSTS: &appsv1.StatefulSet{
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
		},
		validate: func(sts *appsv1.StatefulSet) error {
			if len(sts.Spec.VolumeClaimTemplates) != 1 {
				return fmt.Errorf("unexpected configuration for volumeclaim at sts: %v, want at least one, got: %v", sts.Name, sts.Spec.VolumeClaimTemplates)
			}
			name := *sts.Spec.VolumeClaimTemplates[0].Spec.StorageClassName
			if name != "new-sc" {
				return fmt.Errorf("unexpected sts storageClass name, got: %v, want: %v", name, "new-sc")
			}
			return nil
		},
		recreatedSTS: true,
	}
	f(o)

	// change serviceName
	o = opts{
		newSTS: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: appsv1.StatefulSetSpec{
				ServiceName: "new-service",
			},
		},
		existingSTS: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: appsv1.StatefulSetSpec{
				ServiceName: "old-service",
			},
		},
		validate: func(sts *appsv1.StatefulSet) error {
			if sts.Spec.ServiceName != "new-service" {
				return fmt.Errorf("unexpected serviceName at sts: %s, want: %s", sts.Spec.ServiceName, "new-service")
			}
			return nil
		},
		recreatedSTS:    true,
		mustRecreatePod: true,
	}
	f(o)
}

func Test_growSTSPVC(t *testing.T) {
	type opts struct {
		sts               *appsv1.StatefulSet
		predefinedObjects []runtime.Object
	}
	f := func(opts opts) {
		t.Helper()
		ctx := context.TODO()
		cl := k8stools.GetTestClientWithObjects(opts.predefinedObjects)
		if err := growSTSPVC(ctx, cl, opts.sts); err != nil {
			t.Errorf("growSTSPVC() error = %v", err)
		}
	}

	// no need to expand
	o := opts{
		sts: &appsv1.StatefulSet{
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
		},
		predefinedObjects: []runtime.Object{
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
		},
	}
	f(o)

	// expand successfully
	o = opts{
		sts: &appsv1.StatefulSet{
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
		},
		predefinedObjects: []runtime.Object{
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
		},
	}
	f(o)

	// failed with non-expandable sc
	o = opts{
		sts: &appsv1.StatefulSet{
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
		},
		predefinedObjects: []runtime.Object{
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
		},
	}
	f(o)

	// expand with named class
	o = opts{
		sts: &appsv1.StatefulSet{
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
		},
		predefinedObjects: []runtime.Object{
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
		},
	}
	f(o)
}
