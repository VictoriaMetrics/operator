package vmdistributedcluster

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/v1alpha1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/v1beta1"
)

func Test_reconcileInlineVMCluster(t *testing.T) {
	scheme := runtime.NewScheme()
	_ = vmv1alpha1.AddToScheme(scheme)
	_ = vmv1beta1.AddToScheme(scheme)
	_ = corev1.AddToScheme(scheme)

	type args struct {
		crName    string
		namespace string
		index     int
		refOrSpec vmv1alpha1.VMClusterRefOrSpec
	}
	tests := []struct {
		name         string
		args         args
		want         *vmv1beta1.VMCluster
		wantErr      bool
		existingObjs []runtime.Object
	}{
		{
			name: "Successfully creates a new VMCluster",
			args: args{
				crName:    "test-vmdc",
				namespace: "default",
				index:     0,
				refOrSpec: vmv1alpha1.VMClusterRefOrSpec{
					Name: "inline-vmcluster",
					VMClusterSpec: &vmv1beta1.VMClusterSpec{
						Image: "victoriametrics/vmcluster:latest",
					},
				},
			},
			want: &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmdc-inline-vmcluster",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "vmdistributedcluster",
						"app.kubernetes.io/instance":   "test-vmdc",
					},
					Annotations: map[string]string{
						"vmdistributedcluster.victoriametrics.com/inline-index": "0",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "operator.victoriametrics.com/v1alpha1",
							Kind:       "VMDistributedCluster",
							Name:       "test-vmdc",
						},
					},
				},
				Spec: vmv1beta1.VMClusterSpec{
					Image: "victoriametrics/vmcluster:latest",
				},
			},
			wantErr: false,
		},
		{
			name: "Successfully updates an existing VMCluster",
			args: args{
				crName:    "test-vmdc",
				namespace: "default",
				index:     0,
				refOrSpec: vmv1alpha1.VMClusterRefOrSpec{
					Name: "inline-vmcluster",
					VMClusterSpec: &vmv1beta1.VMClusterSpec{
						Image: "victoriametrics/vmcluster:new-version",
					},
				},
			},
			existingObjs: []runtime.Object{
				&vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmdc-inline-vmcluster",
						Namespace: "default",
						Labels: map[string]string{
							"app.kubernetes.io/managed-by": "vmdistributedcluster",
							"app.kubernetes.io/instance":   "test-vmdc",
						},
						Annotations: map[string]string{
							"vmdistributedcluster.victoriametrics.com/inline-index": "0",
						},
						OwnerReferences: []metav1.OwnerReference{
							{
								APIVersion: "operator.victoriametrics.com/v1alpha1",
								Kind:       "VMDistributedCluster",
								Name:       "test-vmdc",
							},
						},
					},
					Spec: vmv1beta1.VMClusterSpec{
						Image: "victoriametrics/vmcluster:old-version",
					},
				},
			},
			want: &vmv1beta1.VMCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-vmdc-inline-vmcluster",
					Namespace: "default",
					Labels: map[string]string{
						"app.kubernetes.io/managed-by": "vmdistributedcluster",
						"app.kubernetes.io/instance":   "test-vmdc",
					},
					Annotations: map[string]string{
						"vmdistributedcluster.victoriametrics.com/inline-index": "0",
					},
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "operator.victoriametrics.com/v1alpha1",
							Kind:       "VMDistributedCluster",
							Name:       "test-vmdc",
						},
					},
				},
				Spec: vmv1beta1.VMClusterSpec{
					Image: "victoriametrics/vmcluster:new-version",
				},
			},
			wantErr: false,
		},
		{
			name: "Returns error if getting VMCluster fails",
			args: args{
				crName:    "test-vmdc",
				namespace: "default",
				index:     0,
				refOrSpec: vmv1alpha1.VMClusterRefOrSpec{
					Name: "inline-vmcluster",
					VMClusterSpec: &vmv1beta1.VMClusterSpec{
						Image: "victoriametrics/vmcluster:latest",
					},
				},
			},
			existingObjs: []runtime.Object{
				&vmv1beta1.VMCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-vmdc-inline-vmcluster-error", // Different name to simulate not found, but other error
						Namespace: "default",
					},
				},
			},
			want:    nil,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rclient := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(tt.existingObjs...).Build()
			eventRecorder := record.NewFakeRecorder(100)

			vmDistCluster := &vmv1alpha1.VMDistributedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      tt.args.crName,
					Namespace: tt.args.namespace,
				},
			}

			// Mock the owner reference setting logic for testing purposes
			setOwnerFn := func(ctx context.Context, rclient client.Client, instance client.Object, newOwner metav1.Object) error {
				instance.SetOwnerReferences([]metav1.OwnerReference{
					{
						APIVersion: "operator.victoriametrics.com/v1alpha1",
						Kind:       "VMDistributedCluster",
						Name:       newOwner.GetName(),
					},
				})
				return nil
			}

			// Since reconcileInlineVMCluster calls setOwnerAndDefault, we need to mock or
			// ensure setOwnerAndDefault behaves as expected for testing.
			// For simplicity, let\'s just make sure the owner reference is set in the expected object.
			if tt.want != nil {
				_ = setOwnerFn(context.Background(), rclient, tt.want, vmDistCluster)
			}

			got, err := reconcileInlineVMCluster(context.Background(), rclient, tt.args.crName, tt.args.namespace, tt.args.index, tt.args.refOrSpec)
			if (err != nil) != tt.wantErr {
				t.Errorf("reconcileInlineVMCluster() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("reconcileInlineVMCluster() got = %v, want %v", got, tt.want)
			}

			// Verify that the VMCluster was actually created/updated in the fake client
			if tt.want != nil && !tt.wantErr {
				fetchedVMCluster := &vmv1beta1.VMCluster{}
				err := rclient.Get(context.Background(), types.NamespacedName{Name: tt.want.Name, Namespace: tt.want.Namespace}, fetchedVMCluster)
				if err != nil {
					t.Fatalf("Failed to fetch created VMCluster: %v", err)
				}
				if !reflect.DeepEqual(fetchedVMCluster.Spec, tt.want.Spec) {
					t.Errorf("Created VMCluster spec mismatch: got %v, want %v", fetchedVMCluster.Spec, tt.want.Spec)
				}
				if !reflect.DeepEqual(fetchedVMCluster.ObjectMeta.Labels, tt.want.ObjectMeta.Labels) {
					t.Errorf("Created VMCluster labels mismatch: got %v, want %v", fetchedVMCluster.ObjectMeta.Labels, tt.want.ObjectMeta.Labels)
				}
				if !reflect.DeepEqual(fetchedVMCluster.ObjectMeta.Annotations, tt.want.ObjectMeta.Annotations) {
					t.Errorf("Created VMCluster annotations mismatch: got %v, want %v", fetchedVMCluster.ObjectMeta.Annotations, tt.want.ObjectMeta.Annotations)
				}
				// Verify owner references
				if len(fetchedVMCluster.OwnerReferences) != len(tt.want.OwnerReferences) {
					t.Errorf("Created VMCluster owner reference count mismatch: got %d, want %d", len(fetchedVMCluster.OwnerReferences), len(tt.want.OwnerReferences))
				} else if len(tt.want.OwnerReferences) > 0 && !reflect.DeepEqual(fetchedVMCluster.OwnerReferences[0].Name, tt.want.OwnerReferences[0].Name) {
					t.Errorf("Created VMCluster owner reference name mismatch: got %s, want %s", fetchedVMCluster.OwnerReferences[0].Name, tt.want.OwnerReferences[0].Name)
				}
			}
		})
	}
}
