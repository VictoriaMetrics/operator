package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/fsnotify/fsnotify"
	"k8s.io/apimachinery/pkg/util/sets"
)

type fileWatcher struct {
	wg sync.WaitGroup
	w  *fsnotify.Watcher
}

func newFileWatcher(file string) (*fileWatcher, error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	if err := w.Add(filepath.Dir(file)); err != nil {
		return nil, err
	}
	return &fileWatcher{
		w: w,
	}, nil
}

func (fw *fileWatcher) load(_ context.Context) error {
	newData, err := readFileContent(*configFileName)
	if err != nil {
		return fmt.Errorf("cannot read file: %s content, err: %w", *configFileName, err)
	}
	if err := writeNewContent(newData); err != nil {
		return fmt.Errorf("cannot write content to file: %s, err: %w", *configFileDst, err)
	}
	return nil
}

func (fw *fileWatcher) start(ctx context.Context, updates chan struct{}) {
	logger.Infof("starting file watcher")
	var prevContent []byte
	update := func(fileName string) error {
		newData, err := readFileContent(fileName)
		if err != nil {
			return fmt.Errorf("cannot read file: %s content, err: %w", fileName, err)
		}
		if bytes.Equal(prevContent, newData) {
			logger.Infof("files the same, nothing to do")
			return errNotModified
		}
		if err := writeNewContent(newData); err != nil {
			return fmt.Errorf("cannot write content to file: %s, err: %w", *configFileDst, err)
		}
		prevContent = newData
		contentHashes.observe("main", hashBytes(newData))
		select {
		case updates <- struct{}{}:
		default:

		}
		return nil
	}
	if err := update(*configFileName); err != nil {
		logger.Errorf("cannot update file on init: %v", err)
	}
	fw.wg.Add(1)
	go func() {
		defer fw.wg.Done()
		var t time.Ticker
		if *resyncInterval > 0 {
			t = *time.NewTicker(*resyncInterval)
			defer t.Stop()
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := update(*configFileName); err != nil {
					logger.Errorf("cannot update file at force resync :%s", err)
					contentUpdateErrorsTotal.Inc()
					continue
				}
			case event := <-fw.w.Events:
				if event.Name != *configFileName {
					logger.Infof("file name not match: %s", event.Name)
					continue
				}
				logger.Infof("changed: %s, %s", event.Name, event.Op.String())
				if err := update(*configFileName); err != nil {
					logger.Errorf("cannot update file :%s", err)
					contentUpdateErrorsTotal.Inc()
					continue
				}
			}
		}
	}()
}

func (fw *fileWatcher) close() {
	fw.wg.Wait()
}

func readFileContent(src string) ([]byte, error) {
	return os.ReadFile(src)
}

type dirPair struct {
	src    string
	target string // empty means reload-only, no copy
}

// sync atomically replaces p.target with the decompressed contents of p.src.
// All new content is first written to a sibling temp directory; the target is
// swapped in a single rename so readers never observe a partial state.
func (p *dirPair) sync() error {
	if p.target == "" {
		return nil
	}
	tmpDir := p.target + ".new"
	if err := os.RemoveAll(tmpDir); err != nil {
		return fmt.Errorf("cannot clean temp dir %s: %w", tmpDir, err)
	}
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("cannot create temp dir %s: %w", tmpDir, err)
	}
	srcEntries, err := os.ReadDir(p.src)
	if err != nil {
		return fmt.Errorf("cannot read source dir %s: %w", p.src, err)
	}
	srcNames := sets.New[string]()
	for _, e := range srcEntries {
		if e.IsDir() || strings.HasPrefix(e.Name(), "..") {
			continue
		}
		srcNames.Insert(e.Name())
		srcFile := filepath.Join(p.src, e.Name())
		data, err := os.ReadFile(srcFile)
		if err != nil {
			return fmt.Errorf("cannot read %s: %w", srcFile, err)
		}
		data, err = maybeDecompress(data)
		if err != nil {
			return fmt.Errorf("cannot decompress %s: %w", srcFile, err)
		}
		if err := os.WriteFile(filepath.Join(tmpDir, e.Name()), data, 0644); err != nil {
			return fmt.Errorf("cannot write %s: %w", filepath.Join(tmpDir, e.Name()), err)
		}
	}
	logger.Infof("syncing %d files from %s to %s", srcNames.Len(), p.src, p.target)
	// Atomically replace the target directory.
	oldDir := p.target + ".old"
	_ = os.RemoveAll(oldDir)
	if err := os.Rename(p.target, oldDir); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot back up target dir %s: %w", p.target, err)
	}
	if err := os.Rename(tmpDir, p.target); err != nil {
		_ = os.Rename(oldDir, p.target) // best-effort restore
		return fmt.Errorf("cannot swap temp dir to target %s: %w", p.target, err)
	}
	_ = os.RemoveAll(oldDir)
	return nil
}

type dirWatcher struct {
	pairs []dirPair
	wg    sync.WaitGroup
	w     *fsnotify.Watcher
}

