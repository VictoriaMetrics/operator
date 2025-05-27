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

func Test_reCreateStatefulSet(t *testing.T) {
	type args struct {
		ctx                 context.Context
		newStatefulSet      *appsv1.StatefulSet
		existingStatefulSet *appsv1.StatefulSet
	}
	tests := []struct {
		name                          string
		args                          args
		validate                      func(sts *appsv1.StatefulSet) error
		stsRecreated, mustRecreatePod bool
		wantErr                       bool
		predefinedObjects             []runtime.Object
	}{
		{
			name: "add claim to sts",
			args: args{
				ctx: context.TODO(),
				existingStatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect",
						Namespace: "default",
					},
				},
				newStatefulSet: &appsv1.StatefulSet{
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
			},
			validate: func(sts *appsv1.StatefulSet) error {
				if len(sts.Spec.VolumeClaimTemplates) != 1 {
					return fmt.Errorf("unexpected configuration for volumeclaim at sts: %v, want at least one, got: %v", sts.Name, sts.Spec.VolumeClaimTemplates)
				}
				return nil
			},
			stsRecreated:    true,
			mustRecreatePod: true,
		},
		{
			name: "resize claim at sts",
			args: args{
				ctx: context.TODO(),
				existingStatefulSet: &appsv1.StatefulSet{
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
				newStatefulSet: &appsv1.StatefulSet{
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
			stsRecreated:    true,
			mustRecreatePod: false,
		},
		{
			name: "change claim storageClass name",
			args: args{
				ctx: context.TODO(),
				existingStatefulSet: &appsv1.StatefulSet{
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
				newStatefulSet: &appsv1.StatefulSet{
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
			stsRecreated:    true,
			mustRecreatePod: false,
		},
		{
			name: "change serviceName",
			args: args{
				ctx: context.TODO(),
				existingStatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						ServiceName: "old-service",
					},
				},
				newStatefulSet: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmagent",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{
						ServiceName: "new-service",
					},
				},
			},
			validate: func(sts *appsv1.StatefulSet) error {
				if sts.Spec.ServiceName != "new-service" {
					return fmt.Errorf("unexpected serviceName at sts: %s, want: %s", sts.Spec.ServiceName, "new-service")
				}
				return nil
			},
			stsRecreated:    true,
			mustRecreatePod: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects([]runtime.Object{tt.args.existingStatefulSet})
			stsRecreated, mustRecreatePod, err := recreateStatefulSetIfNeed(tt.args.ctx, cl, tt.args.newStatefulSet, tt.args.existingStatefulSet)
			if (err != nil) != tt.wantErr {
				t.Errorf("%s: \nwasCreatedStatefulSet() error = %v, wantErr %v", tt.name, err, tt.wantErr)
				return
			}
			var updatedStatefulSet appsv1.StatefulSet
			if err := cl.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.newStatefulSet.Namespace, Name: tt.args.newStatefulSet.Name}, &updatedStatefulSet); err != nil {
				t.Fatalf("%s: \nunexpected error: %v", tt.name, err)
			}
			if err := tt.validate(&updatedStatefulSet); err != nil {
				t.Fatalf("%s: \nsts validation failed: %v", tt.name, err)
			}
			if stsRecreated != tt.stsRecreated {
				t.Fatalf("%s: \n expect `stsRecreated`: %v, got: %v", tt.name, tt.stsRecreated, stsRecreated)
			}
			if mustRecreatePod != tt.mustRecreatePod {
				t.Fatalf("%s: \n expect `mustRecreatePod`: %v, got: %v", tt.name, tt.mustRecreatePod, mustRecreatePod)
			}
		})
	}
}

func Test_growStatefulSetPVC(t *testing.T) {
	type args struct {
		ctx context.Context
		sts *appsv1.StatefulSet
	}
	tests := []struct {
		name              string
		args              args
		wantErr           bool
		predefinedObjects []runtime.Object
	}{
		{
			name: "no need to expand",
			args: args{
				ctx: context.TODO(),
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
		},
		{
			name: "expand successfully",
			args: args{
				ctx: context.TODO(),
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
		},
		{
			name: "failed with non-expandable sc",
			args: args{
				ctx: context.TODO(),
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
		},
		{
			name: "expand with named class",
			args: args{
				ctx: context.TODO(),
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := growStatefulSetPVC(tt.args.ctx, cl, tt.args.sts); (err != nil) != tt.wantErr {
				t.Errorf("growStatefulSetPVC() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
