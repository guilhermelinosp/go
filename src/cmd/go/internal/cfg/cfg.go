// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package cfg holds configuration shared by multiple parts
// of the go command.
package cfg

import (
	"bytes"
	"fmt"
	"go/build"
	"internal/cfg"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"cmd/internal/objabi"
)

// These are general "build flags" used by build and other commands.
var (
	BuildA                 bool   // -a flag
	BuildBuildmode         string // -buildmode flag
	BuildContext           = defaultContext()
	BuildMod               string             // -mod flag
	BuildModReason         string             // reason -mod flag is set, if set by default
	BuildI                 bool               // -i flag
	BuildLinkshared        bool               // -linkshared flag
	BuildMSan              bool               // -msan flag
	BuildN                 bool               // -n flag
	BuildO                 string             // -o flag
	BuildP                 = runtime.NumCPU() // -p flag
	BuildPkgdir            string             // -pkgdir flag
	BuildRace              bool               // -race flag
	BuildToolexec          []string           // -toolexec flag
	BuildToolchainName     string
	BuildToolchainCompiler func() string
	BuildToolchainLinker   func() string
	BuildTrimpath          bool // -trimpath flag
	BuildV                 bool // -v flag
	BuildWork              bool // -work flag
	BuildX                 bool // -x flag

	ModCacheRW bool   // -modcacherw flag
	ModFile    string // -modfile flag

	CmdName string // "build", "install", "list", "mod tidy", etc.

	DebugActiongraph string // -debug-actiongraph flag (undocumented, unstable)
)

func defaultContext() build.Context {
	ctxt := build.Default
	ctxt.JoinPath = filepath.Join // back door to say "do not use go command"

	ctxt.GOROOT = findGOROOT()
	if runtime.Compiler != "gccgo" {
		// Note that we must use runtime.GOOS and runtime.GOARCH here,
		// as the tool directory does not move based on environment
		// variables. This matches the initialization of ToolDir in
		// go/build, except for using ctxt.GOROOT rather than
		// runtime.GOROOT.
		build.ToolDir = filepath.Join(ctxt.GOROOT, "pkg/tool/"+runtime.GOOS+"_"+runtime.GOARCH)
	}

	return ctxt
}

func init() {
	BuildToolchainCompiler = func() string { return "missing-compiler" }
	BuildToolchainLinker = func() string { return "missing-linker" }
}

// An EnvVar is an environment variable Name=Value.
type EnvVar struct {
	Name  string
	Value string
}

// OrigEnv is the original environment of the program at startup.
var OrigEnv []string

// CmdEnv is the new environment for running go tool commands.
// User binaries (during go test or go run) are run with OrigEnv,
// not CmdEnv.
var CmdEnv []EnvVar

// Global build parameters (used during package load)
var (
	Goarch = BuildContext.GOARCH
	Goos   = BuildContext.GOOS

	ExeSuffix = exeSuffix()

	// ModulesEnabled specifies whether the go command is running
	// in module-aware mode (as opposed to GOPATH mode).
	// It is equal to modload.Enabled, but not all packages can import modload.
	ModulesEnabled bool
)

func exeSuffix() string {
	if Goos == "windows" {
		return ".exe"
	}
	return ""
}

var envCache struct {
	once sync.Once
	m    map[string]string
}

// EnvFile returns the name of the Go environment configuration file.
func EnvFile() (string, error) {
	if file := os.Getenv("GOENV"); file != "" {
		if file == "off" {
			return "", fmt.Errorf("GOENV=off")
		}
		return file, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", fmt.Errorf("missing user-config dir")
	}
	return filepath.Join(dir, "go/env"), nil
}

func initEnvCache() {
	envCache.m = make(map[string]string)
	file, _ := EnvFile()
	if file == "" {
		return
	}
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return
	}

	for len(data) > 0 {
		// Get next line.
		line := data
		i := bytes.IndexByte(data, '\n')
		if i >= 0 {
			line, data = line[:i], data[i+1:]
		} else {
			data = nil
		}

		i = bytes.IndexByte(line, '=')
		if i < 0 || line[0] < 'A' || 'Z' < line[0] {
			// Line is missing = (or empty) or a comment or not a valid env name. Ignore.
			// (This should not happen, since the file should be maintained almost
			// exclusively by "go env -w", but better to silently ignore than to make
			// the go command unusable just because somehow the env file has
			// gotten corrupted.)
			continue
		}
		key, val := line[:i], line[i+1:]
		envCache.m[string(key)] = string(val)
	}
}

// Getenv gets the value for the configuration key.
// It consults the operating system environment
// and then the go/env file.
// If Getenv is called for a key that cannot be set
// in the go/env file (for example GODEBUG), it panics.
// This ensures that CanGetenv is accurate, so that
// 'go env -w' stays in sync with what Getenv can retrieve.
func Getenv(key string) string {
	if !CanGetenv(key) {
		switch key {
		case "CGO_TEST_ALLOW", "CGO_TEST_DISALLOW", "CGO_test_ALLOW", "CGO_test_DISALLOW":
			// used by internal/work/security_test.go; allow
		default:
			panic("internal error: invalid Getenv " + key)
		}
	}
	val := os.Getenv(key)
	if val != "" {
		return val
	}
	envCache.once.Do(initEnvCache)
	return envCache.m[key]
}

