//go:build linux

package native

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	cellruntime "github.com/senasphy/cellbox/internal/runtime"
	"golang.org/x/sync/singleflight"
)

const maxUnpackedImageBytes = int64(20 << 30)
const maxResolvedImageReferences = 128

type imageStore struct {
	root  string
	group singleflight.Group
	mu    sync.RWMutex
	cache map[string]imageStoreResult
	order []string
}

func newImageStore(root string) *imageStore {
	return &imageStore{root: root, cache: make(map[string]imageStoreResult)}
}

// Ensure resolves a host-platform image once per process, verifies and flattens
// its layers, and publishes the rootfs atomically under its content digest.
func (s *imageStore) Ensure(ctx context.Context, reference string) (cellruntime.ImageInfo, string, map[string]string, error) {
	if strings.TrimSpace(reference) == "" {
		return cellruntime.ImageInfo{}, "", nil, errors.New("image reference must not be empty")
	}
	s.mu.RLock()
	cached, ok := s.cache[reference]
	s.mu.RUnlock()
	if ok {
		return cached.info, cached.rootfs, cloneEnv(cached.env), nil
	}
	value, err, _ := s.group.Do(reference, func() (any, error) {
		ref, err := name.ParseReference(reference)
		if err != nil {
			return nil, fmt.Errorf("parse image reference: %w", err)
		}
		image, err := remote.Image(ref, remote.WithContext(ctx), remote.WithAuthFromKeychain(authn.DefaultKeychain), remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}))
		if err != nil {
			return nil, fmt.Errorf("pull image %q: %w", reference, err)
		}
		digest, err := image.Digest()
		if err != nil {
			return nil, fmt.Errorf("resolve image digest: %w", err)
		}
		size, err := image.Size()
		if err != nil {
			return nil, fmt.Errorf("read image size: %w", err)
		}
		configFile, err := image.ConfigFile()
		if err != nil {
			return nil, fmt.Errorf("read image config: %w", err)
		}
		imageEnv, err := parseImageEnvironment(configFile.Config.Env)
		if err != nil {
			return nil, fmt.Errorf("parse image environment: %w", err)
		}
		imagesDir := filepath.Join(s.root, "images")
		if err := os.MkdirAll(imagesDir, 0700); err != nil {
			return nil, fmt.Errorf("create image cache: %w", err)
		}
		destination := filepath.Join(imagesDir, digest.Hex)
		rootfs := filepath.Join(destination, "rootfs")
		if info, err := os.Stat(rootfs); err == nil && info.IsDir() {
			return imageStoreResult{info: cellruntime.ImageInfo{Reference: reference, Digest: digest.String(), Size: size}, rootfs: rootfs, env: imageEnv}, nil
		}
		tmp, err := os.MkdirTemp(imagesDir, ".pull-*")
		if err != nil {
			return nil, fmt.Errorf("create temporary image directory: %w", err)
		}
		defer os.RemoveAll(tmp)
		tmpRoot := filepath.Join(tmp, "rootfs")
		if err := os.Mkdir(tmpRoot, 0700); err != nil {
			return nil, err
		}
		layers, err := image.Layers()
		if err != nil {
			return nil, fmt.Errorf("list image layers: %w", err)
		}
		var expanded int64
		directoryModes := make(map[string]os.FileMode)
		for index, layer := range layers {
			reader, err := layer.Uncompressed()
			if err != nil {
				return nil, fmt.Errorf("open image layer %d: %w", index, err)
			}
			layerBytes, applyErr := applyImageLayerWithModes(tmpRoot, reader, expanded, directoryModes)
			closeErr := reader.Close()
			if applyErr != nil {
				return nil, fmt.Errorf("apply image layer %d: %w", index, applyErr)
			}
			if closeErr != nil {
				return nil, fmt.Errorf("verify image layer %d: %w", index, closeErr)
			}
			expanded += layerBytes
		}
		if err := applyImageDirectoryModes(tmpRoot, directoryModes); err != nil {
			return nil, fmt.Errorf("restore image directory modes: %w", err)
		}
		if err := os.Chmod(tmpRoot, 0755); err != nil {
			return nil, fmt.Errorf("set image root mode: %w", err)
		}
		if err := os.Rename(tmp, destination); err != nil {
			if info, statErr := os.Stat(rootfs); statErr == nil && info.IsDir() {
				return imageStoreResult{info: cellruntime.ImageInfo{Reference: reference, Digest: digest.String(), Size: size}, rootfs: rootfs, env: imageEnv}, nil
			}
			return nil, fmt.Errorf("publish image rootfs: %w", err)
		}
		return imageStoreResult{info: cellruntime.ImageInfo{Reference: reference, Digest: digest.String(), Size: size}, rootfs: rootfs, env: imageEnv}, nil
	})
	if err != nil {
		return cellruntime.ImageInfo{}, "", nil, err
	}
	result := value.(imageStoreResult)
	s.mu.Lock()
	if _, exists := s.cache[reference]; !exists {
		if len(s.order) >= maxResolvedImageReferences {
			delete(s.cache, s.order[0])
			s.order = s.order[1:]
		}
		s.order = append(s.order, reference)
	}
	s.cache[reference] = result
	s.mu.Unlock()
	return result.info, result.rootfs, cloneEnv(result.env), nil
}

