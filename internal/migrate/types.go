package migrate

// DefaultAgentBufferSize is the default buffer-agent persistent-queue disk size.
const DefaultAgentBufferSize = "10Gi"

// Strategy selects the migration cutover mechanism.
type Strategy string

const (
	// StrategyWithDowntime deletes the old workload, rebinds its PV, creates the target CR,
	// then repoints the Service at the new pods.
	StrategyWithDowntime Strategy = "WithDowntime"
	// StrategyNoDowntime buffers writes through a vmagent/vlagent proxy while the target CR
	// is seeded from a snapshot of the old storage, then cuts traffic over once caught up.
	StrategyNoDowntime Strategy = "NoDowntime"
)

// Chart identifies the source Helm chart, matching internal/converter's own chart names.
type Chart string

const (
	ChartVMSingle  Chart = "victoria-metrics-single"
	ChartVMCluster Chart = "victoria-metrics-cluster"
	ChartVLSingle  Chart = "victoria-logs-single"
	ChartVLCluster Chart = "victoria-logs-cluster"
)

// Options configures a single migrate invocation.
type Options struct {
	Chart       Chart
	Strategy    Strategy
	Namespace   string
	ReleaseName string
	ValuesFile  string
	TargetName  string
	Kubeconfig  string
	Yes         bool
	DryRun      bool

	// NoDowntime-only settings.
	AgentBufferSize   string
	SnapshotClassName string
}

// helmLabels returns the standard Helm release-identifying label selector.
func helmLabels(releaseName string) map[string]string {
	return map[string]string{
		"app.kubernetes.io/instance":   releaseName,
		"app.kubernetes.io/managed-by": "Helm",
	}
}
