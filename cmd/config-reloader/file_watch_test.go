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

	// Drain the update queued by the initial sync.
	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after initial sync")
	}

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

	// Drain the update queued by the initial sync.
	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after initial sync")
	}

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

func TestDirWatcherTriggersUpdateWhenFileRemoved(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "rules.yaml")
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

	// Drain the update queued by the initial sync.
	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after initial sync")
	}

	if err := os.Remove(file); err != nil {
		t.Fatalf("failed to remove watched file: %v", err)
	}

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after removing watched file")
	}
}

func TestDirWatcherTriggersReloadAfterInitialSync(t *testing.T) {
	srcDir := t.TempDir()
	targetDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(targetDir, "rules.yaml"), []byte("groups: []\n"), 0o644); err != nil {
		t.Fatalf("failed to write stale target file: %v", err)
	}
	if err := os.WriteFile(filepath.Join(srcDir, "rules.yaml"), []byte("groups:\n- name: test\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	dw, err := newDirWatchers([]string{srcDir}, []string{targetDir})
	if err != nil {
		t.Fatalf("failed to create dir watcher: %v", err)
	}

	updates := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	dw.start(ctx, updates)
	defer dw.close()
	defer cancel()

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after initial sync replaced stale target content")
	}

	data, err := os.ReadFile(filepath.Join(targetDir, "rules.yaml"))
	if err != nil {
		t.Fatalf("failed to read target file after initial sync: %v", err)
	}
	if string(data) != "groups:\n- name: test\n" {
		t.Fatalf("unexpected target content after initial sync: %q", data)
	}
}

func TestDirWatcherRetriesFailedInitialSyncOnNextEvent(t *testing.T) {
	srcDir := t.TempDir()
	base := t.TempDir()
	blocker := filepath.Join(base, "blocked")
	targetDir := filepath.Join(blocker, "out")
	if err := os.WriteFile(filepath.Join(srcDir, "rules.yaml"), []byte("groups: []\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	// A regular file at the target's parent path makes the initial sync fail.
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("failed to write blocker file: %v", err)
	}

	dw, err := newDirWatchers([]string{srcDir}, []string{targetDir})
	if err != nil {
		t.Fatalf("failed to create dir watcher: %v", err)
	}

	updates := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	dw.start(ctx, updates)
	defer dw.close()
	defer cancel()

	// Drain the initial reload, which fires regardless of the failing pair.
	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after initial sync")
	}

	if err := os.Remove(blocker); err != nil {
		t.Fatalf("failed to remove blocker file: %v", err)
	}
	// Rewriting the same content still triggers the sync: the hash of the
	// failed pair was never committed to the cache.
	if err := os.WriteFile(filepath.Join(srcDir, "rules.yaml"), []byte("groups: []\n"), 0o644); err != nil {
		t.Fatalf("failed to rewrite source file: %v", err)
	}

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after initial sync retry succeeded")
	}

	data, err := os.ReadFile(filepath.Join(targetDir, "rules.yaml"))
	if err != nil {
		t.Fatalf("failed to read target file after retried sync: %v", err)
	}
	if string(data) != "groups: []\n" {
		t.Fatalf("unexpected target content after retried sync: %q", data)
	}
}

func TestDirWatcherReloadsSyncedPairsWhenAnotherFails(t *testing.T) {
	srcA := t.TempDir()
	targetA := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcA, "rules.yaml"), []byte("groups:\n- name: a\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	srcB := t.TempDir()
	base := t.TempDir()
	blocker := filepath.Join(base, "blocked")
	targetB := filepath.Join(blocker, "out")
	if err := os.WriteFile(filepath.Join(srcB, "rules.yaml"), []byte("groups: []\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	// A regular file at targetB's parent path makes its sync fail permanently.
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("failed to write blocker file: %v", err)
	}

	dw, err := newDirWatchers([]string{srcA, srcB}, []string{targetA, targetB})
	if err != nil {
		t.Fatalf("failed to create dir watcher: %v", err)
	}

	updates := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	dw.start(ctx, updates)
	defer dw.close()
	defer cancel()

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update for the synced pair despite the failing pair")
	}

	data, err := os.ReadFile(filepath.Join(targetA, "rules.yaml"))
	if err != nil {
		t.Fatalf("failed to read synced target file: %v", err)
	}
	if string(data) != "groups:\n- name: a\n" {
		t.Fatalf("unexpected synced target content: %q", data)
	}
}

