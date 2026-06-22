package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultMaxSizeBytes = 50 * 1024 * 1024
	defaultMaxBackups   = 10
)

type RotatingWriter struct {
	path       string
	maxSize    int64
	maxBackups int
	file       *os.File
	size       int64
	mu         sync.Mutex
}

func NewRotatingWriter(path string) (*RotatingWriter, error) {
	writer := &RotatingWriter{
		path:       path,
		maxSize:    defaultMaxSizeBytes,
		maxBackups: defaultMaxBackups,
	}
	if err := writer.open(); err != nil {
		return nil, err
	}
	return writer, nil
}

func InitBackendLogger() (*RotatingWriter, error) {
	writer, err := NewRotatingWriter(filepath.Join("log", "backend.log"))
	if err != nil {
		return nil, err
	}
	log.SetOutput(io.MultiWriter(os.Stdout, writer))
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	return writer, nil
}

func NewWebLogger() (*log.Logger, *RotatingWriter, error) {
	writer, err := NewRotatingWriter(filepath.Join("log", "web.log"))
	if err != nil {
		return nil, nil, err
	}
	logger := log.New(io.MultiWriter(os.Stdout, writer), "", log.LstdFlags|log.Lmicroseconds)
	return logger, writer, nil
}

func (w *RotatingWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *RotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.file == nil {
		return nil
	}
	return w.file.Close()
}

func (w *RotatingWriter) open() error {
	if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
		return err
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	w.file = file
	w.size = info.Size()
	return nil
}

func (w *RotatingWriter) rotate() error {
	if w.file != nil {
		_ = w.file.Close()
		w.file = nil
	}
	timestamp := time.Now().Format("20060102-150405")
	rotatedPath := fmt.Sprintf("%s.%s", w.path, timestamp)
	if err := os.Rename(w.path, rotatedPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := w.cleanup(); err != nil {
		return err
	}
	return w.open()
}

func (w *RotatingWriter) cleanup() error {
	matches, err := filepath.Glob(w.path + ".*")
	if err != nil {
		return err
	}
	if len(matches) <= w.maxBackups {
		return nil
	}
	type fileInfo struct {
		path    string
		modTime time.Time
	}
	files := make([]fileInfo, 0, len(matches))
	for _, path := range matches {
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		files = append(files, fileInfo{path: path, modTime: info.ModTime()})
	}
	for i := 0; i < len(files)-1; i++ {
		for j := i + 1; j < len(files); j++ {
			if files[j].modTime.Before(files[i].modTime) {
				files[i], files[j] = files[j], files[i]
			}
		}
	}
	removeCount := len(files) - w.maxBackups
	for i := 0; i < removeCount; i++ {
		_ = os.Remove(files[i].path)
	}
	return nil
}
