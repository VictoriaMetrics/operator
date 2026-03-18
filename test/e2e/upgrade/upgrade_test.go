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
	vmv1alpha1 "github.com/VictoriaMetrics/operator/api/operator/v1alpha1"
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
	vlagentName        = "test-vlagent"
	vmanomalyName      = "test-anomaly"
	vmdistributedName  = "test-distributed"
)

var (
	waitTimeout = 5 * time.Minute

	vmsingleUseProxyProtocolFunc = func(cr *vmv1beta1.VMSingle) {
		cr.Spec.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vmagentUseProxyProtocolFunc = func(cr *vmv1beta1.VMAgent) {
		cr.Spec.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vmalertUseProxyProtocolFunc = func(cr *vmv1beta1.VMAlert) {
		cr.Spec.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vmalertmanagerUseProxyProtocolFunc = func(cr *vmv1beta1.VMAlertmanager) {
		cr.Spec.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vmalertmanagerClusterDomainFunc = func(cr *vmv1beta1.VMAlertmanager) {
		cr.Spec.ClusterDomainName = "cluster.local"
	}
	vmclusterUseProxyProtocolFunc = func(cr *vmv1beta1.VMCluster) {
		cr.Spec.VMSelect.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
		cr.Spec.VMInsert.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vlagentLicenseFunc = func(cr *vmv1.VLAgent) {
		cr.Spec.License = &vmv1beta1.License{
			Key: ptr.To("my-key"),
		}
	}
	vlagentUseProxyProtocolFunc = func(cr *vmv1.VLAgent) {
		cr.Spec.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vlagentK8sCollectorFieldsFunc = func(cr *vmv1.VLAgent) {
		cr.Spec.K8sCollector.TenantID = "1:0"
		cr.Spec.K8sCollector.MsgFields = []string{"msg", "message"}
	}
	vtclusterUseProxyProtocolFunc = func(cr *vmv1.VTCluster) {
		cr.Spec.Select.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
		cr.Spec.Insert.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vtsingleUseProxyProtocolFunc = func(cr *vmv1.VTSingle) {
		cr.Spec.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vlsingleUseProxyProtocolFunc = func(cr *vmv1.VLSingle) {
		cr.Spec.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vlsingleLicenseFunc = func(cr *vmv1.VLSingle) {
		cr.Spec.License = &vmv1beta1.License{
			Key: ptr.To("my-key"),
		}
	}
	vlclusterUseProxyProtocolFunc = func(cr *vmv1.VLCluster) {
		cr.Spec.VLInsert.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
		cr.Spec.VLSelect.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
		cr.Spec.VLStorage.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vlclusterLicenseFunc = func(cr *vmv1.VLCluster) {
		cr.Spec.License = &vmv1beta1.License{
			Key: ptr.To("my-key"),
		}
	}
	vmauthUseProxyProtocolFunc = func(cr *vmv1beta1.VMAuth) {
		cr.Spec.UseProxyProtocol = true
	}
	vmagentIngestOnlyWithRelabelFunc = func(cr *vmv1beta1.VMAgent) {
		cr.Spec.IngestOnlyMode = ptr.To(true)
		cr.Spec.InlineRelabelConfig = []*vmv1beta1.RelabelConfig{
			{TargetLabel: "env", Replacement: ptr.To("prod")},
		}
	}
	vmsingleIngestOnlyFunc = func(cr *vmv1beta1.VMSingle) {
		cr.Spec.IngestOnlyMode = ptr.To(true)
	}
	vmsingleIngestOnlyWithProxyProtocolFunc = func(cr *vmv1beta1.VMSingle) {
		cr.Spec.IngestOnlyMode = ptr.To(true)
		cr.Spec.ExtraArgs = map[string]string{"httpListenAddr.useProxyProtocol": "true"}
	}
	vmsingleIngestOnlyWithRelabelFunc = func(cr *vmv1beta1.VMSingle) {
		cr.Spec.IngestOnlyMode = ptr.To(true)
		cr.Spec.InlineRelabelConfig = []*vmv1beta1.RelabelConfig{
			{TargetLabel: "env", Replacement: ptr.To("prod")},
		}
	}
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
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vmagentUseProxyProtocolFunc),
		// Broken on 0.68
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vmagentUseProxyProtocolFunc),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1926
		Entry("from v0.67.0 with IngestOnly and relabeling", "v0.67.0", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.0 with IngestOnly and relabeling", "v0.68.0", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.1 with IngestOnly and relabeling", "v0.68.1", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.2 with IngestOnly and relabeling", "v0.68.2", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.3 with IngestOnly and relabeling", "v0.68.3", vmagentIngestOnlyWithRelabelFunc),
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
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vmagentUseProxyProtocolFunc),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1926
		Entry("from v0.67.0 with IngestOnly and relabeling", "v0.67.0", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.0 with IngestOnly and relabeling", "v0.68.0", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.1 with IngestOnly and relabeling", "v0.68.1", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.2 with IngestOnly and relabeling", "v0.68.2", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.3 with IngestOnly and relabeling", "v0.68.3", vmagentIngestOnlyWithRelabelFunc),
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
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		// Changes the probe - nothing we can do about it
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vmagentUseProxyProtocolFunc),

		// Fails to start
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vmagentUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vmagentUseProxyProtocolFunc),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1926
		Entry("from v0.67.0 with IngestOnly and relabeling", "v0.67.0", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.0 with IngestOnly and relabeling", "v0.68.0", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.1 with IngestOnly and relabeling", "v0.68.1", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.2 with IngestOnly and relabeling", "v0.68.2", vmagentIngestOnlyWithRelabelFunc),
		Entry("from v0.68.3 with IngestOnly and relabeling", "v0.68.3", vmagentIngestOnlyWithRelabelFunc),
	)

	//nolint:dupl
	DescribeTable("should not rollout VLAgent changes", func(operatorVersion string, mod func(*vmv1.VLAgent)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		cr := &vmv1.VLAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vlagentName,
				Namespace: namespace,
			},
			Spec: vmv1.VLAgentSpec{
				RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
					{
						URL: "http://vlogs:9428/insert/loki/api/v1/push",
					},
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/vlagent",
						Tag:        "v1.44.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VLAgent to become operational")
		nsn := types.NamespacedName{Name: vlagentName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child StatefulSet spec")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlagent-%s", vlagentName),
			Namespace: namespace,
		}

		expectedStatefulSetSpec := snapshotStatefulSet(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VLAgent")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying StatefulSet spec remains stable over time")
		Consistently(func() string {
			return verifyStatefulSet(ctx, k8sClient, resourceNSN, expectedStatefulSetSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1722
		Entry("from v0.67.0 with License", "v0.67.0", vlagentLicenseFunc),
		Entry("from v0.68.0 with License", "v0.68.0", vlagentLicenseFunc),
		Entry("from v0.68.1 with License", "v0.68.1", vlagentLicenseFunc),
		Entry("from v0.68.2 with License", "v0.68.2", vlagentLicenseFunc),
		Entry("from v0.68.3 with License", "v0.68.3", vlagentLicenseFunc),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vlagentUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vlagentUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vlagentUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vlagentUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vlagentUseProxyProtocolFunc),
	)

	//nolint:dupl
	DescribeTable("should not rollout VLAgent changes (K8sCollector)", func(operatorVersion string, mod func(*vmv1.VLAgent)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		cr := &vmv1.VLAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vlagentName,
				Namespace: namespace,
			},
			Spec: vmv1.VLAgentSpec{
				RemoteWrite: []vmv1.VLAgentRemoteWriteSpec{
					{
						URL: "http://vlogs:9428/insert/loki/api/v1/push",
					},
				},
				K8sCollector: vmv1.VLAgentK8sCollector{
					Enabled: true,
				},
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/vlagent",
						Tag:        "v1.48.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VLAgent to become operational")
		nsn := types.NamespacedName{Name: vlagentName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child DaemonSet spec")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vlagent-%s", vlagentName),
			Namespace: namespace,
		}
		expectedDaemonSetSpec := snapshotDaemonSet(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VLAgent")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VLAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying DaemonSet spec remains stable over time")
		Consistently(func() string {
			return verifyDaemonSet(ctx, k8sClient, resourceNSN, expectedDaemonSetSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1755
		Entry("from v0.67.0 with K8sCollector fields", "v0.67.0", vlagentK8sCollectorFieldsFunc),
		Entry("from v0.68.0 with K8sCollector fields", "v0.68.0", vlagentK8sCollectorFieldsFunc),
		Entry("from v0.68.1 with K8sCollector fields", "v0.68.1", vlagentK8sCollectorFieldsFunc),
		Entry("from v0.68.2 with K8sCollector fields", "v0.68.2", vlagentK8sCollectorFieldsFunc),
		Entry("from v0.68.3 with K8sCollector fields", "v0.68.3", vlagentK8sCollectorFieldsFunc),
	)

	//nolint:dupl
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
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying deployment spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vmsingleUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vmsingleUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vmsingleUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vmsingleUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vmsingleUseProxyProtocolFunc),

		Entry("from v0.68.3 IngestOnly", "v0.68.3", vmsingleIngestOnlyFunc),
		Entry("from v0.68.3 IngestOnly with UseProxyProtocol", "v0.68.3", vmsingleIngestOnlyWithProxyProtocolFunc),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1926
		Entry("from v0.67.0 with IngestOnly and relabeling", "v0.67.0", vmsingleIngestOnlyWithRelabelFunc),
		Entry("from v0.68.0 with IngestOnly and relabeling", "v0.68.0", vmsingleIngestOnlyWithRelabelFunc),
		Entry("from v0.68.1 with IngestOnly and relabeling", "v0.68.1", vmsingleIngestOnlyWithRelabelFunc),
		Entry("from v0.68.2 with IngestOnly and relabeling", "v0.68.2", vmsingleIngestOnlyWithRelabelFunc),
		Entry("from v0.68.3 with IngestOnly and relabeling", "v0.68.3", vmsingleIngestOnlyWithRelabelFunc),
	)

	//nolint:dupl
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
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vmauthUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vmauthUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vmauthUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vmauthUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vmauthUseProxyProtocolFunc),
	)

	//nolint:dupl
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
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		// Fails on latest master
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vmalertUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vmalertUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vmalertUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vmalertUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vmalertUseProxyProtocolFunc),
	)

	//nolint:dupl
	DescribeTable("should not rollout VMAlertmanager changes", func(operatorVersion string, mod func(*vmv1beta1.VMAlertmanager)) {
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
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vmalertmanagerUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vmalertmanagerUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vmalertmanagerUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vmalertmanagerUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vmalertmanagerUseProxyProtocolFunc),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1751
		Entry("from v0.67.0 with ClusterDomainName", "v0.67.0", vmalertmanagerClusterDomainFunc),
		Entry("from v0.68.0 with ClusterDomainName", "v0.68.0", vmalertmanagerClusterDomainFunc),
		Entry("from v0.68.1 with ClusterDomainName", "v0.68.1", vmalertmanagerClusterDomainFunc),
		Entry("from v0.68.2 with ClusterDomainName", "v0.68.2", vmalertmanagerClusterDomainFunc),
		Entry("from v0.68.3 with ClusterDomainName", "v0.68.3", vmalertmanagerClusterDomainFunc),
	)

	//nolint:dupl
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
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vminsert",
							Tag:        "v1.136.0-cluster",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vmstorage",
							Tag:        "v1.136.0-cluster",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		// Fails on latest master
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vmclusterUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vmclusterUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vmclusterUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vmclusterUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vmclusterUseProxyProtocolFunc),
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
							TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				VMInsert: &vmv1beta1.VMInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vminsert",
							Tag:        "v1.136.0-cluster",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				VMStorage: &vmv1beta1.VMStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/vmstorage",
							Tag:        "v1.136.0-cluster",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		// Fails on latest master
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vmclusterUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vmclusterUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vmclusterUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vmclusterUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vmclusterUseProxyProtocolFunc),
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
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying deployment spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1686
		Entry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vlsingleUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vlsingleUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vlsingleUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vlsingleUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vlsingleUseProxyProtocolFunc),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1722
		Entry("from v0.67.0 with License", "v0.67.0", vlsingleLicenseFunc),
		Entry("from v0.68.0 with License", "v0.68.0", vlsingleLicenseFunc),
		Entry("from v0.68.1 with License", "v0.68.1", vlsingleLicenseFunc),
		Entry("from v0.68.2 with License", "v0.68.2", vlsingleLicenseFunc),
		Entry("from v0.68.3 with License", "v0.68.3", vlsingleLicenseFunc),
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
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				VLInsert: &vmv1.VLInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				VLStorage: &vmv1.VLStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1916
		// Fails on latest master
		Entry("from v0.64.0 with UseProxyProtocol", "v0.64.0", vlclusterUseProxyProtocolFunc),
		Entry("from v0.64.1 with UseProxyProtocol", "v0.64.1", vlclusterUseProxyProtocolFunc),
		Entry("from v0.65.0 with UseProxyProtocol", "v0.65.0", vlclusterUseProxyProtocolFunc),
		// This was broken again
		Entry("from v0.66.0 with UseProxyProtocol", "v0.66.0", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.66.1 with UseProxyProtocol", "v0.66.1", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vlclusterUseProxyProtocolFunc),
		// Broken on 0.68
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vlclusterUseProxyProtocolFunc),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1722
		Entry("from v0.67.0 with License", "v0.67.0", vlclusterLicenseFunc),
		Entry("from v0.68.0 with License", "v0.68.0", vlclusterLicenseFunc),
		Entry("from v0.68.1 with License", "v0.68.1", vlclusterLicenseFunc),
		Entry("from v0.68.2 with License", "v0.68.2", vlclusterLicenseFunc),
		Entry("from v0.68.3 with License", "v0.68.3", vlclusterLicenseFunc),
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
							TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				VLInsert: &vmv1.VLInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				VLStorage: &vmv1.VLStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-logs",
							Tag:        "v1.44.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1916
		// Fails on latest master
		PEntry("from v0.64.0 with UseProxyProtocol", "v0.64.0", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.64.1 with UseProxyProtocol", "v0.64.1", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.65.0 with UseProxyProtocol", "v0.65.0", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.66.0 with UseProxyProtocol", "v0.66.0", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.66.1 with UseProxyProtocol", "v0.66.1", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vlclusterUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vlclusterUseProxyProtocolFunc),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1722
		Entry("from v0.67.0 with License", "v0.67.0", vlclusterLicenseFunc),
		Entry("from v0.68.0 with License", "v0.68.0", vlclusterLicenseFunc),
		Entry("from v0.68.1 with License", "v0.68.1", vlclusterLicenseFunc),
		Entry("from v0.68.2 with License", "v0.68.2", vlclusterLicenseFunc),
		Entry("from v0.68.3 with License", "v0.68.3", vlclusterLicenseFunc),
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
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying deployment spec remains stable over time")
		Consistently(func() string {
			return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1916
		Entry("from v0.64.0 with UseProxyProtocol", "v0.64.0", vtsingleUseProxyProtocolFunc),
		Entry("from v0.64.1 with UseProxyProtocol", "v0.64.1", vtsingleUseProxyProtocolFunc),
		Entry("from v0.65.0 with UseProxyProtocol", "v0.65.0", vtsingleUseProxyProtocolFunc),
		Entry("from v0.66.0 with UseProxyProtocol", "v0.66.0", vtsingleUseProxyProtocolFunc),
		Entry("from v0.66.1 with UseProxyProtocol", "v0.66.1", vtsingleUseProxyProtocolFunc),
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vtsingleUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vtsingleUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vtsingleUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vtsingleUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vtsingleUseProxyProtocolFunc),
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
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				Insert: &vmv1.VTInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				Storage: &vmv1.VTStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1916
		// Fails on latest master
		PEntry("from v0.64.0 with UseProxyProtocol", "v0.64.0", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.64.1 with UseProxyProtocol", "v0.64.1", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.65.0 with UseProxyProtocol", "v0.65.0", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.66.0 with UseProxyProtocol", "v0.66.0", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.66.1 with UseProxyProtocol", "v0.66.1", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vtclusterUseProxyProtocolFunc),
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
							TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				Insert: &vmv1.VTInsert{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
				Storage: &vmv1.VTStorage{
					CommonAppsParams: vmv1beta1.CommonAppsParams{
						ReplicaCount: ptr.To[int32](1),
						Image: vmv1beta1.Image{
							Repository: "quay.io/victoriametrics/victoria-traces",
							Tag:        "v0.4.0",
						},
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),

		// introduced in https://github.com/VictoriaMetrics/operator/pull/1916
		// Probe changed
		PEntry("from v0.64.0 with UseProxyProtocol", "v0.64.0", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.64.1 with UseProxyProtocol", "v0.64.1", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.65.0 with UseProxyProtocol", "v0.65.0", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.66.0 with UseProxyProtocol", "v0.66.0", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.66.1 with UseProxyProtocol", "v0.66.1", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.67.0 with UseProxyProtocol", "v0.67.0", vtclusterUseProxyProtocolFunc),
		// Broken on 0.68
		PEntry("from v0.68.0 with UseProxyProtocol", "v0.68.0", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.68.1 with UseProxyProtocol", "v0.68.1", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.68.2 with UseProxyProtocol", "v0.68.2", vtclusterUseProxyProtocolFunc),
		PEntry("from v0.68.3 with UseProxyProtocol", "v0.68.3", vtclusterUseProxyProtocolFunc),
	)

	//nolint:dupl
	DescribeTable("should not rollout VMAnomaly changes", func(operatorVersion string, mod func(*vmv1.VMAnomaly)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		By("creating VMAnomaly in " + namespace)
		cr := &vmv1.VMAnomaly{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmanomalyName,
				Namespace: namespace,
				Annotations: map[string]string{
					vmv1beta1.SkipValidationAnnotation: vmv1beta1.SkipValidationValue,
				},
			},
			Spec: vmv1.VMAnomalySpec{
				CommonAppsParams: vmv1beta1.CommonAppsParams{
					ReplicaCount: ptr.To[int32](1),
					Image: vmv1beta1.Image{
						Repository: "quay.io/victoriametrics/vmanomaly",
						Tag:        "v1.21.0",
					},
					TerminationGracePeriodSeconds: ptr.To(int64(1)),
				},
				ConfigRawYaml: "preset: ui:demo",
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMAnomaly to become operational")
		nsn := types.NamespacedName{Name: vmanomalyName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VMAnomaly{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child StatefulSet spec")
		resourceNSN := types.NamespacedName{
			Name:      fmt.Sprintf("vmanomaly-%s", vmanomalyName),
			Namespace: namespace,
		}
		expectedStatefulSetSpec := snapshotStatefulSet(ctx, k8sClient, resourceNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMAnomaly")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1.VMAnomaly{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload spec remains stable over time")
		Consistently(func() string {
			return verifyStatefulSet(ctx, k8sClient, resourceNSN, expectedStatefulSetSpec)
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
		Entry("from v0.64.0", "v0.64.0", nil),
		Entry("from v0.64.1", "v0.64.1", nil),
		Entry("from v0.65.0", "v0.65.0", nil),
		Entry("from v0.66.0", "v0.66.0", nil),
		Entry("from v0.66.1", "v0.66.1", nil),
		Entry("from v0.67.0", "v0.67.0", nil),
		Entry("from v0.68.0", "v0.68.0", nil),
		Entry("from v0.68.1", "v0.68.1", nil),
		Entry("from v0.68.2", "v0.68.2", nil),
		Entry("from v0.68.3", "v0.68.3", nil),
	)

	//nolint:dupl
	// TODO[vrutkovs]: snapshot created VMClusters?
	DescribeTable("should not rollout VMDistributed changes", func(operatorVersion string, mod func(*vmv1alpha1.VMDistributed)) {
		namespace := createRandomNamespace(ctx, k8sClient)
		deployOldOperator(ctx, k8sClient, operatorVersion, namespace)

		By("creating VMDistributed in " + namespace)
		zoneName := "a"
		cr := &vmv1alpha1.VMDistributed{
			ObjectMeta: metav1.ObjectMeta{
				Name:      vmdistributedName,
				Namespace: namespace,
			},
			Spec: vmv1alpha1.VMDistributedSpec{
				// disable VMAuth to keep the test focused on VMCluster and VMAgent workloads
				VMAuth: vmv1alpha1.VMDistributedAuth{
					Enabled: ptr.To(false),
				},
				ZoneCommon: vmv1alpha1.VMDistributedZoneCommon{
					ReadyTimeout: &metav1.Duration{Duration: 2 * time.Minute},
					UpdatePause:  &metav1.Duration{Duration: 1 * time.Second},
					VMCluster: vmv1alpha1.VMDistributedZoneCluster{
						Spec: vmv1beta1.VMClusterSpec{
							RetentionPeriod: "1",
							VMSelect: &vmv1beta1.VMSelect{
								CommonAppsParams: vmv1beta1.CommonAppsParams{
									ReplicaCount: ptr.To[int32](1),
									Image: vmv1beta1.Image{
										Repository: "quay.io/victoriametrics/vmselect",
										Tag:        "v1.136.0-cluster",
									},
									TerminationGracePeriodSeconds: ptr.To(int64(1)),
								},
							},
							VMInsert: &vmv1beta1.VMInsert{
								CommonAppsParams: vmv1beta1.CommonAppsParams{
									ReplicaCount: ptr.To[int32](1),
									Image: vmv1beta1.Image{
										Repository: "quay.io/victoriametrics/vminsert",
										Tag:        "v1.136.0-cluster",
									},
									TerminationGracePeriodSeconds: ptr.To(int64(1)),
								},
							},
							VMStorage: &vmv1beta1.VMStorage{
								CommonAppsParams: vmv1beta1.CommonAppsParams{
									ReplicaCount: ptr.To[int32](1),
									Image: vmv1beta1.Image{
										Repository: "quay.io/victoriametrics/vmstorage",
										Tag:        "v1.136.0-cluster",
									},
									TerminationGracePeriodSeconds: ptr.To(int64(1)),
								},
							},
						},
					},
					VMAgent: vmv1alpha1.VMDistributedZoneAgent{
						Spec: vmv1alpha1.VMDistributedZoneAgentSpec{
							CommonAppsParams: vmv1beta1.CommonAppsParams{
								ReplicaCount: ptr.To[int32](1),
								Image: vmv1beta1.Image{
									Repository: "quay.io/victoriametrics/vmagent",
									Tag:        "v1.136.0",
								},
								TerminationGracePeriodSeconds: ptr.To(int64(1)),
							},
						},
					},
				},
				Zones: []vmv1alpha1.VMDistributedZone{
					{Name: zoneName},
				},
			},
		}
		if mod != nil {
			mod(cr)
		}
		Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

		By("waiting for VMDistributed to become operational")
		nsn := types.NamespacedName{Name: vmdistributedName, Namespace: namespace}
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1alpha1.VMDistributed{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 3*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

		By("snapshotting child workload specs")
		clusterName := fmt.Sprintf("%s-%s", vmdistributedName, zoneName)
		insertNSN := types.NamespacedName{Name: fmt.Sprintf("vminsert-%s", clusterName), Namespace: namespace}
		expectedInsertSpec := snapshotDeployment(ctx, k8sClient, insertNSN)
		selectNSN := types.NamespacedName{Name: fmt.Sprintf("vmselect-%s", clusterName), Namespace: namespace}
		expectedSelectSpec := snapshotDeployment(ctx, k8sClient, selectNSN)
		storageNSN := types.NamespacedName{Name: fmt.Sprintf("vmstorage-%s", clusterName), Namespace: namespace}
		expectedStorageSpec := snapshotStatefulSet(ctx, k8sClient, storageNSN)
		agentNSN := types.NamespacedName{Name: fmt.Sprintf("vmagent-%s", clusterName), Namespace: namespace}
		expectedAgentSpec := snapshotDeployment(ctx, k8sClient, agentNSN)

		restartManagerAndCleanup(ctx, k8sClient, namespace)

		By("waiting for latest operator to reconcile VMDistributed")
		Eventually(func() error {
			return suite.ExpectObjectStatus(ctx, k8sClient,
				&vmv1alpha1.VMDistributed{}, nsn, vmv1beta1.UpdateStatusOperational)
		}, 3*time.Minute, 5*time.Second).ShouldNot(HaveOccurred())

		By("verifying workload specs remain stable over time")
		Consistently(func() string {
			if diff := verifyDeployment(ctx, k8sClient, insertNSN, expectedInsertSpec); diff != "" {
				return "insert:\n" + diff
			}
			if diff := verifyDeployment(ctx, k8sClient, selectNSN, expectedSelectSpec); diff != "" {
				return "select:\n" + diff
			}
			if diff := verifyStatefulSet(ctx, k8sClient, storageNSN, expectedStorageSpec); diff != "" {
				return "storage:\n" + diff
			}
			if diff := verifyDeployment(ctx, k8sClient, agentNSN, expectedAgentSpec); diff != "" {
				return "agent:\n" + diff
			}
			return ""
		}, 5*time.Second, 1*time.Second).Should(BeEmpty())
	},
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
						TerminationGracePeriodSeconds: ptr.To(int64(1)),
					},
				},
			}
			Expect(k8sClient.Create(ctx, cr)).ToNot(HaveOccurred())

			By("waiting for VMAgent to become operational")
			nsn := types.NamespacedName{Name: vmagentName, Namespace: namespace}
			Eventually(func() error {
				return suite.ExpectObjectStatus(ctx, k8sClient,
					&vmv1beta1.VMAgent{}, nsn, vmv1beta1.UpdateStatusOperational)
			}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

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
			}, waitTimeout, 5*time.Second).ShouldNot(HaveOccurred())

			By("verifying workload spec has not changed due to VM_LOOPBACK")
			Eventually(func() string {
				return verifyDeployment(ctx, k8sClient, resourceNSN, expectedDeploymentSpec)
			}, 5*time.Second, 1*time.Second).ShouldNot(BeEmpty(), "expected rollout because VM_LOOPBACK changed")
		})
	})
})
