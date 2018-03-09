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

type licenseInfo struct {
	license    *lic.License
	ImportPath string `json:"path,omitempty"`
	Repository string `json:"repo,omitempty"`
	Type       string `json:"type,omitempty"`
	Revision   string `json:"rev,omitempty"`
	URL        string `json:"url,omitempty"`
}

func (l licenseInfo) include(pkg string) bool {
	return strings.HasPrefix(pkg, l.Repository)
}

type licenseInfoList []licenseInfo

func (ll licenseInfoList) include(pkg string) bool {
	for _, l := range ll {
		if l.include(pkg) {
			return true
		}
	}
	return false
}

func (ll licenseInfoList) writeJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		Licenses []licenseInfo `json:"licenses,omitempty"`
	}{ll})
}

func (ll licenseInfoList) writeCSV(w io.Writer) error {
	cw := csv.NewWriter(w)
	cw.UseCRLF = true
	defer cw.Flush()

	if err := cw.Write([]string{"path", "repo", "type", "rev", "url"}); err != nil {
		return err
	}

	for _, l := range ll {
		if err := cw.Write([]string{l.ImportPath, l.Repository, l.Type, l.Revision, l.URL}); err != nil {
			return err
		}
	}
	return nil
}

func (ll licenseInfoList) dump(dir string) error {
	for _, l := range ll {
		if l.license == nil {
			continue
		}

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

		_, err = io.Copy(f, strings.NewReader(l.license.Text))
		if err != nil {
			return err
		}
	}
	return nil
}

func getLicenses(imports []string) (licenseInfoList, error) {
	var ls licenseInfoList

	for _, im := range imports {
		if ls.include(im) {
			continue
		}

		src := filepath.Join(os.Getenv("GOPATH"), "src")
		dir := filepath.Join(src, im)

		g, err := getGitInfo(dir)
		if err != nil {
			continue
		}

		li := licenseInfo{
			ImportPath: im,
			Revision:   g.Commit,
			Type:       "Unknown",
			URL:        g.OriginURL,
		}

		for rep := im; rep != "."; rep = filepath.Dir(rep) {
			l, err := lic.NewFromDir(filepath.Join(src, rep))
			if err != nil {
				continue
			}

			li.license = l
			li.Repository = rep
			li.Type = l.Type
			break
		}
		ls = append(ls, li)
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
