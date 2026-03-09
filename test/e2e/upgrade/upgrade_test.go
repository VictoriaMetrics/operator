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
	vmagentName   = "test-agent"
	vmsingleName  = "test-single"
	vmclusterName = "test-cluster"
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

type vmClusterTestCase struct {
	operatorVersion string
	mod             func(*vmv1beta1.VMCluster)
	insertDepSpec   appsv1.DeploymentSpec
	selectDepSpec   appsv1.DeploymentSpec
	storageStsSpec  appsv1.StatefulSetSpec
}

var _ = Describe("operator upgrade", Label("upgrade"), func() {
	DescribeTable("should not rollout VMAgent changes", func(operatorVersion string, mod func(*vmv1beta1.VMAgent)) {
		namespace := fmt.Sprintf("upgrade-%d", GinkgoParallelProcess())
		tc := vmAgentTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, &corev1.Namespace{})
			return k8serrors.IsNotFound(err)
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
		err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		Expect(err).ToNot(HaveOccurred())
		deployOldOperator(ctx, k8sClient, tc.operatorVersion, namespace)

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
			namespace := fmt.Sprintf("upgrade-%d", GinkgoParallelProcess())
			cleanupNamespace(ctx, k8sClient, namespace)

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

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, &corev1.Namespace{})
			return k8serrors.IsNotFound(err)
		}, 60*time.Second, 2*time.Second).Should(BeTrue())
		err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		Expect(err).ToNot(HaveOccurred())
		deployOldOperator(ctx, k8sClient, tc.operatorVersion, namespace)

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
			namespace := fmt.Sprintf("upgrade-%d", GinkgoParallelProcess())
			cleanupNamespace(ctx, k8sClient, namespace)
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

	DescribeTable("should not rollout VMCluster changes", func(operatorVersion string, mod func(*vmv1beta1.VMCluster)) {
		namespace := fmt.Sprintf("upgrade-%d", GinkgoParallelProcess())
		tc := vmClusterTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: namespace}, &corev1.Namespace{})
			return k8serrors.IsNotFound(err)
		}, 5*time.Minute, 5*time.Second).Should(BeTrue())
		err := k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}})
		Expect(err).ToNot(HaveOccurred())
		deployOldOperator(ctx, k8sClient, tc.operatorVersion, namespace)

		By("creating VMCluster in " + namespace)
		cr := &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmclusterName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod: "1",
				VMSelect: &vmv1beta1.VMSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vmselect",
							Tag:        "v1.136.0-cluster",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vminsert",
							Tag:        "v1.136.0-cluster",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vmstorage",
							Tag:        "v1.136.0-cluster",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
			},
		}
		if tc.mod != nil {
			tc.mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMCluster to become operational")
		nsn := types.NamespacedName{Name: vmclusterName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment and StatefulSet specs")
		insertNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vminsert-%s", vmclusterName),
			Namespace: namespace,
		}
		var insertDep appsv1.Deployment
		Expect(k8sClient.Get(ctx, insertNSN, &insertDep)).ToNot(HaveOccurred())
		tc.insertDepSpec = *insertDep.Spec.DeepCopy()
		expectedInsertDepSpec := sanitizeDeploymentSpec(tc.insertDepSpec.DeepCopy())
		expectedInsertGeneration := insertDep.Generation

		selectNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmselect-%s", vmclusterName),
			Namespace: namespace,
		}
		var selectDep appsv1.Deployment
		Expect(k8sClient.Get(ctx, selectNSN, &selectDep)).ToNot(HaveOccurred())
		tc.selectDepSpec = *selectDep.Spec.DeepCopy()
		expectedSelectDepSpec := sanitizeDeploymentSpec(tc.selectDepSpec.DeepCopy())
		expectedSelectGeneration := selectDep.Generation

		storageNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmstorage-%s", vmclusterName),
			Namespace: namespace,
		}
		var storageSts appsv1.StatefulSet
		Expect(k8sClient.Get(ctx, storageNSN, &storageSts)).ToNot(HaveOccurred())
		tc.storageStsSpec = *storageSts.Spec.DeepCopy()
		expectedStorageStsSpec := tc.storageStsSpec.DeepCopy()
		expectedStorageGeneration := storageSts.Generation

		removeOldOperator(ctx, k8sClient, namespace)

		cancelManager, managerDone := startNewOperator(ctx)
		DeferCleanup(func() {
			namespace := fmt.Sprintf("upgrade-%d", GinkgoParallelProcess())
			cleanupNamespace(ctx, k8sClient, namespace)
			cancelManager()
			Eventually(managerDone, 60*time.Second, 2*time.Second).Should(BeClosed())
		})

		By("waiting for latest operator to reconcile VMCluster")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 3*time.Second).ShouldNot(HaveOccurred())

		By("checking VMInsert Deployment spec is unchanged")
		Expect(k8sClient.Get(ctx, insertNSN, &insertDep)).ToNot(HaveOccurred())

		By("checking VMSelect Deployment spec is unchanged")
		Expect(k8sClient.Get(ctx, selectNSN, &selectDep)).ToNot(HaveOccurred())

		By("checking VMStorage StatefulSet spec is unchanged")
		Expect(k8sClient.Get(ctx, storageNSN, &storageSts)).ToNot(HaveOccurred())

		By("verifying specs remain stable over time")
		Consistently(func() string {
			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, insertNSN, &d)).ToNot(HaveOccurred())
			if d.Generation != expectedInsertGeneration {
				s := sanitizeDeploymentSpec(d.Spec.DeepCopy())
				return "insert:\n" + cmp.Diff(*expectedInsertDepSpec, *s)
			}

			Expect(k8sClient.Get(ctx, selectNSN, &d)).ToNot(HaveOccurred())
			if d.Generation != expectedSelectGeneration {
				s := sanitizeDeploymentSpec(d.Spec.DeepCopy())
				return "select:\n" + cmp.Diff(*expectedSelectDepSpec, *s)
			}

			var sts appsv1.StatefulSet
			Expect(k8sClient.Get(ctx, storageNSN, &sts)).ToNot(HaveOccurred())
			if sts.Generation != expectedStorageGeneration {
				s := sts.Spec.DeepCopy()
				return "storage:\n" + cmp.Diff(*expectedStorageStsSpec, *s)
			}
			return ""
		}, 15*time.Second, 3*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1beta1.VMCluster) {

		}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1beta1.VMCluster) {

		}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1beta1.VMCluster) {

		}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
	)
})
