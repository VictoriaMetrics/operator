/*
The following code was adapted from https://github.com/ramich2077/allure-ginkgo/
License: No explicit license found in original repository (All Rights Reserved).
*/

package allure

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/onsi/gomega"
)

type attachment struct {
	uuid    string
	Name    string   `json:"name"`
	Source  string   `json:"source"`
	Type    MimeType `json:"type"`
	content []byte
}

type MimeType string

const (
	MimeTypeGZIP MimeType = "application/gzip"
)

const attachmentReportEntryName = "ATTACHMENT"

func saveAsJSONAttachment(msg any) []byte {
	res, e := json.MarshalIndent(msg, "", "  ")
	gomega.Expect(e).ShouldNot(gomega.HaveOccurred(), "while marshalling raw message")
	return res
}

func addAttachment(name string, mimeType MimeType, content []byte) (*attachment, error) {
	attachment := newAttachment(name, mimeType, content)
	err := attachment.writeAttachmentFile()
	if err != nil {
		return nil, fmt.Errorf("failed to create an attachment file: %w", err)
	}

	return attachment, nil
}

func (a *attachment) writeAttachmentFile() error {
	resultsPathEnv := os.Getenv(resultsPathEnvKey)
	ensureFolderCreated()
	if resultsPath == "" {
		resultsPath = fmt.Sprintf("%s/allure-results", resultsPathEnv)
	}

	a.Source = fmt.Sprintf("%s-attachment.%s", a.uuid, resolveExtension(a.Type))
	err := os.WriteFile(strings.Join([]string{resultsPath, a.Source}, "/"), a.content, 0600)
	if err != nil {
		return fmt.Errorf("failed to write in file: %w", err)
	}

	return nil
}

func newAttachment(name string, mimeType MimeType, content []byte) *attachment {
	result := &attachment{
		uuid:    uuid.New().String(),
		content: content,
		Name:    name,
		Type:    mimeType,
	}

	return result
}

func resolveExtension(mimeType MimeType) string {
	switch mimeType {
	case MimeTypeGZIP:
		return "tar.gz"
	default:
		return ""
	}
}