type imageStoreResult struct {
	info   cellruntime.ImageInfo
	rootfs string
	env    map[string]string
}

func parseImageEnvironment(values []string) (map[string]string, error) {
	env := make(map[string]string, len(values))
	for _, value := range values {
		key, content, ok := strings.Cut(value, "=")
		if !ok || key == "" || strings.ContainsAny(key, "=\x00") || strings.IndexByte(content, 0) >= 0 {
			return nil, fmt.Errorf("invalid OCI environment entry %q", value)
		}
		env[key] = content
	}
	return env, nil
}

func applyImageLayer(root string, input io.Reader, alreadyExpanded int64) (int64, error) {
	modes := make(map[string]os.FileMode)
	expanded, err := applyImageLayerWithModes(root, input, alreadyExpanded, modes)
	if err != nil {
		return expanded, err
	}
	return expanded, applyImageDirectoryModes(root, modes)
}

func applyImageLayerWithModes(root string, input io.Reader, alreadyExpanded int64, directoryModes map[string]os.FileMode) (int64, error) {
	reader := tar.NewReader(input)
	var expanded int64
	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return expanded, nil
		}
		if err != nil {
			return expanded, err
		}
		relative, err := safeImagePath(header.Name)
		if err != nil {
			return expanded, err
		}
		if relative == "." {
			continue
		}
		base := path.Base(relative)
		if base == ".wh..wh..opq" {
			directory := path.Dir(relative)
			target, err := imageTarget(root, directory, true)
			if err != nil {
				return expanded, err
			}
			if info, err := os.Lstat(target); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
				if err := os.RemoveAll(target); err != nil {
					return expanded, err
				}
			}
			if err := os.MkdirAll(target, 0755); err != nil {
				return expanded, err
			}
			entries, err := os.ReadDir(target)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return expanded, err
			}
			for _, entry := range entries {
				if err := os.RemoveAll(filepath.Join(target, entry.Name())); err != nil {
					return expanded, err
				}
				deleteImageDirectoryModes(directoryModes, path.Join(directory, entry.Name()))
			}
			continue
		}
		if strings.HasPrefix(base, ".wh.") {
			whiteoutName := strings.TrimPrefix(base, ".wh.")
			if whiteoutName == "" {
				return expanded, errors.New("empty image whiteout target")
			}
			whiteout := path.Join(path.Dir(relative), whiteoutName)
			target, err := imageTarget(root, whiteout, false)
			if err != nil {
				return expanded, err
			}
			if err := os.RemoveAll(target); err != nil {
				return expanded, err
			}
			deleteImageDirectoryModes(directoryModes, whiteout)
			continue
		}
		target, err := imageTarget(root, relative, true)
		if err != nil {
			return expanded, err
		}
		mode := os.FileMode(header.Mode) & 0777
		switch header.Typeflag {
		case tar.TypeDir:
			if info, err := os.Lstat(target); err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
				if err := os.RemoveAll(target); err != nil {
					return expanded, err
				}
			}
			if err := os.MkdirAll(target, 0700); err != nil {
				return expanded, err
			}
			if err := os.Chmod(target, 0700); err != nil {
				return expanded, err
			}
			directoryModes[relative] = mode
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > maxUnpackedImageBytes-alreadyExpanded-expanded {
				return expanded, errors.New("unpacked image exceeds 20 GiB limit")
			}
			if err := os.RemoveAll(target); err != nil {
				return expanded, err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return expanded, err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
			if err != nil {
				return expanded, err
			}
			_, copyErr := io.CopyN(file, reader, header.Size)
			closeErr := file.Close()
			if copyErr != nil {
				return expanded, copyErr
			}
			if closeErr != nil {
				return expanded, closeErr
			}
			expanded += header.Size
		case tar.TypeSymlink:
			if err := os.RemoveAll(target); err != nil {
				return expanded, err
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return expanded, err
			}
		case tar.TypeLink:
			link, err := safeImagePath(header.Linkname)
			if err != nil || link == "." {
				return expanded, errors.New("image hardlink target is unsafe")
			}
			linkTarget, err := imageTarget(root, link, false)
			if err != nil {
				return expanded, err
			}
			info, err := os.Lstat(linkTarget)
			if err != nil || !info.Mode().IsRegular() {
				return expanded, errors.New("image hardlink target must be an existing regular file")
			}
			if err := os.RemoveAll(target); err != nil {
				return expanded, err
			}
			if err := os.Link(linkTarget, target); err != nil {
				return expanded, err
			}
		default:
			// Device nodes, FIFOs and other special entries are intentionally omitted.
		}
	}
}

