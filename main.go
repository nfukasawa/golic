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

	lic "github.com/nfukasawa/go-license"
)

const usageTmpl = `
Usage of %s:
  %s [options] packages...

options:
`

func init() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, usageTmpl, os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
}

func main() {
	format := ""
	licensesDir := ""
	flag.StringVar(&format, "format", "json", "specify the format. json or csv.")
	flag.StringVar(&licensesDir, "licenses_dir", "", "output LICENSE files to the specified directory.")
	flag.Parse()

	pkgs := flag.Args()
	if len(pkgs) == 0 {
		flag.Usage()
		return
	}

	imports, err := getImportPaths(pkgs)
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
	Type string `json:"type,omitempty"`
	File string `json:"file,omitempty"`
	Text string `json:"-"`
}

func newLicense(l *lic.License) license {
	return license{
		Type: l.Type,
		File: filepath.Base(l.File),
		Text: l.Text,
	}
}

func newLicenses(ls []*lic.License) []license {
	ret := make([]license, len(ls))
	for i, l := range ls {
		ret[i] = newLicense(l)
	}
	return ret
}

type pkgInfo struct {
	ImportPath string    `json:"path,omitempty"`
	Repository string    `json:"repo,omitempty"`
	Revision   string    `json:"rev,omitempty"`
	URL        string    `json:"url,omitempty"`
	Licenses   []license `json:"licenses,omitempty"`
}

func (l pkgInfo) include(pkg string) bool {
	if l.Repository == "" {
		return false
	}
	return strings.HasPrefix(pkg, l.Repository)
}

type pkgInfoList []pkgInfo

func (pl pkgInfoList) include(pkg string) bool {
	for _, p := range pl {
		if p.include(pkg) {
			return true
		}
	}
	return false
}

func (pl pkgInfoList) writeJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Licenses []pkgInfo `json:"packages,omitempty"`
	}{pl})
}

func (pl pkgInfoList) writeCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	defer cw.Flush()

	if err := cw.Write([]string{"path", "repo", "type", "rev", "url"}); err != nil {
		return err
	}

	for _, p := range pl {
		types := make([]string, len(p.Licenses))
		for i, l := range p.Licenses {
			types[i] = l.File + ":" + l.Type
		}
		if err := cw.Write([]string{p.ImportPath, p.Repository, strings.Join(types, " "), p.Revision, p.URL}); err != nil {
			return err
		}
	}
	return nil
}

func (pl pkgInfoList) dump(dir string) error {
	for _, p := range pl {
		if len(p.Licenses) == 0 {
			continue
		}

		subdir := filepath.Join(dir, p.Repository)
		err := os.MkdirAll(subdir, 0755)
		if err != nil {
			return err
		}

		for _, l := range p.Licenses {
			f, err := os.Create(filepath.Join(subdir, l.File))
			if err != nil {
				return err
			}
			defer f.Close()

			_, err = io.Copy(f, strings.NewReader(l.Text))
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func getLicenses(imports []string) (pkgInfoList, error) {
	var ps pkgInfoList

	gopaths := strings.Split(os.Getenv("GOPATH"), ";")

	for _, im := range imports {
		if ps.include(im) {
			continue
		}

		for _, gopath := range gopaths {
			src := filepath.Join(gopath, "src")
			dir := filepath.Join(src, im)

			if src == dir {
				break
			}

			g, err := getGitInfo(dir)
			if err != nil {
				continue
			}

			p := pkgInfo{
				ImportPath: im,
				Revision:   g.Commit,
				URL:        g.OriginURL,
			}

			for rep := im; rep != "."; rep = filepath.Dir(rep) {
				ls, err := lic.NewLicencesFromDir(filepath.Join(src, rep))
				if err != nil {
					continue
				}
				p.Licenses = newLicenses(ls)
				p.Repository = rep
				break
			}
			ps = append(ps, p)
			break
		}
	}

	return ps, nil
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
		return info, err
	}
	info.Commit = strings.TrimSpace(string(commit))

	origin, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return info, err
	}
	info.OriginURL = strings.TrimSpace(string(origin))

	return info, nil
}
