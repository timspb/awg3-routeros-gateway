package runtime

import (
	"context"
	"errors"
	"fmt"

	"awg3routerosgateway/internal/awg3profile"
	"awg3routerosgateway/internal/config"
)

type CanonicalProfilePreflightAdapter struct {
	Render func(config.Pair) (string, error)
	Parse  func(string) (config.Pair, error)
}

func NewCanonicalProfilePreflightAdapter() *CanonicalProfilePreflightAdapter {
	return &CanonicalProfilePreflightAdapter{
		Render: awg3profile.Render,
		Parse:  awg3profile.Parse,
	}
}

func (a *CanonicalProfilePreflightAdapter) Check(ctx context.Context, pair config.Pair) error {
	_ = ctx
	return a.preflight(pair)
}

func (a *CanonicalProfilePreflightAdapter) preflight(pair config.Pair) error {
	if err := pair.Validate(); err != nil {
		return err
	}
	if a.Render == nil || a.Parse == nil {
		return errors.New("preflight adapter is incomplete")
	}
	rendered, err := a.Render(pair)
	if err != nil {
		return err
	}
	parsed, err := a.Parse(rendered)
	if err != nil {
		return err
	}
	if !equalSafePair(pair, parsed) {
		return fmt.Errorf("rendered config round-trip mismatch")
	}
	return nil
}

func equalSafePair(a, b config.Pair) bool {
	if !equalSafeConfig(a.Config, b.Config) {
		return false
	}
	if a.Secrets.Generation != b.Secrets.Generation {
		return false
	}
	if a.Secrets.PrivateKey != b.Secrets.PrivateKey {
		return false
	}
	if a.Secrets.PeerPublicKey != b.Secrets.PeerPublicKey {
		return false
	}
	if a.Secrets.PresharedKey != b.Secrets.PresharedKey {
		return false
	}
	if a.Secrets.HeaderProtectionKey != b.Secrets.HeaderProtectionKey {
		return false
	}
	return true
}

func equalSafeConfig(a, b config.Config) bool {
	return a.Version == b.Version &&
		a.Generation == b.Generation &&
		a.InterfaceName == b.InterfaceName &&
		a.ListenPort == b.ListenPort &&
		a.MTU == b.MTU &&
		a.TunnelAddress == b.TunnelAddress &&
		a.Gateway == b.Gateway &&
		a.VethAddress == b.VethAddress &&
		slicesEqual(a.AllowedIPs, b.AllowedIPs) &&
		a.Endpoint == b.Endpoint &&
		a.Jc == b.Jc &&
		a.Jmin == b.Jmin &&
		a.Jmax == b.Jmax &&
		a.S1 == b.S1 &&
		a.S2 == b.S2 &&
		a.S3 == b.S3 &&
		a.S4 == b.S4 &&
		a.H1 == b.H1 &&
		a.H2 == b.H2 &&
		a.H3 == b.H3 &&
		a.H4 == b.H4 &&
		a.I1 == b.I1 &&
		a.I2 == b.I2 &&
		a.I3 == b.I3 &&
		a.I4 == b.I4 &&
		a.I5 == b.I5 &&
		a.ContentPaddingAdditionMin == b.ContentPaddingAdditionMin &&
		a.ContentPaddingAdditionMax == b.ContentPaddingAdditionMax &&
		a.RekeyAfterTimeMin == b.RekeyAfterTimeMin &&
		a.RekeyAfterTimeMax == b.RekeyAfterTimeMax &&
		a.RekeyTimeoutMin == b.RekeyTimeoutMin &&
		a.RekeyTimeoutMax == b.RekeyTimeoutMax &&
		a.RejectAfterTimeMin == b.RejectAfterTimeMin &&
		a.RejectAfterTimeMax == b.RejectAfterTimeMax &&
		a.KeepaliveTimeoutMin == b.KeepaliveTimeoutMin &&
		a.KeepaliveTimeoutMax == b.KeepaliveTimeoutMax &&
		a.MaxHandshakeAttemptsMin == b.MaxHandshakeAttemptsMin &&
		a.MaxHandshakeAttemptsMax == b.MaxHandshakeAttemptsMax &&
		a.PersistentKeepaliveMin == b.PersistentKeepaliveMin &&
		a.PersistentKeepaliveMax == b.PersistentKeepaliveMax &&
		a.HealthAddress == b.HealthAddress &&
		a.UIMode == b.UIMode
}

func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
