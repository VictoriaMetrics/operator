/*
The following code was adapted from https://github.com/ramich2077/allure-ginkgo/
License: No explicit license found in original repository (All Rights Reserved).
*/

package allure

import (
	"encoding/json"
	"fmt"
	"path"

	"github.com/google/uuid"
	"github.com/onsi/ginkgo/v2/types"
)

type container struct {
	UUID        string       `json:"uuid"`
	Name        string       `json:"name"`
	Children    []string     `json:"children"`
	Description string       `json:"description"`
	Befores     []stepObject `json:"befores"`
	Afters      []stepObject `json:"afters"`
	Links       []string     `json:"links"`
	Start       int64        `json:"start"`
	Stop        int64        `json:"stop"`
}

func (c *container) write() error {
	content, err := json.Marshal(c)
	if err != nil {
		return err
	}

	writeErr := writeFile(fmt.Sprintf("%s-container.json", c.UUID), content)
	if writeErr != nil {
		return writeErr
	}

	return nil
}

func (c *container) createFromReport(report types.Report) *container {
	c.Start = getTimestampMsFromTime(report.StartTime)
	c.Stop = getTimestampMsFromTime(report.EndTime)

	c.Name = path.Base(report.SuitePath)
	c.Description = report.SuiteDescription

	for _, specReport := range report.SpecReports {
		switch specReport.LeafNodeType {
		case types.NodeTypeBeforeSuite, types.NodeTypeSynchronizedBeforeSuite:
			attachmentEntries := filterForAttachments(specReport.ReportEntries)
			befores, _ := createSteps(specReport.SpecEvents, attachmentEntries)
			c.Befores = append(c.Befores, befores...)
		case types.NodeTypeIt:
			res := newResult().
				addParentSuite(report.SuiteDescription).
				createFromSpecReport(specReport)

			c.Children = append(c.Children, res.UUID)

			res.write()
		case types.NodeTypeAfterSuite, types.NodeTypeSynchronizedAfterSuite, types.NodeTypeCleanupAfterSuite:
			attachmentEntries := filterForAttachments(specReport.ReportEntries)
			afters, _ := createSteps(specReport.SpecEvents, attachmentEntries)
			c.Afters = append(c.Afters, afters...)
		default:
			continue
		}
	}

	return c
}

func newTestContainer() *container {
	return &container{
		UUID:    uuid.New().String(),
		Befores: []stepObject{},
		Afters:  []stepObject{},
	}
}
