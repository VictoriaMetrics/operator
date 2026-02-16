package reconcile

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_reCreateSTS(t *testing.T) {
	type opts struct {
		newSTS          *appsv1.StatefulSet
		oldSTS          *appsv1.StatefulSet
		validate        func(sts *appsv1.StatefulSet) error
		mustRecreateSTS bool
		mustRecreatePod bool
	}
	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects([]runtime.Object{o.oldSTS})
		t.Helper()
		ctx := context.TODO()
		mustRecreateSTS, mustRecreatePod := isSTSRecreateRequired(ctx, o.newSTS, o.oldSTS)
		if mustRecreateSTS {
			assert.NoError(t, removeStatefulSetKeepPods(ctx, cl, o.newSTS, o.oldSTS))
		}
		var updatedSTS appsv1.StatefulSet
		nsn := types.NamespacedName{Namespace: o.newSTS.Namespace, Name: o.newSTS.Name}
		assert.NoError(t, cl.Get(ctx, nsn, &updatedSTS))
		assert.NoError(t, o.validate(&updatedSTS))
		assert.Equal(t, mustRecreateSTS, o.mustRecreateSTS)
		assert.Equal(t, mustRecreatePod, o.mustRecreatePod)
	}

	// add claim to sts
	f(opts{
		oldSTS: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect",
				Namespace: "default",
			},
		},
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
		validate: func(sts *appsv1.StatefulSet) error {
			if len(sts.Spec.VolumeClaimTemplates) != 1 {
				return fmt.Errorf("unexpected configuration for volumeclaim at sts: %v, want at least one, got: %v", sts.Name, sts.Spec.VolumeClaimTemplates)
			}
			return nil
		},
		mustRecreateSTS: true,
		mustRecreatePod: true,
	})

	// resize claim at sts
	f(opts{
		oldSTS: &appsv1.StatefulSet{
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
					},
				},
			}},
		},
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
								corev1.ResourceStorage: resource.MustParse("15Gi"),
							},
						},
					},
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
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// change claim storageClass name
	f(opts{
		oldSTS: &appsv1.StatefulSet{
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
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// change serviceName
	f(opts{
		oldSTS: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: appsv1.StatefulSetSpec{
				ServiceName: "old-service",
			},
		},
		newSTS: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: appsv1.StatefulSetSpec{
				ServiceName: "new-service",
			},
		},
		validate: func(sts *appsv1.StatefulSet) error {
			if sts.Spec.ServiceName != "new-service" {
				return fmt.Errorf("unexpected serviceName at sts: %s, want: %s", sts.Spec.ServiceName, "new-service")
			}
			return nil
		},
		mustRecreateSTS: true,
		mustRecreatePod: true,
	})
}

func Test_updateSTSPVC(t *testing.T) {
	type opts struct {
		sts               *appsv1.StatefulSet
		wantErr           bool
		predefinedObjects []runtime.Object
		expected          []corev1.PersistentVolumeClaim
	}
	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects(o.predefinedObjects)
		ctx := context.TODO()
		err := updateSTSPVC(ctx, cl, o.sts, nil)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		var pvcs corev1.PersistentVolumeClaimList
		opts := &client.ListOptions{
			Namespace:     o.sts.Namespace,
			LabelSelector: labels.SelectorFromSet(o.sts.Spec.Selector.MatchLabels),
		}
		assert.NoError(t, cl.List(ctx, &pvcs, opts))
		assert.ElementsMatch(t, o.expected, pvcs.Items)
	}

	// update metadata only
	f(opts{
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
							Annotations: map[string]string{
								"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
								"test": "after",
							},
							Labels: map[string]string{
								"app": "vmselect",
							},
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
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-cachedir-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
						"test": "after",
					},
					ResourceVersion: "1000",
					Finalizers: []string{
						vmv1beta1.FinalizerName,
					},
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
		predefinedObjects: []runtime.Object{
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-cachedir-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
						"test": "before",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
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
	})

	// expand successfully
	f(opts{
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
							Labels: map[string]string{
								"app": "vmselect",
							},
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
							Labels: map[string]string{
								"app": "vmselect",
							},
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
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
					ResourceVersion: "1000",
					Finalizers: []string{
						vmv1beta1.FinalizerName,
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("5Gi"),
						},
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-cachedir-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
					ResourceVersion: "1000",
					Finalizers: []string{
						vmv1beta1.FinalizerName,
					},
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
		predefinedObjects: []runtime.Object{
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-cachedir-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			},
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("3Gi"),
						},
					},
				},
			},
			&storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standard",
					Annotations: map[string]string{
						"storageclass.kubernetes.io/is-default-class": "true",
					},
				},
				AllowVolumeExpansion: ptr.To(true),
			},
		},
	})

	// failed with non-expandable sc
	f(opts{
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
							Labels: map[string]string{
								"app": "vmselect",
							},
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
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-cachedir-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
					ResourceVersion: "1000",
					Finalizers: []string{
						vmv1beta1.FinalizerName,
					},
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
		predefinedObjects: []runtime.Object{
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-cachedir-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
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
	})

	// expand with named class
	f(opts{
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
							Labels: map[string]string{
								"app": "vmselect",
							},
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
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-cachedir-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
					ResourceVersion: "999",
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
		predefinedObjects: []runtime.Object{
			&corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "vmselect-cachedir-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			},
			&storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standard",
					Annotations: map[string]string{
						"storageclass.kubernetes.io/is-default-class": "true",
					},
				},
				AllowVolumeExpansion: ptr.To(true),
			},
			&storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ssd",
					Annotations: map[string]string{
						"storageclass.kubernetes.io/is-default-class": "false",
					},
				},
				AllowVolumeExpansion: ptr.To(true),
			},
		},
	})
}
