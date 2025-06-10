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

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

//nolint:dupl,lll
var _ = Describe("test vmuser Controller", func() {
	namespace := "default"
	ctx := context.Background()
	type opts struct {
		vmauths []*vmv1beta1.VMAuth
		vmusers []*vmv1beta1.VMUser
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
						return k8sClient.Get(ctx, nsn, &vmv1beta1.VMAuth{})
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
						&vmv1beta1.VMAuth{},
						types.NamespacedName{Name: am.Name, Namespace: am.Namespace},
						vmv1beta1.UpdateStatusOperational)
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
				vmauths: []*vmv1beta1.VMAuth{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "single-1",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMAuthSpec{
							SelectAllByDefault: true,
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								Image: vmv1beta1.Image{
									Tag: "v1.108.0",
								},
							},
						},
					},
				},
				vmusers: []*vmv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "inline-1",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMUserSpec{
							UserName: ptr.To("user"),
							Password: ptr.To("password-1"),
							TargetRefs: []vmv1beta1.TargetRef{
								{
									Static: &vmv1beta1.StaticRef{
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
							var vmuser vmv1beta1.VMUser
							Expect(k8sClient.Get(ctx, nsn, &vmuser)).To(Succeed())
							Expect(vmuser.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusOperational))
						}
					},
				},
			},
			),
			Entry("by creating single auth and 1 broken user with transition to ok", &opts{
				vmauths: []*vmv1beta1.VMAuth{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "multiple-1",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMAuthSpec{
							SelectAllByDefault: true,
							CommonDefaultableParams: vmv1beta1.CommonDefaultableParams{
								Image: vmv1beta1.Image{
									Tag: "v1.108.0",
								},
							},
						},
					},
				},
				vmusers: []*vmv1beta1.VMUser{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "missing-ref-1",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMUserSpec{
							UserName: ptr.To("user"),
							PasswordRef: &corev1.SecretKeySelector{
								Key: "password",
								LocalObjectReference: corev1.LocalObjectReference{
									Name: "non-exist-secret",
								},
							},
							TargetRefs: []vmv1beta1.TargetRef{
								{
									Static: &vmv1beta1.StaticRef{
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
							var vmuser vmv1beta1.VMUser
							Expect(k8sClient.Get(ctx, nsn, &vmuser)).To(Succeed())
							Expect(vmuser.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusFailed))
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
							StringData: map[string]string{"password": "password-2"},
						}
						Expect(k8sClient.Create(ctx, &passwordSecret)).To(Succeed())
						DeferCleanup(func() {
							Expect(k8sClient.Delete(ctx, &passwordSecret)).To(Succeed())
						})
					},
					modify: func() {
						nsn := types.NamespacedName{Name: "missing-ref-1", Namespace: namespace}
						var toUpdate vmv1beta1.VMUser
						Expect(k8sClient.Get(ctx, nsn, &toUpdate)).To(Succeed())
						toUpdate.Spec.PasswordRef.Name = "backend-e2e-access-1"
						Expect(k8sClient.Update(ctx, &toUpdate)).To(Succeed())
					},
					verify: func() {
						for _, nsn := range []types.NamespacedName{
							{Name: "missing-ref-1", Namespace: namespace},
						} {
							Eventually(func() error {
								var toUpdate vmv1beta1.VMUser
								Expect(k8sClient.Get(ctx, nsn, &toUpdate)).To(Succeed())
								return expectConditionOkFor(toUpdate.Status.Conditions, "multiple-1.")
							}, eventualReadyTimeout, 2).Should(Succeed())
						}
					},
				},
			},
			),
		)
	})
})
