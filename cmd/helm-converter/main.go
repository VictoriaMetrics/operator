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
	name       = flag.String("name", "victoria-metrics-single", "name of the generated CR")
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

	inputData, err := os.ReadFile(*inputFile)
	if err != nil {
		fmt.Printf("cannot read input file: %v\n", err)
		os.Exit(1)
	}

	values, err := converter.UnmarshalValues(inputData, *chart)
	if err != nil {
		fmt.Printf("cannot unmarshal input file: %v\n", err)
		os.Exit(1)
	}

	cr := converter.Convert(*name, *namespace, values)

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
