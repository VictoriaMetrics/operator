package controller

import (
	"github.com/VictoriaMetrics/operator/pkg/controller/vmservicescrape"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, vmservicescrape.Add)
}
