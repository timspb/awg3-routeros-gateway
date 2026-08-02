package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
)

type Pair struct {
	Config  Config  `json:"config"`
	Secrets Secrets `json:"secrets"`
}

type Config struct {
	Version    string `json:"version"`
	Generation string `json:"generation"`

	InterfaceName string `json:"interface_name"`
	ListenPort    int    `json:"listen_port"`
	MTU           int    `json:"mtu"`

	TunnelAddress string   `json:"tunnel_address"`
	Gateway       string   `json:"gateway"`
	VethAddress   string   `json:"veth_address"`
	AllowedIPs    []string `json:"allowed_ips"`
	Endpoint      string   `json:"endpoint"`

	Jc   int `json:"jc"`
	Jmin int `json:"jmin"`
	Jmax int `json:"jmax"`

	S1 int `json:"s1"`
	S2 int `json:"s2"`
	S3 int `json:"s3"`
	S4 int `json:"s4"`

	H1 int `json:"h1"`
	H2 int `json:"h2"`
	H3 int `json:"h3"`
	H4 int `json:"h4"`

	I1 string `json:"i1"`
	I2 string `json:"i2"`
	I3 string `json:"i3"`
	I4 string `json:"i4"`
	I5 string `json:"i5"`

	ContentPaddingAdditionMin int `json:"content_padding_addition_min"`
	ContentPaddingAdditionMax int `json:"content_padding_addition_max"`

	RekeyAfterTimeMin int `json:"rekey_after_time_min"`
	RekeyAfterTimeMax int `json:"rekey_after_time_max"`

	RekeyTimeoutMin int `json:"rekey_timeout_min"`
	RekeyTimeoutMax int `json:"rekey_timeout_max"`

	RejectAfterTimeMin int `json:"reject_after_time_min"`
	RejectAfterTimeMax int `json:"reject_after_time_max"`

	KeepaliveTimeoutMin int `json:"keepalive_timeout_min"`
	KeepaliveTimeoutMax int `json:"keepalive_timeout_max"`

	MaxHandshakeAttemptsMin int `json:"max_handshake_attempts_min"`
	MaxHandshakeAttemptsMax int `json:"max_handshake_attempts_max"`

	PersistentKeepaliveMin int `json:"persistent_keepalive_min"`
	PersistentKeepaliveMax int `json:"persistent_keepalive_max"`

	HealthAddress string `json:"health_address"`
	UIMode        string `json:"ui_mode"`

	OuterPath *OuterPath `json:"outer_path,omitempty"`
}

type OuterPath struct {
	EndpointExclusion *EndpointExclusionConfig `json:"endpoint_exclusion,omitempty"`
}

type EndpointExclusionConfig struct {
	Owner                    string `json:"owner"`
	RoutingTable             string `json:"routing_table"`
	OuterGateway             string `json:"outer_gateway"`
	OuterEgressInterface     string `json:"outer_egress_interface"`
	SourceAddress            string `json:"source_address,omitempty"`
	EndpointResolutionPolicy string `json:"endpoint_resolution_policy"`
	DynamicRenewal           bool   `json:"dynamic_renewal"`
	RefreshOwner             string `json:"refresh_owner"`
}

type Secrets struct {
	Version             string `json:"version"`
	Generation          string `json:"generation"`
	PrivateKey          string `json:"private_key"`
	PeerPublicKey       string `json:"peer_public_key"`
	PresharedKey        string `json:"preshared_key"`
	HeaderProtectionKey string `json:"header_protection_key"`
	ControlToken        string `json:"control_token"`
}

