package runtime

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"awg3routerosgateway/internal/awg3profile"
	"awg3routerosgateway/internal/config"
)

type OfficialParserPreflightOptions struct {
	ValidatorBinary string
	Timeout         time.Duration
	TempDir         string
	CommandFactory  CommandFactory
}

type OfficialParserPreflightAdapter struct {
	opts OfficialParserPreflightOptions
}

func NewOfficialParserPreflightAdapter(opts OfficialParserPreflightOptions) (*OfficialParserPreflightAdapter, error) {
	if opts.ValidatorBinary == "" {
		opts.ValidatorBinary = "/usr/local/bin/awg3-parser-validate"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 10 * time.Second
	}
	if opts.CommandFactory == nil {
		opts.CommandFactory = osCommandFactory{}
	}
	return &OfficialParserPreflightAdapter{opts: opts}, nil
}

func (a *OfficialParserPreflightAdapter) Check(ctx context.Context, pair config.Pair) error {
	if err := pair.Validate(); err != nil {
		return err
	}
	rendered, err := awg3profile.RenderSetconf(pair)
	if err != nil {
		return err
	}
	tmpPath, cleanup, err := a.writeTemp(rendered)
	if err != nil {
		return err
	}
	defer cleanup()
	stepCtx, cancel := context.WithTimeout(ctx, a.opts.Timeout)
	defer cancel()
	cmd := a.opts.CommandFactory.CommandContext(stepCtx, a.opts.ValidatorBinary, tmpPath)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("official parser validation failed: %w", err)
	}
	return nil
}

func (a *OfficialParserPreflightAdapter) writeTemp(rendered string) (string, func(), error) {
	dir := a.opts.TempDir
	tmp, err := os.CreateTemp(dir, "awg3-official-*.conf")
	if err != nil {
		return "", func() {}, err
	}
	if err := tmp.Chmod(0o600); err != nil {
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", func() {}, err
	}
	if _, err := tmp.WriteString(rendered); err != nil {
		name := tmp.Name()
		_ = tmp.Close()
		_ = os.Remove(name)
		return "", func() {}, err
	}
	if err := tmp.Close(); err != nil {
		name := tmp.Name()
		_ = os.Remove(name)
		return "", func() {}, err
	}
	return tmp.Name(), func() { _ = os.Remove(tmp.Name()) }, nil
}

func (a *OfficialParserPreflightAdapter) String() string {
	return fmt.Sprintf("official parser validator %q", a.opts.ValidatorBinary)
}

var _ ProfilePreflight = (*OfficialParserPreflightAdapter)(nil)
