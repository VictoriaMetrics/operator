package k8stools

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/utils/pointer"

	v12 "k8s.io/api/storage/v1"

	"k8s.io/apimachinery/pkg/api/resource"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	appsv1 "k8s.io/api/apps/v1"
)

func Test_reCreateSTS(t *testing.T) {
	type args struct {
		ctx         context.Context
		newSTS      *appsv1.StatefulSet
		existingSTS *appsv1.StatefulSet
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
				existingSTS: &appsv1.StatefulSet{
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
					Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []v1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.ResourceRequirements{},
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
				existingSTS: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []v1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
							Spec: v1.PersistentVolumeClaimSpec{Resources: v1.ResourceRequirements{
								Requests: map[v1.ResourceName]resource.Quantity{
									v1.ResourceStorage: resource.MustParse("10Gi"),
								},
							}},
						},
					}},
				},
				newSTS: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []v1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
							Spec: v1.PersistentVolumeClaimSpec{Resources: v1.ResourceRequirements{
								Requests: map[v1.ResourceName]resource.Quantity{
									v1.ResourceStorage: resource.MustParse("15Gi"),
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
				existingSTS: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []v1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.ResourceRequirements{
									Requests: map[v1.ResourceName]resource.Quantity{
										v1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
								StorageClassName: pointer.String("old-sc"),
							},
						},
					}},
				},
				newSTS: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect",
						Namespace: "default",
					},
					Spec: appsv1.StatefulSetSpec{VolumeClaimTemplates: []v1.PersistentVolumeClaim{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "new-claim"},
							Spec: v1.PersistentVolumeClaimSpec{
								Resources: v1.ResourceRequirements{
									Requests: map[v1.ResourceName]resource.Quantity{
										v1.ResourceStorage: resource.MustParse("10Gi"),
									},
								},
								StorageClassName: pointer.String("new-sc"),
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := GetTestClientWithObjects([]runtime.Object{tt.args.existingSTS})
			stsRecreated, mustRecreatePod, err := recreateSTSIfNeed(tt.args.ctx, cl, tt.args.newSTS, tt.args.existingSTS)
			if (err != nil) != tt.wantErr {
				t.Errorf("%s: \nwasCreatedSTS() error = %v, wantErr %v", tt.name, err, tt.wantErr)
				return
			}
			var updatedSts appsv1.StatefulSet
			if err := cl.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.newSTS.Namespace, Name: tt.args.newSTS.Name}, &updatedSts); err != nil {
				t.Fatalf("%s: \nunexpected error: %v", tt.name, err)
			}
			if err := tt.validate(&updatedSts); err != nil {
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

func Test_growSTSPVC(t *testing.T) {
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
						VolumeClaimTemplates: []v1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "vmselect-cachedir",
								},
								Spec: v1.PersistentVolumeClaimSpec{
									Resources: v1.ResourceRequirements{
										Requests: map[v1.ResourceName]resource.Quantity{
											v1.ResourceStorage: resource.MustParse("10Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-cachedir-vmselect-insight-victoria-metrics-k8s-stack-0",
						Namespace: "default",
						Labels: map[string]string{
							"app": "vmselect",
						},
						Annotations: map[string]string{"operator.victoriametrics.com/pvc/allow-volume-expansion": "true"},
					},
					Spec: v1.PersistentVolumeClaimSpec{
						Resources: v1.ResourceRequirements{Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceStorage: resource.MustParse("10Gi"),
						}},
					},
				},
				&v12.StorageClass{
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
						VolumeClaimTemplates: []v1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "vmselect-cachedir",
								},
								Spec: v1.PersistentVolumeClaimSpec{
									Resources: v1.ResourceRequirements{
										Requests: map[v1.ResourceName]resource.Quantity{
											v1.ResourceStorage: resource.MustParse("15Gi"),
										},
									},
								},
							},
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "test",
								},
								Spec: v1.PersistentVolumeClaimSpec{
									Resources: v1.ResourceRequirements{
										Requests: map[v1.ResourceName]resource.Quantity{
											v1.ResourceStorage: resource.MustParse("5Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-cachedir-vmselect-insight-victoria-metrics-k8s-stack-0",
						Namespace: "default",
						Labels: map[string]string{
							"app": "vmselect",
						},
					},
					Spec: v1.PersistentVolumeClaimSpec{
						Resources: v1.ResourceRequirements{Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceStorage: resource.MustParse("10Gi"),
						}},
					},
				},
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmselect-insight-victoria-metrics-k8s-stack-0",
						Namespace: "default",
						Labels: map[string]string{
							"app": "vmselect",
						},
					},
					Spec: v1.PersistentVolumeClaimSpec{
						Resources: v1.ResourceRequirements{Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceStorage: resource.MustParse("3Gi"),
						}},
					},
				},
				&v12.StorageClass{
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
						VolumeClaimTemplates: []v1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "vmselect-cachedir",
								},
								Spec: v1.PersistentVolumeClaimSpec{
									Resources: v1.ResourceRequirements{
										Requests: map[v1.ResourceName]resource.Quantity{
											v1.ResourceStorage: resource.MustParse("15Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-cachedir-vmselect-insight-victoria-metrics-k8s-stack-0",
						Namespace: "default",
						Labels: map[string]string{
							"app": "vmselect",
						},
					},
					Spec: v1.PersistentVolumeClaimSpec{
						Resources: v1.ResourceRequirements{Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceStorage: resource.MustParse("10Gi"),
						}},
					},
				},
				&v12.StorageClass{
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
						VolumeClaimTemplates: []v1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "vmselect",
								},
								Spec: v1.PersistentVolumeClaimSpec{
									StorageClassName: pointer.StringPtr("ssd"),
									Resources: v1.ResourceRequirements{
										Requests: map[v1.ResourceName]resource.Quantity{
											v1.ResourceStorage: resource.MustParse("15Gi"),
										},
									},
								},
							},
						},
					},
				},
			},
			predefinedObjects: []runtime.Object{
				&v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect-cachedir-vmselect-insight-victoria-metrics-k8s-stack-0",
						Namespace: "default",
						Labels: map[string]string{
							"app": "vmselect",
						},
					},
					Spec: v1.PersistentVolumeClaimSpec{
						Resources: v1.ResourceRequirements{Requests: map[v1.ResourceName]resource.Quantity{
							v1.ResourceStorage: resource.MustParse("10Gi"),
						}},
					},
				},
				&v12.StorageClass{
					ObjectMeta: metav1.ObjectMeta{
						Name: "standard",
						Annotations: map[string]string{
							"storageclass.kubernetes.io/is-default-class": "true",
						},
					},
					AllowVolumeExpansion: func() *bool { b := true; return &b }(),
				},
				&v12.StorageClass{
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
			cl := GetTestClientWithObjects(tt.predefinedObjects)
			if err := growSTSPVC(tt.args.ctx, cl, tt.args.sts); (err != nil) != tt.wantErr {
				t.Errorf("growSTSPVC() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
