/*
The following code was adapted from https://github.com/ramich2077/allure-ginkgo/
License: No explicit license found in original repository (All Rights Reserved).
*/

package allure

import (
	"fmt"
)

type stepObject struct {
	Name          string         `json:"name,omitempty"`
	Status        string         `json:"status,omitempty"`
	Description   string         `json:"description,omitempty"`
	StatusDetails *statusDetails `json:"statusDetails,omitempty"`
	Stage         string         `json:"stage"`
	ChildrenSteps []stepObject   `json:"steps"`
	Attachments   []attachment   `json:"attachments"`
	Start         int64          `json:"start"`
	Stop          int64          `json:"stop"`
}

func (sc *stepObject) addName(name string) {
	sc.Name = name
}

func (sc *stepObject) addAttachment(attachment *attachment) {
	if attachment == nil {
		panic(fmt.Errorf("nil attachment pointer"))
	}
	sc.Attachments = append(sc.Attachments, *attachment)
}

func newStep() *stepObject {
	return &stepObject{
		Attachments:   make([]attachment, 0),
		ChildrenSteps: make([]stepObject, 0),
	}
}
