package suite

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Moon1706/ginkgo2allure/pkg/convert"
	fmngr "github.com/Moon1706/ginkgo2allure/pkg/convert/file_manager"
	"github.com/Moon1706/ginkgo2allure/pkg/convert/parser"
	allureReport "github.com/Moon1706/ginkgo2allure/pkg/convert/report"
	. "github.com/onsi/ginkgo/v2" //nolint
	ginkgoTypes "github.com/onsi/ginkgo/v2/types"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vmv1beta1 "github.com/VictoriaMetrics/operator/api/operator/v1beta1"
	"github.com/VictoriaMetrics/operator/test/utils"
)

const (
	allureReportPath = "/tmp/allure-results"
)

// ExpectObjectStatus perform assert on given object status
//
//nolint:dupl,lll
func ExpectObjectStatus(ctx context.Context,
	rclient client.Client,
	object client.Object,
	name types.NamespacedName,
	status vmv1beta1.UpdateStatus) error {
	if err := rclient.Get(ctx, name, object); err != nil {
		return err
	}
	jsD, err := json.Marshal(object)
	if err != nil {
		return err
	}
	type objectStatus struct {
		Status struct {
			vmv1beta1.StatusMetadata `json:",inline"`
		} `json:"status"`
	}
	var obs objectStatus
	if err := json.Unmarshal(jsD, &obs); err != nil {
		return err
	}
	if object.GetGeneration() > obs.Status.ObservedGeneration {
		return fmt.Errorf("expected generation: %d be greater than: %d", obs.Status.ObservedGeneration, object.GetGeneration())
	}
	if obs.Status.UpdateStatus != status {
		var conds []string
		for _, cond := range obs.Status.Conditions {
			conds = append(conds, fmt.Sprintf("type=%s,message=%q,generation=%d,status=%q", cond.Type, cond.Message, cond.ObservedGeneration, cond.Status))
		}
		return fmt.Errorf("not expected object status=%q, reason=%q,conditions=%s", obs.Status.UpdateStatus, obs.Status.Reason, strings.Join(conds, ","))
	}

	return nil
}

func AllureReport(report ginkgoTypes.Report) {
	parserConfig := parser.Config{}
	parserConfig.LabelsScraperOpts = append(parserConfig.LabelsScraperOpts, allureReport.WillAutoGenerateID(true))
	allureReports, err := convert.GinkgoToAllureReport([]ginkgoTypes.Report{report}, parser.NewDefaultParser, parserConfig)
	if err != nil {
		panic(fmt.Sprintf("allure report: %v", err))
	}

	reportPath, err := filepath.Abs(allureReportPath)
	if err != nil {
		panic(fmt.Sprintf("allure report: %v", err))
	}

	if err := os.MkdirAll(reportPath, 0755); err != nil {
		panic(fmt.Sprintf("failed to create report dir: %v", err))
	}

	fileManager := fmngr.NewFileManager(reportPath)

	errs := convert.PrintAllureReports(allureReports, fileManager)
	if len(errs) > 0 {
		panic(fmt.Sprintf("allure report: %v", errs))
	}
}

func CollectK8SResources() {
	if CurrentSpecReport().Failed() {
		err := utils.RunCrustGather(context.Background(), 10*time.Minute)
		if err != nil {
			panic(fmt.Sprintf("crust-gather: %v", err))
		}
	}
}
