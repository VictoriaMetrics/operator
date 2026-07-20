package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"

	k8syaml "sigs.k8s.io/yaml"

	"github.com/VictoriaMetrics/operator/internal/converter"
	"github.com/VictoriaMetrics/operator/internal/migrate"
	"github.com/VictoriaMetrics/operator/internal/migrate/vl"
	"github.com/VictoriaMetrics/operator/internal/migrate/vm"
)

func main() {
	args := os.Args[1:]
	// Preserve the legacy no-subcommand invocation (helm-converter -input=... -output=...)
	// as an alias for `convert`, so existing scripts keep working.
	if len(args) == 0 || (len(args) > 0 && args[0] != "convert" && args[0] != "migrate") {
		runConvert(args)
		return
	}
	switch args[0] {
	case "convert":
		runConvert(args[1:])
	case "migrate":
		runMigrate(args[1:])
	}
}

func runConvert(args []string) {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	inputFile := fs.String("input", "", "input file with helm values")
	outputFile := fs.String("output", "", "output file with operator manifests")
	name := fs.String("name", "", "name of the generated CR (defaults to the chart name)")
	namespace := fs.String("namespace", "default", "namespace of the generated CR")
	chart := fs.String("chart", "victoria-metrics-single", "name of the helm chart")
	if err := fs.Parse(args); err != nil {
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

	cr, err := converter.Convert(*name, *namespace, values)
	if err != nil {
		fmt.Printf("cannot convert values: %v\n", err)
		os.Exit(1)
	}

	outputData, err := k8syaml.Marshal(cr)
	if err != nil {
		fmt.Printf("cannot marshal CR: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outputFile, outputData, 0644); err != nil {
		fmt.Printf("cannot write output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("successfully converted %s to %s\n", *inputFile, *outputFile)
}

func runMigrate(args []string) {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	chart := fs.String("chart", "", "helm chart of the release being migrated (victoria-metrics-single, victoria-metrics-cluster, victoria-logs-single, victoria-logs-cluster)")
	strategy := fs.String("strategy", "WithDowntime", "migration strategy: WithDowntime or NoDowntime")
	namespace := fs.String("namespace", "", "namespace of the existing helm release and the target CR")
	release := fs.String("release", "", "name of the existing helm release to migrate")
	valuesFile := fs.String("values", "", "path to the release's values.yaml")
	targetName := fs.String("target-name", "", "name of the CR to create (defaults to -release)")
	kubeconfig := fs.String("kubeconfig", "", "path to kubeconfig (defaults to standard kubeconfig loading rules)")
	yes := fs.Bool("yes", false, "skip interactive confirmation before destructive steps")
	dryRun := fs.Bool("dry-run", false, "print the migration plan and exit without changing anything")
	agentBufferSize := fs.String("agent-buffer-size", "", "NoDowntime only: disk size for the buffering VMAgent's persistent queue (e.g. 10Gi)")
	snapshotClassName := fs.String("snapshot-class", "", "NoDowntime only: VolumeSnapshotClass to use (defaults to the cluster's default class)")
	if err := fs.Parse(args); err != nil {
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

	cr, err := migrate.ConvertSpec(opts.Chart, opts.ValuesFile, opts.TargetName, opts.Namespace)
	if err != nil {
		fmt.Printf("cannot convert helm values: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	switch {
	case opts.Chart == migrate.ChartVMSingle && opts.Strategy == migrate.StrategyWithDowntime:
		target, convErr := migrate.AsVMSingle(cr)
		if convErr != nil {
			fmt.Printf("%v\n", convErr)
			os.Exit(1)
		}
		err = migrate.WithDowntimeSingleNode(ctx, c, opts, target)
	case opts.Chart == migrate.ChartVLSingle && opts.Strategy == migrate.StrategyWithDowntime:
		target, convErr := migrate.AsVLSingle(cr)
		if convErr != nil {
			fmt.Printf("%v\n", convErr)
			os.Exit(1)
		}
		err = migrate.WithDowntimeSingleNode(ctx, c, opts, target)
	case opts.Chart == migrate.ChartVMSingle && opts.Strategy == migrate.StrategyNoDowntime:
		target, convErr := migrate.AsVMSingle(cr)
		if convErr != nil {
			fmt.Printf("%v\n", convErr)
			os.Exit(1)
		}
		err = vm.NoDowntimeSingleNode(ctx, c, http.DefaultClient, opts, target)
	case opts.Chart == migrate.ChartVLSingle && opts.Strategy == migrate.StrategyNoDowntime:
		target, convErr := migrate.AsVLSingle(cr)
		if convErr != nil {
			fmt.Printf("%v\n", convErr)
			os.Exit(1)
		}
		err = vl.NoDowntimeSingleNode(ctx, c, http.DefaultClient, opts, target)
	case opts.Chart == migrate.ChartVMCluster && opts.Strategy == migrate.StrategyWithDowntime:
		target, convErr := migrate.AsVMCluster(cr)
		if convErr != nil {
			fmt.Printf("%v\n", convErr)
			os.Exit(1)
		}
		err = vm.WithDowntimeCluster(ctx, c, opts, target)
	case opts.Chart == migrate.ChartVLCluster && opts.Strategy == migrate.StrategyWithDowntime:
		target, convErr := migrate.AsVLCluster(cr)
		if convErr != nil {
			fmt.Printf("%v\n", convErr)
			os.Exit(1)
		}
		err = vl.WithDowntimeCluster(ctx, c, opts, target)
	case opts.Chart == migrate.ChartVMCluster && opts.Strategy == migrate.StrategyNoDowntime:
		target, convErr := migrate.AsVMCluster(cr)
		if convErr != nil {
			fmt.Printf("%v\n", convErr)
			os.Exit(1)
		}
		err = vm.NoDowntimeCluster(ctx, c, http.DefaultClient, opts, target)
	case opts.Chart == migrate.ChartVLCluster && opts.Strategy == migrate.StrategyNoDowntime:
		target, convErr := migrate.AsVLCluster(cr)
		if convErr != nil {
			fmt.Printf("%v\n", convErr)
			os.Exit(1)
		}
		err = vl.NoDowntimeCluster(ctx, c, http.DefaultClient, opts, target)
	default:
		fmt.Printf("chart %q with strategy %q is not yet supported by this command\n", opts.Chart, opts.Strategy)
		os.Exit(1)
	}
	if err != nil {
		fmt.Printf("migration failed: %v\n", err)
		os.Exit(1)
	}
}
