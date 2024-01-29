// Copyright (c) 2023 The Decred developers.
//
// Builder file for the CUDA-based version of gominer.
//
// Unfortunately, most of the complexity of this generator comes from the
// Windows build, along with limitations of the cgo tool.
//
// For non-Windows OSes, generating an appropriate library to include in the
// resulting binary is usually just a matter of installing the required
// packages from the distribution's package manager and then calling the Nvidia
// CUDA compiler (nvcc) which should automatically be in the $PATH environment
// variable.
//
// On Windows however, the situation is much more complicated: nvcc only works
// with cl.exe, which is included in the Desktop Development with C++ component
// of Microsoft's Visual Studio software. OTOH, cl.exe has little to no support
// as a compiler for cgo, thus gominer is expected to be built with gcc inside
// an MSYS2 environment when building on Windows. Additionally, neither cl.exe
// nor the required Nvidia headers and libraries are included by default on
// the system's or user's environment variables.
//
// Thus, in order to attempt to reduce the need for manual setup of the entire
// building environment, this generator makes guesses and uses tricks to
// ease building gominer, specially on Windows platforms. To generate
// CUDA-enabled gominer binaries, run:
//
//   go generate -tags cuda .
//
// Instead of the usual way of using go run/go build.

// The following build constraint enforces this file is only built as a
// consequence of running `go generate -tags cuda .`.
//go:build cudabuilder
// +build cudabuilder

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

var (
	errNotOnMsys        = errors.New("not on MSYS2 MINGW64 terminal")
	errCudaPathEnvUnset = errors.New("CUDA_PATH environment variable not set")
	errNoCudartDll      = errors.New("cudart64_xx.dll not found")
	errVswhereFailed    = errors.New("vswhere failed to find MSVC install")
	errNoMsDevToolsDir  = errors.New("no C++ desktop compiler tools found")
)

// runCmd runs the command until it ends and returns an error. Stdout and Stderr
// are redirected to the process's own, instead of discarded.
func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// runWithOutput runs the command until it ends and returns the output if no
// errors are found.
func runWithOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	if err != nil {
		if out != nil {
			fmt.Println(string(out))
		}
		return "", err
	}
	return string(out), nil
}

