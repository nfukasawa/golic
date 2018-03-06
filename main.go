package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	lic "github.com/ryanuber/go-license"
)

func main() {
	flag.Parse()
	targets := flag.Args()
	if len(targets) == 0 {
		// TODO
		panic("targets required")
	}

	pkgs, err := getPackages(targets)
	if err != nil {
		// TODO
		panic(err)
	}

	ls, err := getLicenses(pkgs)
	if err != nil {
		// TODO
		panic(err)
	}

	for _, l := range ls {
		fmt.Println(l.Repository, l.Type, l.Revision)
	}
}

func getPackages(targets []string) ([]string, error) {
	args := append([]string{
		"list",
		"-f",
		`{{join .Deps "\n"}}`,
	}, targets...)
	out, err := exec.Command("go", args...).Output()
	if err != nil {
		return nil, err
	}
	pkgs := strings.Split(strings.TrimRight(string(out), "\n"), "\n")

	args = append([]string{
		"list",
		"-f",
		`{{if not .Standard}}{{.ImportPath}}{{end}}`,
	}, pkgs...)
	out, err = exec.Command("go", args...).Output()
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(string(out), "\n"), "\n"), nil
}

type license struct {
	*lic.License
	Repository string
	Revision   string
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

func getLicenses(pkgs []string) ([]license, error) {
	gopath := os.Getenv("GOPATH")

	var ls licenseList

	for _, pkg := range pkgs {
		if ls.include(pkg) {
			continue
		}

		dirs := strings.Split(pkg, "/")
		for i := len(dirs); i != 0; i-- {
			subdir := dirs[0:i]
			dir := filepath.Join(append([]string{gopath, "src"}, subdir...)...)
			l, err := lic.NewFromDir(dir)
			if err != nil {
				continue
			}
			rev, err := getGitCommit(dir)
			if err != nil {
				// TODO
			}
			repo := strings.Join(subdir, "/")
			ls = append(ls, license{
				License:    l,
				Repository: repo,
				Revision:   rev,
			})
		}
	}

	return ls, nil
}

func getGitCommit(dir string) (string, error) {
	cd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	err = os.Chdir(dir)
	if err != nil {
		return "", err
	}
	defer os.Chdir(cd)

	out, err := exec.Command("git", "log", "-1", "--format=%H").Output()
	if err != nil {
		fmt.Println(err)
		return "", err
	}
	return string(out), nil
}
