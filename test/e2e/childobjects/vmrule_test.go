package childobjects

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

//nolint:dupl,lll
var _ = Describe("test vmrule Controller", Label("vm", "child", "alert"), func() {
	namespace := fmt.Sprintf("default-%d", GinkgoParallelProcess())
	ctx := context.Background()
	type opts struct {
		alerts []*vmv1beta1.VMAlert
		rules  []*vmv1beta1.VMRule
	}
	type step struct {
		setup  func()
		modify func()
		verify func()
	}
	Context("with operator controller performing", func() {
		DescribeTable("config build and status update", func(args *opts, steps []step) {
			DeferCleanup(func() {
				for _, alert := range args.alerts {
					alert.Namespace = namespace
					Expect(k8sClient.Delete(ctx, alert)).To(Succeed())
				}
				for _, rule := range args.rules {
					Expect(k8sClient.Delete(ctx, rule)).To(Succeed())
				}
				for _, alert := range args.alerts {
					nsn := types.NamespacedName{Name: alert.Name, Namespace: alert.Namespace}
					Eventually(func() error {
						return k8sClient.Get(ctx, nsn, &vmv1beta1.VMAlert{})
					}, eventualDeletionTimeout).Should(MatchError(k8serrors.IsNotFound, "isNotFound"))
				}
			})
			step := steps[0]
			if step.setup != nil {
				step.setup()
			}
			for _, alert := range args.alerts {
				Expect(k8sClient.Create(ctx, alert)).To(Succeed())
			}
			for _, rule := range args.rules {
				Expect(k8sClient.Create(ctx, rule)).To(Succeed())
			}

			for _, alert := range args.alerts {
				Eventually(func() error {
					return suite.ExpectObjectStatus(ctx,
						k8sClient,
						&vmv1beta1.VMAlert{},
						types.NamespacedName{Name: alert.Name, Namespace: alert.Namespace},
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
			Entry("by applying rule to the single vmalert ok", &opts{
				alerts: []*vmv1beta1.VMAlert{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "vmalert-single-test",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMAlertSpec{
							SelectAllByDefault: true,
							Datasource: vmv1beta1.VMAlertDatasourceSpec{
								URL: "http://localhost:8428",
							},
						},
					},
				},
				rules: []*vmv1beta1.VMRule{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "rule-1",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMRuleSpec{
							Groups: []vmv1beta1.RuleGroup{
								{
									Name: "simple-rule",
									Rules: []vmv1beta1.Rule{
										{
											Expr:   `vector(1) > 0`,
											Alert:  "always firing",
											Labels: map[string]string{"severity": "error"},
										},
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
							{Name: "rule-1", Namespace: namespace},
						} {
							var vmrule vmv1beta1.VMRule
							Expect(k8sClient.Get(ctx, nsn, &vmrule)).To(Succeed())
							Expect(vmrule.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusOperational))
						}
					},
				},
			},
			),
			Entry("by using vmalert selector", &opts{
				alerts: []*vmv1beta1.VMAlert{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "selector-1",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMAlertSpec{
							SelectAllByDefault: false,
							RuleSelector:       metav1.SetAsLabelSelector(map[string]string{"exact-match-label": "value-1"}),
							Datasource: vmv1beta1.VMAlertDatasourceSpec{
								URL: "http://localhost:8428",
							},
						},
					},
				},
				rules: []*vmv1beta1.VMRule{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "selector-1",
							Namespace: namespace,
							Labels: map[string]string{
								"exact-match-label": "value-1",
							},
						},
						Spec: vmv1beta1.VMRuleSpec{
							Groups: []vmv1beta1.RuleGroup{
								{
									Name: "simple-rule",
									Rules: []vmv1beta1.Rule{
										{
											Expr:   `vector(1) > 0`,
											Alert:  "always firing",
											Labels: map[string]string{"severity": "error"},
										},
									},
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "selector-miss-1",
							Namespace: namespace,
							Labels: map[string]string{
								"exact-match-label": "value-2",
							},
						},
						Spec: vmv1beta1.VMRuleSpec{
							Groups: []vmv1beta1.RuleGroup{
								{
									Name: "simple-rule",
									Rules: []vmv1beta1.Rule{
										{
											Expr:   `vector(1) > 0`,
											Alert:  "always firing",
											Labels: map[string]string{"severity": "error"},
										},
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
							{Name: "selector-1", Namespace: namespace},
						} {
							var vmrule vmv1beta1.VMRule
							Expect(k8sClient.Get(ctx, nsn, &vmrule)).To(Succeed())
							Expect(vmrule.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusOperational))
							var matched bool
							for _, stCond := range vmrule.Status.Conditions {
								if strings.Contains(stCond.Type, "selector-1") {
									matched = true
									break
								}
							}
							Expect(matched).To(BeTrue(), "should match vmalert with specific selector")

						}
						for _, nsn := range []types.NamespacedName{
							{Name: "selector-miss-1", Namespace: namespace},
						} {
							var vmrule vmv1beta1.VMRule
							Expect(k8sClient.Get(ctx, nsn, &vmrule)).To(Succeed())
							var matched bool
							for _, stCond := range vmrule.Status.Conditions {
								if strings.Contains(stCond.Type, "selector-1") {
									matched = true
									break
								}
							}
							Expect(matched).To(BeFalse(), "should not match vmalert with specific selector")
						}
					},
				},
			},
			),
			Entry("by skipping broken vmrules", &opts{
				alerts: []*vmv1beta1.VMAlert{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "parsing-test",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMAlertSpec{
							SelectAllByDefault: true,
							Datasource: vmv1beta1.VMAlertDatasourceSpec{
								URL: "http://localhost:8428",
							},
						},
					},
				},
				rules: []*vmv1beta1.VMRule{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "bad-func-1",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMRuleSpec{
							Groups: []vmv1beta1.RuleGroup{
								{
									Name: "func-rule",
									Rules: []vmv1beta1.Rule{
										{
											Expr:   `non_exist_func(1) > 0`,
											Alert:  "always firing",
											Labels: map[string]string{"severity": "error"},
										},
									},
								},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "bad-template-1",
							Namespace: namespace,
						},
						Spec: vmv1beta1.VMRuleSpec{
							Groups: []vmv1beta1.RuleGroup{
								{
									Name: "templ-rule",
									Rules: []vmv1beta1.Rule{
										{
											Expr:   `vector(1) > 0`,
											Alert:  "always firing vector",
											Labels: map[string]string{"severity": `error: {{ bad_temp_func 1 }}`},
										},
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
							{Name: "bad-func-1", Namespace: namespace},
							{Name: "bad-template-1", Namespace: namespace},
						} {
							var vmrule vmv1beta1.VMRule
							Expect(k8sClient.Get(ctx, nsn, &vmrule)).To(Succeed())
							Expect(vmrule.Status.UpdateStatus).To(Equal(vmv1beta1.UpdateStatusFailed))
							for _, cond := range vmrule.Status.Conditions {
								if strings.HasSuffix(cond.Type, vmv1beta1.ConditionDomainTypeAppliedSuffix) {
									Expect(cond.Status).To(Equal(metav1.ConditionFalse), "reason=%q,type=%q,rule=%q", cond.Reason, cond.Type, vmrule.Name)
								}
							}
						}
					},
				},
			},
			),
		)
	})
})