func TestDirWatcherProcessesEventsWhileAnotherPairFails(t *testing.T) {
	srcA := t.TempDir()
	targetA := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcA, "rules.yaml"), []byte("groups: []\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	srcB := t.TempDir()
	base := t.TempDir()
	blocker := filepath.Join(base, "blocked")
	targetB := filepath.Join(blocker, "out")
	if err := os.WriteFile(filepath.Join(srcB, "rules.yaml"), []byte("groups: []\n"), 0o644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}
	// A regular file at targetB's parent path makes its sync fail.
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatalf("failed to write blocker file: %v", err)
	}

	dw, err := newDirWatchers([]string{srcA, srcB}, []string{targetA, targetB})
	if err != nil {
		t.Fatalf("failed to create dir watcher: %v", err)
	}

	updates := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	dw.start(ctx, updates)
	defer dw.close()
	defer cancel()

	// Drain the initial reload.
	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after initial sync")
	}

	if err := os.WriteFile(filepath.Join(srcA, "rules.yaml"), []byte("groups:\n- name: test\n"), 0o644); err != nil {
		t.Fatalf("failed to update source file: %v", err)
	}

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update from event loop while another pair's sync fails")
	}

	data, err := os.ReadFile(filepath.Join(targetA, "rules.yaml"))
	if err != nil {
		t.Fatalf("failed to read target file: %v", err)
	}
	if string(data) != "groups:\n- name: test\n" {
		t.Fatalf("unexpected target content: %q", data)
	}
}

func TestDirWatcherNoInitialReloadWithoutWatchedDirs(t *testing.T) {
	dw, err := newDirWatchers(nil, nil)
	if err != nil {
		t.Fatalf("failed to create dir watcher: %v", err)
	}

	updates := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	dw.start(ctx, updates)
	defer dw.close()
	defer cancel()

	select {
	case <-updates:
		t.Fatal("did not expect update on start without watched dirs")
	case <-time.After(300 * time.Millisecond):
	}
}

func TestDirWatcherSyncRemovesDeletedFilesFromTargetDir(t *testing.T) {
	srcDir := t.TempDir()
	targetDir := t.TempDir()
	file := filepath.Join(srcDir, "rules.yaml")
	targetFile := filepath.Join(targetDir, "rules.yaml")
	if err := os.WriteFile(file, []byte("groups: []\n"), 0o644); err != nil {
		t.Fatalf("failed to write initial source file: %v", err)
	}

	dw, err := newDirWatchers([]string{srcDir}, []string{targetDir})
	if err != nil {
		t.Fatalf("failed to create dir watcher: %v", err)
	}

	updates := make(chan struct{}, 10)
	ctx, cancel := context.WithCancel(context.Background())
	dw.start(ctx, updates)
	defer dw.close()
	defer cancel()

	if _, err := os.Stat(targetFile); err != nil {
		t.Fatalf("expected file to exist in target dir after initial sync: %v", err)
	}

	// Drain the update queued by the initial sync.
	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after initial sync")
	}

	if err := os.Remove(file); err != nil {
		t.Fatalf("failed to remove source file: %v", err)
	}

	select {
	case <-updates:
	case <-time.After(2 * time.Second):
		t.Fatal("expected update after removing source file")
	}

	if _, err := os.Stat(targetFile); !os.IsNotExist(err) {
		t.Fatalf("expected target file to be removed after source deletion, got err=%v", err)
	}
}
