package upgrade

import (
	"fmt"
	"os"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/test/e2e/suite"
)

const (
	vmagentName        = "test-agent"
	vmsingleName       = "test-single"
	vlsingleName       = "test-vlsingle"
	vlclusterName      = "test-vlcluster"
	vmauthName         = "test-auth"
	vmalertName        = "test-alert"
	vmclusterName      = "test-cluster"
	vtsingleName       = "test-vtsingle"
	vtclusterName      = "test-vtcluster"
	vmalertmanagerName = "test-am"
)

var _ = Describe("operator upgrade", Label("upgrade"), func() {
	DescribeTable("should not rollout VMAgent changes", func(operatorVersion string, mod func(*vmv1beta1.VMAgent)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

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
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
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
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAgent to become operational")
		nsn := types.NamespacedName{Name: vmagentName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		deploymentNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmagent-%s", vmagentName),
			Namespace: namespace,
		}
		expectedDeploymentSpec := snapshotDeployment(ctx, k8sClient, deploymentNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMAgent")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, deploymentNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		// Configurations before 0.67 would be forcibly rolled out
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	DescribeTable("should not rollout VMAgent changes (DaemonSet)", func(operatorVersion string, mod func(*vmv1beta1.VMAgent)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

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
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
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
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAgent to become operational")
		nsn := types.NamespacedName{Name: vmagentName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		daemonsetNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmagent-%s", vmagentName),
			Namespace: namespace,
		}
		expectedDaemonsetSpec := snapshotDaemonSet(ctx, k8sClient, daemonsetNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMAgent")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			return verifyDaemonSet(ctx, k8sClient, daemonsetNSN, expectedDaemonsetSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		// Configurations before 0.67 would be forcibly rolled out
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	DescribeTable("should not rollout VMAgent changes (StatefulSet)", func(operatorVersion string, mod func(*vmv1beta1.VMAgent)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

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
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
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
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAgent to become operational")
		nsn := types.NamespacedName{Name: vmagentName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmagent-%s", vmagentName),
			Namespace: namespace,
		}
		expectedStatefulSetSpec := snapshotStatefulSet(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMAgent")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			return verifyStatefulSet(ctx, k8sClient, resourceNSN, expectedStatefulSetSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		// Configurations before 0.67 would be forcibly rolled out
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	DescribeTable("should not rollout VMSingle changes", func(operatorVersion string, mod func(*vmv1beta1.VMSingle)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		By("creating VMSingle in " + namespace)
		cr := &vmv1beta1.VMSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmsingleName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMSingleSpec{
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
				},
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
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMSingle to become operational")
		nsn := types.NamespacedName{Name: vmsingleName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMSingle{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment specs")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmsingle-%s", vmsingleName),
			Namespace: namespace,
		}

		expectedDeploymentSpec := snapshotDeployment(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMSingle")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMSingle{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying deployment spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
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
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	DescribeTable("should not rollout VMAuth changes", func(operatorVersion string, mod func(*vmv1beta1.VMAuth)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		By("creating VMAuth in " + namespace)
		cr := &vmv1beta1.VMAuth{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmauthName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMAuthSpec{
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
				},
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
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAuth to become operational")
		nsn := types.NamespacedName{Name: vmauthName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAuth{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmauth-%s", vmauthName),
			Namespace: namespace,
		}

		expectedDeploymentSpec := snapshotDeployment(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMAuth")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAuth{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		// Configurations before 0.67 would be forcibly rolled out
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	DescribeTable("should not rollout VMAlert changes", func(operatorVersion string, mod func(*vmv1beta1.VMAlert)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		By("creating VMAlert in " + namespace)
		cr := &vmv1beta1.VMAlert{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmalertName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMAlertSpec{
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
				},
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
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAlert to become operational")
		nsn := types.NamespacedName{Name: vmalertName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAlert{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmalert-%s", vmalertName),
			Namespace: namespace,
		}

		expectedDeploymentSpec := snapshotDeployment(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMAlert")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAlert{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
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
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	FDescribeTable("should not rollout VMAlertmanager changes", func(operatorVersion string, mod func(*vmv1beta1.VMAlertmanager)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		By("creating VMAlertmanager in " + namespace)
		cr := &vmv1beta1.VMAlertmanager{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmalertmanagerName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMAlertmanagerSpec{
				CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
					ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/prometheus/alertmanager",
						Tag:        "v0.27.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(5)),
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAlertmanager to become operational")
		nsn := types.NamespacedName{Name: vmalertmanagerName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAlertmanager{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmalertmanager-%s", vmalertmanagerName),
			Namespace: namespace,
		}

		expectedStatefulSetSpec := snapshotStatefulSet(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMAlertmanager")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMAlertmanager{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			return verifyStatefulSet(ctx, k8sClient, resourceNSN, expectedStatefulSetSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		// Configurations before 0.67 would be forcibly rolled out
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	DescribeTable("should not rollout VMCluster changes", func(operatorVersion string, mod func(*vmv1beta1.VMCluster)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

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
		if mod != nil {
			mod(cr)
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
		expectedInsertSpec := snapshotDeployment(ctx, k8sClient, insertNSN)

		selectNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmselect-%s", vmclusterName),
			Namespace: namespace,
		}
		expectedSelectSpec := snapshotStatefulSet(ctx, k8sClient, selectNSN)

		storageNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmstorage-%s", vmclusterName),
			Namespace: namespace,
		}
		expectedStorageSpec := snapshotStatefulSet(ctx, k8sClient, storageNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMCluster")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying specs remain stable over time")
		Consistently(func() string {
			diff := verifyDeployment(ctx, k8sClient, insertNSN, expectedInsertSpec)
			if diff != "" {
				return "insert:\n" + diff
			}

			diff = verifyStatefulSet(ctx, k8sClient, selectNSN, expectedSelectSpec)
			if diff != "" {
				return "select:\n" + diff
			}

			diff = verifyStatefulSet(ctx, k8sClient, storageNSN, expectedStorageSpec)
			if diff != "" {
				return "storage:\n" + diff
			}
			return ""
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
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
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	DescribeTable("should not rollout VMCluster changes (RequestsLoadBalancer)", func(operatorVersion string, mod func(*vmv1beta1.VMCluster)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		By("creating VMCluster in " + namespace)
		cr := &vmv1beta1.VMCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmclusterName,
				Namespace: namespace,
			},
			Spec: vmv1beta1.VMClusterSpec{
				RetentionPeriod: "1",
				RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
					Enabled: true,
					Spec: vmv1beta1.VMAuthLoadBalancerSpec{
						CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
							ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
						},
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
							Image: vmv1beta1.Image{
								Repository: "quay.io/victoriametrics/vmauth",
								Tag:        "v1.136.0",
							},
							TerminationGracePeriodSeconds: ptr.To(int64(5)),
						},
					},
				},
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
		if mod != nil {
			mod(cr)
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
		expectedInsertSpec := snapshotDeployment(ctx, k8sClient, insertNSN)

		selectNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmselect-%s", vmclusterName),
			Namespace: namespace,
		}
		expectedSelectSpec := snapshotStatefulSet(ctx, k8sClient, selectNSN)

		storageNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmstorage-%s", vmclusterName),
			Namespace: namespace,
		}
		expectedStorageSpec := snapshotStatefulSet(ctx, k8sClient, storageNSN)

		lbNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmclusterlb-%s", vmclusterName),
			Namespace: namespace,
		}
		expectedLBSpec := snapshotDeployment(ctx, k8sClient, lbNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMCluster")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1beta1.VMCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying specs remain stable over time")
		Consistently(func() string {
			diff := verifyDeployment(ctx, k8sClient, insertNSN, expectedInsertSpec)
			if diff != "" {
				return "insert:\n" + diff
			}

			diff = verifyStatefulSet(ctx, k8sClient, selectNSN, expectedSelectSpec)
			if diff != "" {
				return "select:\n" + diff
			}

			diff = verifyStatefulSet(ctx, k8sClient, storageNSN, expectedStorageSpec)
			if diff != "" {
				return "storage:\n" + diff
			}

			diff = verifyDeployment(ctx, k8sClient, lbNSN, expectedLBSpec)
			if diff != "" {
				return "lb:\n" + diff
			}

			return ""
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1beta1.VMCluster) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1beta1.VMCluster) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1beta1.VMCluster) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	DescribeTable("should not rollout VLSingle changes", func(operatorVersion string, mod func(*vmv1.VLSingle)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		cr := &vmv1.VLSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vlsingleName,
				Namespace: namespace,
			},
			Spec: vmv1.VLSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/victoria-logs",
						Tag:        "v1.44.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(5)),
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VLSingle to become operational")
		nsn := types.NamespacedName{Name: vlsingleName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLSingle{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment specs")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlsingle-%s", vlsingleName),
			Namespace: namespace,
		}

		expectedDeploymentSpec := snapshotDeployment(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VLSingle")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLSingle{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying deployment spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1.VLSingle) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1.VLSingle) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1.VLSingle) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	DescribeTable("should not rollout VLCluster changes", func(operatorVersion string, mod func(*vmv1.VLCluster)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		cr := &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vlclusterName,
				Namespace: namespace,
			},
			Spec: vmv1.VLClusterSpec{
				VLSelect: &vmv1.VLSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				VLInsert: &vmv1.VLInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				VLStorage: &vmv1.VLStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VLCluster to become operational")
		nsn := types.NamespacedName{Name: vlclusterName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment and StatefulSet specs")
		insertNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlinsert-%s", vlclusterName),
			Namespace: namespace,
		}
		expectedInsertSpec := snapshotDeployment(ctx, k8sClient, insertNSN)

		selectNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlselect-%s", vlclusterName),
			Namespace: namespace,
		}
		expectedSelectSpec := snapshotDeployment(ctx, k8sClient, selectNSN)

		storageNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlstorage-%s", vlclusterName),
			Namespace: namespace,
		}
		expectedStorageSpec := snapshotStatefulSet(ctx, k8sClient, storageNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VLCluster")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying specs remain stable over time")
		Consistently(func() string {
			diff := verifyDeployment(ctx, k8sClient, insertNSN, expectedInsertSpec)
			if diff != "" {
				return "insert:\n" + diff
			}

			diff = verifyDeployment(ctx, k8sClient, selectNSN, expectedSelectSpec)
			if diff != "" {
				return "select:\n" + diff
			}

			diff = verifyStatefulSet(ctx, k8sClient, storageNSN, expectedStorageSpec)
			if diff != "" {
				return "storage:\n" + diff
			}
			return ""
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1.VLCluster) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1.VLCluster) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1.VLCluster) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	DescribeTable("should not rollout VLCluster changes (RequestsLoadBalancer)", func(operatorVersion string, mod func(*vmv1.VLCluster)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		By("creating VLCluster in " + namespace)
		cr := &vmv1.VLCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vlclusterName,
				Namespace: namespace,
			},
			Spec: vmv1.VLClusterSpec{
				RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
					Enabled: true,
					Spec: vmv1beta1.VMAuthLoadBalancerSpec{
						CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
							ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
						},
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
							Image: vmv1beta1.Image{
								Repository: "quay.io/victoriametrics/vmauth",
								Tag:        "v1.136.0",
							},
							TerminationGracePeriodSeconds: ptr.To(int64(5)),
						},
					},
				},
				VLSelect: &vmv1.VLSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				VLInsert: &vmv1.VLInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				VLStorage: &vmv1.VLStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VLCluster to become operational")
		nsn := types.NamespacedName{Name: vlclusterName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment and StatefulSet specs")
		insertNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlinsert-%s", vlclusterName),
			Namespace: namespace,
		}
		expectedInsertSpec := snapshotDeployment(ctx, k8sClient, insertNSN)

		selectNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlselect-%s", vlclusterName),
			Namespace: namespace,
		}
		expectedSelectSpec := snapshotDeployment(ctx, k8sClient, selectNSN)

		storageNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlstorage-%s", vlclusterName),
			Namespace: namespace,
		}
		expectedStorageSpec := snapshotStatefulSet(ctx, k8sClient, storageNSN)

		lbNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlclusterlb-%s", vlclusterName),
			Namespace: namespace,
		}
		expectedLBSpec := snapshotDeployment(ctx, k8sClient, lbNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VLCluster")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying specs remain stable over time")
		Consistently(func() string {
			diff := verifyDeployment(ctx, k8sClient, insertNSN, expectedInsertSpec)
			if diff != "" {
				return "insert:\n" + diff
			}

			diff = verifyDeployment(ctx, k8sClient, selectNSN, expectedSelectSpec)
			if diff != "" {
				return "select:\n" + diff
			}

			diff = verifyStatefulSet(ctx, k8sClient, storageNSN, expectedStorageSpec)
			if diff != "" {
				return "storage:\n" + diff
			}

			diff = verifyDeployment(ctx, k8sClient, lbNSN, expectedLBSpec)
			if diff != "" {
				return "lb:\n" + diff
			}

			return ""
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1.VLCluster) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1.VLCluster) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1.VLCluster) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	DescribeTable("should not rollout VTSingle changes", func(operatorVersion string, mod func(*vmv1.VTSingle)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		cr := &vmv1.VTSingle{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vtsingleName,
				Namespace: namespace,
			},
			Spec: vmv1.VTSingleSpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/victoria-traces",
						Tag:        "v0.4.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(5)),
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VTSingle to become operational")
		nsn := types.NamespacedName{Name: vtsingleName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VTSingle{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment specs")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vtsingle-%s", vtsingleName),
			Namespace: namespace,
		}

		expectedDeploymentSpec := snapshotDeployment(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VTSingle")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VTSingle{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying deployment spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1.VTSingle) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1.VTSingle) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1.VTSingle) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	DescribeTable("should not rollout VTCluster changes", func(operatorVersion string, mod func(*vmv1.VTCluster)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		cr := &vmv1.VTCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vtclusterName,
				Namespace: namespace,
			},
			Spec: vmv1.VTClusterSpec{
				Select: &vmv1.VTSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				Insert: &vmv1.VTInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				Storage: &vmv1.VTStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VTCluster to become operational")
		nsn := types.NamespacedName{Name: vtclusterName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VTCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment and StatefulSet specs")
		insertNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vtinsert-%s", vtclusterName),
			Namespace: namespace,
		}
		expectedInsertSpec := snapshotDeployment(ctx, k8sClient, insertNSN)

		selectNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vtselect-%s", vtclusterName),
			Namespace: namespace,
		}
		expectedSelectSpec := snapshotDeployment(ctx, k8sClient, selectNSN)

		storageNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vtstorage-%s", vtclusterName),
			Namespace: namespace,
		}
		expectedStorageSpec := snapshotStatefulSet(ctx, k8sClient, storageNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VTCluster")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VTCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying specs remain stable over time")
		Consistently(func() string {
			diff := verifyDeployment(ctx, k8sClient, insertNSN, expectedInsertSpec)
			if diff != "" {
				return "insert:\n" + diff
			}

			diff = verifyDeployment(ctx, k8sClient, selectNSN, expectedSelectSpec)
			if diff != "" {
				return "select:\n" + diff
			}

			diff = verifyStatefulSet(ctx, k8sClient, storageNSN, expectedStorageSpec)
			if diff != "" {
				return "storage:\n" + diff
			}
			return ""
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1.VTCluster) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1.VTCluster) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1.VTCluster) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	DescribeTable("should not rollout VTCluster changes (RequestsLoadBalancer)", func(operatorVersion string, mod func(*vmv1.VTCluster)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		By("creating VTCluster in " + namespace)
		cr := &vmv1.VTCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vtclusterName,
				Namespace: namespace,
			},
			Spec: vmv1.VTClusterSpec{
				RequestsLoadBalancer: vmv1beta1.VMAuthLoadBalancer{
					Enabled: true,
					Spec: vmv1beta1.VMAuthLoadBalancerSpec{
						CommonConfigReloaderParams: vmv1beta1.CommonConfigReloaderParams{
							ConfigReloaderImage: "quay.io/victoriametrics/operator:config-reloader-v0.65.0",
						},
						CommonAppsParams: vmv1beta1.CommonAppsParams{
							ReplicaCount: ptr.To[int32](1),
							Image: vmv1beta1.Image{
								Repository: "quay.io/victoriametrics/vmauth",
								Tag:        "v1.136.0",
							},
							TerminationGracePeriodSeconds: ptr.To(int64(5)),
						},
					},
				},
				Select: &vmv1.VTSelect{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				Insert: &vmv1.VTInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
				Storage: &vmv1.VTStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(5)),
					},
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VTCluster to become operational")
		nsn := types.NamespacedName{Name: vtclusterName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VTCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child Deployment and StatefulSet specs")
		insertNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vtinsert-%s", vtclusterName),
			Namespace: namespace,
		}
		expectedInsertSpec := snapshotDeployment(ctx, k8sClient, insertNSN)

		selectNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vtselect-%s", vtclusterName),
			Namespace: namespace,
		}
		expectedSelectSpec := snapshotDeployment(ctx, k8sClient, selectNSN)

		storageNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vtstorage-%s", vtclusterName),
			Namespace: namespace,
		}
		expectedStorageSpec := snapshotStatefulSet(ctx, k8sClient, storageNSN)

		lbNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vtclusterlb-%s", vtclusterName),
			Namespace: namespace,
		}
		expectedLBSpec := snapshotDeployment(ctx, k8sClient, lbNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VTCluster")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VTCluster{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying specs remain stable over time")
		Consistently(func() string {
			diff := verifyDeployment(ctx, k8sClient, insertNSN, expectedInsertSpec)
			if diff != "" {
				return "insert:\n" + diff
			}

			diff = verifyDeployment(ctx, k8sClient, selectNSN, expectedSelectSpec)
			if diff != "" {
				return "select:\n" + diff
			}

			diff = verifyStatefulSet(ctx, k8sClient, storageNSN, expectedStorageSpec)
			if diff != "" {
				return "storage:\n" + diff
			}

			diff = verifyDeployment(ctx, k8sClient, lbNSN, expectedLBSpec)
			if diff != "" {
				return "lb:\n" + diff
			}

			return ""
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", func(cr *vmv1.VTCluster) {}),
		Entry("from v0.64.1", "v0.64.1", func(cr *vmv1.VTCluster) {}),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", func(cr *vmv1.VTCluster) {}),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	Describe("VM_LOOPBACK behavior", func() {
		It("should handle VM_LOOPBACK env var behaviour", func() {
			namespace := createRandomNamespace(ctx, k8sClient)
			deployOldOperator(ctx, k8sClient, "v0.68.2", namespace)

			By("creating VMAgent in " + namespace)
			cr := &vmv1beta1.VMAgent{
				ObjectMeta: metav1.ObjectMeta{
					Name:      vmagentName,
					Namespace: namespace,
				},
				Spec: vmv1beta1.VMAgentSpec{
					RemoteWrite: []vmv1beta1.VMAgentRemoteWriteSpec{
						{URL: "http://[::1]:8428/api/v1/write"},
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
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

			By("waiting for VMAgent to become operational")
			nsn := types.NamespacedName{Name: vmagentName, Namespace: namespace}
			Eventually(func() error {
				return suite.ExpectObjectStatus(ctx, k8sClient,
					&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
			}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

			By("snapshotting child workload specs")
			resourceNSN := types.NamespacedName{
				Name:      fmt.Sprintf("vmagent-%s", vmagentName),
				Namespace: namespace,
			}
			expectedDeploymentSpec := snapshotDeployment(ctx, k8sClient, resourceNSN)

			By("updating VM_LOOPBACK on the new operator and restarting it")
			os.Setenv("VM_LOOPBACK", "[::1]")
			config.MustGetBaseConfig().Loopback = "[::1]"
			DeferCleanup(func() {
				os.Unsetenv("VM_LOOPBACK")
				config.MustGetBaseConfig().Loopback = ""
			})

			restartManagerAndCleanup(ctx, k8sClient, namespace)

			By("waiting for latest operator to reconcile VMAgent")
			Eventually(func() error {
				return suite.ExpectObjectStatus(ctx, k8sClient,
					&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
			}, 90*time.Second, 5*time.Second).ShouldNot(HaveOccurred())

			By("verifying workload spec has not changed due to VM_LOOPBACK")
			Eventually(func() string {
				return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
			}, 5*time.Second, 1*time.Second).ShouldNot(BeEmpty(), "expected rollout because VM_LOOPBACK changed")
		})
	})
})
