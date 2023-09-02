/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"context"
	"flag"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/VictoriaMetrics/operator/internal/config"
	"github.com/VictoriaMetrics/operator/internal/manager"
)

var (
	setupLog      = ctrl.Log.WithName("setup")
	printDefaults = flag.Bool("printDefaults", false, "print all variables with their default values and exit")
	printFormat   = flag.String("printFormat", "table", "output format for --printDefaults. Can be table, json, yaml or list")
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	stop := signals.SetupSignalHandler()
	go func() {
		<-stop.Done()
		cancel()
	}()

	flag.Parse()
	baseConfig := config.MustGetBaseConfig()
	if *printDefaults {
		err := baseConfig.PrintDefaults(*printFormat)
		if err != nil {
			setupLog.Error(err, "cannot print variables")
			os.Exit(1)
		}
		return
	}

	err := manager.RunManager(ctx, baseConfig)
	if err != nil {
		setupLog.Error(err, "cannot setup manager")
		os.Exit(1)
	}
	setupLog.Info("stopped")

}