// winPathToShort converts a windows filepath into its short filepath
// representation on a best effort basis. Works only on absolute paths.
//
// This is a limited version of the Windows API function GetShortPathName(),
// implemented here to avoid having to create platform specific files for
// cuda_builder.go.
//
// This is used as a trick to get over the fact that CGO_CFLAGS and CGO_LDFLAGS
// do not support paths with spaces in them.
func winPathToShort(path string) string {
	if len(path) < 3 || path[1:3] != `:\` {
		return path
	}

	parts := strings.Split(path, `\`)
	parts[0] = parts[0] + `\`
	for i, p := range parts {
		if len(p) <= 8 {
			continue
		}

		// List contents of the parent dir and find the index of the entry with
		// the needed prefix. If any error occurs or the parent does not exist
		// or is empty, assume the index is 1. Supports up to 2 digits.
		prefix := p[:5]
		index := 1
		parent := filepath.Join(parts[:i]...)
		pattern := filepath.Join(parent, prefix+"*")
		entries, _ := filepath.Glob(pattern)
		if len(entries) < 9 {
			prefix = p[:6]
		}
		for j := range entries {
			if filepath.Base(entries[j]) == p {
				index = j + 1
				break
			}
		}

		parts[i] = fmt.Sprintf("%s~%d", prefix, index)
	}

	return filepath.Join(parts...)
}

// checkRequirementsWindows checks for the requirements to a Windows build and
// sets the environment variables as necessary.
func checkRequirementsWindows() error {
	// Ensure the user is in an MSYS2 MINGW64 terminal, otherwise go build
	// won't use the correct C compiler.
	if os.Getenv("MSYSTEM_CHOST") != "x86_64-w64-mingw32" {
		return errNotOnMsys
	}

	// Ensure CUDA Toolkit is installed and fetch its path to add it to
	// PATH, CGO_CFLAGS and CGO_LDFLAGS.
	cudaPath := os.Getenv("CUDA_PATH")
	if cudaPath == "" {
		return errCudaPathEnvUnset
	}

	// Copy cudart_xx.dll to the current dir. The xx part depends on the
	// installed version of the toolkit. This file is necessary for gominer
	// to run and is not generally installed to the global system32 dir
	// on Windows.
	entries, err := filepath.Glob(cudaPath + `\bin\cudart64_*.dll`)
	if err != nil {
		return fmt.Errorf("%w due to %v", errNoCudartDll, err)
	}
	if len(entries) == 0 {
		return errNoCudartDll
	}
	if err := runCmd("cp", entries[0], "."); err != nil {
		return fmt.Errorf("unable to copy cudart64_xx.dll to current dir: %v", err)
	}

	// Ensure MSVC is installed and that there's an appropriate cl.exe. This
	// attempts to find an install path for a 2017+ MSVC by querying
	// vswhere.exe (which is installed in a well-known path), then reading
	// the contents of a file that should have the default version of the
	// desktop C++ compiler component, and then deriving the final path to
	// the x64 version of such compiler.
	//
	// This was largely derived from the following vswhere guide:
	// https://github.com/microsoft/vswhere/wiki/Find-VC
	vswherePath := `C:\Program Files (x86)\Microsoft Visual Studio\Installer\vswhere.exe`
	msvcRootPath, err := runWithOutput(vswherePath, "-latest", "-property", "installationPath")
	if err != nil {
		return fmt.Errorf("%w due to %v", errVswhereFailed, err)
	} else if msvcRootPath = strings.TrimSpace(msvcRootPath); msvcRootPath == "" {
		return errVswhereFailed
	}
	auxBuildPath := msvcRootPath + `\VC\Auxiliary\Build\Microsoft.VCToolsVersion.default.txt`
	vcppVersionBytes, err := os.ReadFile(auxBuildPath)
	if err != nil {
		return fmt.Errorf("%w due to %v", errNoMsDevToolsDir, err)
	}
	if vcppVersionBytes = bytes.TrimSpace(vcppVersionBytes); len(vcppVersionBytes) == 0 {
		return fmt.Errorf("%w due to no version in Microsoft.VCToolsVersion.default.txt file",
			errNoMsDevToolsDir)
	}
	clPath := msvcRootPath + `\VC\Tools\MSVC\` + string(vcppVersionBytes) + `\bin\Hostx64\x64`

	// Populate the env vars that will be needed to build blake3.dll and
	// gominer.exe.
	cgoCflags := os.Getenv("CGO_CFLAGS")
	cgoCflags = fmt.Sprintf("%s -I%s", cgoCflags, winPathToShort(cudaPath)+`\include`)
	os.Setenv("CGO_CFLAGS", cgoCflags)

	cgoLdflags := os.Getenv("CGO_LDFLAGS")
	cgoLdflags = fmt.Sprintf("%s -L%s", cgoLdflags, winPathToShort(cudaPath)+`\lib\x64`)
	os.Setenv("CGO_LDFLAGS", cgoLdflags)

	path := os.Getenv("PATH")
	path = fmt.Sprintf("%s;%s", cudaPath+`\bin`, path)
	path = fmt.Sprintf("%s;%s", clPath, path)
	os.Setenv("PATH", path)

	return nil
}

// checkRequirementsDefault checks the requirements for any other OS/architecture
// combinations.
func checkRequirementsDefault() error {
	// Create the obj/ dir.
	return os.MkdirAll("obj", 0o755)
}

// determineGPUArch determines the `--gpu-architecture` argument to use with
// nvcc, based on the currently installed CUDA toolkit version.
//
// The GOMINER_CUDA_GPU_ARCH environment variable may be used to override
// autodetection.
func determineGPUArch() (string, error) {
	if envVal := os.Getenv("GOMINER_CUDA_GPU_ARCH"); envVal != "" {
		return envVal, nil
	}

	// defaultGPUArch is a sensible default architecture, which already
	// includes the intrinsic used in the ROTR() macro of the CUDA kernel.
	const defaultGPUArch = "compute_50"

	// Run `nvcc --version` and base the decision on the toolkit version.
	output, err := exec.Command("nvcc", "--version").Output()
	if err != nil {
		return "", fmt.Errorf("unable to run 'nvcc --version': %v", err)
	}

	re, err := regexp.Compile("(?mi:^Cuda compilation tools, release ([\\d]+)\\.([\\d]+))")
	if err != nil {
		return "", err
	}
	matches := re.FindStringSubmatch(string(output))
	if len(matches) != 3 {
		// nvcc --version failed to output the expected version string,
		// so downgrade to the default arch.
		return defaultGPUArch, nil
	}

	major, err := strconv.Atoi(matches[1])
	if err != nil {
		return "", err
	}
	minor, err := strconv.Atoi(matches[2])
	if err != nil {
		return "", err
	}
	if major > 11 || (major == 11 && minor >= 5) {
		// Toolkit versions >= 11.5 have the "all" selector for
		// --gpu-architecture, so use that as it's the broadest arch
		// selector.
		return "all", nil
	}

	// Otherwise, use the default.
	return defaultGPUArch, nil
}

// buildBlake3Windows builds the blake3.dll library with a compiled version of
// the Blake3 kernel.
func buildBlake3Windows() error {
	gpuArch, err := determineGPUArch()
	if err != nil {
		return err
	}
	return runCmd("nvcc", "--shared", "--optimize=3",
		"--gpu-architecture="+gpuArch,
		"--compiler-options=-GS-,-MD",
		"-I.", "blake3.cu", "-o", "blake3.dll")
}

// buildBlake3Default builds the blake3.a library with a compiled version of the
// Blake3 kernel.
func buildBlake3Default() error {
	gpuArch, err := determineGPUArch()
	if err != nil {
		return err
	}
	return runCmd("nvcc", "--lib", "--optimize=3",
		"--gpu-architecture="+gpuArch,
		"-I.", "blake3.cu", "-o", "obj/blake3.a")
}

// buildGominer builds the necessary platform-specific dependencies and then
// builds the final gominer binary.
func buildGominer() error {
	// Determine the platform-specific functions.
	checkRequirements := checkRequirementsDefault
	buildBlake3 := buildBlake3Default
	if runtime.GOOS == "windows" {
		checkRequirements = checkRequirementsWindows
		buildBlake3 = buildBlake3Windows
	}

	if err := checkRequirements(); err != nil {
		return err
	}
	if err := buildBlake3(); err != nil {
		return err
	}

	// Build final gominer binary.
	if err := runCmd("go", "build", "-tags", "cuda", "."); err != nil {
		return err
	}

	return nil
}

func main() {
	err := buildGominer()
	if err == nil {
		return
	}

	p := func(format string, args ...interface{}) {
		fmt.Fprintf(os.Stderr, format, args...)
		fmt.Fprintf(os.Stderr, "\n")
	}
	p("Error generating blake3 CUDA library: %v", err.Error())

	// Offer some advice to the user if we can.
	switch {
	case errors.Is(err, errNotOnMsys):
		p("")
		p("On Windows, this needs to be run from an MSYS2 MINGW64 terminal.")
		p("Install MSYS2 (if not already installed), then look for the 'MSYS2 MINGW64' " +
			"entry in the start menu to open the appropriate terminal, then try again")

	case errors.Is(err, errNoCudartDll):
		p("")
		p("This means a suitable Nvidia CUDA Toolkit is not installed, an incompatible " +
			"version is installed or the installation is corrupt.")

	case errors.Is(err, errVswhereFailed):
		p("")
		p("This usually means Microsoft Visual Studio is not installed or a version " +
			"older than MSVC 2017 is installed. Install a recent version of MSVC, available " +
			"at https://visualstudio.microsoft.com/vs/community/")

	case errors.Is(err, errNoMsDevToolsDir):
		p("")
		p("This usually means that the \"Desktop Development with C++\" component " +
			"of MSVC is not installed. Re-run the MSVC installer and add this " +
			"component, then try again.")
	}
	os.Exit(1)
}