// newDirWatchers watches dirs; targetDirs is paired by index with dirs (dirs[i] → targetDirs[i]).
// If non-empty, targetDirs must have the same length as dirs. A watched dir with no
// corresponding target only triggers a reload on change.
func newDirWatchers(dirs []string, targetDirs []string) (*dirWatcher, error) {
	if err := validateTargetDirCount(len(targetDirs), len(dirs)); err != nil {
		return nil, err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("cannot create new dir watcher: %w", err)
	}
	dw := &dirWatcher{w: w, pairs: make([]dirPair, 0, len(dirs))}
	for i, dir := range dirs {
		logger.Infof("starting watcher for dir: %s", dir)
		if err := w.Add(dir); err != nil {
			return nil, fmt.Errorf("cannot add dir: %s to watcher: %w", dir, err)
		}
		var target string
		if len(targetDirs) > 0 {
			target = targetDirs[i]
		}
		dw.pairs = append(dw.pairs, dirPair{src: dir, target: target})
	}
	return dw, nil
}

func (dw *dirWatcher) start(ctx context.Context, updates chan struct{}) {
	dw.wg.Add(1)
	filesContentHashPath := map[string][]byte{}
	dirHash := sha256.New()
	fHash := sha256.New()

	updateCache := func(eventPath string) (bool, error) {
		dirHash.Reset()
		walkDir, err := filepath.EvalSymlinks(eventPath)
		if err != nil {
			return false, fmt.Errorf("cannot eval symlinks for path: %s", eventPath)
		}

		err = filepath.WalkDir(walkDir, func(path string, d fs.DirEntry, err error) error {
			// Kubernetes projected volumes expose hidden ..* entries such as
			// ..data and timestamped symlink targets. Skip only those synthetic
			// path elements, not regular files like "rules..yaml".
			if strings.HasPrefix(filepath.Base(path), "..") {
				return nil
			}

			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return fmt.Errorf("unexpected error during walk, base path: %s, err: %w", walkDir, err)
			}

			f, err := os.Stat(path)
			if err != nil {
				return fmt.Errorf("cannot check file stat for path: %s, err: %w", path, err)
			}
			if f.IsDir() {
				return nil
			}
			dirHash.Write([]byte(path)) // nolint:errcheck
			newData, err := readFileContent(path)
			if err != nil {
				logger.Errorf("cannot read file content: %s", err)
				return fmt.Errorf("cannot read file content: %w", err)
			}
			dirHash.Write(newData) // nolint:errcheck
			// reset file hash.
			fHash.Reset()
			fHash.Write(newData) // nolint:errcheck
			newFileHash := fHash.Sum(nil)

			prevFileHash := filesContentHashPath[path]
			if bytes.Equal(prevFileHash, newFileHash) {
				return nil
			}
			filesContentHashPath[path] = newFileHash
			logger.Infof("update needed file for path: %s", path)
			return nil
		})
		if err != nil {
			logger.Errorf("cannot walk: %s", err)
			return false, fmt.Errorf("cannot walk path: %s, err: %w", eventPath, err)
		}

		newHash := dirHash.Sum(nil)
		prevHash := filesContentHashPath[eventPath]
		if bytes.Equal(prevHash, newHash) {
			return false, nil
		}
		filesContentHashPath[eventPath] = newHash
		logger.Infof("base dir: %s hash not the same, update needed", eventPath)
		return true, nil
	}
	for _, p := range dw.pairs {
		if _, err := updateCache(p.src); err != nil {
			logger.Errorf("cannot update dir cache during start: %s", err)
		}
		if err := p.sync(); err != nil {
			logger.Errorf("cannot copy dir %s to target on start: %s", p.src, err)
		}
	}
	go func() {
		defer dw.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-dw.w.Events:
				baseDir := filepath.Dir(event.Name)
				logger.Infof("dir update: base dir: %s", baseDir)
				reloadNeeded, err := updateCache(baseDir)
				if err != nil {
					logger.Errorf("cannot update dir watch cache: %s", err)
					continue
				}
				if !reloadNeeded {
					continue
				}
				synced := true
				for _, p := range dw.pairs {
					if p.src == baseDir {
						if err := p.sync(); err != nil {
							logger.Errorf("cannot copy dir %s to target: %s", p.src, err)
							synced = false
						}
						break
					}
				}
				if !synced {
					continue
				}
				select {
				case updates <- struct{}{}:
				default:
				}
			}
		}
	}()
}

func (dw *dirWatcher) close() {
	dw.wg.Wait()
}

var firstGzipBytes = []byte{0x1f, 0x8b, 0x08}

// maybeDecompress returns data decompressed if gzip magic is detected, otherwise as-is.
func maybeDecompress(data []byte) ([]byte, error) {
	if len(data) <= 3 || !bytes.Equal(data[:3], firstGzipBytes) {
		return data, nil
	}
	r, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("cannot create gzip reader: %w", err)
	}
	defer r.Close()
	return io.ReadAll(r)
}
