/*
The following code was adapted from https://github.com/ramich2077/allure-ginkgo/
License: No explicit license found in original repository (All Rights Reserved).
*/

package allure

import (
	"fmt"
	"os"
	"sync"
	"time"
)

const (
	resultsPathEnvKey = "REPORTS_DIR"
	wsPathEnvKey      = "ALLURE_WORKSPACE_PATH"
)

var (
	resultsPath      string
	createFolderOnce sync.Once
)

func getTimestampMs() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func getTimestampMsFromTime(t time.Time) int64 {
	return t.UnixNano() / int64(time.Millisecond)
}

func writeFile(filename string, content []byte) error {
	ensureFolderCreated()

	err := os.WriteFile(fmt.Sprintf("%s/%s", resultsPath, filename), content, 0600)
	if err != nil {
		return fmt.Errorf("failed to write in file: %w", err)
	}

	return nil
}

func createFolderIfNotExists() {
	resultsPathEnv := os.Getenv(resultsPathEnvKey)
	if resultsPathEnv == "" {
		cwd, err := os.Getwd()
		if err != nil {
			panic(fmt.Errorf("cannot get current workdir: %w", err))
		}

		resultsPathEnv = cwd

		err = os.Setenv(resultsPathEnvKey, resultsPathEnv)
		if err != nil {
			panic(fmt.Errorf("cannot set resultsPathEnv: %w", err))
		}
	}

	if _, err := os.Stat(resultsPathEnv); os.IsNotExist(err) {
		err = os.Mkdir(resultsPathEnv, 0755)
		if err != nil {
			panic(fmt.Errorf("failed to create reports folder: %w", err))
		}
	}

	resultsPath = fmt.Sprintf("%s/allure-results", resultsPathEnv)

	if _, err := os.Stat(resultsPath); os.IsNotExist(err) {
		err = os.Mkdir(resultsPath, 0755)
		if err != nil {
			panic(fmt.Errorf("failed to create allure-results folder: %w", err))
		}
	}
}

func ensureFolderCreated() {
	createFolderOnce.Do(createFolderIfNotExists)
}
