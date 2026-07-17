package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	k8syaml "sigs.k8s.io/yaml"

	vmv1 "github.com/VictoriaMetrics/operator/api/operator/v1"
	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/internal/converter"
	"github.com/VictoriaMetrics/operator/internal/migrate"
	"github.com/VictoriaMetrics/operator/internal/migrate/vl"
	"github.com/VictoriaMetrics/operator/internal/migrate/vm"
)

func main() {
	args := os.Args[1:]
	switch {
	case len(args) == 0:
		runConvert(args)
	case args[0] == "convert":
		runConvert(args[1:])
	case args[0] == "migrate":
		runMigrate(args[1:])
	case strings.HasPrefix(args[0], "-"):
		runConvert(args)
	default:
		fmt.Printf("unknown subcommand %q (expected \"convert\" or \"migrate\")\n", args[0])
		os.Exit(1)
	}
}

func runConvert(args []string) {
	fs := flag.NewFlagSet("convert", flag.ContinueOnError)
	inputFile := fs.String("input", "", "input file with helm values")
	outputFile := fs.String("output", "", "output file with operator manifests")
	name := fs.String("name", "", "name of the generated CR (defaults to the chart name)")
	namespace := fs.String("namespace", "default", "namespace of the generated CR")
	chart := fs.String("chart", "victoria-metrics-single", "name of the helm chart")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Printf("cannot parse flags: %v\n", err)
		os.Exit(1)
	}

	if *inputFile == "" {
		fmt.Println("input file is required")
		os.Exit(1)
	}

	if *outputFile == "" {
		fmt.Println("output file is required")
		os.Exit(1)
	}

	if *name == "" {
		*name = *chart
	}

	inputData, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Printf("cannot read input file: %v\n", err)
		os.Exit(1)
	}

	chartDefaults, err := converter.FetchChartDefaults(*chart)
	if err != nil {
		fmt.Printf("cannot fetch chart defaults: %v\n", err)
		os.Exit(1)
	}

	mergedData, err := converter.MergeValues(chartDefaults, inputData)
	if err != nil {
		fmt.Printf("cannot merge values: %v\n", err)
		os.Exit(1)
	}

	values, err := converter.UnmarshalValues(mergedData, *chart)
	if err != nil {
		fmt.Printf("cannot unmarshal input file: %v\n", err)
		os.Exit(1)
	}

	var objects []any
	if alertValues, ok := values.(*converter.VMAlertHelmValues); ok {
		alert, secrets, err := converter.ConvertVMAlert(*name, *namespace, alertValues)
		if err != nil {
			fmt.Printf("cannot convert values: %v\n", err)
			os.Exit(1)
		}
		objects = append(objects, alert)
		for _, s := range secrets {
			objects = append(objects, s)
		}
		rule, err := converter.ConvertVMAlertRules(*name, *namespace, alertValues)
		if err != nil {
			fmt.Printf("cannot convert config.alerts.groups: %v\n", err)
			os.Exit(1)
		}
		if rule != nil {
			objects = append(objects, rule)
		}
	} else {
		cr, err := converter.Convert(*name, *namespace, values)
		if err != nil {
			fmt.Printf("cannot convert values: %v\n", err)
			os.Exit(1)
		}
		objects = append(objects, cr)
		if authValues, ok := values.(*converter.VMAuthHelmValues); ok {
			users, err := converter.ConvertVMAuthUsers(*name, *namespace, authValues)
			if err != nil {
				fmt.Printf("cannot convert config.users: %v\n", err)
				os.Exit(1)
			}
			for _, u := range users {
				objects = append(objects, u)
			}
		}
	}

	var outputData []byte
	for i, obj := range objects {
		if i > 0 {
			outputData = append(outputData, []byte("---\n")...)
		}
		data, err := k8syaml.Marshal(obj)
		if err != nil {
			fmt.Printf("cannot marshal CR: %v\n", err)
			os.Exit(1)
		}
		outputData = append(outputData, data...)
	}

	if err := os.WriteFile(*outputFile, outputData, 0644); err != nil {
		fmt.Printf("cannot write output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("successfully converted %s to %s\n", *inputFile, *outputFile)
}

func mustConvert[T any](cr any) T {
	v, err := migrate.AsCR[T](cr)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}
	return v
}

