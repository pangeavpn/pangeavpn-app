// naive-cc-wrapper adapts a clang-cl build (copied to clang.exe to get
// GCC-style flags) for use as Go's CGO CC on Windows:
//
//   - strips -mthreads, which Go's cgo unconditionally injects for
//     GOOS=windows builds and which this MSVC-target clang rejects.
//   - adds --target=<NAIVE_CC_TARGET> and -fuse-ld=lld to link with lld.
//
// pangea_naive.lib is native COFF (built with use_thin_lto=false), so any
// recent clang links it — either the pinned Chromium toolchain from a local
// checkout or a stock system LLVM. Configured entirely via environment
// variables so one compiled binary works for either Windows target arch:
//
//	NAIVE_CC_REAL_CC     path to a clang.exe (pinned toolchain or system LLVM)
//	NAIVE_CC_TARGET      e.g. aarch64-pc-windows-msvc or x86_64-pc-windows-msvc
package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	realCC := os.Getenv("NAIVE_CC_REAL_CC")
	target := os.Getenv("NAIVE_CC_TARGET")
	if realCC == "" || target == "" {
		fmt.Fprintln(os.Stderr, "naive-cc-wrapper: NAIVE_CC_REAL_CC and NAIVE_CC_TARGET must be set")
		os.Exit(1)
	}

	args := make([]string, 0, len(os.Args)+2)
	args = append(args, "--target="+target, "-fuse-ld=lld")
	for _, a := range os.Args[1:] {
		if a == "-mthreads" {
			continue
		}
		args = append(args, a)
	}

	cmd := exec.Command(realCC, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
