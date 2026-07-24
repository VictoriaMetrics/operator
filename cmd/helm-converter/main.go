package main

import (
	"flag"
	"fmt"
	"os"

	k8syaml "sigs.k8s.io/yaml"

	"github.com/VictoriaMetrics/operator/internal/converter"
)

var (
	inputFile  = flag.String("input", "", "input file with helm values")
	outputFile = flag.String("output", "", "output file with operator manifests")
	name       = flag.String("name", "", "name of the generated CR (defaults to the chart name)")
	namespace  = flag.String("namespace", "default", "namespace of the generated CR")
	chart      = flag.String("chart", "victoria-metrics-single", "name of the helm chart")
)

func main() {
	flag.Parse()

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
		// ConvertVMAlert also returns the auth Secrets, so both come from one conversion pass.
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
