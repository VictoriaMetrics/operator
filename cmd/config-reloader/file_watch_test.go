package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDirWatcherProcessesRegularFilesWithDoubleDotsInName(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rules..yaml")
	if err := os.WriteFile(file, []byte("groups: []\n"), 0o644); err != nil {
		t.Fatalf("failed to write initial file: %v", err)
	}

	dw, err := newDirWatchers([]string{dir}, nil)
	if err != nil {
		t.Fatalf("failed to create dir watcher: %v", err)
	}

	updates := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	dw.start(ctx, updates)
	defer dw.close()
	defer cancel()

	if err := os.WriteFile(file, []byte("groups:\n- name: test\n"), 0o644); err != nil {
		t.Fatalf("failed to update file: %v", err)
	}

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after modifying a regular file containing '..' in its name")
	}
}

func TestDirWatcherSkipsKubernetesHiddenEntries(t *testing.T) {
	dir := t.TempDir()
	visibleFile := filepath.Join(dir, "rules.yaml")
	if err := os.WriteFile(visibleFile, []byte("groups: []\n"), 0o644); err != nil {
		t.Fatalf("failed to write visible file: %v", err)
	}
	hiddenFile := filepath.Join(dir, "..data")
	if err := os.WriteFile(hiddenFile, []byte("v1\n"), 0o644); err != nil {
		t.Fatalf("failed to write hidden file: %v", err)
	}

	dw, err := newDirWatchers([]string{dir}, nil)
	if err != nil {
		t.Fatalf("failed to create dir watcher: %v", err)
	}

	updates := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	dw.start(ctx, updates)
	defer dw.close()
	defer cancel()

	if err := os.WriteFile(hiddenFile, []byte("v2\n"), 0o644); err != nil {
		t.Fatalf("failed to update hidden file: %v", err)
	}

	select {
	case <-updates:
		t.Fatal("did not expect update after modifying kubernetes hidden entry")
	case <-time.After(300 * time.Millisecond):
	}

	if err := os.WriteFile(visibleFile, []byte("groups:\n- name: test\n"), 0o644); err != nil {
		t.Fatalf("failed to update visible file: %v", err)
	}

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after modifying visible file")
	}
}
