package migration

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Parser loads and validates YAML migration files.
type Parser struct{}

// NewParser creates a new Parser.
func NewParser() *Parser {
	return &Parser{}
}

// ParseFile reads and parses a single YAML migration file.
// The returned MigrationFile has its FilePath set to the resolved path.
func (p *Parser) ParseFile(path string) (*MigrationFile, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving path %q: %w", path, err)
	}

	data, err := os.ReadFile(absPath)
	if err != nil {
		return nil, fmt.Errorf("reading migration file %q: %w", absPath, err)
	}

	var mf MigrationFile
	if err := yaml.Unmarshal(data, &mf); err != nil {
		return nil, fmt.Errorf("parsing YAML in %q: %w", absPath, err)
	}

	mf.FilePath = absPath
	return &mf, nil
}

// ParseDir reads all .yaml and .yml files in a directory, sorted by filename.
// Returns the parsed migration files in sorted order.
func (p *Parser) ParseDir(dir string) ([]*MigrationFile, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving directory %q: %w", dir, err)
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %q: %w", absDir, err)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".yaml" || ext == ".yml" {
			paths = append(paths, filepath.Join(absDir, name))
		}
	}

	sort.Strings(paths)
	return p.parsePaths(paths)
}

// ParseFiles reads multiple specific file paths, processing in the given order.
// Each path can be a file or directory:
//   - Files are parsed directly
//   - Directories have all their .yaml/.yml files parsed in sorted order
func (p *Parser) ParseFiles(paths []string) ([]*MigrationFile, error) {
	var allPaths []string

	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			return nil, fmt.Errorf("stat %q: %w", path, err)
		}

		if info.IsDir() {
			dirFiles, err := p.collectDirFiles(path)
			if err != nil {
				return nil, err
			}
			allPaths = append(allPaths, dirFiles...)
		} else {
			absPath, err := filepath.Abs(path)
			if err != nil {
				return nil, fmt.Errorf("resolving path %q: %w", path, err)
			}
			allPaths = append(allPaths, absPath)
		}
	}

	return p.parsePaths(allPaths)
}

// collectDirFiles returns sorted YAML file paths from a directory.
func (p *Parser) collectDirFiles(dir string) ([]string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolving directory %q: %w", dir, err)
	}

	entries, err := os.ReadDir(absDir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %q: %w", absDir, err)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".yaml" || ext == ".yml" {
			paths = append(paths, filepath.Join(absDir, name))
		}
	}

	sort.Strings(paths)
	return paths, nil
}

// parsePaths parses a list of absolute file paths in order.
func (p *Parser) parsePaths(paths []string) ([]*MigrationFile, error) {
	var results []*MigrationFile

	for _, path := range paths {
		mf, err := p.ParseFile(path)
		if err != nil {
			return nil, err
		}
		results = append(results, mf)
	}

	return results, nil
}
