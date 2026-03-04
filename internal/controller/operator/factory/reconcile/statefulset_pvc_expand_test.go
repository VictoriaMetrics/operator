package reconcile

import (
	"context"
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

	"github.com/VictoriaMetrics/operator/internal/controller/operator/factory/k8stools"
)

func Test_recreateOrUpdateSTS(t *testing.T) {
	type opts struct {
		newObj          *appsv1.StatefulSet
		existingObj     *appsv1.StatefulSet
		validate        func(sts *appsv1.StatefulSet)
		mustRecreateSTS bool
		mustRecreatePod bool
	}
	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithObjects([]runtime.Object{o.existingObj})
		t.Helper()
		ctx := context.TODO()
		mustRecreateSTS, mustRecreatePod := isSTSRecreateRequired(ctx, o.existingObj, o.newObj, nil)
		if mustRecreateSTS {
			assert.NoError(t, removeStatefulSetKeepPods(ctx, cl, o.existingObj, o.newObj))
		} else {
			newObj := o.existingObj.DeepCopy()
			newObj.Spec = o.newObj.Spec
			assert.NoError(t, cl.Update(ctx, newObj))
		}
		var updatedSTS appsv1.StatefulSet
		nsn := types.NamespacedName{Namespace: o.newObj.Namespace, Name: o.newObj.Name}
		assert.NoError(t, cl.Get(ctx, nsn, &updatedSTS))
		o.validate(&updatedSTS)
		assert.Equal(t, mustRecreateSTS, o.mustRecreateSTS)
		assert.Equal(t, mustRecreatePod, o.mustRecreatePod)
	}

	// add claim to sts
	f(opts{
		existingObj: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect",
				Namespace: "default",
			},
		},
		newObj: &appsv1.StatefulSet{
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
		validate: func(sts *appsv1.StatefulSet) {
			assert.Len(t, sts.Spec.VolumeClaimTemplates, 1)
		},
		mustRecreateSTS: true,
		mustRecreatePod: true,
	})

	// resize claim at sts
	f(opts{
		existingObj: &appsv1.StatefulSet{
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
		newObj: &appsv1.StatefulSet{

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
		validate: func(sts *appsv1.StatefulSet) {
			assert.Len(t, sts.Spec.VolumeClaimTemplates, 1)
			sz := sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String()
			assert.Equal(t, sz, "15Gi")
		},
		mustRecreateSTS: false,
		mustRecreatePod: false,
	})

	// downsize claim
	f(opts{
		existingObj: &appsv1.StatefulSet{
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
		newObj: &appsv1.StatefulSet{

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
		validate: func(sts *appsv1.StatefulSet) {
			assert.Len(t, sts.Spec.VolumeClaimTemplates, 1)
			sz := sts.Spec.VolumeClaimTemplates[0].Spec.Resources.Requests.Storage().String()
			assert.Equal(t, sz, "10Gi")
		},
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// change claim labels
	f(opts{
		existingObj: &appsv1.StatefulSet{
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
		newObj: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect",
				Namespace: "default",
			},
			Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "new-claim",
						Labels: map[string]string{
							"test": "test",
						},
					},
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
		validate: func(sts *appsv1.StatefulSet) {
			assert.Len(t, sts.Spec.VolumeClaimTemplates, 1)
			labels := sts.Spec.VolumeClaimTemplates[0].Labels
			assert.Equal(t, labels["test"], "test")
		},
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// change claim storageClass name
	f(opts{
		existingObj: &appsv1.StatefulSet{
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
		newObj: &appsv1.StatefulSet{
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
		validate: func(sts *appsv1.StatefulSet) {
			assert.Len(t, sts.Spec.VolumeClaimTemplates, 1)
			name := *sts.Spec.VolumeClaimTemplates[0].Spec.StorageClassName
			assert.Equal(t, name, "new-sc")
		},
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// change serviceName
	f(opts{
		existingObj: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: appsv1.StatefulSetSpec{
				ServiceName: "old-service",
			},
		},
		newObj: &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmagent",
				Namespace: "default",
			},
			Spec: appsv1.StatefulSetSpec{
				ServiceName: "new-service",
			},
		},
		validate: func(sts *appsv1.StatefulSet) {
			assert.Equal(t, sts.Spec.ServiceName, "new-service")
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
		actions           []k8stools.ClientAction
	}
	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithActions(o.predefinedObjects)
		ctx := context.TODO()
		err := updateSTSPVC(ctx, cl, o.sts)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		if o.actions != nil {
			assert.Equal(t, o.actions, cl.Actions)
		}
		var pvcs corev1.PersistentVolumeClaimList
		opts := &client.ListOptions{
			Namespace:     o.sts.Namespace,
			LabelSelector: labels.SelectorFromSet(o.sts.Spec.Selector.MatchLabels),
		}
		assert.NoError(t, cl.List(ctx, &pvcs, opts))
		assert.ElementsMatch(t, o.expected, pvcs.Items)
	}

	pvc1 := types.NamespacedName{Name: "vmselect-cachedir-vmselect-0", Namespace: "default"}
	pvc2 := types.NamespacedName{Name: "test-vmselect-0", Namespace: "default"}

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
		actions: []k8stools.ClientAction{
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: pvc1},
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
		actions: []k8stools.ClientAction{
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: pvc2},
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: pvc1},
		},
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmselect-0",
					Namespace: "default",
					Labels: map[string]string{
						"app": "vmselect",
					},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
					},
					ResourceVersion: "1000",
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
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
					},
					ResourceVersion: "1000",
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
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
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
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
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
			},
		},
	})

	// expand with named class
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
		actions: []k8stools.ClientAction{
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: pvc1},
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
					},
					ResourceVersion: "1000",
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
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
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
