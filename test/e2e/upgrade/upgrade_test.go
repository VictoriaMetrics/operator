package upgrade

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

const (
	vmagentName  = "test-agent"
	vmsingleName = "test-single"
)

type vmAgentTestCase struct {
	operatorVersion string
	mod             func(*vmv1beta1.VMAgent)
	depSpec         appsv1.DeploymentSpec
}

type vmSingleTestCase struct {
	operatorVersion string
	mod             func(*vmv1beta1.VMSingle)
	depSpec         appsv1.DeploymentSpec
}

var _ = Describe("operator upgrade", Label("upgrade"), func() {
	DescribeTable("should not rollout VMAgent changes", func(operatorVersion string, mod func(*vmv1beta1.VMAgent)) {
		namespace := fmt.Sprintf("upgrade-%d", GinkgoParallelProcess())
		tc := vmAgentTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}

		err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		Expect(err).ToNot(HaveOccurred())
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By("creating VMAgent in " + namespace)
		cr := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmagentName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMAgentSpec{
				RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
					{URL: "http://localhost:8428/api/v1/write"},
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/vmagent",
						Tag:        "v1.136.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(5)),
				},
			},
		}
		if tc.mod != nil {
			tc.mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAgent to become operational")
		nsn := types.NamespacedName{Name: vmagentName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment and Service specs")
		childNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmagent-%s", vmagentName),
			Namespace: namespace,
		}

		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, childNSN, &dep)).ToNot(HaveOccurred())
		tc.depSpec = *dep.Spec.DeepCopy()
		expectedDepSpec := sanitizeDeploymentSpec(tc.depSpec.DeepCopy())
		expectedGeneration := dep.Generation

		removeOldOperator(ctx, k8sClient, namespace)

		cancelManager, managerDone := startNewOperator(ctx)
		DeferCleanup(func() {
			cancelManager()
			Eventually(managerDone, 60*time.Second, 2*time.Second).Should(BeClosed())
		})

		By("waiting for latest operator to reconcile VMAgent")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

		By("checking Deployment spec is unchanged")
		Expect(k8sClient.Get(ctx, childNSN, &dep)).ToNot(HaveOccurred())

		By("verifying deployment spec remains stable over time")
		Consistently(func() string {
			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, childNSN, &d)).ToNot(HaveOccurred())
			if d.Generation != expectedGeneration {
				s := sanitizeDeploymentSpec(d.Spec.DeepCopy())
				return cmp.Diff(*expectedDepSpec, *s)
			}
			return ""
		}, 15*time.Second, 3*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1beta1.VMAgent) {

		}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1beta1.VMAgent) {

		}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1beta1.VMAgent) {

		}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
	)

	DescribeTable("should not rollout VMSingle changes", func(operatorVersion string, mod func(*vmv1beta1.VMSingle)) {
		namespace := fmt.Sprintf("upgrade-%d", GinkgoParallelProcess())
		tc := vmSingleTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}

		err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		Expect(err).ToNot(HaveOccurred())
		if err != nil && !k8serrors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}

		By("creating VMSingle in " + namespace)
		cr := &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmsingleName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/victoria-metrics",
						Tag:        "v1.136.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(5)),
				},
			},
		}
		if tc.mod != nil {
			tc.mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMSingle to become operational")
		nsn := types.NamespacedName{Name: vmsingleName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMSingle{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment and Service specs")
		childNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmsingle-%s", vmsingleName),
			Namespace: namespace,
		}

		var dep appsv1.Deployment
		Expect(k8sClient.Get(ctx, childNSN, &dep)).ToNot(HaveOccurred())
		tc.depSpec = *dep.Spec.DeepCopy()
		expectedDepSpec := sanitizeDeploymentSpec(tc.depSpec.DeepCopy())
		expectedGeneration := dep.Generation

		removeOldOperator(ctx, k8sClient, namespace)

		cancelManager, managerDone := startNewOperator(ctx)
		DeferCleanup(func() {
			cancelManager()
			Eventually(managerDone, 60*time.Second, 2*time.Second).Should(BeClosed())
		})

		By("waiting for latest operator to reconcile VMSingle")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMSingle{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

		By("checking Deployment spec is unchanged")
		Expect(k8sClient.Get(ctx, childNSN, &dep)).ToNot(HaveOccurred())

		By("verifying deployment spec remains stable over time")
		Consistently(func() string {
			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, childNSN, &d)).ToNot(HaveOccurred())
			if d.Generation != expectedGeneration {
				s := sanitizeDeploymentSpec(d.Spec.DeepCopy())
				return cmp.Diff(*expectedDepSpec, *s)
			}
			return ""
		}, 15*time.Second, 3*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1beta1.VMSingle) {

		}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1beta1.VMSingle) {

		}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1beta1.VMSingle) {

		}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
	)

	AfterEach(func() {
		namespace := fmt.Sprintf("upgrade-%d", GinkgoParallelProcess())
		cleanupNamespace(ctx, k8sClient, namespace)
	})
})
