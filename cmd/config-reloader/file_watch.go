package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/VictoriaMetrics/VictoriaMetrics/lib/logger"
	"github.com/fsnotify/fsnotify"
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
					contentUpdateErrosTotal.Inc()
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
					contentUpdateErrosTotal.Inc()
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

type dirWatcher struct {
	dirs map[string]struct{}
	wg   sync.WaitGroup
	w    *fsnotify.Watcher
}

func newDirWatchers(dirs []string) (*dirWatcher, error) {
	dws := map[string]struct{}{}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("cannot create new dir watcher: %w", err)
	}
	for _, dir := range dirs {
		logger.Infof("starting watcher for dir: %s", dir)
		if err := w.Add(dir); err != nil {
			return nil, fmt.Errorf("cannot dir: %s to watcher: %w", dir, err)
		}
		dws[dir] = struct{}{}
	}
	return &dirWatcher{
		w:    w,
		dirs: dws,
	}, nil
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
			// hack for kubernetes configmaps and secrets.
			// it uses ..YEAR_MONTH_DAY_HOUR.MIN.S directory for content updates
			// and links it as a symlink
			// just skip it, stat for the file will be evaluated with os.Stat below
			if strings.Contains(path, "..") {
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
				// todo metric.
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
	for dir := range dw.dirs {
		if _, err := updateCache(dir); err != nil {
			logger.Errorf("cannot update dir cache during start: %s", err)
		}
	}
	go func() {
		defer dw.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-dw.w.Events:
				if event.Op == fsnotify.Remove {
					continue
				}
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
