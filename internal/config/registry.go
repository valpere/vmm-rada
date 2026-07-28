package config

import (
	"errors"
	"io/fs"
	"log/slog"
	"sort"

	"github.com/valpere/vmm-rada/internal/council"
)

// BuildRegistry constructs the council type registry for the server, either
// from a YAML file (cfg.CouncilConfigPath) or, if that file doesn't exist,
// from cfg's per-strategy env var fields (see buildRegistryFromEnv). This is
// the single registry-construction path shared by cmd/server and cmd/eval —
// previously each binary built its own inline copy, and the eval copy had
// silently drifted to omit the "role-based" registration entirely.
//
// YAML wins wholesale when present: every strategy in the returned registry
// comes from the file, with no merge against env vars. A malformed YAML file
// (present but invalid) is a hard error — it does NOT fall back to env, since
// silently ignoring a broken, deliberately-authored config file would be far
// more surprising than failing startup.
func BuildRegistry(cfg *Config, logger *slog.Logger) (map[string]council.CouncilType, error) {
	if cfg.CouncilConfigPath == "" {
		logger.Info("COUNCIL_CONFIG_PATH is explicitly empty; using env-var council registry")
		return buildRegistryFromEnv(cfg, logger), nil
	}

	registry, err := LoadCouncilRegistryYAML(cfg.CouncilConfigPath, cfg.DefaultCouncilTemperature)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			logger.Info("council YAML not found; using env-var council registry", "path", cfg.CouncilConfigPath)
			return buildRegistryFromEnv(cfg, logger), nil
		}
		return nil, err
	}

	keys := make([]string, 0, len(registry))
	for k := range registry {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	logger.Info("council registry loaded from YAML; per-strategy env vars are ignored",
		"path", cfg.CouncilConfigPath, "types", keys)

	return registry, nil
}
