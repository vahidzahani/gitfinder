package main

import (
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed report.html
var reportTemplate string

const staleDays = 180

type reportData struct {
	App       string   `json:"app"`
	Version   string   `json:"version"`
	Generated int64    `json:"generated"`
	Host      string   `json:"host"`
	Mode      string   `json:"mode"`
	Roots     []string `json:"roots"`
	Fetched   bool     `json:"fetched"`
	Seconds   float64  `json:"seconds"`
	Folders   int64    `json:"folders"`
	StaleDays int      `json:"staleDays"`
	Repos     []*Repo  `json:"repos"`
}

// writeReport injects the data as JSON into the embedded page. json.Marshal
// escapes <, > and &, so the data cannot close the script tag.
func writeReport(path string, d reportData) error {
	d.App, d.Version, d.StaleDays = appName, appVersion, staleDays
	d.Generated = time.Now().Unix()
	d.Host, _ = os.Hostname()
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	page := strings.Replace(reportTemplate, "/*__GITFINDER_DATA__*/null", string(b), 1)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(page), 0o644)
}

func openInBrowser(path string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", path).Start()
}

// markDuplicates links repositories that were cloned from the same remote.
func markDuplicates(repos []*Repo) {
	groups := map[string][]*Repo{}
	for _, r := range repos {
		r.Duplicates = nil
		seen := map[string]bool{}
		for _, rem := range r.Remotes {
			if rem.Key != "" && !seen[rem.Key] {
				seen[rem.Key] = true
				groups[rem.Key] = append(groups[rem.Key], r)
			}
		}
	}
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		for _, r := range g {
			for _, o := range g {
				if o != r && !contains(r.Duplicates, o.Path) {
					r.Duplicates = append(r.Duplicates, o.Path)
				}
			}
		}
	}
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