type EffectiveConfig struct {
	Version    string `json:"version"`
	Generation string `json:"generation"`

	InterfaceName string   `json:"interface_name"`
	ListenPort    int      `json:"listen_port"`
	MTU           int      `json:"mtu"`
	TunnelAddress string   `json:"tunnel_address"`
	Gateway       string   `json:"gateway"`
	VethAddress   string   `json:"veth_address"`
	AllowedIPs    []string `json:"allowed_ips"`
	Endpoint      string   `json:"endpoint"`

	Jc   int    `json:"jc"`
	Jmin int    `json:"jmin"`
	Jmax int    `json:"jmax"`
	S1   int    `json:"s1"`
	S2   int    `json:"s2"`
	S3   int    `json:"s3"`
	S4   int    `json:"s4"`
	H1   int    `json:"h1"`
	H2   int    `json:"h2"`
	H3   int    `json:"h3"`
	H4   int    `json:"h4"`
	I1   string `json:"i1"`
	I2   string `json:"i2"`
	I3   string `json:"i3"`
	I4   string `json:"i4"`
	I5   string `json:"i5"`

	ContentPaddingAdditionMin int `json:"content_padding_addition_min"`
	ContentPaddingAdditionMax int `json:"content_padding_addition_max"`
	RekeyAfterTimeMin         int `json:"rekey_after_time_min"`
	RekeyAfterTimeMax         int `json:"rekey_after_time_max"`
	RekeyTimeoutMin           int `json:"rekey_timeout_min"`
	RekeyTimeoutMax           int `json:"rekey_timeout_max"`
	RejectAfterTimeMin        int `json:"reject_after_time_min"`
	RejectAfterTimeMax        int `json:"reject_after_time_max"`
	KeepaliveTimeoutMin       int `json:"keepalive_timeout_min"`
	KeepaliveTimeoutMax       int `json:"keepalive_timeout_max"`
	MaxHandshakeAttemptsMin   int `json:"max_handshake_attempts_min"`
	MaxHandshakeAttemptsMax   int `json:"max_handshake_attempts_max"`
	PersistentKeepaliveMin    int `json:"persistent_keepalive_min"`
	PersistentKeepaliveMax    int `json:"persistent_keepalive_max"`

	HeaderProtectionEnabled bool              `json:"header_protection_enabled"`
	HealthAddress           string            `json:"health_address"`
	UIMode                  string            `json:"ui_mode"`
	SecretFingerprints      map[string]string `json:"secret_fingerprints"`
}

func (p Pair) Validate() error {
	if err := p.Config.Validate(); err != nil {
		return err
	}
	if err := p.Secrets.Validate(); err != nil {
		return err
	}
	if p.Config.Generation == "" || p.Secrets.Generation == "" {
		return errors.New("generation is required in both config and secrets")
	}
	if p.Config.Generation != p.Secrets.Generation {
		return errors.New("generation mismatch between config and secrets")
	}
	return nil
}

