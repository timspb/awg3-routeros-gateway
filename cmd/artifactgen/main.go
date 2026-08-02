package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"awg3routerosgateway/internal/artifacts"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("artifactgen", flag.ContinueOnError)
	output := fs.String("output", "", "output manifest path")
	platformOS := fs.String("os", "", "target os")
	platformArch := fs.String("arch", "", "target arch")
	platformVariant := fs.String("variant", "", "target variant")
	sourceRepoSelf := fs.String("self-repo", "file://awg3routerosgateway", "self source repo")
	sourceCommitSelf := fs.String("self-commit", "", "self source commit")
	sourceRepoGo := fs.String("go-repo", "", "amneziawg-go source repo")
	sourceCommitGo := fs.String("go-commit", "", "amneziawg-go source commit")
	sourceRepoTools := fs.String("tools-repo", "", "amneziawg-tools source repo")
	sourceCommitTools := fs.String("tools-commit", "", "amneziawg-tools source commit")
	sourceRepoPkg := fs.String("pkg-repo", "debian:bookworm-slim", "package source repo")
	sourceCommitPkg := fs.String("pkg-commit", "", "package source commit")
	buildRecipe := fs.String("build-recipe", "", "build recipe digest")
	toolchainDigest := fs.String("toolchain-digest", "", "toolchain digest")
	gatewayPath := fs.String("gateway", "/gateway", "gateway manifest path")
	gatewaySource := fs.String("gateway-source", "/gateway", "gateway source path")
	runtimePath := fs.String("runtime", "/usr/local/bin/amneziawg-go", "runtime manifest path")
	runtimeSource := fs.String("runtime-source", "/usr/local/bin/amneziawg-go", "runtime source path")
	toolsPath := fs.String("tools", "/usr/local/bin/awg", "tools manifest path")
	toolsSource := fs.String("tools-source", "/usr/local/bin/awg", "tools source path")
	parserPath := fs.String("parser", "/usr/local/bin/awg3-parser-validate", "parser manifest path")
	parserSource := fs.String("parser-source", "/usr/local/bin/awg3-parser-validate", "parser source path")
	ipPath := fs.String("ip", "/usr/sbin/ip", "ip manifest path")
	ipSource := fs.String("ip-source", "/usr/sbin/ip", "ip source path")
	sysctlPath := fs.String("sysctl", "/usr/sbin/sysctl", "sysctl manifest path")
	sysctlSource := fs.String("sysctl-source", "/usr/sbin/sysctl", "sysctl source path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *output == "" {
		return fmt.Errorf("output is required")
	}
	if *platformOS == "" || *platformArch == "" {
		return fmt.Errorf("os and arch are required")
	}
	if *sourceCommitSelf == "" {
		return fmt.Errorf("self commit is required")
	}
	if *sourceCommitGo == "" || *sourceCommitTools == "" || *sourceCommitPkg == "" {
		return fmt.Errorf("all source commits are required")
	}
	if *buildRecipe == "" || *toolchainDigest == "" {
		return fmt.Errorf("build-recipe and toolchain-digest are required")
	}

	entries := []artifacts.Artifact{
		{
			Name:            "gateway",
			Path:            filepath.Clean(*gatewayPath),
			SHA256:          mustSHA256(*gatewaySource),
			OS:              *platformOS,
			Arch:            *platformArch,
			Variant:         *platformVariant,
			SourceRepo:      *sourceRepoSelf,
			SourceCommit:    *sourceCommitSelf,
			BuildRecipe:     *buildRecipe,
			ToolchainDigest: *toolchainDigest,
		},
		{
			Name:            "amneziawg-go",
			Path:            filepath.Clean(*runtimePath),
			SHA256:          mustSHA256(*runtimeSource),
			OS:              *platformOS,
			Arch:            *platformArch,
			Variant:         *platformVariant,
			SourceRepo:      *sourceRepoGo,
			SourceCommit:    *sourceCommitGo,
			BuildRecipe:     *buildRecipe,
			ToolchainDigest: *toolchainDigest,
		},
		{
			Name:            "awg",
			Path:            filepath.Clean(*toolsPath),
			SHA256:          mustSHA256(*toolsSource),
			OS:              *platformOS,
			Arch:            *platformArch,
			Variant:         *platformVariant,
			SourceRepo:      *sourceRepoTools,
			SourceCommit:    *sourceCommitTools,
			BuildRecipe:     *buildRecipe,
			ToolchainDigest: *toolchainDigest,
		},
		{
			Name:            "awg3-parser-validate",
			Path:            filepath.Clean(*parserPath),
			SHA256:          mustSHA256(*parserSource),
			OS:              *platformOS,
			Arch:            *platformArch,
			Variant:         *platformVariant,
			SourceRepo:      *sourceRepoTools,
			SourceCommit:    *sourceCommitTools,
			BuildRecipe:     *buildRecipe,
			ToolchainDigest: *toolchainDigest,
		},
		{
			Name:            "ip",
			Path:            filepath.Clean(*ipPath),
			SHA256:          mustSHA256(*ipSource),
			OS:              *platformOS,
			Arch:            *platformArch,
			Variant:         *platformVariant,
			SourceRepo:      *sourceRepoPkg,
			SourceCommit:    *sourceCommitPkg,
			BuildRecipe:     *buildRecipe,
			ToolchainDigest: *toolchainDigest,
		},
		{
			Name:            "sysctl",
			Path:            filepath.Clean(*sysctlPath),
			SHA256:          mustSHA256(*sysctlSource),
			OS:              *platformOS,
			Arch:            *platformArch,
			Variant:         *platformVariant,
			SourceRepo:      *sourceRepoPkg,
			SourceCommit:    *sourceCommitPkg,
			BuildRecipe:     *buildRecipe,
			ToolchainDigest: *toolchainDigest,
		},
	}

	manifest := artifacts.Manifest{Artifacts: entries}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*output, payload, 0o644)
}

func mustSHA256(path string) string {
	sum, err := artifacts.FileSHA256(path)
	if err != nil {
		panic(err)
	}
	return sum
}
