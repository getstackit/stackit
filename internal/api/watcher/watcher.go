// Package watcher provides file system monitoring for git ref changes.
//
// It watches .git/refs/heads/ and .git/refs/stackit/ for modifications,
// debounces rapid changes, and notifies subscribers via a callback.
package watcher

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// RefWatcher watches git ref directories for changes and triggers callbacks.
type RefWatcher struct {
	repoRoot    string
	debounce    time.Duration
	onUpdate    func()
	watcher     *fsnotify.Watcher
	stopCh      chan struct{}
	stoppedOnce sync.Once
}

// NewRefWatcher creates a watcher that monitors git refs in the given repo.
// The onUpdate callback is called (debounced) when ref files change.
func NewRefWatcher(repoRoot string, debounce time.Duration, onUpdate func()) *RefWatcher {
	return &RefWatcher{
		repoRoot: repoRoot,
		debounce: debounce,
		onUpdate: onUpdate,
		stopCh:   make(chan struct{}),
	}
}

// Start begins watching git ref directories. It blocks, so call it in a goroutine.
func (w *RefWatcher) Start() error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("failed to create file watcher: %w", err)
	}
	w.watcher = fsw

	// Watch .git/refs/heads/ and .git/refs/stackit/
	dirsToWatch := []string{
		filepath.Join(w.repoRoot, ".git", "refs", "heads"),
		filepath.Join(w.repoRoot, ".git", "refs", "stackit"),
	}

	for _, dir := range dirsToWatch {
		if _, err := os.Stat(dir); err != nil {
			continue // Directory may not exist yet
		}
		if err := addRecursive(fsw, dir); err != nil {
			slog.Warn("failed to watch directory", "dir", dir, "error", err)
		}
	}

	// Also watch HEAD for branch switches
	headPath := filepath.Join(w.repoRoot, ".git", "HEAD")
	if err := fsw.Add(headPath); err != nil {
		slog.Warn("failed to watch HEAD", "path", headPath, "error", err)
	}

	var timer *time.Timer
	for {
		select {
		case <-w.stopCh:
			if timer != nil {
				timer.Stop()
			}
			return fsw.Close()

		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			// Only trigger on writes, creates, and removes
			if event.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Remove) == 0 {
				continue
			}

			// If a new directory was created, watch it too
			if event.Op&fsnotify.Create != 0 {
				if info, err := os.Stat(event.Name); err == nil && info.IsDir() {
					_ = addRecursive(fsw, event.Name)
				}
			}

			// Debounce: reset timer on each event
			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(w.debounce, w.onUpdate)

		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			slog.Error("watcher error", "error", err)
		}
	}
}

// Stop terminates the watcher.
func (w *RefWatcher) Stop() {
	w.stoppedOnce.Do(func() {
		close(w.stopCh)
	})
}

// addRecursive adds a directory and all its subdirectories to the watcher.
func addRecursive(fsw *fsnotify.Watcher, dir string) error {
	return filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return fsw.Add(path)
		}
		return nil
	})
}
