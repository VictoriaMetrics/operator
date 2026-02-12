/*
The following code was adapted from https://github.com/ramich2077/allure-ginkgo/
License: No explicit license found in original repository (All Rights Reserved).
*/

package allure

type label struct {
	Name  string `json:"name,omitempty"`
	Value string `json:"value,omitempty"`
}

const (
	labelSuite = "suite"

	labelParentSuite = "parentSuite"
)
