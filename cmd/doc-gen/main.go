package main

import (
	"fmt"
	"os"
)

func main() {
	switch os.Args[1] {
	case "api":
		printAPIDocs(os.Args[2:])
	default:
		fmt.Printf("command not found, use api as first arg and paths to crd.go as additional args")
	}
}
