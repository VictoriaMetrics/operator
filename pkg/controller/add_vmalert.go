package controller

import (
	"github.com/VictoriaMetrics/operator/pkg/controller/vmalert"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, vmalert.Add)
}
