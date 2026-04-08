package main

import (
	"flag"
	"fmt"
	"os"

	"gopkg.in/yaml.v2"
	k8syaml "sigs.k8s.io/yaml"

	"github.com/VictoriaMetrics/operator/internal/converter"
)

var (
	inputFile  = flag.String("input", "", "input file with helm values")
	outputFile = flag.String("output", "", "output file with operator manifests")
	name       = flag.String("name", "victoria-metrics-single", "name of the generated VMSingle CR")
	namespace  = flag.String("namespace", "default", "namespace of the generated VMSingle CR")
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

	inputData, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Printf("cannot read input file: %v\n", err)
		os.Exit(1)
	}

	var values converter.VMSingleHelmValues
	if err := yaml.Unmarshal(inputData, &values); err != nil {
		fmt.Printf("cannot unmarshal input file: %v\n", err)
		os.Exit(1)
	}

	cr := converter.ConvertVMSingle(*name, *namespace, &values)

	outputData, err := k8syaml.Marshal(cr)
	if err != nil {
		fmt.Printf("cannot marshal VMSingle CR: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outputFile, outputData, 0644); err != nil {
		fmt.Printf("cannot write output file: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("successfully converted %s to %s\n", *inputFile, *outputFile)
}