func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	chart := fs.String("chart", "", "helm chart of the release being migrated (victoria-metrics-single, victoria-metrics-cluster, victoria-logs-single, victoria-logs-cluster)")
	strategy := fs.String("strategy", "WithDowntime", "migration strategy: WithDowntime or NoDowntime")
	namespace := fs.String("namespace", "", "namespace of the existing helm release and the target CR")
	release := fs.String("release", "", "name of the existing helm release to migrate")
	valuesFile := fs.String("values", "", "path to the release's values.yaml")
	targetName := fs.String("target-name", "", "name of the CR to create (defaults to -release)")
	kubeconfig := fs.String("kubeconfig", "", "path to kubeconfig (defaults to standard kubeconfig loading rules)")
	yes := fs.Bool("yes", false, "skip interactive confirmation before destructive steps")
	dryRun := fs.Bool("dry-run", false, "print the migration plan and exit without changing anything")
	agentBufferSize := fs.String("agent-buffer-size", "", "disk size for the buffering VMAgent's persistent queue (e.g. 10Gi)")
	snapshotClassName := fs.String("snapshot-class", "", "NoDowntime only: VolumeSnapshotClass to use (defaults to the cluster's default class)")
	chartVersion := fs.String("chart-version", "", "version of the currently-installed helm chart, to fetch matching defaults to merge -values over (auto-discovered from the release's helm.sh/chart label if not set)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Printf("cannot parse flags: %v\n", err)
		os.Exit(1)
	}

	if *chart == "" {
		fmt.Println("-chart is required")
		os.Exit(1)
	}
	if *namespace == "" {
		fmt.Println("-namespace is required")
		os.Exit(1)
	}
	if *release == "" {
		fmt.Println("-release is required")
		os.Exit(1)
	}
	if *valuesFile == "" {
		fmt.Println("-values is required")
		os.Exit(1)
	}
	if *targetName == "" {
		*targetName = *release
	}
	switch migrate.Chart(*chart) {
	case migrate.ChartVMSingle, migrate.ChartVLSingle, migrate.ChartVMCluster, migrate.ChartVLCluster:
	default:
		fmt.Printf("-chart: unsupported value %q\n", *chart)
		os.Exit(1)
	}
	switch migrate.Strategy(*strategy) {
	case migrate.StrategyWithDowntime, migrate.StrategyNoDowntime:
	default:
		fmt.Printf("-strategy: unsupported value %q\n", *strategy)
		os.Exit(1)
	}
	if _, err := migrate.ParseAgentBufferSize(*agentBufferSize); err != nil {
		fmt.Printf("-agent-buffer-size: %v\n", err)
		os.Exit(1)
	}

	opts := migrate.Options{
		Chart:             migrate.Chart(*chart),
		Strategy:          migrate.Strategy(*strategy),
		Namespace:         *namespace,
		ReleaseName:       *release,
		ValuesFile:        *valuesFile,
		TargetName:        *targetName,
		Kubeconfig:        *kubeconfig,
		Yes:               *yes,
		DryRun:            *dryRun,
		AgentBufferSize:   *agentBufferSize,
		SnapshotClassName: *snapshotClassName,
	}

	c, err := migrate.NewClient(opts.Kubeconfig)
	if err != nil {
		fmt.Printf("cannot build kubernetes client: %v\n", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	version := *chartVersion
	if version == "" {
		version, err = migrate.DiscoverChartVersion(ctx, c, opts.Namespace, opts.ReleaseName, string(opts.Chart))
		if err != nil {
			fmt.Printf("cannot determine the installed chart version (pass -chart-version to override): %v\n", err)
			os.Exit(1) //nolint:gocritic // process is terminating either way, skipping the deferred stop() is fine
		}
	}

	cr, err := migrate.ConvertSpecAtVersion(opts.Chart, version, opts.ValuesFile, opts.TargetName, opts.Namespace)
	if err != nil {
		fmt.Printf("cannot convert helm values: %v\n", err)
		os.Exit(1)
	}

	switch {
	case opts.Chart == migrate.ChartVMSingle && opts.Strategy == migrate.StrategyWithDowntime:
		err = vm.WithDowntimeSingleNode(ctx, c, http.DefaultClient, opts, mustConvert[*vmv1beta1.VMSingle](cr))
	case opts.Chart == migrate.ChartVLSingle && opts.Strategy == migrate.StrategyWithDowntime:
		err = vl.WithDowntimeSingleNode(ctx, c, http.DefaultClient, opts, mustConvert[*vmv1.VLSingle](cr))
	case opts.Chart == migrate.ChartVMSingle && opts.Strategy == migrate.StrategyNoDowntime:
		err = vm.NoDowntimeSingleNode(ctx, c, http.DefaultClient, opts, mustConvert[*vmv1beta1.VMSingle](cr))
	case opts.Chart == migrate.ChartVLSingle && opts.Strategy == migrate.StrategyNoDowntime:
		err = vl.NoDowntimeSingleNode(ctx, c, http.DefaultClient, opts, mustConvert[*vmv1.VLSingle](cr))
	case opts.Chart == migrate.ChartVMCluster && opts.Strategy == migrate.StrategyWithDowntime:
		err = vm.WithDowntimeCluster(ctx, c, http.DefaultClient, opts, mustConvert[*vmv1beta1.VMCluster](cr))
	case opts.Chart == migrate.ChartVLCluster && opts.Strategy == migrate.StrategyWithDowntime:
		err = vl.WithDowntimeCluster(ctx, c, http.DefaultClient, opts, mustConvert[*vmv1.VLCluster](cr))
	case opts.Chart == migrate.ChartVMCluster && opts.Strategy == migrate.StrategyNoDowntime:
		err = vm.NoDowntimeCluster(ctx, c, http.DefaultClient, opts, mustConvert[*vmv1beta1.VMCluster](cr))
	case opts.Chart == migrate.ChartVLCluster && opts.Strategy == migrate.StrategyNoDowntime:
		err = vl.NoDowntimeCluster(ctx, c, http.DefaultClient, opts, mustConvert[*vmv1.VLCluster](cr))
	default:
		fmt.Printf("chart %q with strategy %q is not yet supported by this command\n", opts.Chart, opts.Strategy)
		os.Exit(1)
	}
	if err != nil {
		fmt.Printf("migration failed: %v\n", err)
		os.Exit(1)
	}
}
