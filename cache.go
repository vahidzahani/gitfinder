package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const cacheVersion = 1

// Cache keeps the last results in %LOCALAPPDATA%\GitFinder so the next run
// can refresh known repositories without walking the disks again.
type Cache struct {
	Version        int              `json:"version"`
	LastScan       int64            `json:"lastScan"`
	SelectedDrives []string         `json:"selectedDrives,omitempty"`
	Repos          map[string]*Repo `json:"repos"`
}

func appDataDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, appName)
}

func cachePath() string { return filepath.Join(appDataDir(), "cache.json") }

func repoKey(p string) string { return strings.ToLower(filepath.Clean(p)) }

func loadCache() *Cache {
	c := &Cache{}
	if b, err := os.ReadFile(cachePath()); err == nil {
		json.Unmarshal(b, c)
	}
	if c.Version != cacheVersion || c.Repos == nil {
		c = &Cache{Version: cacheVersion, Repos: map[string]*Repo{}}
	}
	return c
}

func (c *Cache) save() error {
	if err := os.MkdirAll(appDataDir(), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := cachePath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, cachePath())
}

func (c *Cache) get(path string) *Repo { return c.Repos[repoKey(path)] }

// knownPaths returns cached repositories that still exist on disk and
// forgets the ones that were deleted or moved.
func (c *Cache) knownPaths() []string {
	var paths []string
	for k, r := range c.Repos {
		if _, err := os.Lstat(filepath.Join(r.Path, ".git")); err != nil {
			delete(c.Repos, k)
			continue
		}
		paths = append(paths, r.Path)
	}
	return paths
}

// update stores fresh results. After a full scan of roots, cached repos under
// those roots that were not found again are dropped.
func (c *Cache) update(roots []string, repos []*Repo, fullScan bool) {
	if fullScan {
		for k := range c.Repos {
			if underAny(k, roots) {
				delete(c.Repos, k)
			}
		}
	}
	for _, r := range repos {
		c.Repos[repoKey(r.Path)] = r
	}
	c.LastScan = time.Now().Unix()
}

func underAny(key string, roots []string) bool {
	for _, root := range roots {
		r := repoKey(root)
		if !strings.HasSuffix(r, `\`) {
			r += `\`
		}
		if strings.HasPrefix(key+`\`, r) {
			return true
		}
	}
	return false
}

func ago(unix int64) string {
	d := time.Since(time.Unix(unix, 0))
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute") + " ago"
	case d < 48*time.Hour:
		return plural(int(d.Hours()), "hour") + " ago"
	default:
		return plural(int(d.Hours()/24), "day") + " ago"
	}
}

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return itoa(n) + " " + word + "s"
}

func itoa(n int) string { return thousands(int64(n)) }
