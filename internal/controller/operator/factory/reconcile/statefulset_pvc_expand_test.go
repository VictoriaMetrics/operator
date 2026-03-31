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
		mustRecreateSTS bool
		mustRecreatePod bool
	}
	buildSTS := func(fns ...func(sts *appsv1.StatefulSet)) *appsv1.StatefulSet {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect",
				Namespace: "default",
			},
		}
		for _, fn := range fns {
			fn(sts)
		}
		return sts
	}
	f := func(o opts) {
		t.Helper()
		// use deep copy to avoid side effects
		existingObj := o.existingObj.DeepCopy()
		newObj := o.newObj.DeepCopy()
		cl := k8stools.GetTestClientWithObjects([]runtime.Object{existingObj})
		t.Helper()
		ctx := context.TODO()
		mustRecreateSTS, mustRecreatePod := isSTSRecreateRequired(ctx, existingObj, newObj, nil)
		var updatedSTS appsv1.StatefulSet
		nsn := types.NamespacedName{Namespace: newObj.Namespace, Name: newObj.Name}
		assert.NoError(t, cl.Get(ctx, nsn, &updatedSTS))
		assert.Equal(t, mustRecreateSTS, o.mustRecreateSTS)
		assert.Equal(t, mustRecreatePod, o.mustRecreatePod)
	}

	setVCT := func(name, size, sc string) func(sts *appsv1.StatefulSet) {
		return func(sts *appsv1.StatefulSet) {
			vct := corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{Name: name},
			}
			if size != "" {
				vct.Spec.Resources.Requests = map[corev1.ResourceName]resource.Quantity{
					corev1.ResourceStorage: resource.MustParse(size),
				}
			}
			if sc != "" {
				vct.Spec.StorageClassName = ptr.To(sc)
			}
			sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{vct}
		}
	}

	// create sts with a new claim
	f(opts{
		existingObj:     buildSTS(),
		newObj:          buildSTS(setVCT("new-claim", "", "")),
		mustRecreateSTS: true,
		mustRecreatePod: true,
	})

	// resize claim at sts
	f(opts{
		existingObj:     buildSTS(setVCT("new-claim", "10Gi", "")),
		newObj:          buildSTS(setVCT("new-claim", "15Gi", "")),
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// downsize claim
	f(opts{
		existingObj:     buildSTS(setVCT("new-claim", "15Gi", "")),
		newObj:          buildSTS(setVCT("new-claim", "10Gi", "")),
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// change claim labels
	f(opts{
		existingObj: buildSTS(setVCT("new-claim", "10Gi", "old-sc")),
		newObj: buildSTS(setVCT("new-claim", "10Gi", "old-sc"), func(sts *appsv1.StatefulSet) {
			sts.Spec.VolumeClaimTemplates[0].Labels = map[string]string{"test": "test"}
		}),
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// change claim storageClass name
	f(opts{
		existingObj:     buildSTS(setVCT("new-claim", "10Gi", "old-sc")),
		newObj:          buildSTS(setVCT("new-claim", "10Gi", "new-sc")),
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// change serviceName
	setService := func(name string) func(sts *appsv1.StatefulSet) {
		return func(sts *appsv1.StatefulSet) {
			sts.Name = "vmagent"
			sts.Spec.ServiceName = name
		}
	}
	f(opts{
		existingObj:     buildSTS(setService("old-service")),
		newObj:          buildSTS(setService("new-service")),
		mustRecreateSTS: true,
		mustRecreatePod: true,
	})

	// change PodManagementPolicy
	setPolicy := func(policy appsv1.PodManagementPolicyType) func(sts *appsv1.StatefulSet) {
		return func(sts *appsv1.StatefulSet) {
			sts.Spec.PodManagementPolicy = policy
		}
	}
	f(opts{
		existingObj:     buildSTS(setPolicy(appsv1.OrderedReadyPodManagement)),
		newObj:          buildSTS(setPolicy(appsv1.ParallelPodManagement)),
		mustRecreateSTS: true,
		mustRecreatePod: false,
	})

	// rename claim
	f(opts{
		existingObj:     buildSTS(setVCT("old-claim", "", "")),
		newObj:          buildSTS(setVCT("new-claim", "", "")),
		mustRecreateSTS: true,
		mustRecreatePod: true,
	})
}

func Test_updateSTSPVC(t *testing.T) {
	type opts struct {
		sts      *appsv1.StatefulSet
		prevVCTs []corev1.PersistentVolumeClaim
		wantErr  bool
		preRun   func(c client.Client)
		expected []corev1.PersistentVolumeClaim
		actions  []k8stools.ClientAction
	}
	f := func(o opts) {
		t.Helper()
		cl := k8stools.GetTestClientWithActionsAndObjects(nil)
		ctx := context.TODO()
		if o.preRun != nil {
			o.preRun(cl)
			cl.Actions = nil
		}
		err := updateSTSPVC(ctx, cl, o.sts, o.prevVCTs)
		if o.wantErr {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
		}
		if o.actions != nil {
			assert.Equal(t, o.actions, cl.Actions)
		}
		var pvcs corev1.PersistentVolumeClaimList
		listOpts := &client.ListOptions{
			Namespace:     o.sts.Namespace,
			LabelSelector: labels.SelectorFromSet(o.sts.Spec.Selector.MatchLabels),
		}
		assert.NoError(t, cl.List(ctx, &pvcs, listOpts))
		assert.Equal(t, o.expected, pvcs.Items)
	}

	buildSTS := func(fns ...func(*appsv1.StatefulSet)) *appsv1.StatefulSet {
		sts := &appsv1.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "vmselect",
				Namespace: "default",
				Labels:    map[string]string{"app": "vmselect"},
			},
			Spec: appsv1.StatefulSetSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"app": "vmselect"},
				},
			},
		}
		for _, fn := range fns {
			fn(sts)
		}
		return sts
	}

	pvc1NSN := types.NamespacedName{Name: "vmselect-cachedir-vmselect-0", Namespace: "default"}
	pvc2NSN := types.NamespacedName{Name: "test-vmselect-0", Namespace: "default"}

	// do not update if only 3-rd party metadata differs
	f(opts{
		sts: buildSTS(func(sts *appsv1.StatefulSet) {
			sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmselect-cachedir",
						Annotations: map[string]string{
							"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
							"test": "test",
						},
						Labels: map[string]string{"app": "vmselect"},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
					Status: corev1.PersistentVolumeClaimStatus{
						Capacity: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}
		}),
		prevVCTs: []corev1.PersistentVolumeClaim{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "vmselect-cachedir",
				Annotations: map[string]string{
					"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
					"test": "test",
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
		}},
		preRun: func(c client.Client) {
			assert.NoError(t, c.Create(context.TODO(), &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels: map[string]string{
						"app":       "vmselect",
						"3rd-party": "value",
					},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
						"test":      "test",
						"3rd-party": "value",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}))
			assert.NoError(t, c.Create(context.TODO(), &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standard",
					Annotations: map[string]string{
						"volume.beta.kubernetes.io/storage-class": "true",
					},
				},
			}))
		},
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels: map[string]string{
						"app":       "vmselect",
						"3rd-party": "value",
					},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
						"test":      "test",
						"3rd-party": "value",
					},
					ResourceVersion: "2",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		},
	})

	// update metadata only
	f(opts{
		sts: buildSTS(func(sts *appsv1.StatefulSet) {
			sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "vmselect-cachedir",
						Annotations: map[string]string{
							"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
							"test": "after",
						},
						Labels: map[string]string{"app": "vmselect"},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			}
		}),
		prevVCTs: []corev1.PersistentVolumeClaim{{
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
		}},
		preRun: func(c client.Client) {
			assert.NoError(t, c.Create(context.TODO(), &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels: map[string]string{
						"app":       "vmselect",
						"3rd-party": "value",
					},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
						"test":      "before",
						"3rd-party": "value",
					},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}))
			assert.NoError(t, c.Create(context.TODO(), &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standard",
					Annotations: map[string]string{
						"volume.beta.kubernetes.io/storage-class": "true",
					},
				},
			}))
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
		},
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels: map[string]string{
						"app":       "vmselect",
						"3rd-party": "value",
					},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
						"test":      "after",
						"3rd-party": "value",
					},
					ResourceVersion: "4",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		},
	})

	// expand successfully
	f(opts{
		sts: buildSTS(func(sts *appsv1.StatefulSet) {
			sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "vmselect-cachedir",
						Labels: map[string]string{"app": "vmselect"},
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
						Name:   "test",
						Labels: map[string]string{"app": "vmselect"},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("5Gi"),
							},
						},
					},
				},
			}
		}),
		preRun: func(c client.Client) {
			assert.NoError(t, c.Create(context.TODO(), &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels:    map[string]string{"app": "vmselect"},
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
			}))
			assert.NoError(t, c.Create(context.TODO(), &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc2NSN.Name,
					Namespace: pvc2NSN.Namespace,
					Labels:    map[string]string{"app": "vmselect"},
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
			}))
			assert.NoError(t, c.Create(context.TODO(), &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standard",
					Annotations: map[string]string{
						"storageclass.kubernetes.io/is-default-class": "true",
					},
				},
				AllowVolumeExpansion: ptr.To(true),
			}))
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc2NSN},
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: pvc2NSN},
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc2NSN},
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
		},
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc2NSN.Name,
					Namespace: pvc2NSN.Namespace,
					Labels:    map[string]string{"app": "vmselect"},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
					},
					ResourceVersion: "4",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("5Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("5Gi"),
					},
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels:    map[string]string{"app": "vmselect"},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
					},
					ResourceVersion: "4",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("15Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("15Gi"),
					},
				},
			},
		},
	})

	// failed with non-expandable sc
	f(opts{
		sts: buildSTS(func(sts *appsv1.StatefulSet) {
			sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "vmselect-cachedir",
						Labels: map[string]string{"app": "vmselect"},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("15Gi"),
							},
						},
					},
				},
			}
		}),
		preRun: func(c client.Client) {
			assert.NoError(t, c.Create(context.TODO(), &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels:    map[string]string{"app": "vmselect"},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
			}))
			assert.NoError(t, c.Create(context.TODO(), &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standard",
					Annotations: map[string]string{
						"storageclass.kubernetes.io/is-default-class": "true",
					},
				},
			}))
		},
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:            pvc1NSN.Name,
					Namespace:       pvc1NSN.Namespace,
					Labels:          map[string]string{"app": "vmselect"},
					ResourceVersion: "2",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("10Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		},
		wantErr: true,
	})

	// expand with annotation on non-expandable sc
	f(opts{
		sts: buildSTS(func(sts *appsv1.StatefulSet) {
			sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "vmselect-cachedir",
						Labels: map[string]string{"app": "vmselect"},
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("15Gi"),
							},
						},
					},
				},
			}
		}),
		preRun: func(c client.Client) {
			assert.NoError(t, c.Create(context.TODO(), &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels:    map[string]string{"app": "vmselect"},
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
			}))
			assert.NoError(t, c.Create(context.TODO(), &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standard",
					Annotations: map[string]string{
						"storageclass.kubernetes.io/is-default-class": "true",
					},
				},
				AllowVolumeExpansion: ptr.To(false),
			}))
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
		},
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels:    map[string]string{"app": "vmselect"},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
					},
					ResourceVersion: "4",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("15Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("15Gi"),
					},
				},
			},
		},
	})

	// expand with named class
	f(opts{
		sts: buildSTS(func(sts *appsv1.StatefulSet) {
			sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "vmselect-cachedir",
						Labels: map[string]string{"app": "vmselect"},
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
			}
		}),
		preRun: func(c client.Client) {
			assert.NoError(t, c.Create(context.TODO(), &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels:    map[string]string{"app": "vmselect"},
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
			}))
			assert.NoError(t, c.Create(context.TODO(), &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standard",
					Annotations: map[string]string{
						"storageclass.kubernetes.io/is-default-class": "true",
					},
				},
				AllowVolumeExpansion: ptr.To(true),
			}))
			assert.NoError(t, c.Create(context.TODO(), &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ssd",
					Annotations: map[string]string{
						"storageclass.kubernetes.io/is-default-class": "false",
					},
				},
				AllowVolumeExpansion: ptr.To(true),
			}))
		},
		actions: []k8stools.ClientAction{
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
			{Verb: "Update", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
			{Verb: "Get", Kind: "PersistentVolumeClaim", Resource: pvc1NSN},
		},
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      pvc1NSN.Name,
					Namespace: pvc1NSN.Namespace,
					Labels:    map[string]string{"app": "vmselect"},
					Annotations: map[string]string{
						"operator.victoriametrics.com/pvc-allow-volume-expansion": "true",
					},
					ResourceVersion: "4",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("15Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("15Gi"),
					},
				},
			},
		},
	})

	// ignore orphan PVC
	f(opts{
		sts: buildSTS(func(sts *appsv1.StatefulSet) {
			sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
				},
			}
		}),
		preRun: func(c client.Client) {
			assert.NoError(t, c.Create(context.TODO(), &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "orphan-vmselect-0",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect"},
				},
			}))
		},
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "orphan-vmselect-0",
					Namespace:       "default",
					Labels:          map[string]string{"app": "vmselect"},
					ResourceVersion: "2",
				},
			},
		},
	})

	// PVC bigger than VCT (manual expansion case), should log info and skip
	f(opts{
		sts: buildSTS(func(sts *appsv1.StatefulSet) {
			sts.Spec.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{Name: "data"},
					Spec: corev1.PersistentVolumeClaimSpec{
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: resource.MustParse("10Gi"),
							},
						},
					},
				},
			}
		}),
		preRun: func(c client.Client) {
			assert.NoError(t, c.Create(context.TODO(), &corev1.PersistentVolumeClaim{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "data-vmselect-0",
					Namespace: "default",
					Labels:    map[string]string{"app": "vmselect"},
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("20Gi"),
						},
					},
				},
			}))
			assert.NoError(t, c.Create(context.TODO(), &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "standard",
					Annotations: map[string]string{
						"storageclass.kubernetes.io/is-default-class": "true",
					},
				},
				AllowVolumeExpansion: ptr.To(true),
			}))
		},
		expected: []corev1.PersistentVolumeClaim{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:            "data-vmselect-0",
					Namespace:       "default",
					Labels:          map[string]string{"app": "vmselect"},
					ResourceVersion: "2",
				},
				Spec: corev1.PersistentVolumeClaimSpec{
					Resources: corev1.VolumeResourceRequirements{
						Requests: map[corev1.ResourceName]resource.Quantity{
							corev1.ResourceStorage: resource.MustParse("20Gi"),
						},
					},
				},
				Status: corev1.PersistentVolumeClaimStatus{
					Capacity: map[corev1.ResourceName]resource.Quantity{
						corev1.ResourceStorage: resource.MustParse("20Gi"),
					},
				},
			},
		},
	})
}
