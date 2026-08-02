package awg3profile

import (
	"bufio"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"awg3routerosgateway/internal/config"
)

func RenderSetconf(pair config.Pair) (string, error) {
	if err := validateSetconfPair(pair); err != nil {
		return "", err
	}
	var b strings.Builder
	writeSetconfBody(&b, pair)
	return b.String(), nil
}

func Render(pair config.Pair) (string, error) {
	if err := validateRenderedPair(pair); err != nil {
		return "", err
	}
	var b strings.Builder
	if pair.Config.Generation != "" {
		fmt.Fprintf(&b, "# generation = %s\n", pair.Config.Generation)
	}
	if pair.Config.Version != "" {
		fmt.Fprintf(&b, "# version = %s\n", pair.Config.Version)
	}
	if pair.Config.InterfaceName != "" {
		fmt.Fprintf(&b, "# interface_name = %s\n", pair.Config.InterfaceName)
	}
	if pair.Config.Gateway != "" {
		fmt.Fprintf(&b, "# gateway = %s\n", pair.Config.Gateway)
	}
	if pair.Config.VethAddress != "" {
		fmt.Fprintf(&b, "# veth_address = %s\n", pair.Config.VethAddress)
	}
	if pair.Config.HealthAddress != "" {
		fmt.Fprintf(&b, "# health_address = %s\n", pair.Config.HealthAddress)
	}
	if pair.Config.UIMode != "" {
		fmt.Fprintf(&b, "# ui_mode = %s\n", pair.Config.UIMode)
	}
	writeRuntimeBody(&b, pair)
	return b.String(), nil
}

func writeSetconfBody(b *strings.Builder, pair config.Pair) {
	b.WriteString("[Interface]\n")
	writeKV(b, "PrivateKey", pair.Secrets.PrivateKey)
	writeKV(b, "ListenPort", strconv.Itoa(pair.Config.ListenPort))
	writeKV(b, "Jc", strconv.Itoa(pair.Config.Jc))
	writeKV(b, "Jmin", strconv.Itoa(pair.Config.Jmin))
	writeKV(b, "Jmax", strconv.Itoa(pair.Config.Jmax))
	writeKV(b, "S1", strconv.Itoa(pair.Config.S1))
	writeKV(b, "S2", strconv.Itoa(pair.Config.S2))
	writeKV(b, "S3", strconv.Itoa(pair.Config.S3))
	writeKV(b, "S4", strconv.Itoa(pair.Config.S4))
	writeKV(b, "H1", strconv.Itoa(pair.Config.H1))
	writeKV(b, "H2", strconv.Itoa(pair.Config.H2))
	writeKV(b, "H3", strconv.Itoa(pair.Config.H3))
	writeKV(b, "H4", strconv.Itoa(pair.Config.H4))
	writeKV(b, "I1", pair.Config.I1)
	writeKV(b, "I2", pair.Config.I2)
	writeKV(b, "I3", pair.Config.I3)
	writeKV(b, "I4", pair.Config.I4)
	writeKV(b, "I5", pair.Config.I5)
	writeKV(b, "HeaderProtectionKey", pair.Secrets.HeaderProtectionKey)
	writeKV(b, "ContentPaddingAddition", formatRange(pair.Config.ContentPaddingAdditionMin, pair.Config.ContentPaddingAdditionMax))
	writeKV(b, "RekeyAfterTime", formatRange(pair.Config.RekeyAfterTimeMin, pair.Config.RekeyAfterTimeMax))
	writeKV(b, "RekeyTimeout", formatRange(pair.Config.RekeyTimeoutMin, pair.Config.RekeyTimeoutMax))
	writeKV(b, "RejectAfterTime", formatRange(pair.Config.RejectAfterTimeMin, pair.Config.RejectAfterTimeMax))
	writeKV(b, "KeepaliveTimeout", formatRange(pair.Config.KeepaliveTimeoutMin, pair.Config.KeepaliveTimeoutMax))
	writeKV(b, "MaxHandshakeAttempts", formatRange(pair.Config.MaxHandshakeAttemptsMin, pair.Config.MaxHandshakeAttemptsMax))
	b.WriteString("\n[Peer]\n")
	writeKV(b, "PublicKey", pair.Secrets.PeerPublicKey)
	writeKV(b, "PresharedKey", pair.Secrets.PresharedKey)
	writeKV(b, "AllowedIPs", strings.Join(pair.Config.AllowedIPs, ", "))
	writeKV(b, "Endpoint", pair.Config.Endpoint)
	writeKV(b, "PersistentKeepalive", formatRange(pair.Config.PersistentKeepaliveMin, pair.Config.PersistentKeepaliveMax))
}

