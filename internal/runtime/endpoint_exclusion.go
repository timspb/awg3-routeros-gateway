package runtime

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"sync"
	"time"

	"awg3routerosgateway/internal/config"
)

type RouteEndpointExclusionAdapter struct {
	mu             sync.Mutex
	CommandFactory CommandFactory
	IPBinary       string
	Timeout        time.Duration
	current        *endpointRouteState
}

type endpointRouteState struct {
	spec    endpointRouteSpec
	managed bool
}

type endpointRouteSpec struct {
	owner                    string
	routingTable             string
	outerGateway             string
	outerEgressInterface     string
	sourceAddress            string
	endpointResolutionPolicy string
	refreshOwner             string
	awgInterface             string
	endpoint                 string
	target                   string
	prefix                   string
}

func NewRouteEndpointExclusionAdapter() *RouteEndpointExclusionAdapter {
	return &RouteEndpointExclusionAdapter{
		CommandFactory: osCommandFactory{},
		IPBinary:       "ip",
		Timeout:        5 * time.Second,
	}
}

func (a *RouteEndpointExclusionAdapter) Apply(ctx context.Context, pair config.Pair) error {
	return a.installRoute(ctx, pair, false)
}

func (a *RouteEndpointExclusionAdapter) Ready(ctx context.Context, pair config.Pair) error {
	spec, err := a.specFromPair(pair)
	if err != nil {
		return err
	}
	return a.checkRoute(ctx, spec)
}

func (a *RouteEndpointExclusionAdapter) Restore(ctx context.Context, pair config.Pair) error {
	return a.installRoute(ctx, pair, true)
}

func (a *RouteEndpointExclusionAdapter) Cleanup(ctx context.Context, pair config.Pair) error {
	spec, err := a.specFromPair(pair)
	if err != nil {
		return err
	}
	if spec.owner != "container" {
		return nil
	}
	a.mu.Lock()
	current := a.current
	a.mu.Unlock()
	if current == nil {
		return nil
	}
	if current.spec.target != spec.target {
		return nil
	}
	if err := a.deleteRoute(ctx, current.spec); err != nil {
		return err
	}
	a.mu.Lock()
	if a.current != nil && a.current.spec.target == current.spec.target {
		a.current = nil
	}
	a.mu.Unlock()
	return nil
}

func (a *RouteEndpointExclusionAdapter) installRoute(ctx context.Context, pair config.Pair, restore bool) error {
	spec, err := a.specFromPair(pair)
	if err != nil {
		return err
	}
	if a.IPBinary == "" {
		a.IPBinary = "ip"
	}
	if a.CommandFactory == nil {
		a.CommandFactory = osCommandFactory{}
	}

	a.mu.Lock()
	current := a.current
	a.mu.Unlock()
	if current != nil && current.spec.target != spec.target {
		if current.managed && current.spec.owner == "container" {
			if err := a.deleteRoute(ctx, current.spec); err != nil {
				return err
			}
		}
	}

	if spec.owner == "container" {
		if err := a.createOrReplaceRoute(ctx, spec); err != nil {
			return err
		}
	}

	a.mu.Lock()
	a.current = &endpointRouteState{
		spec:    spec,
		managed: spec.owner == "container",
	}
	a.mu.Unlock()
	return nil
}

func (a *RouteEndpointExclusionAdapter) createOrReplaceRoute(ctx context.Context, spec endpointRouteSpec) error {
	args := []string{"route", "replace", spec.target + spec.prefix, "via", spec.outerGateway, "dev", spec.outerEgressInterface, "table", spec.routingTable}
	if spec.sourceAddress != "" {
		args = append(args, "src", spec.sourceAddress)
	}
	return a.runIP(ctx, args...)
}

func (a *RouteEndpointExclusionAdapter) deleteRoute(ctx context.Context, spec endpointRouteSpec) error {
	args := []string{"route", "del", spec.target + spec.prefix, "table", spec.routingTable}
	return a.runIP(ctx, args...)
}

func (a *RouteEndpointExclusionAdapter) checkRoute(ctx context.Context, spec endpointRouteSpec) error {
	args := []string{"route", "get", spec.target}
	if spec.routingTable != "" {
		args = append(args, "table", spec.routingTable)
	}
	if spec.sourceAddress != "" {
		args = append(args, "from", spec.sourceAddress)
	}
	out, err := a.commandOutput(ctx, a.IPBinary, a.Timeout, args...)
	if err != nil {
		return err
	}
	report, err := parseRouteGetOutput(out)
	if err != nil {
		return err
	}
	if report.via != spec.outerGateway {
		return fmt.Errorf("endpoint route gateway mismatch: want %q got %q", spec.outerGateway, report.via)
	}
	if report.dev != spec.outerEgressInterface {
		return fmt.Errorf("endpoint route device mismatch: want %q got %q", spec.outerEgressInterface, report.dev)
	}
	if report.dev == spec.awgInterface {
		return fmt.Errorf("endpoint route must not use awg interface %q", spec.awgInterface)
	}
	if spec.sourceAddress != "" && report.src != spec.sourceAddress {
		return fmt.Errorf("endpoint route source mismatch: want %q got %q", spec.sourceAddress, report.src)
	}
	if report.table != "" && report.table != spec.routingTable {
		return fmt.Errorf("endpoint route table mismatch: want %q got %q", spec.routingTable, report.table)
	}
	if report.dest != "" && report.dest != spec.target {
		return fmt.Errorf("endpoint route target mismatch: want %q got %q", spec.target, report.dest)
	}
	return nil
}

