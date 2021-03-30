package factory

import (
	"context"
	"fmt"
	"testing"

	"k8s.io/utils/pointer"

	v12 "k8s.io/api/storage/v1"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/VictoriaMetrics/operator/controllers/factory/k8stools"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	appsv1 "k8s.io/api/apps/v1"
)

func Test_reCreateSTS(t *testing.T) {
	type args struct {
		ctx         context.Context
		pvcName     string
		newSTS      *appsv1.StatefulSet
		existingSTS *appsv1.StatefulSet
	}
	tests := []struct {
		name              string
		args              args
		validate          func(sts *appsv1.StatefulSet) error
		wantErr           bool
		predefinedObjects []runtime.Object
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
				pvcName: "new-claim",
			},
			predefinedObjects: []runtime.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect",
						Namespace: "default",
					},
				},
			},
			validate: func(sts *appsv1.StatefulSet) error {
				if len(sts.Spec.VolumeClaimTemplates) != 1 {
					return fmt.Errorf("unexpected configuration for volumeclaim at sts: %v, want at least one, got: %v", sts.Name, sts.Spec.VolumeClaimTemplates)
				}
				return nil
			},
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
				pvcName: "new-claim",
			},
			predefinedObjects: []runtime.Object{
				&appsv1.StatefulSet{
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
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			_, err := wasCreatedSTS(tt.args.ctx, cl, tt.args.pvcName, tt.args.newSTS, tt.args.existingSTS)
			if (err != nil) != tt.wantErr {
				t.Errorf("wasCreatedSTS() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			var updatedSts appsv1.StatefulSet
			if err := cl.Get(tt.args.ctx, types.NamespacedName{Namespace: tt.args.newSTS.Namespace, Name: tt.args.newSTS.Name}, &updatedSts); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if err := tt.validate(&updatedSts); err != nil {
				t.Fatalf("sts validation failed: %v", err)
			}
		})
	}
}

func Test_growSTSPVC(t *testing.T) {
	type args struct {
		ctx     context.Context
		sts     *appsv1.StatefulSet
		pvcName string
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
				ctx:     context.TODO(),
				pvcName: "vmselect",
				sts: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect",
						Namespace: "default",
						Labels: map[string]string{
							"app": "vmselect",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						VolumeClaimTemplates: []v1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "vmselect",
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
						Name: "pvc-0",
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
							"volume.beta.kubernetes.io/storage-class": "true",
						},
					},
				},
			},
		},
		{
			name: "expand",
			args: args{
				ctx:     context.TODO(),
				pvcName: "vmselect",
				sts: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect",
						Namespace: "default",
						Labels: map[string]string{
							"app": "vmselect",
						},
					},
					Spec: appsv1.StatefulSetSpec{
						VolumeClaimTemplates: []v1.PersistentVolumeClaim{
							{
								ObjectMeta: metav1.ObjectMeta{
									Name: "vmselect",
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
						Name:      "pvc-0",
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
			},
		},
		{
			name: "expand with named class",
			args: args{
				ctx:     context.TODO(),
				pvcName: "vmselect",
				sts: &appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "vmselect",
						Namespace: "default",
						Labels: map[string]string{
							"app": "vmselect",
						},
					},
					Spec: appsv1.StatefulSetSpec{
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
						Name:      "pvc-0",
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
			cl := k8stools.GetTestClientWithObjects(tt.predefinedObjects)
			if err := growSTSPVC(tt.args.ctx, cl, tt.args.sts, tt.args.pvcName); (err != nil) != tt.wantErr {
				t.Errorf("growSTSPVC() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