func writeRuntimeBody(b *strings.Builder, pair config.Pair) {
	b.WriteString("[Interface]\n")
	writeKV(b, "PrivateKey", pair.Secrets.PrivateKey)
	writeKV(b, "Address", pair.Config.TunnelAddress)
	writeKV(b, "ListenPort", strconv.Itoa(pair.Config.ListenPort))
	writeKV(b, "MTU", strconv.Itoa(pair.Config.MTU))
	writeKV(b, "Jc", strconv.Itoa(pair.Config.Jc))
	writeKV(b, "Jmin", strconv.Itoa(pair.Config.Jmin))
	writeKV(b, "Jmax", strconv.Itoa(pair.Config.Jmax))
	writeKV(b, "S1", strconv.Itoa(pair.Config.S1))
	writeKV(b, "S2", strconv.Itoa(pair.Config.S2))
	writeKV(b, "S3", strconv.Itoa(pair.Config.S3))
	writeKV(b, "S4", strconv.Itoa(pair.Config.S4))
	writeKV(b, "H1", strconv.Itoa(pair.Config.H1))
	writeKV(b, "H2", strconv.Itoa(pair.Config.H2))
	writeKV(b, "H3", strconv.Itoa(pair.Config.H3))
	writeKV(b, "H4", strconv.Itoa(pair.Config.H4))
	writeKV(b, "I1", pair.Config.I1)
	writeKV(b, "I2", pair.Config.I2)
	writeKV(b, "I3", pair.Config.I3)
	writeKV(b, "I4", pair.Config.I4)
	writeKV(b, "I5", pair.Config.I5)
	writeKV(b, "HeaderProtectionKey", pair.Secrets.HeaderProtectionKey)
	writeKV(b, "ContentPaddingAddition", formatRange(pair.Config.ContentPaddingAdditionMin, pair.Config.ContentPaddingAdditionMax))
	writeKV(b, "RekeyAfterTime", formatRange(pair.Config.RekeyAfterTimeMin, pair.Config.RekeyAfterTimeMax))
	writeKV(b, "RekeyTimeout", formatRange(pair.Config.RekeyTimeoutMin, pair.Config.RekeyTimeoutMax))
	writeKV(b, "RejectAfterTime", formatRange(pair.Config.RejectAfterTimeMin, pair.Config.RejectAfterTimeMax))
	writeKV(b, "KeepaliveTimeout", formatRange(pair.Config.KeepaliveTimeoutMin, pair.Config.KeepaliveTimeoutMax))
	writeKV(b, "MaxHandshakeAttempts", formatRange(pair.Config.MaxHandshakeAttemptsMin, pair.Config.MaxHandshakeAttemptsMax))
	b.WriteString("\n[Peer]\n")
	writeKV(b, "PublicKey", pair.Secrets.PeerPublicKey)
	writeKV(b, "PresharedKey", pair.Secrets.PresharedKey)
	writeKV(b, "AllowedIPs", strings.Join(pair.Config.AllowedIPs, ","))
	writeKV(b, "Endpoint", pair.Config.Endpoint)
	writeKV(b, "PersistentKeepalive", formatRange(pair.Config.PersistentKeepaliveMin, pair.Config.PersistentKeepaliveMax))
}

func validateRenderedPair(pair config.Pair) error {
	if err := pair.Config.Validate(); err != nil {
		return err
	}
	if pair.Secrets.PrivateKey == "" {
		return errors.New("private_key is required")
	}
	if pair.Secrets.PeerPublicKey == "" {
		return errors.New("peer_public_key is required")
	}
	if pair.Secrets.PresharedKey == "" {
		return errors.New("preshared_key is required")
	}
	if pair.Secrets.HeaderProtectionKey == "" {
		return errors.New("header_protection_key is required")
	}
	return nil
}

