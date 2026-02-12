/*
The following code was adapted from https://github.com/ramich2077/allure-ginkgo/
License: No explicit license found in original repository (All Rights Reserved).
*/

package allure

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/types"
)

const (
	broken  = "broken"
	passed  = "passed"
	failed  = "failed"
	skipped = "skipped"
)

type statusDetails struct {
	Known   bool   `json:"known,omitempty"`
	Muted   bool   `json:"muted,omitempty"`
	Flaky   bool   `json:"flaky,omitempty"`
	Message string `json:"message,omitempty"`
	Trace   string `json:"trace,omitempty"`
}

func getTestStatus(report ginkgo.SpecReport) string {
	switch report.State {
	case types.SpecStatePanicked:
		return broken
	case types.SpecStateAborted, types.SpecStateInterrupted, types.SpecStateSkipped, types.SpecStatePending:
		return skipped
	case types.SpecStateFailed:
		return failed
	case types.SpecStatePassed:
		return passed
	default:
		return ""
	}
}
