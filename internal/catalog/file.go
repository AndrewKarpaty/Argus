package catalog

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/AndrewKarpaty/Argus/internal/domain"
)

// fileDoc is the on-disk shape of the catalog.
type fileDoc struct {
	Services     []domain.Service    `yaml:"services"`
	Dependencies []domain.Dependency `yaml:"dependencies"`
}

// File is a Source backed by a YAML file. Keeping the catalog in the repo
// next to the code is the cheapest way to start: it is reviewable, diffable
// and does not need a control plane.
type File struct {
	Path string
}

// NewFile returns a file-backed catalog source.
func NewFile(path string) File { return File{Path: path} }

func (f File) Name() string { return "file:" + filepath.Base(f.Path) }

// Load reads and parses the catalog file.
func (f File) Load(context.Context) (Snapshot, error) {
	raw, err := os.ReadFile(f.Path)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read %s: %w", f.Path, err)
	}
	var doc fileDoc
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true) // a typo in a catalog key must not pass silently
	if err := dec.Decode(&doc); err != nil {
		return Snapshot{}, fmt.Errorf("parse %s: %w", f.Path, err)
	}
	if len(doc.Services) == 0 {
		return Snapshot{}, fmt.Errorf("parse %s: no services defined", f.Path)
	}

	info, err := os.Stat(f.Path)
	loadedAt := time.Now().UTC()
	if err == nil {
		loadedAt = info.ModTime().UTC()
	}
	return Snapshot{
		Services:     doc.Services,
		Dependencies: doc.Dependencies,
		Source:       f.Name(),
		LoadedAt:     loadedAt,
	}, nil
}