func validateSetconfPair(pair config.Pair) error {
	var errs []string
	if pair.Secrets.PrivateKey == "" {
		errs = append(errs, "private_key is required")
	}
	if pair.Secrets.PeerPublicKey == "" {
		errs = append(errs, "peer_public_key is required")
	}
	if pair.Secrets.PresharedKey == "" {
		errs = append(errs, "preshared_key is required")
	}
	if pair.Secrets.HeaderProtectionKey == "" {
		errs = append(errs, "header_protection_key is required")
	}
	if pair.Config.ListenPort <= 0 || pair.Config.ListenPort > 65535 {
		errs = append(errs, "listen_port must be 1..65535")
	}
	if pair.Config.Jc < 0 {
		errs = append(errs, "jc must be non-negative")
	}
	checkRange := func(name string, min, max, hardMin, hardMax int) {
		if min < hardMin || max < hardMin || max < min || max > hardMax {
			errs = append(errs, fmt.Sprintf("%s must be a valid range", name))
		}
	}
	checkRange("j range", pair.Config.Jmin, pair.Config.Jmax, 0, 1000)
	checkRange("content_padding_addition", pair.Config.ContentPaddingAdditionMin, pair.Config.ContentPaddingAdditionMax, 0, 32)
	checkRange("rekey_after_time", pair.Config.RekeyAfterTimeMin, pair.Config.RekeyAfterTimeMax, 1, 600)
	checkRange("rekey_timeout", pair.Config.RekeyTimeoutMin, pair.Config.RekeyTimeoutMax, 1, 60)
	checkRange("reject_after_time", pair.Config.RejectAfterTimeMin, pair.Config.RejectAfterTimeMax, 1, 900)
	checkRange("keepalive_timeout", pair.Config.KeepaliveTimeoutMin, pair.Config.KeepaliveTimeoutMax, 1, 120)
	checkRange("max_handshake_attempts", pair.Config.MaxHandshakeAttemptsMin, pair.Config.MaxHandshakeAttemptsMax, 1, 64)
	checkRange("persistent_keepalive", pair.Config.PersistentKeepaliveMin, pair.Config.PersistentKeepaliveMax, 0, 600)
	if pair.Config.S1 < 12 || pair.Config.S2 < 12 || pair.Config.S3 < 12 || pair.Config.S4 < 12 {
		errs = append(errs, "s1-s4 must each be >= 12")
	}
	if pair.Config.H1 < 0 || pair.Config.H2 < 0 || pair.Config.H3 < 0 || pair.Config.H4 < 0 {
		errs = append(errs, "h1-h4 must be non-negative")
	}
	for i, v := range []string{pair.Config.I1, pair.Config.I2, pair.Config.I3, pair.Config.I4, pair.Config.I5} {
		if strings.TrimSpace(v) == "" {
			errs = append(errs, fmt.Sprintf("i%d is required", i+1))
		}
	}
	if pair.Config.RekeyAfterTimeMax >= pair.Config.RejectAfterTimeMin && pair.Config.RejectAfterTimeMin > 0 {
		errs = append(errs, "rekey_after_time must stay below reject_after_time")
	}
	if pair.Config.RekeyTimeoutMax >= pair.Config.RekeyAfterTimeMin && pair.Config.RekeyAfterTimeMin > 0 {
		errs = append(errs, "rekey_timeout must stay below rekey_after_time")
	}
	if len(errs) > 0 {
		sort.Strings(errs)
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func Parse(text string) (config.Pair, error) {
	type builder struct {
		pair config.Pair
		seen map[string]bool
	}
	st := builder{seen: make(map[string]bool)}
	section := ""
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			key, val, ok := strings.Cut(strings.TrimSpace(strings.TrimPrefix(line, "#")), "=")
			if ok {
				switch strings.TrimSpace(key) {
				case "generation":
					st.pair.Config.Generation = strings.TrimSpace(val)
					st.pair.Secrets.Generation = strings.TrimSpace(val)
				case "version":
					st.pair.Config.Version = strings.TrimSpace(val)
				case "interface_name":
					st.pair.Config.InterfaceName = strings.TrimSpace(val)
				case "gateway":
					st.pair.Config.Gateway = strings.TrimSpace(val)
				case "veth_address":
					st.pair.Config.VethAddress = strings.TrimSpace(val)
				case "health_address":
					st.pair.Config.HealthAddress = strings.TrimSpace(val)
				case "ui_mode":
					st.pair.Config.UIMode = strings.TrimSpace(val)
				}
			}
			continue
		}
		if strings.HasPrefix(line, "[") {
			if !strings.HasSuffix(line, "]") {
				return config.Pair{}, fmt.Errorf("invalid section header %q", line)
			}
			section = strings.Trim(line, "[]")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return config.Pair{}, fmt.Errorf("invalid line %q", line)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch section {
		case "Interface":
			if err := parseInterfaceKV(&st.pair, key, value, st.seen); err != nil {
				return config.Pair{}, err
			}
		case "Peer":
			if err := parsePeerKV(&st.pair, key, value, st.seen); err != nil {
				return config.Pair{}, err
			}
		default:
			return config.Pair{}, fmt.Errorf("field %q outside section", key)
		}
	}
	if err := scanner.Err(); err != nil {
		return config.Pair{}, err
	}
	if st.pair.Config.Generation == "" {
		st.pair.Config.Generation = "gen-parse"
		st.pair.Secrets.Generation = st.pair.Config.Generation
	}
	if err := validateRenderedPair(st.pair); err != nil {
		return config.Pair{}, err
	}
	return st.pair, nil
}

func parseInterfaceKV(pair *config.Pair, key, value string, seen map[string]bool) error {
	if seen[key] {
		return fmt.Errorf("duplicate key %q", key)
	}
	seen[key] = true
	switch key {
	case "PrivateKey":
		pair.Secrets.PrivateKey = value
	case "Address":
		pair.Config.TunnelAddress = value
	case "ListenPort":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		pair.Config.ListenPort = v
	case "MTU":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		pair.Config.MTU = v
	case "Jc":
		v, err := strconv.Atoi(value)
		if err != nil {
			return err
		}
		pair.Config.Jc = v
	case "Jmin":
		return setInt(&pair.Config.Jmin, value)
	case "Jmax":
		return setInt(&pair.Config.Jmax, value)
	case "S1":
		return setInt(&pair.Config.S1, value)
	case "S2":
		return setInt(&pair.Config.S2, value)
	case "S3":
		return setInt(&pair.Config.S3, value)
	case "S4":
		return setInt(&pair.Config.S4, value)
	case "H1":
		return setInt(&pair.Config.H1, value)
	case "H2":
		return setInt(&pair.Config.H2, value)
	case "H3":
		return setInt(&pair.Config.H3, value)
	case "H4":
		return setInt(&pair.Config.H4, value)
	case "I1":
		pair.Config.I1 = value
	case "I2":
		pair.Config.I2 = value
	case "I3":
		pair.Config.I3 = value
	case "I4":
		pair.Config.I4 = value
	case "I5":
		pair.Config.I5 = value
	case "HeaderProtectionKey":
		pair.Secrets.HeaderProtectionKey = value
	case "ContentPaddingAddition":
		return setRange(&pair.Config.ContentPaddingAdditionMin, &pair.Config.ContentPaddingAdditionMax, value)
	case "RekeyAfterTime":
		return setRange(&pair.Config.RekeyAfterTimeMin, &pair.Config.RekeyAfterTimeMax, value)
	case "RekeyTimeout":
		return setRange(&pair.Config.RekeyTimeoutMin, &pair.Config.RekeyTimeoutMax, value)
	case "RejectAfterTime":
		return setRange(&pair.Config.RejectAfterTimeMin, &pair.Config.RejectAfterTimeMax, value)
	case "KeepaliveTimeout":
		return setRange(&pair.Config.KeepaliveTimeoutMin, &pair.Config.KeepaliveTimeoutMax, value)
	case "MaxHandshakeAttempts":
		return setRange(&pair.Config.MaxHandshakeAttemptsMin, &pair.Config.MaxHandshakeAttemptsMax, value)
	default:
		return fmt.Errorf("unknown interface key %q", key)
	}
	return nil
}

func parsePeerKV(pair *config.Pair, key, value string, seen map[string]bool) error {
	if seen["peer:"+key] {
		return fmt.Errorf("duplicate key %q", key)
	}
	seen["peer:"+key] = true
	switch key {
	case "PublicKey":
		pair.Secrets.PeerPublicKey = value
	case "PresharedKey":
		pair.Secrets.PresharedKey = value
	case "AllowedIPs":
		if value == "" {
			pair.Config.AllowedIPs = nil
			return nil
		}
		parts := strings.Split(value, ",")
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		pair.Config.AllowedIPs = parts
	case "Endpoint":
		pair.Config.Endpoint = value
	case "PersistentKeepalive":
		return setRange(&pair.Config.PersistentKeepaliveMin, &pair.Config.PersistentKeepaliveMax, value)
	default:
		return fmt.Errorf("unknown peer key %q", key)
	}
	return nil
}

func writeKV(b *strings.Builder, key, value string) {
	fmt.Fprintf(b, "%s = %s\n", key, value)
}

func formatRange(min, max int) string {
	if min == max {
		return strconv.Itoa(min)
	}
	return fmt.Sprintf("%d-%d", min, max)
}

func setInt(target *int, value string) error {
	v, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	*target = v
	return nil
}

func setRange(min, max *int, value string) error {
	if strings.Contains(value, "-") {
		a, b, ok := strings.Cut(value, "-")
		if !ok {
			return errors.New("invalid range")
		}
		if err := setInt(min, strings.TrimSpace(a)); err != nil {
			return err
		}
		return setInt(max, strings.TrimSpace(b))
	}
	v, err := strconv.Atoi(value)
	if err != nil {
		return err
	}
	*min = v
	*max = v
	return nil
}