// CanGetenv reports whether key is a valid go/env configuration key.
func CanGetenv(key string) bool {
	return strings.Contains(cfg.KnownEnv, "\t"+key+"\n")
}

var (
	GOROOT       = BuildContext.GOROOT
	GOBIN        = Getenv("GOBIN")
	GOROOTbin    = filepath.Join(GOROOT, "bin")
	GOROOTpkg    = filepath.Join(GOROOT, "pkg")
	GOROOTsrc    = filepath.Join(GOROOT, "src")
	GOROOT_FINAL = findGOROOT_FINAL()
	GOMODCACHE   = envOr("GOMODCACHE", gopathDir("pkg/mod"))

	// Used in envcmd.MkEnv and build ID computations.
	GOARM    = envOr("GOARM", fmt.Sprint(objabi.GOARM))
	GO386    = envOr("GO386", objabi.GO386)
	GOMIPS   = envOr("GOMIPS", objabi.GOMIPS)
	GOMIPS64 = envOr("GOMIPS64", objabi.GOMIPS64)
	GOPPC64  = envOr("GOPPC64", fmt.Sprintf("%s%d", "power", objabi.GOPPC64))
	GOWASM   = envOr("GOWASM", fmt.Sprint(objabi.GOWASM))

	GOPROXY    = envOr("GOPROXY", "https://proxy.golang.org,direct")
	GOSUMDB    = envOr("GOSUMDB", "sum.golang.org")
	GOPRIVATE  = Getenv("GOPRIVATE")
	GONOPROXY  = envOr("GONOPROXY", GOPRIVATE)
	GONOSUMDB  = envOr("GONOSUMDB", GOPRIVATE)
	GOINSECURE = Getenv("GOINSECURE")
)

var SumdbDir = gopathDir("pkg/sumdb")

// GetArchEnv returns the name and setting of the
// GOARCH-specific architecture environment variable.
// If the current architecture has no GOARCH-specific variable,
// GetArchEnv returns empty key and value.
func GetArchEnv() (key, val string) {
	switch Goarch {
	case "arm":
		return "GOARM", GOARM
	case "386":
		return "GO386", GO386
	case "mips", "mipsle":
		return "GOMIPS", GOMIPS
	case "mips64", "mips64le":
		return "GOMIPS64", GOMIPS64
	case "ppc64", "ppc64le":
		return "GOPPC64", GOPPC64
	case "wasm":
		return "GOWASM", GOWASM
	}
	return "", ""
}

// envOr returns Getenv(key) if set, or else def.
func envOr(key, def string) string {
	val := Getenv(key)
	if val == "" {
		val = def
	}
	return val
}

// There is a copy of findGOROOT, isSameDir, and isGOROOT in
// x/tools/cmd/godoc/goroot.go.
// Try to keep them in sync for now.

// findGOROOT returns the GOROOT value, using either an explicitly
// provided environment variable, a GOROOT that contains the current
// os.Executable value, or else the GOROOT that the binary was built
// with from runtime.GOROOT().
//
// There is a copy of this code in x/tools/cmd/godoc/goroot.go.
func findGOROOT() string {
	if env := Getenv("GOROOT"); env != "" {
		return filepath.Clean(env)
	}
	def := filepath.Clean(runtime.GOROOT())
	if runtime.Compiler == "gccgo" {
		// gccgo has no real GOROOT, and it certainly doesn't
		// depend on the executable's location.
		return def
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.Abs(exe)
		if err == nil {
			if dir := filepath.Join(exe, "../.."); isGOROOT(dir) {
				// If def (runtime.GOROOT()) and dir are the same
				// directory, prefer the spelling used in def.
				if isSameDir(def, dir) {
					return def
				}
				return dir
			}
			exe, err = filepath.EvalSymlinks(exe)
			if err == nil {
				if dir := filepath.Join(exe, "../.."); isGOROOT(dir) {
					if isSameDir(def, dir) {
						return def
					}
					return dir
				}
			}
		}
	}
	return def
}

func findGOROOT_FINAL() string {
	// $GOROOT_FINAL is only for use during make.bash
	// so it is not settable using go/env, so we use os.Getenv here.
	def := GOROOT
	if env := os.Getenv("GOROOT_FINAL"); env != "" {
		def = filepath.Clean(env)
	}
	return def
}

// isSameDir reports whether dir1 and dir2 are the same directory.
func isSameDir(dir1, dir2 string) bool {
	if dir1 == dir2 {
		return true
	}
	info1, err1 := os.Stat(dir1)
	info2, err2 := os.Stat(dir2)
	return err1 == nil && err2 == nil && os.SameFile(info1, info2)
}

// isGOROOT reports whether path looks like a GOROOT.
//
// It does this by looking for the path/pkg/tool directory,
// which is necessary for useful operation of the cmd/go tool,
// and is not typically present in a GOPATH.
//
// There is a copy of this code in x/tools/cmd/godoc/goroot.go.
func isGOROOT(path string) bool {
	stat, err := os.Stat(filepath.Join(path, "pkg", "tool"))
	if err != nil {
		return false
	}
	return stat.IsDir()
}

func gopathDir(rel string) string {
	list := filepath.SplitList(BuildContext.GOPATH)
	if len(list) == 0 || list[0] == "" {
		return ""
	}
	return filepath.Join(list[0], rel)
}
