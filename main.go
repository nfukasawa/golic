package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	lic "github.com/ryanuber/go-license"
)

func main() {

	format := ""
	licensesDir := ""
	flag.StringVar(&format, "format", "json", "output format. json or csv. default is json.")
	flag.StringVar(&licensesDir, "licenses_dir", "", "directory path to output LICENSE files.")
	flag.Parse()

	targets := flag.Args()
	if len(targets) == 0 {
		fmt.Fprintln(os.Stderr, "targets required.")
		os.Exit(1)
	}

	imports, err := getImportPaths(targets)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to get import paths:", err)
		os.Exit(1)
	}

	ll, err := getLicenses(imports)
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to get licenses:", err)
		os.Exit(1)
	}

	switch format {
	case "csv":
		err = ll.writeCSV(os.Stdout)
	default:
		err = ll.writeJSON(os.Stdout)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "failed to output:", err)
		os.Exit(1)
	}

	if licensesDir != "" {
		err = ll.dump(licensesDir)
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to output LICENSE files:", err)
			os.Exit(1)
		}
	}
}

func getImportPaths(targets []string) ([]string, error) {
	args := append([]string{
		"list",
		"-f",
		`{{join .Deps "\n"}}`,
	}, targets...)
	out, err := exec.Command("go", args...).Output()
	if err != nil {
		return nil, err
	}
	imports := strings.Split(strings.TrimRight(string(out), "\n"), "\n")

	args = append([]string{
		"list",
		"-f",
		`{{if not .Standard}}{{.ImportPath}}{{end}}`,
	}, imports...)
	out, err = exec.Command("go", args...).Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n"), nil
}

type license struct {
	l          *lic.License
	Repository string `json:"repo,omitempty"`
	Type       string `json:"type,omitempty"`
	Revision   string `json:"rev,omitempty"`
	URL        string `json:"url,omitempty"`
}

func (l license) include(pkg string) bool {
	return strings.HasPrefix(pkg, l.Repository)
}

type licenseList []license

func (ll licenseList) include(pkg string) bool {
	for _, l := range ll {
		if l.include(pkg) {
			return true
		}
	}
	return false
}

func (ll licenseList) writeJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	return enc.Encode(struct {
		Licenses []license `json:"licenses,omitempty"`
	}{ll})
}

func (ll licenseList) writeCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	defer cw.Flush()

	if err := cw.Write([]string{"repo", "type", "rev", "url"}); err != nil {
		return err
	}

	for _, l := range ll {
		if err := cw.Write([]string{l.Repository, l.Type, l.Revision, l.URL}); err != nil {
			return err
		}
	}
	return nil
}

func (ll licenseList) dump(dir string) error {
	for _, l := range ll {
		p := filepath.Join(dir, l.Repository)
		err := os.MkdirAll(p, 0755)
		if err != nil {
			return err
		}

		f, err := os.Create(filepath.Join(p, "LICENSE"))
		if err != nil {
			return err
		}
		defer f.Close()

		_, err = io.Copy(f, strings.NewReader(l.l.Text))
		if err != nil {
			return err
		}
	}
	return nil
}

func getLicenses(imports []string) (licenseList, error) {
	gopath := os.Getenv("GOPATH")

	var ls licenseList

	for _, im := range imports {
		if ls.include(im) {
			continue
		}

		dirs := strings.Split(im, "/")
		for i := len(dirs); i != 0; i-- {
			subdir := dirs[0:i]
			dir := filepath.Join(append([]string{gopath, "src"}, subdir...)...)
			l, err := lic.NewFromDir(dir)
			if err != nil {
				continue
			}

			repo := strings.Join(subdir, "/")
			g, err := getGitInfo(dir)
			if err != nil {
				continue
			}

			ls = append(ls, license{
				l:          l,
				Repository: repo,
				Type:       l.Type,
				Revision:   g.Commit,
				URL:        g.OriginURL,
			})
			break
		}
	}

	return ls, nil
}

type gitInfo struct {
	OriginURL string
	Commit    string
}

func getGitInfo(dir string) (info gitInfo, err error) {
	cd, err := os.Getwd()
	if err != nil {
		return info, err
	}
	err = os.Chdir(dir)
	if err != nil {
		return info, err
	}
	defer os.Chdir(cd)

	commit, err := exec.Command("git", "log", "-1", "--format=%H").Output()
	if err != nil {
		fmt.Println(err)
		return info, err
	}
	info.Commit = strings.TrimSpace(string(commit))

	origin, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		fmt.Println(err)
		return info, err
	}
	info.OriginURL = strings.TrimSpace(string(origin))

	return info, nil
}
