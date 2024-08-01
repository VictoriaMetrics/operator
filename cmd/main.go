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
	"os"

	_ "github.com/VictoriaMetrics/VictoriaMetrics/lib/cgroup"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/VictoriaMetrics/operator/internal/manager"
)

var (
	setupLog = ctrl.Log.WithName("setup")
)

func main() {
	ctx, cancel := context.WithCancel(context.Background())
	stop := signals.SetupSignalHandler()
	go func() {
		<-stop.Done()
		cancel()
	}()

	err := manager.RunManager(ctx)
	if err != nil {
		setupLog.Error(err, "cannot setup manager")
		os.Exit(1)
	}
	setupLog.Info("stopped")
}
