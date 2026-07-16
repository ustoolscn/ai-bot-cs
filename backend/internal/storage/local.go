package storage

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Local struct{ root string }

func NewLocal(root string) (*Local, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	return &Local{root: root}, nil
}
func (l *Local) Save(_ context.Context, name string, data []byte) (string, error) {
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".txt" && ext != ".md" && ext != ".png" && ext != ".jpg" && ext != ".jpeg" && ext != ".gif" && ext != ".webp" {
		return "", fmt.Errorf("unsupported file type %s", ext)
	}
	key := uuid.NewString() + ext
	if err := os.WriteFile(filepath.Join(l.root, key), data, 0600); err != nil {
		return "", err
	}
	return key, nil
}

func (l *Local) CleanupOlderThan(cutoff time.Time) error {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err == nil && info.ModTime().Before(cutoff) {
			if err := os.Remove(filepath.Join(l.root, entry.Name())); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
func (l *Local) Read(_ context.Context, key string) ([]byte, error) {
	return os.ReadFile(filepath.Join(l.root, filepath.Base(key)))
}
func (l *Local) Delete(_ context.Context, key string) error {
	err := os.Remove(filepath.Join(l.root, filepath.Base(key)))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}
