package taskassets

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/leonardo2api/leonardo2api/internal/domain"
)

type Store struct {
	root string
}

func New(root string) (*Store, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("task asset directory is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: abs}, nil
}

func (s *Store) Save(header *multipart.FileHeader, maxBytes int64) (domain.SourceMedia, error) {
	if header == nil {
		return domain.SourceMedia{}, errors.New("media file is required")
	}
	if maxBytes < 1 {
		return domain.SourceMedia{}, errors.New("media size limit is invalid")
	}
	source, err := header.Open()
	if err != nil {
		return domain.SourceMedia{}, err
	}
	defer source.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	target, err := os.CreateTemp(s.root, "asset-*"+ext)
	if err != nil {
		return domain.SourceMedia{}, err
	}
	name := filepath.Base(target.Name())
	cleanup := func() {
		_ = target.Close()
		_ = os.Remove(target.Name())
	}
	probe := make([]byte, 512)
	probeSize, probeErr := io.ReadFull(source, probe)
	if probeErr != nil && !errors.Is(probeErr, io.EOF) && !errors.Is(probeErr, io.ErrUnexpectedEOF) {
		cleanup()
		return domain.SourceMedia{}, probeErr
	}
	probe = probe[:probeSize]
	digest := sha256.New()
	written, err := io.Copy(io.MultiWriter(target, digest), io.LimitReader(io.MultiReader(bytes.NewReader(probe), source), maxBytes+1))
	if err != nil {
		cleanup()
		return domain.SourceMedia{}, err
	}
	if written > maxBytes {
		cleanup()
		return domain.SourceMedia{}, fmt.Errorf("%s exceeds configured size limit", filepath.Base(header.Filename))
	}
	if err := target.Sync(); err != nil {
		cleanup()
		return domain.SourceMedia{}, err
	}
	if err := target.Close(); err != nil {
		_ = os.Remove(target.Name())
		return domain.SourceMedia{}, err
	}
	return domain.SourceMedia{
		Filename:  filepath.Base(header.Filename),
		MediaType: http.DetectContentType(probe),
		Path:      name,
		Size:      written,
		SHA256:    hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func (s *Store) Open(asset domain.SourceMedia) (*os.File, error) {
	path, err := s.resolve(asset.Path)
	if err != nil {
		return nil, err
	}
	return os.Open(path)
}

func (s *Store) Remove(asset domain.SourceMedia) error {
	if asset.Path == "" {
		return nil
	}
	path, err := s.resolve(asset.Path)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) CleanupVideoRequest(request domain.VideoRequest) error {
	assets := append([]domain.SourceMedia(nil), request.ReferenceImages...)
	assets = append(assets, request.ReferenceVideos...)
	if request.StartFrame != nil {
		assets = append(assets, *request.StartFrame)
	}
	if request.EndFrame != nil {
		assets = append(assets, *request.EndFrame)
	}
	assets = append(assets, request.AudioReferences()...)
	var joined error
	for _, asset := range assets {
		joined = errors.Join(joined, s.Remove(asset))
	}
	return joined
}

func (s *Store) CleanupImageRequest(request domain.ImageRequest) error {
	var joined error
	for _, asset := range request.ReferenceImages {
		joined = errors.Join(joined, s.Remove(asset))
	}
	return joined
}

func (s *Store) CleanupOlderThan(age time.Duration) error {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return err
	}
	cutoff := time.Now().Add(-age)
	var joined error
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "asset-") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			joined = errors.Join(joined, infoErr)
			continue
		}
		if info.ModTime().Before(cutoff) {
			joined = errors.Join(joined, os.Remove(filepath.Join(s.root, entry.Name())))
		}
	}
	return joined
}

func (s *Store) resolve(name string) (string, error) {
	if name == "" || filepath.Base(name) != name {
		return "", errors.New("invalid task asset path")
	}
	path := filepath.Join(s.root, name)
	rel, err := filepath.Rel(s.root, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("task asset path escapes configured root")
	}
	return path, nil
}
