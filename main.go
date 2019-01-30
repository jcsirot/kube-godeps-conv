package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/golang/dep"
	"github.com/golang/dep/gps"
	log "github.com/sirupsen/logrus"
)

var (
	version = flag.String("version", "", ``+
		`Kubernetes version for which dependencies have to be converted into a Gopkg.toml`)

	kubeDeps = []string{
		"apiextensions-apiserver", "apimachinery", "client-go", "api", "apiserver", "metrics",
	}
)

func main() {
	flag.Parse()
	var input Godeps

	// read the Godeps.json file
	url := fmt.Sprintf("https://raw.githubusercontent.com/kubernetes/kubernetes/v%s/Godeps/Godeps.json", *version)
	data, err := downloadFile(url)
	if err != nil {
		log.Fatalf("Error fetchin input Godeps file %s: %s", url, err)
	}

	// decode the Godeps.json file
	err = json.Unmarshal(data, &input)
	if err != nil {
		log.Fatalf("Error unmarshaling Godeps file %s: %s", url, err)
	}

	// construct a dep source manager in order to resolve package names to
	// their project roots
	sourceMgr, err := gps.NewSourceManager(gps.SourceManagerConfig{
		Cachedir: filepath.Join(defaultGOPATH(), "pkg", "dep"),
	})
	if err != nil {
		log.Fatalf("Error constructing Dep source manager: %s", err)
	}

	// we flatten dependencies here as Dep requires that only project roots are
	// specified in the Gopkg.toml file
	flattenedDeps, err := flattenDepsToRoot(sourceMgr, input.Deps)
	if err != nil {
		log.Fatalf("Error flattening project dependencies: %s", err)
	}

	kubeVersionConstraints := make(gps.ProjectConstraints)
	kubeVersionConstraints[gps.ProjectRoot("k8s.io/kubernetes")] = gps.ProjectProperties{
		Constraint: gps.Revision(fmt.Sprintf("v%s", *version)),
	}

	manifest := dep.Manifest{
		Ignored:     []string{"github.com/docker/kube-e2e-image/*"},
		Constraints: kubeVersionConstraints,
		Ovr:         rewriteDepsWithPrefix(flattenedDeps, kubeDeps),
	}
	tomlBytes, err := manifest.MarshalTOML()
	if err != nil {
		log.Fatalf("Error marshaling Dep manifest to TOML: %s", err)
	}
	fmt.Print(string(tomlBytes))
}

// flattenDepsToRoot will 'flatten' all dependencies given in deps down to the
// root packages they are contained within.
// For example, it will convert 'k8s.io/api/core/v1' and 'k8s.io/api/extensions'
// into 'k8s.io/api'.
func flattenDepsToRoot(manager gps.SourceManager, deps []Dependency) (map[string]string, error) {
	depMap := make(map[string]string)
	for _, d := range deps {
		root, err := manager.DeduceProjectRoot(d.ImportPath)
		if err != nil {
			return nil, err
		}
		depMap[string(root)] = d.Rev
	}
	return depMap, nil
}

func rewriteDepsWithPrefix(deps map[string]string, kubeDeps []string) gps.ProjectConstraints {
	constraints := make(gps.ProjectConstraints)

	for pkg, rev := range deps {
		// rewrite the constraints for some specifics packages
		if pkg == "github.com/onsi/ginkgo" {
			constraints[gps.ProjectRoot(pkg)] = gps.ProjectProperties{
				Constraint: gps.Revision("8a7f310861b2f59f13b339dc506cd8d8c28b147c"),
				Source:     "https://github.com/jcsirot/ginkgo.git",
			}
		} else if pkg == "github.com/fsnotify/fsnotify" { // dep bug? https://github.com/golang/dep/issues/1799
			constraints[gps.ProjectRoot("gopkg.in/fsnotify.v1")] = gps.ProjectProperties{
				Constraint: gps.Revision(rev),
				Source:     "https://github.com/fsnotify/fsnotify.git",
			}
		} else {
			constraints[gps.ProjectRoot(pkg)] = gps.ProjectProperties{
				Constraint: gps.Revision(rev),
			}
		}
	}

	for _, pkg := range kubeDeps {
		constraints[gps.ProjectRoot(fmt.Sprintf("k8s.io/%s", pkg))] = gps.ProjectProperties{
			Constraint: gps.Revision(fmt.Sprintf("kubernetes-%s", *version)),
		}
	}

	return constraints
}

// defaultGOPATH gets the default GOPATH that was added in 1.8
// copied from go/build/build.go
func defaultGOPATH() string {
	env := "HOME"
	if runtime.GOOS == "windows" {
		env = "USERPROFILE"
	} else if runtime.GOOS == "plan9" {
		env = "home"
	}
	if home := os.Getenv(env); home != "" {
		def := filepath.Join(home, "go")
		if def == runtime.GOROOT() {
			// Don't set the default GOPATH to GOROOT,
			// as that will trigger warnings from the go tool.
			return ""
		}
		return def
	}
	return ""
}

func downloadFile(url string) ([]byte, error) {
	// Get the data
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return bodyBytes, nil
}
