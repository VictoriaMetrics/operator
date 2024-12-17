package childobjects

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/context"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	v1beta1vm "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

//nolint:dupl,lll
var _ = Describe("test vmalertmanagerconfig Controller", func() {
	namespace := "default"
	ctx := context.Background()
	type opts struct {
		ams    []*v1beta1vm.VMAlertmanager
		amCfgs []*v1beta1vm.VMAlertmanagerConfig
	}
	type step struct {
		setup  func()
		modify func()
		verify func()
	}
	Context("with operator controller performing", func() {
		DescribeTable("build and check status", func(args *opts, steps []step) {
			DeferCleanup(func() {
				for _, am := range args.ams {
					am.Namespace = namespace
					Expect(k8sClient.Delete(ctx, am)).To(Succeed())
				}
				for _, amcfg := range args.amCfgs {
					Expect(k8sClient.Delete(ctx, amcfg)).To(Succeed())
				}
				for _, alert := range args.ams {
					nsn := types.NamespacedName{Name: alert.Name, Namespace: alert.Namespace}
					Eventually(func() error {
						return k8sClient.Get(ctx, nsn, &v1beta1vm.VMAlertmanager{})
					}, eventualDeletionTimeout).Should(MatchError(errors.IsNotFound, "isNotFound"))
				}
			})
			step := steps[0]
			if step.setup != nil {
				step.setup()
			}
			for _, am := range args.ams {
				Expect(k8sClient.Create(ctx, am)).To(Succeed())
			}
			for _, amcfg := range args.amCfgs {
				Expect(k8sClient.Create(ctx, amcfg)).To(Succeed())
			}

			for _, am := range args.ams {
				Eventually(func() error {
					return suite.ExpectObjectStatus(ctx,
						k8sClient,
						&v1beta1vm.VMAlertmanager{},
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
			Entry("by creating single alertmanager and rule", &opts{
				ams: []*v1beta1vm.VMAlertmanager{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "single-1",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMAlertmanagerSpec{
							SelectAllByDefault: true,
						},
					},
				},
				amCfgs: []*v1beta1vm.VMAlertmanagerConfig{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "blackhole-1",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMAlertmanagerConfigSpec{
							Route:     &v1beta1vm.Route{Receiver: "blackhole"},
							Receivers: []v1beta1vm.Receiver{{Name: "blackhole"}},
						},
					},
				},
			}, []step{
				{
					verify: func() {
						for _, nsn := range []types.NamespacedName{
							{Name: "blackhole-1", Namespace: namespace},
						} {
							var amcfg v1beta1vm.VMAlertmanagerConfig
							Expect(k8sClient.Get(ctx, nsn, &amcfg)).To(Succeed())
							Expect(amcfg.Status.UpdateStatus).To(Equal(v1beta1vm.UpdateStatusOperational))
						}
					},
				},
			},
			),
			Entry("by using specific resource by label selector", &opts{
				ams: []*v1beta1vm.VMAlertmanager{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "selector-1",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMAlertmanagerSpec{
							SelectAllByDefault: false,
							ConfigSelector: metav1.SetAsLabelSelector(map[string]string{
								"exact-match-label": "value-1",
							}),
						},
					},
				},
				amCfgs: []*v1beta1vm.VMAlertmanagerConfig{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "selector-1",
							Namespace: namespace,
							Labels: map[string]string{
								"exact-match-label": "value-1",
							},
						},
						Spec: v1beta1vm.VMAlertmanagerConfigSpec{
							Route:     &v1beta1vm.Route{Receiver: "blackhole"},
							Receivers: []v1beta1vm.Receiver{{Name: "blackhole"}},
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
						Spec: v1beta1vm.VMAlertmanagerConfigSpec{
							Route:     &v1beta1vm.Route{Receiver: "blackhole"},
							Receivers: []v1beta1vm.Receiver{{Name: "blackhole"}},
						},
					},
				},
			}, []step{
				{
					verify: func() {
						for _, nsn := range []types.NamespacedName{
							{Name: "selector-1", Namespace: namespace},
						} {
							var amcfg v1beta1vm.VMAlertmanagerConfig
							Expect(k8sClient.Get(ctx, nsn, &amcfg)).To(Succeed())
							Expect(amcfg.Status.UpdateStatus).To(Equal(v1beta1vm.UpdateStatusOperational))
							var matched bool
							for _, stCond := range amcfg.Status.Conditions {
								if strings.Contains(stCond.Type, "selector-1") {
									matched = true
									break
								}
							}
							Expect(matched).To(BeTrue(),
								"should match VMAlertmanager with specific selector, has conditions=%d,reason=%q,status=%q",
								len(amcfg.Status.Conditions), amcfg.Status.Reason, amcfg.Status.UpdateStatus)
						}
						for _, nsn := range []types.NamespacedName{
							{Name: "selector-miss-1", Namespace: namespace},
						} {
							var vmrule v1beta1vm.VMAlertmanagerConfig
							Expect(k8sClient.Get(ctx, nsn, &vmrule)).To(Succeed())
							var matched bool
							for _, stCond := range vmrule.Status.Conditions {
								Expect(stCond.Type).NotTo(ContainSubstring("selector-1"), "should not match VMAlertmanager with specific selector,got=%q", stCond.Type)
							}
							Expect(matched).To(BeFalse(), "should not match VMAlertmanager with specific selector")
						}
					},
				},
			},
			),
			Entry("by checking status of bad configs", &opts{
				ams: []*v1beta1vm.VMAlertmanager{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "parsing-test",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMAlertmanagerSpec{
							SelectAllByDefault: true,
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "parsing-test-with-global-option",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMAlertmanagerSpec{
							SelectAllByDefault: true,
							ConfigRawYaml: `
global:
 slack_api_url: https://example.com
receivers:
- name: blackhole
route:
 receiver: blackhole
              `,
						},
					},
				},
				amCfgs: []*v1beta1vm.VMAlertmanagerConfig{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "bad-route",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMAlertmanagerConfigSpec{
							Route: &v1beta1vm.Route{Receiver: "blackhole", ActiveTimeIntervals: []string{"not-exist"}},
							Receivers: []v1beta1vm.Receiver{
								{Name: "blackhole"},
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "partiallly-ok",
							Namespace: namespace,
						},
						Spec: v1beta1vm.VMAlertmanagerConfigSpec{
							Route: &v1beta1vm.Route{Receiver: "slack"},
							Receivers: []v1beta1vm.Receiver{
								{
									Name: "slack",
									SlackConfigs: []v1beta1vm.SlackConfig{
										{
											Channel:   "some-test",
											Text:      "some-text",
											Title:     "some-title",
											LinkNames: false,
											ThumbURL:  "some-url",
											Pretext:   "text-1",
											Username:  "some-user",
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
							{Name: "bad-route", Namespace: namespace},
						} {
							var amcfg v1beta1vm.VMAlertmanagerConfig
							Expect(k8sClient.Get(ctx, nsn, &amcfg)).To(Succeed())
							Expect(amcfg.Status.UpdateStatus).To(Equal(v1beta1vm.UpdateStatusFailed))
							for _, cond := range amcfg.Status.Conditions {
								if strings.HasSuffix(cond.Type, v1beta1vm.ConditionDomainTypeAppliedSuffix) {
									Expect(cond.Status).To(Equal(metav1.ConditionFalse), "reason=%q,type=%q,rule=%q", cond.Reason, cond.Type, amcfg.Name)
								}
							}
						}
						for _, nsn := range []types.NamespacedName{
							{Name: "partiallly-ok", Namespace: namespace},
						} {
							var amcfg v1beta1vm.VMAlertmanagerConfig
							Expect(k8sClient.Get(ctx, nsn, &amcfg)).To(Succeed())
							Expect(amcfg.Status.UpdateStatus).To(Equal(v1beta1vm.UpdateStatusFailed))
							for _, cond := range amcfg.Status.Conditions {
								if strings.HasPrefix(cond.Type, "parsing-test-with-global-option") {
									Expect(cond.Status).To(Equal(metav1.ConditionTrue), "reason=%q,type=%q,rule=%q", cond.Reason, cond.Type, amcfg.Name)
									continue
								}
								if strings.HasSuffix(cond.Type, v1beta1vm.ConditionDomainTypeAppliedSuffix) {
									Expect(cond.Status).To(Equal(metav1.ConditionFalse), "reason=%q,type=%q,rule=%q", cond.Reason, cond.Type, amcfg.Name)
								}
							}
						}

					},
				},
				// add global option to the all alertmanagers
				{
					modify: func() {
						var toUpdate v1beta1vm.VMAlertmanager
						nsn := types.NamespacedName{
							Namespace: namespace,
							Name:      "parsing-test",
						}
						Expect(k8sClient.Get(ctx, nsn, &toUpdate)).To(Succeed())
						toUpdate.Spec.ConfigRawYaml = `
global:
 slack_api_url: https://example.com
receivers:
- name: blackhole
route:
 receiver: blackhole
`
						Expect(k8sClient.Update(ctx, &toUpdate)).To(Succeed())
						Eventually(func() error {
							return suite.ExpectObjectStatus(ctx, k8sClient, &v1beta1vm.VMAlertmanager{}, nsn, v1beta1vm.UpdateStatusOperational)
						}, eventualReadyTimeout).Should(Succeed())
					},
					verify: func() {
						for _, nsn := range []types.NamespacedName{
							{Name: "partiallly-ok", Namespace: namespace},
						} {
							var amcfg v1beta1vm.VMAlertmanagerConfig
							Expect(k8sClient.Get(ctx, nsn, &amcfg)).To(Succeed())
							if len(amcfg.Status.Conditions) == 2 {
								Expect(amcfg.Status.UpdateStatus).To(Equal(v1beta1vm.UpdateStatusOperational))
								Expect(amcfg.Status.Reason).To(BeEmpty())
							}
							for _, cond := range amcfg.Status.Conditions {
								switch {
								case strings.HasPrefix(cond.Type, "parsing-test."):
								case strings.HasPrefix(cond.Type, "parsing-test-with-global-option."):
								default:
									continue
								}
								Expect(cond.Status).To(Equal(metav1.ConditionTrue), "reason=%q,type=%q,rule=%q", cond.Reason, cond.Type, amcfg.Name)
							}
						}

					},
				},
			},
			),
		)
	})
})