func (a *RouteEndpointExclusionAdapter) specFromPair(pair config.Pair) (endpointRouteSpec, error) {
	if err := pair.Validate(); err != nil {
		return endpointRouteSpec{}, err
	}
	if pair.Config.OuterPath == nil || pair.Config.OuterPath.EndpointExclusion == nil {
		return endpointRouteSpec{}, errors.New("outer_path.endpoint_exclusion is required")
	}
	cfg := pair.Config.OuterPath.EndpointExclusion
	if err := cfg.Validate(); err != nil {
		return endpointRouteSpec{}, err
	}
	target, prefix, err := resolveEndpointTarget(pair.Config.Endpoint, cfg.EndpointResolutionPolicy)
	if err != nil {
		return endpointRouteSpec{}, err
	}
	if cfg.OuterEgressInterface == pair.Config.InterfaceName {
		return endpointRouteSpec{}, fmt.Errorf("endpoint exclusion outer_egress_interface must not equal awg interface %q", pair.Config.InterfaceName)
	}
	return endpointRouteSpec{
		owner:                    cfg.Owner,
		routingTable:             cfg.RoutingTable,
		outerGateway:             cfg.OuterGateway,
		outerEgressInterface:     cfg.OuterEgressInterface,
		sourceAddress:            cfg.SourceAddress,
		endpointResolutionPolicy: cfg.EndpointResolutionPolicy,
		refreshOwner:             cfg.RefreshOwner,
		awgInterface:             pair.Config.InterfaceName,
		endpoint:                 pair.Config.Endpoint,
		target:                   target,
		prefix:                   prefix,
	}, nil
}

func (a *RouteEndpointExclusionAdapter) runIP(ctx context.Context, args ...string) error {
	if a.IPBinary == "" {
		a.IPBinary = "ip"
	}
	if a.CommandFactory == nil {
		a.CommandFactory = osCommandFactory{}
	}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := a.CommandFactory.CommandContext(stepCtx, a.IPBinary, args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ip %s failed: %w", strings.Join(args, " "), err)
	}
	return nil
}

func (a *RouteEndpointExclusionAdapter) commandOutput(ctx context.Context, binary string, timeout time.Duration, args ...string) (string, error) {
	if binary == "" {
		binary = "ip"
	}
	if a.CommandFactory == nil {
		a.CommandFactory = osCommandFactory{}
	}
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	stepCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := a.CommandFactory.CommandContext(stepCtx, binary, args...)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s failed: %w", binary, strings.Join(args, " "), err)
	}
	return out.String(), nil
}

type routeGetReport struct {
	dest  string
	via   string
	dev   string
	src   string
	table string
}

func parseRouteGetOutput(out string) (routeGetReport, error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) == 0 {
		return routeGetReport{}, errors.New("route get output empty")
	}
	report := routeGetReport{dest: fields[0]}
	for i := 1; i < len(fields); i++ {
		switch fields[i] {
		case "via", "dev", "src", "table":
			if i+1 >= len(fields) {
				return routeGetReport{}, fmt.Errorf("route get output missing value after %q", fields[i])
			}
			value := fields[i+1]
			switch fields[i] {
			case "via":
				report.via = value
			case "dev":
				report.dev = value
			case "src":
				report.src = value
			case "table":
				report.table = value
			}
			i++
		}
	}
	if report.dev == "" {
		return routeGetReport{}, errors.New("route get output missing device")
	}
	if report.via == "" {
		return routeGetReport{}, errors.New("route get output missing gateway")
	}
	return report, nil
}

func resolveEndpointTarget(endpoint, policy string) (string, string, error) {
	if endpoint == "" {
		return "", "", errors.New("endpoint is required")
	}
	host := endpoint
	if h, _, err := net.SplitHostPort(endpoint); err == nil {
		host = h
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.To4() != nil {
			return ip.String(), "/32", nil
		}
		return ip.String(), "/128", nil
	}
	switch policy {
	case "dns":
		ips, err := net.LookupIP(host)
		if err != nil {
			return "", "", err
		}
		for _, ip := range ips {
			if ip != nil {
				if ip.To4() != nil {
					return ip.String(), "/32", nil
				}
				return ip.String(), "/128", nil
			}
		}
		return "", "", fmt.Errorf("no ip resolved for %q", host)
	case "", "literal":
		return "", "", fmt.Errorf("endpoint %q must be an ip literal when endpoint_resolution_policy is %q", endpoint, policy)
	default:
		return "", "", fmt.Errorf("unsupported endpoint_resolution_policy %q", policy)
	}
}

var _ EndpointExclusionAdapter = (*RouteEndpointExclusionAdapter)(nil)
var _ = exec.ErrNotFound
