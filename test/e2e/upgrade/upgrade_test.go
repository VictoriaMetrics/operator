package upgrade

import (
	"fmt"
	"time"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

const (
	vmagentName   = "test-agent"
	vmsingleName  = "test-single"
	vmauthName    = "test-auth"
	vmalertName   = "test-alert"
	vmclusterName = "test-cluster"
)

type vmAgentTestCase struct {
	operatorVersion string
	mod             func(*vmv1beta1.VMAgent)
	depSpec         *appsv1.DeploymentSpec
	dsSpec          *appsv1.DaemonSetSpec
	stsSpec         *appsv1.StatefulSetSpec
}

type vmAuthTestCase struct {
	operatorVersion string
	mod             func(*vmv1beta1.VMAuth)
	depSpec         *appsv1.DeploymentSpec
}

type vmAlertTestCase struct {
	operatorVersion string
	mod             func(*vmv1beta1.VMAlert)
	depSpec         *appsv1.DeploymentSpec
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
	selectDepSpec   appsv1.StatefulSetSpec
	storageStsSpec  appsv1.StatefulSetSpec
}

var _ = Describe("operator upgrade", Label("upgrade"), func() {
	DescribeTable("should not rollout VMAgent changes", func(operatorVersion string, mod func(*vmv1beta1.VMAgent)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		tc := vmAgentTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}
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
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		childNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmagent-%s", vmagentName),
			Namespace: namespace,
		}

		var expectedGeneration int64
		Eventually(func() error {
			dep := &appsv1.Deployment{}
			getErr := k8sClient.Get(ctx, childNSN, dep)
			if getErr == nil {
				tc.depSpec = dep.Spec.DeepCopy()
				expectedGeneration = dep.Generation
			}
			return getErr
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

		removeOldOperator(ctx, k8sClient, namespace)

		startNewOperator(ctx)
		DeferCleanup(func() {
			defer GinkgoRecover()
			cleanupNamespace(ctx, k8sClient, namespace)
		})

		By("waiting for latest operator to reconcile VMAgent")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, childNSN, &d)).ToNot(HaveOccurred())
			if d.Generation != expectedGeneration {
				s := sanitizeDeploymentSpec(d.Spec.DeepCopy())
				expectedDepSpec := sanitizeDeploymentSpec(tc.depSpec.DeepCopy())
				return cmp.Diff(*expectedDepSpec, *s)
			}
			return ""
		}, 15*time.Second, 5*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1beta1.VMAgent) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1beta1.VMAgent) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1beta1.VMAgent) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
	)

	DescribeTable("should not rollout VMAgent changes (DaemonSet)", func(operatorVersion string, mod func(*vmv1beta1.VMAgent)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		tc := vmAgentTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}
		deployOldOperator(ctx, k8sClient, tc.operatorVersion, namespace)

		By("creating VMAgent in " + namespace)
		cr := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmagentName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMAgentSpec{
				DaemonSetMode: true,
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
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		childNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmagent-%s", vmagentName),
			Namespace: namespace,
		}

		var expectedGeneration int64
		Eventually(func() error {
			ds := &appsv1.DaemonSet{}
			getErr := k8sClient.Get(ctx, childNSN, ds)
			if getErr == nil {
				tc.dsSpec = ds.Spec.DeepCopy()
				expectedGeneration = ds.Generation
			}
			return getErr
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

		removeOldOperator(ctx, k8sClient, namespace)

		startNewOperator(ctx)
		DeferCleanup(func() {
			defer GinkgoRecover()
			cleanupNamespace(ctx, k8sClient, namespace)
		})

		By("waiting for latest operator to reconcile VMAgent")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			var d appsv1.DaemonSet
			Expect(k8sClient.Get(ctx, childNSN, &d)).ToNot(HaveOccurred())
			if d.Generation != expectedGeneration {
				return cmp.Diff(*tc.dsSpec, d.Spec)
			}
			return ""
		}, 15*time.Second, 5*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1beta1.VMAgent) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1beta1.VMAgent) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1beta1.VMAgent) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
	)

	DescribeTable("should not rollout VMAgent changes (StatefulSet)", func(operatorVersion string, mod func(*vmv1beta1.VMAgent)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		tc := vmAgentTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}
		deployOldOperator(ctx, k8sClient, tc.operatorVersion, namespace)

		By("creating VMAgent in " + namespace)
		cr := &vmv1beta1.VMAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmagentName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMAgentSpec{
				StatefulMode: true,
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
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		childNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmagent-%s", vmagentName),
			Namespace: namespace,
		}

		Eventually(func() error {
			sts := &appsv1.StatefulSet{}
			getErr := k8sClient.Get(ctx, childNSN, sts)
			if getErr == nil {
				tc.stsSpec = sts.Spec.DeepCopy()
			}
			return getErr
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

		removeOldOperator(ctx, k8sClient, namespace)

		startNewOperator(ctx)
		DeferCleanup(func() {
			defer GinkgoRecover()
			cleanupNamespace(ctx, k8sClient, namespace)
		})

		By("waiting for latest operator to reconcile VMAgent")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			var s appsv1.StatefulSet
			Expect(k8sClient.Get(ctx, childNSN, &s)).ToNot(HaveOccurred())
			return cmp.Diff(*tc.stsSpec, s.Spec)
		}, 15*time.Second, 5*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1beta1.VMAgent) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1beta1.VMAgent) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1beta1.VMAgent) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
	)

	DescribeTable("should not rollout VMSingle changes", func(operatorVersion string, mod func(*vmv1beta1.VMSingle)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		tc := vmSingleTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}
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
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment and Service specs")
		childNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmsingle-%s", vmsingleName),
			Namespace: namespace,
		}

		var dep appsv1.Deployment
		Eventually(func() error {
			return k8sClient.Get(ctx, childNSN, &dep)
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		tc.depSpec = *dep.Spec.DeepCopy()
		expectedDepSpec := sanitizeDeploymentSpec(tc.depSpec.DeepCopy())

		removeOldOperator(ctx, k8sClient, namespace)

		startNewOperator(ctx)
		DeferCleanup(func() {
			defer GinkgoRecover()

			cleanupNamespace(ctx, k8sClient, namespace)
		})

		By("waiting for latest operator to reconcile VMSingle")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMSingle{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("checking Deployment spec is unchanged")
		Expect(k8sClient.Get(ctx, childNSN, &dep)).ToNot(HaveOccurred())

		By("verifying deployment spec remains stable over time")
		Consistently(func() string {
			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, childNSN, &d)).ToNot(HaveOccurred())
			s := sanitizeDeploymentSpec(d.Spec.DeepCopy())
			return cmp.Diff(*expectedDepSpec, *s)
		}, 15*time.Second, 5*time.Second).Should(BeEmpty())
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

	DescribeTable("should not rollout VMAuth changes", func(operatorVersion string, mod func(*vmv1beta1.VMAuth)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		tc := vmAuthTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}
		deployOldOperator(ctx, k8sClient, tc.operatorVersion, namespace)

		By("creating VMAuth in " + namespace)
		cr := &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmauthName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMAuthSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/vmauth",
						Tag:        "v1.136.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(5)),
				},
				UnauthorizedAccessConfig: []vmv1beta1.UnauthorizedAccessConfigURLMap{
					{
						SrcPaths:  []string{"/api/v1/query"},
						URLPrefix: vmv1beta1.StringOrArray{"http://localhost:8428"},
					},
				},
			},
		}
		if tc.mod != nil {
			tc.mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAuth to become operational")
		nsn := types.NamespacedName{Name: vmauthName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAuth{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		childNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmauth-%s", vmauthName),
			Namespace: namespace,
		}

		var dep appsv1.Deployment
		Eventually(func() error {
			return k8sClient.Get(ctx, childNSN, &dep)
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		tc.depSpec = dep.Spec.DeepCopy()

		removeOldOperator(ctx, k8sClient, namespace)

		startNewOperator(ctx)
		DeferCleanup(func() {
			defer GinkgoRecover()
			cleanupNamespace(ctx, k8sClient, namespace)
		})

		By("waiting for latest operator to reconcile VMAuth")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAuth{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, childNSN, &d)).ToNot(HaveOccurred())
			s := sanitizeDeploymentSpec(d.Spec.DeepCopy())
			expectedDepSpec := sanitizeDeploymentSpec(tc.depSpec.DeepCopy())
			return cmp.Diff(*expectedDepSpec, *s)
		}, 15*time.Second, 5*time.Second).Should(BeEmpty())
	},
		PEntry("from v0.64.0", "v0.64.0", func(cr *vmv1beta1.VMAuth) {}),
		PEntry("from v0.64.1", "v0.64.1", func(cr *vmv1beta1.VMAuth) {}),
		PEntry("from v0.65.0", "v0.65.0", nil),
		PEntry("from v0.66.0", "v0.66.0", func(cr *vmv1beta1.VMAuth) {}),
		PEntry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
	)

	DescribeTable("should not rollout VMAlert changes", func(operatorVersion string, mod func(*vmv1beta1.VMAlert)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		tc := vmAlertTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}
		deployOldOperator(ctx, k8sClient, tc.operatorVersion, namespace)

		By("creating VMAlert in " + namespace)
		cr := &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmalertName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMAlertSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/vmalert",
						Tag:        "v1.136.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(5)),
				},
				Datasource: vmv1beta1.VMAlertDatasourceSpec{
					URL: "http://localhost:8428",
				},
				Notifier: &vmv1beta1.VMAlertNotifierSpec{
					URL: "http://localhost:9093",
				},
				EvaluationInterval: "15s",
			},
		}
		if tc.mod != nil {
			tc.mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAlert to become operational")
		nsn := types.NamespacedName{Name: vmalertName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAlert{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		childNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmalert-%s", vmalertName),
			Namespace: namespace,
		}

		var dep appsv1.Deployment
		Eventually(func() error {
			return k8sClient.Get(ctx, childNSN, &dep)
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		tc.depSpec = dep.Spec.DeepCopy()

		removeOldOperator(ctx, k8sClient, namespace)

		startNewOperator(ctx)
		DeferCleanup(func() {
			defer GinkgoRecover()
			cleanupNamespace(ctx, k8sClient, namespace)
		})

		By("waiting for latest operator to reconcile VMAlert")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAlert{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, childNSN, &d)).ToNot(HaveOccurred())
			s := sanitizeDeploymentSpec(d.Spec.DeepCopy())
			expectedDepSpec := sanitizeDeploymentSpec(tc.depSpec.DeepCopy())
			return cmp.Diff(*expectedDepSpec, *s)
		}, 15*time.Second, 5*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1beta1.VMAlert) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1beta1.VMAlert) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1beta1.VMAlert) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
	)

	DescribeTable("should not rollout VMCluster changes", func(operatorVersion string, mod func(*vmv1beta1.VMCluster)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		tc := vmClusterTestCase{
			operatorVersion: operatorVersion,
			mod:             mod,
		}
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
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment and StatefulSet specs")
		insertNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vminsert-%s", vmclusterName),
			Namespace: namespace,
		}
		var insertDep appsv1.Deployment
		Eventually(func() error {
			return k8sClient.Get(ctx, insertNSN, &insertDep)
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		tc.insertDepSpec = *insertDep.Spec.DeepCopy()
		expectedInsertDepSpec := sanitizeDeploymentSpec(tc.insertDepSpec.DeepCopy())

		selectNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmselect-%s", vmclusterName),
			Namespace: namespace,
		}
		var selectSTS appsv1.StatefulSet
		Eventually(func() error {
			return k8sClient.Get(ctx, selectNSN, &selectSTS)
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		tc.selectDepSpec = *selectSTS.Spec.DeepCopy()
		expectedSelectDepSpec := tc.selectDepSpec.DeepCopy()

		storageNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmstorage-%s", vmclusterName),
			Namespace: namespace,
		}
		var storageSts appsv1.StatefulSet
		Eventually(func() error {
			return k8sClient.Get(ctx, storageNSN, &storageSts)
		}, 5*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())
		tc.storageStsSpec = *storageSts.Spec.DeepCopy()

		removeOldOperator(ctx, k8sClient, namespace)

		startNewOperator(ctx)
		DeferCleanup(func() {
			defer GinkgoRecover()

			cleanupNamespace(ctx, k8sClient, namespace)
		})

		By("waiting for latest operator to reconcile VMCluster")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("checking VMInsert Deployment spec is unchanged")
		Expect(k8sClient.Get(ctx, insertNSN, &insertDep)).ToNot(HaveOccurred())

		By("checking VMSelect StatefulSet spec is unchanged")
		Expect(k8sClient.Get(ctx, selectNSN, &selectSTS)).ToNot(HaveOccurred())

		By("checking VMStorage StatefulSet spec is unchanged")
		Expect(k8sClient.Get(ctx, storageNSN, &storageSts)).ToNot(HaveOccurred())

		By("verifying specs remain stable over time")
		Consistently(func() string {
			var d appsv1.Deployment
			Expect(k8sClient.Get(ctx, insertNSN, &d)).ToNot(HaveOccurred())
			s := sanitizeDeploymentSpec(d.Spec.DeepCopy())
			diff := cmp.Diff(*expectedInsertDepSpec, *s)
			if diff != "" {
				return "insert:\n" + diff
			}

			var sts appsv1.StatefulSet
			Expect(k8sClient.Get(ctx, selectNSN, &sts)).ToNot(HaveOccurred())
			stsSpec := sts.Spec.DeepCopy()
			diff = cmp.Diff(*expectedSelectDepSpec, *stsSpec)
			if diff != "" {
				return "select:\n" + diff
			}

			Expect(k8sClient.Get(ctx, storageNSN, &sts)).ToNot(HaveOccurred())
			stsSpec = sts.Spec.DeepCopy()
			diff = cmp.Diff(*expectedSelectDepSpec, *stsSpec)
			if diff != "" {
				return "select:\n" + diff
			}
			return ""
		}, 15*time.Second, 5*time.Second).Should(BeEmpty())
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
