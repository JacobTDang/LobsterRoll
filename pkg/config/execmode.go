// Package config holds parsed configuration shared across services.
package config

import (
	"fmt"
	"strconv"
	"strings"
)

// ExecutionMode controls whether copy-trades require human approval.
type ExecutionMode int

const (
	ModeApproval  ExecutionMode = iota // every trade needs Telegram approval
	ModeAuto                           // auto-execute everything
	ModeAutoBelow                      // auto below CeilingUSD, else approval
)

func (m ExecutionMode) String() string {
	switch m {
	case ModeApproval:
		return "approval"
	case ModeAuto:
		return "auto"
	case ModeAutoBelow:
		return "auto_below"
	default:
		return "unknown"
	}
}

// ExecutionPolicy is the parsed EXECUTION_MODE setting.
type ExecutionPolicy struct {
	Mode       ExecutionMode
	CeilingUSD float64 // only meaningful for ModeAutoBelow
}

// ParseExecutionMode parses "approval", "auto", or "auto_below:<usd>".
func ParseExecutionMode(s string) (ExecutionPolicy, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch {
	case s == "approval":
		return ExecutionPolicy{Mode: ModeApproval}, nil
	case s == "auto":
		return ExecutionPolicy{Mode: ModeAuto}, nil
	case strings.HasPrefix(s, "auto_below:"):
		raw := strings.TrimPrefix(s, "auto_below:")
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return ExecutionPolicy{}, fmt.Errorf("invalid auto_below ceiling %q: %w", raw, err)
		}
		if v <= 0 {
			return ExecutionPolicy{}, fmt.Errorf("auto_below ceiling must be > 0, got %v", v)
		}
		return ExecutionPolicy{Mode: ModeAutoBelow, CeilingUSD: v}, nil
	default:
		return ExecutionPolicy{}, fmt.Errorf("unknown execution mode %q", s)
	}
}

// RequiresApproval reports whether a trade of sizeUSD needs human approval.
func (p ExecutionPolicy) RequiresApproval(sizeUSD float64) bool {
	switch p.Mode {
	case ModeAuto:
		return false
	case ModeAutoBelow:
		return sizeUSD >= p.CeilingUSD
	default: // ModeApproval and anything unexpected: fail safe to approval
		return true
	}
}
