package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"awg3routerosgateway/internal/artifacts"
	"awg3routerosgateway/internal/config"
	"awg3routerosgateway/internal/runtime"
	"awg3routerosgateway/internal/supervisor"
)

var buildCommit = "unknown"

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("gateway", flag.ContinueOnError)
	configPath := fs.String("config", "/config/awg3.json", "path to awg3 config")
	secretsPath := fs.String("secrets", "/config/secrets.json", "path to secrets config")
	statusAddr := fs.String("status-listen", "127.0.0.1:8080", "status/control listen address")
	configAddr := fs.String("config-listen", "127.0.0.1:8081", "on-demand config listen address")
	mode := fs.String("mode", "run", "run|validate|apply|status")
	sessionTTL := fs.Duration("session-ttl", 10*time.Minute, "legacy default for on-demand UI ttl")
	sessionIdleTTL := fs.Duration("ui-idle-ttl", 10*time.Minute, "on-demand UI idle ttl")
	sessionMaxTTL := fs.Duration("ui-max-ttl", time.Hour, "on-demand UI absolute ttl")
	artifactManifest := fs.String("artifact-manifest", "/etc/awg3/runtime-artifacts.json", "pinned runtime artifact manifest")
	parserTimeout := fs.Duration("parser-timeout", 5*time.Second, "official parser validation timeout")
	parserValidatorBinary := fs.String("parser-validator-binary", "/usr/local/bin/awg3-parser-validate", "pinned official parser validator binary")
	runtimeBinary := fs.String("runtime-binary", "/usr/local/bin/amneziawg-go", "pinned amneziawg-go binary")
	awgBinary := fs.String("awg-binary", "/usr/local/bin/awg", "pinned awg binary")
	ipBinary := fs.String("ip-binary", "/usr/sbin/ip", "pinned ip binary")
	sysctlBinary := fs.String("sysctl-binary", "/usr/sbin/sysctl", "pinned sysctl binary")
	runtimeInterfaceDebug := fs.String("runtime-interface-debug", "", "debug/test override for interface name; must match canonical config")
	candidateConfig := fs.String("candidate-config", "", "candidate awg3 config file")
	candidateSecrets := fs.String("candidate-secrets", "", "candidate secrets file")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *mode == "run" || *mode == "apply" {
		executable, err := os.Executable()
		if err != nil {
			return err
		}
		executable = filepath.Clean(executable)
		if err := artifacts.VerifyAll(*artifactManifest, executable, *runtimeBinary, *awgBinary, *parserValidatorBinary, *ipBinary, *sysctlBinary); err != nil {
			return fmt.Errorf("artifact_verification_failed: %w", err)
		}
		if err := verifyGatewayProvenance(*artifactManifest, executable, buildCommit); err != nil {
			return err
		}
	}

	canonicalPreflight := runtime.NewCanonicalProfilePreflightAdapter()
	officialPreflight, err := runtime.NewOfficialParserPreflightAdapter(runtime.OfficialParserPreflightOptions{
		ValidatorBinary: *parserValidatorBinary,
		Timeout:         *parserTimeout,
	})
	if err != nil {
		return err
	}
	execAdapter, err := runtime.NewExecAdapter(runtime.ExecOptions{
		Binary:                 *runtimeBinary,
		ToolsBinary:            *awgBinary,
		IPBinary:               *ipBinary,
		SysctlBinary:           *sysctlBinary,
		ParserTimeout:          *parserTimeout,
		Preflight:              canonicalPreflight,
		DebugInterfaceOverride: *runtimeInterfaceDebug,
		Stdout:                 io.Discard,
		Stderr:                 io.Discard,
		StopTimeout:            5 * time.Second,
		KillTimeout:            2 * time.Second,
		EndpointExclusion:      runtime.NewRouteEndpointExclusionAdapter(),
	})
	if err != nil {
		return err
	}

	var runtimeController *runtime.Controller
	runtimeController, err = runtime.New(
		func(ctx context.Context, pair config.Pair) (runtime.Process, error) {
			return execAdapter.Start(ctx, pair)
		},
		func(ctx context.Context, pair config.Pair, proc runtime.Process) error {
			return officialPreflight.Check(ctx, pair)
		},
		execAdapter,
		func(ctx context.Context, proc runtime.Process) error {
			handle, ok := proc.(runtime.ProcessHandle)
			if !ok {
				return proc.Kill()
			}
			return execAdapter.Stop(ctx, handle)
		},
	)
	if err != nil {
		return err
	}

	sup, err := supervisor.New(supervisor.Options{
		ConfigPath:     *configPath,
		SecretsPath:    *secretsPath,
		StatusAddr:     *statusAddr,
		ConfigAddr:     *configAddr,
		Mode:           *mode,
		SessionTTL:     *sessionTTL,
		SessionIdleTTL: *sessionIdleTTL,
		SessionMaxTTL:  *sessionMaxTTL,
		Runtime:        runtimeController,
		Clock:          time.Now,
		ConfigLoader:   config.LoadPair,
		ConfigSaver:    config.SavePair,
	})
	if err != nil {
		return err
	}

	if *candidateConfig != "" || *candidateSecrets != "" {
		if *candidateConfig == "" || *candidateSecrets == "" {
			return fmt.Errorf("candidate-config and candidate-secrets must be set together")
		}
		pair, err := config.LoadPair(*candidateConfig, *candidateSecrets)
		if err != nil {
			return err
		}
		if err := sup.SetCandidate(pair); err != nil {
			return err
		}
	}

	switch *mode {
	case "validate":
		return sup.Validate(ctx)
	case "apply":
		return sup.Apply(ctx)
	case "status":
		return sup.WriteStatus(ctx, os.Stdout)
	case "run":
		return sup.Run(ctx)
	default:
		return fmt.Errorf("unsupported mode %q", *mode)
	}
}

func verifyGatewayProvenance(manifestPath, executablePath, embeddedCommit string) error {
	if embeddedCommit == "" || embeddedCommit == "unknown" {
		return fmt.Errorf("gateway_build_commit_missing")
	}
	manifest, err := artifacts.Load(manifestPath)
	if err != nil {
		return err
	}
	gateway, ok := manifest.ArtifactByPath(filepath.Clean(executablePath))
	if !ok {
		return fmt.Errorf("gateway artifact missing for %s", executablePath)
	}
	if gateway.SourceCommit != embeddedCommit {
		return fmt.Errorf("gateway_build_commit_mismatch: manifest=%s binary=%s", gateway.SourceCommit, embeddedCommit)
	}
	return nil
}
