package childobjects

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

//nolint:dupl,lll
var _ = Describe("test vmuser Controller", func() {
	namespace := "default"
	ctx := context.Background()
	type opts struct {
		vmauths []*v1beta1vm.VMAuth
		vmusers []*v1beta1vm.VMUser
	}
	type step struct {
		setup  func()
		modify func()
		verify func()
	}
	Context("with operator controller performing", func() {
		DescribeTable("build and check status", func(args *opts, steps []step) {
			DeferCleanup(func() {
				for _, vmauth := range args.vmauths {
					vmauth.Namespace = namespace
					Expect(k8sClient.Delete(ctx, vmauth)).To(Succeed())
				}
				for _, vmuser := range args.vmusers {
					Expect(k8sClient.Delete(ctx, vmuser)).To(Succeed())
				}
				for _, alert := range args.vmauths {
					nsn := types.NamespacedName{Name: alert.Name, Namespace: alert.Namespace}
					Eventually(func() error {
						return k8sClient.Get(ctx, nsn, &v1beta1vm.VMAuth{})
					}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "isNotFound"))
				}
			})
			step := steps[0]
			if step.setup != nil {
				step.setup()
			}
			for _, vmauth := range args.vmauths {
				Expect(k8sClient.Create(ctx, vmauth)).To(Succeed())
			}
			for _, vmuser := range args.vmusers {
				Expect(k8sClient.Create(ctx, vmuser)).To(Succeed())
			}

			for _, am := range args.vmauths {
				Eventually(func() error {
					return suite.ExpectObjectStatus(ctx,
						k8sClient,
						&v1beta1vm.VMAuth{},
						types.NamespacedName{Name: am.Name, Namespace: am.Namespace},
						v1beta1vm.UpdateStatusOperational)
				}, eventualReadyTimeout).Should(Succeed())
			}
			if step.modify != nil {
				step.modify()
			}
			step.verify()
			for _, step := range steps[1:] {
				if step.setup != nil {
					step.setup()
				}
				if step.modify != nil {
					step.modify()
				}
				step.verify()
			}
		},
			Entry("by creating single auth and user", &opts{
				vmauths: []*v1beta1vm.VMAuth{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "single-1",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMAuthSpec{
							SelectAllByDefault: true,
							CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
								Image: v1beta1vm.Image{
									Tag: "v1.108.0",
								},
							},
						},
					},
				},
				vmusers: []*v1beta1vm.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "inline-1",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMUserSpec{
							UserName: ptr.To("user"),
							Password: ptr.To("password"),
							TargetRefs: []v1beta1vm.TargetRef{
								{
									Static: &v1beta1vm.StaticRef{
										URL: "http://localhost:8085",
									},
								},
							},
						},
					},
				},
			}, []step{
				{
					verify: func() {
						for _, nsn := range []types.NamespacedName{
							{Name: "inline-1", Namespace: namespace},
						} {
							var vmuser v1beta1vm.VMUser
							Expect(k8sClient.Get(ctx, nsn, &vmuser)).To(Succeed())
							Expect(vmuser.Status.UpdateStatus).To(Equal(v1beta1vm.UpdateStatusOperational))
						}
					},
				},
			},
			),
			Entry("by creating single auth and 1 broken user with transition to ok", &opts{
				vmauths: []*v1beta1vm.VMAuth{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "multiple-1",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMAuthSpec{
							SelectAllByDefault: true,
							CommonDefaultableParams: v1beta1vm.CommonDefaultableParams{
								Image: v1beta1vm.Image{
									Tag: "v1.108.0",
								},
							},
						},
					},
				},
				vmusers: []*v1beta1vm.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "missing-ref-1",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMUserSpec{
							UserName: ptr.To("user"),
							PasswordRef: &corev1.SecretKeySelector{
								Key: "password",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "non-exist-secret",
								},
							},
							TargetRefs: []v1beta1vm.TargetRef{
								{
									Static: &v1beta1vm.StaticRef{
										URL: "http://localhost:8085",
									},
								},
							},
						},
					},
				},
			}, []step{
				{
					verify: func() {
						for _, nsn := range []types.NamespacedName{
							{Name: "missing-ref-1", Namespace: namespace},
						} {
							var vmuser v1beta1vm.VMUser
							Expect(k8sClient.Get(ctx, nsn, &vmuser)).To(Succeed())
							Expect(vmuser.Status.UpdateStatus).To(Equal(v1beta1vm.UpdateStatusFailed))
							Expect(vmuser.Status.Conditions).NotTo(BeEmpty())
						}
					},
				},
				{
					setup: func() {
						passwordSecret := corev1.Secret{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "backend-e2e-access-1",
								Namespace: namespace,
							},
							StringData: map[string]string{"password": "password"},
						}
						Expect(k8sClient.Create(ctx, &passwordSecret)).To(Succeed())
						DeferCleanup(func() {
							Expect(k8sClient.Delete(ctx, &passwordSecret)).To(Succeed())
						})
					},
					modify: func() {
						nsn := types.NamespacedName{Name: "missing-ref-1", Namespace: namespace}
						var toUpdate v1beta1vm.VMUser
						Expect(k8sClient.Get(ctx, nsn, &toUpdate)).To(Succeed())
						toUpdate.Spec.PasswordRef.Name = "backend-e2e-access-1"
						Expect(k8sClient.Update(ctx, &toUpdate)).To(Succeed())
						Eventually(func() error {
							return suite.ExpectObjectStatus(ctx, k8sClient, &v1beta1vm.VMUser{}, nsn, v1beta1vm.UpdateStatusOperational)
						}, eventualReadyTimeout).Should(Succeed())
					},
					verify: func() {
						for _, nsn := range []types.NamespacedName{
							{Name: "missing-ref-1", Namespace: namespace},
						} {
							var vmuser v1beta1vm.VMUser
							Expect(k8sClient.Get(ctx, nsn, &vmuser)).To(Succeed())
							Expect(vmuser.Status.UpdateStatus).To(Equal(v1beta1vm.UpdateStatusOperational))
							for _, cond := range vmuser.Status.Conditions {
								Expect(cond.Status).To(Equal(metav1.ConditionTrue), "expect condition to be True, reason=%q,type=%q", cond.Reason, cond.Type)
							}
						}
					},
				},
			},
			),
		)
	})
})