func deleteImageDirectoryModes(directoryModes map[string]os.FileMode, removed string) {
	for name := range directoryModes {
		if name == removed || strings.HasPrefix(name, removed+"/") {
			delete(directoryModes, name)
		}
	}
}

func applyImageDirectoryModes(root string, directoryModes map[string]os.FileMode) error {
	names := make([]string, 0, len(directoryModes))
	for name := range directoryModes {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		return strings.Count(names[i], "/") > strings.Count(names[j], "/")
	})
	for _, name := range names {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(name)), directoryModes[name]); err != nil {
			return err
		}
	}
	return nil
}

func safeImagePath(name string) (string, error) {
	if name == "" || strings.IndexByte(name, 0) >= 0 || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return "", fmt.Errorf("unsafe image path %q", name)
	}
	clean := path.Clean(strings.TrimPrefix(name, "./"))
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\\") {
		return "", fmt.Errorf("unsafe image path %q", name)
	}
	return clean, nil
}

// imageTarget validates existing parent components before returning a path in
// our private extraction directory; symlink parents are never traversed.
func imageTarget(root, relative string, createParents bool) (string, error) {
	relative, err := safeImagePath(relative)
	if err != nil {
		return "", err
	}
	if relative == "." {
		return root, nil
	}
	parts := strings.Split(path.Dir(relative), "/")
	current := root
	for _, part := range parts {
		if part == "." || part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) && createParents {
			if err := os.Mkdir(current, 0755); err != nil && !errors.Is(err, os.ErrExist) {
				return "", err
			}
			info, err = os.Lstat(current)
		}
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err == nil && (info.Mode()&os.ModeSymlink != 0 || !info.IsDir()) {
			return "", fmt.Errorf("image path traverses non-directory %q", part)
		}
	}
	return filepath.Join(root, filepath.FromSlash(relative)), nil
}