func (c Config) Validate() error {
	var errs []string
	if c.Version == "" {
		errs = append(errs, "version is required")
	}
	if c.InterfaceName == "" {
		errs = append(errs, "interface_name is required")
	}
	if c.ListenPort <= 0 || c.ListenPort > 65535 {
		errs = append(errs, "listen_port must be 1..65535")
	}
	if c.MTU < 576 || c.MTU > 9000 {
		errs = append(errs, "mtu must be 576..9000")
	}
	if _, _, err := net.ParseCIDR(c.TunnelAddress); err != nil {
		errs = append(errs, "tunnel_address must be a CIDR")
	}
	if c.Gateway == "" {
		errs = append(errs, "gateway is required")
	}
	if c.VethAddress == "" {
		errs = append(errs, "veth_address is required")
	}
	if len(c.AllowedIPs) == 0 {
		errs = append(errs, "allowed_ips must not be empty")
	}
	for _, ip := range c.AllowedIPs {
		if _, _, err := net.ParseCIDR(ip); err != nil {
			errs = append(errs, fmt.Sprintf("allowed_ips entry %q must be a CIDR", ip))
		}
	}
	if c.Endpoint == "" {
		errs = append(errs, "endpoint is required")
	}
	if c.Jc < 0 {
		errs = append(errs, "jc must be non-negative")
	}
	checkRange := func(name string, min, max, hardMin, hardMax int) {
		if min < hardMin || max < hardMin || max < min || max > hardMax {
			errs = append(errs, fmt.Sprintf("%s must be a valid range", name))
		}
	}
	checkRange("j range", c.Jmin, c.Jmax, 0, 1000)
	checkRange("content_padding_addition", c.ContentPaddingAdditionMin, c.ContentPaddingAdditionMax, 0, 32)
	checkRange("rekey_after_time", c.RekeyAfterTimeMin, c.RekeyAfterTimeMax, 1, 600)
	checkRange("rekey_timeout", c.RekeyTimeoutMin, c.RekeyTimeoutMax, 1, 60)
	checkRange("reject_after_time", c.RejectAfterTimeMin, c.RejectAfterTimeMax, 1, 900)
	checkRange("keepalive_timeout", c.KeepaliveTimeoutMin, c.KeepaliveTimeoutMax, 1, 120)
	checkRange("max_handshake_attempts", c.MaxHandshakeAttemptsMin, c.MaxHandshakeAttemptsMax, 1, 64)
	checkRange("persistent_keepalive", c.PersistentKeepaliveMin, c.PersistentKeepaliveMax, 0, 600)
	if c.S1 < 12 || c.S2 < 12 || c.S3 < 12 || c.S4 < 12 {
		errs = append(errs, "s1-s4 must each be >= 12")
	}
	if c.H1 < 0 || c.H2 < 0 || c.H3 < 0 || c.H4 < 0 {
		errs = append(errs, "h1-h4 must be non-negative")
	}
	for i, v := range []string{c.I1, c.I2, c.I3, c.I4, c.I5} {
		if strings.TrimSpace(v) == "" {
			errs = append(errs, fmt.Sprintf("i%d is required", i+1))
		}
	}
	if c.RekeyAfterTimeMax >= c.RejectAfterTimeMin && c.RejectAfterTimeMin > 0 {
		errs = append(errs, "rekey_after_time must stay below reject_after_time")
	}
	if c.RekeyTimeoutMax >= c.RekeyAfterTimeMin && c.RekeyAfterTimeMin > 0 {
		errs = append(errs, "rekey_timeout must stay below rekey_after_time")
	}
	if c.HealthAddress == "" {
		errs = append(errs, "health_address is required")
	}
	if c.UIMode != "always" && c.UIMode != "on_demand" {
		errs = append(errs, "ui_mode must be always or on_demand")
	}
	if c.OuterPath != nil {
		if c.OuterPath.EndpointExclusion == nil {
			errs = append(errs, "outer_path.endpoint_exclusion is required when outer_path is set")
		} else if pathErr := c.OuterPath.EndpointExclusion.Validate(); pathErr != nil {
			errs = append(errs, pathErr.Error())
		}
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (e EndpointExclusionConfig) Validate() error {
	var errs []string
	switch e.Owner {
	case "container", "routeros":
	default:
		errs = append(errs, "endpoint_exclusion.owner must be container or routeros")
	}
	if strings.TrimSpace(e.RoutingTable) == "" {
		errs = append(errs, "endpoint_exclusion.routing_table is required")
	}
	if strings.TrimSpace(e.OuterGateway) == "" {
		errs = append(errs, "endpoint_exclusion.outer_gateway is required")
	}
	if strings.TrimSpace(e.OuterEgressInterface) == "" {
		errs = append(errs, "endpoint_exclusion.outer_egress_interface is required")
	}
	switch e.EndpointResolutionPolicy {
	case "", "literal", "dns":
	default:
		errs = append(errs, "endpoint_exclusion.endpoint_resolution_policy must be literal or dns")
	}
	switch e.RefreshOwner {
	case "container", "routeros":
	default:
		errs = append(errs, "endpoint_exclusion.refresh_owner must be container or routeros")
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (s Secrets) Validate() error {
	var errs []string
	if s.Version == "" {
		errs = append(errs, "secrets version is required")
	}
	if s.Generation == "" {
		errs = append(errs, "generation is required")
	}
	if s.PrivateKey == "" {
		errs = append(errs, "private_key is required")
	}
	if s.PeerPublicKey == "" {
		errs = append(errs, "peer_public_key is required")
	}
	if s.PresharedKey == "" {
		errs = append(errs, "preshared_key is required")
	}
	if s.HeaderProtectionKey == "" {
		errs = append(errs, "header_protection_key is required")
	}
	if s.ControlToken == "" {
		errs = append(errs, "control_token is required")
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (p Pair) Effective() EffectiveConfig {
	return EffectiveConfig{
		Version:                   p.Config.Version,
		Generation:                p.Config.Generation,
		InterfaceName:             p.Config.InterfaceName,
		ListenPort:                p.Config.ListenPort,
		MTU:                       p.Config.MTU,
		TunnelAddress:             p.Config.TunnelAddress,
		Gateway:                   p.Config.Gateway,
		VethAddress:               p.Config.VethAddress,
		AllowedIPs:                append([]string(nil), p.Config.AllowedIPs...),
		Endpoint:                  p.Config.Endpoint,
		Jc:                        p.Config.Jc,
		Jmin:                      p.Config.Jmin,
		Jmax:                      p.Config.Jmax,
		S1:                        p.Config.S1,
		S2:                        p.Config.S2,
		S3:                        p.Config.S3,
		S4:                        p.Config.S4,
		H1:                        p.Config.H1,
		H2:                        p.Config.H2,
		H3:                        p.Config.H3,
		H4:                        p.Config.H4,
		I1:                        p.Config.I1,
		I2:                        p.Config.I2,
		I3:                        p.Config.I3,
		I4:                        p.Config.I4,
		I5:                        p.Config.I5,
		ContentPaddingAdditionMin: p.Config.ContentPaddingAdditionMin,
		ContentPaddingAdditionMax: p.Config.ContentPaddingAdditionMax,
		RekeyAfterTimeMin:         p.Config.RekeyAfterTimeMin,
		RekeyAfterTimeMax:         p.Config.RekeyAfterTimeMax,
		RekeyTimeoutMin:           p.Config.RekeyTimeoutMin,
		RekeyTimeoutMax:           p.Config.RekeyTimeoutMax,
		RejectAfterTimeMin:        p.Config.RejectAfterTimeMin,
		RejectAfterTimeMax:        p.Config.RejectAfterTimeMax,
		KeepaliveTimeoutMin:       p.Config.KeepaliveTimeoutMin,
		KeepaliveTimeoutMax:       p.Config.KeepaliveTimeoutMax,
		MaxHandshakeAttemptsMin:   p.Config.MaxHandshakeAttemptsMin,
		MaxHandshakeAttemptsMax:   p.Config.MaxHandshakeAttemptsMax,
		PersistentKeepaliveMin:    p.Config.PersistentKeepaliveMin,
		PersistentKeepaliveMax:    p.Config.PersistentKeepaliveMax,
		HeaderProtectionEnabled:   true,
		HealthAddress:             p.Config.HealthAddress,
		UIMode:                    p.Config.UIMode,
		SecretFingerprints: map[string]string{
			"private_key":           fingerprint("private_key", p.Secrets.PrivateKey),
			"peer_public_key":       fingerprint("peer_public_key", p.Secrets.PeerPublicKey),
			"preshared_key":         fingerprint("preshared_key", p.Secrets.PresharedKey),
			"header_protection_key": fingerprint("header_protection_key", p.Secrets.HeaderProtectionKey),
			"control_token":         fingerprint("control_token", p.Secrets.ControlToken),
		},
	}
}

func (p Pair) RedactedJSON() ([]byte, error) {
	redacted := p.Effective()
	return json.MarshalIndent(redacted, "", "  ")
}
