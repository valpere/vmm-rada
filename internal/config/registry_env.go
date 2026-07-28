package config

import (
	"log/slog"

	"github.com/valpere/vmm-rada/internal/council"
)

// buildRegistryFromEnv builds the council type registry from cfg's
// per-strategy env var fields. This is the fallback path used when no YAML
// council config is available (see BuildRegistry) — moved verbatim from
// cmd/server/main.go's former inline block, same warn-and-skip semantics for
// every opt-in strategy.
func buildRegistryFromEnv(cfg *Config, logger *slog.Logger) map[string]council.CouncilType {
	registry := map[string]council.CouncilType{
		cfg.DefaultCouncilType: {
			Name:          cfg.DefaultCouncilType,
			Strategy:      council.PeerReview,
			Models:        cfg.DefaultCouncilModels,
			ChairmanModel: cfg.DefaultCouncilChairmanModel,
			Temperature:   cfg.DefaultCouncilTemperature,
		},
	}

	// Majority strategy registration is opt-in: it's only added to the
	// registry when MAJORITY_MODELS is explicitly set. Existing deployments
	// without the env var don't get the new council type silently exposed.
	//
	// The chairman model is genuinely optional. It is NOT defaulted to the
	// global CHAIRMAN_MODEL — config.Load() always populates that with a
	// dev-fallback, so falling back here would make Majority's no-chairman
	// path (verbatim winner emission, loud-error on tie) unreachable. Users
	// who want a chairman for tiebreak/polish must set MAJORITY_CHAIRMAN_MODEL.
	if len(cfg.MajorityModels) > 0 {
		registry["majority"] = council.CouncilType{
			Name:          "majority",
			Strategy:      council.Majority,
			Models:        cfg.MajorityModels,
			ChairmanModel: cfg.MajorityChairmanModel,
			Temperature:   cfg.DefaultCouncilTemperature,
		}
	}

	// GenerateRankRefine registration is opt-in AND requires both env vars.
	// Unlike Majority, this strategy has no no-LLM path — Stage 2 ranking is
	// always an arbiter call and Stage 3 refinement is always a chairman call.
	// If models are set but the chairman is missing, log a warning and skip
	// registration rather than fail at request time.
	if len(cfg.GenerateRankRefineModels) > 0 {
		if cfg.GenerateRankRefineChairmanModel == "" {
			logger.Warn("GENERATE_RANK_REFINE_MODELS set but GENERATE_RANK_REFINE_CHAIRMAN_MODEL is empty; skipping registration of \"generate-rank-refine\" council type")
		} else {
			registry["generate-rank-refine"] = council.CouncilType{
				Name:          "generate-rank-refine",
				Strategy:      council.GenerateRankRefine,
				Models:        cfg.GenerateRankRefineModels,
				ChairmanModel: cfg.GenerateRankRefineChairmanModel,
				Temperature:   cfg.DefaultCouncilTemperature,
			}
		}
	}

	// MultiAgentDebate registration is opt-in AND requires both env vars.
	// Stage 3 chairman always runs; no no-LLM path. DebateMaxRounds=0 is the
	// sentinel for "use runner default of 2".
	if len(cfg.DebateModels) > 0 {
		if cfg.DebateChairmanModel == "" {
			logger.Warn("DEBATE_MODELS set but DEBATE_CHAIRMAN_MODEL is empty; skipping registration of \"debate\" council type")
		} else {
			registry["debate"] = council.CouncilType{
				Name:            "debate",
				Strategy:        council.MultiAgentDebate,
				Models:          cfg.DebateModels,
				ChairmanModel:   cfg.DebateChairmanModel,
				Temperature:     cfg.DefaultCouncilTemperature,
				MaxDebateRounds: cfg.DebateMaxRounds,
			}
		}
	}

	// MixtureOfAgents registration is opt-in AND requires ALL THREE env vars.
	// MoA has no no-LLM path: every layer needs models. Models / ChairmanModel
	// are NOT used — the runner reads ProposerModels / AggregatorModels /
	// RefinerModel directly. Partial config is logged and skipped (not failed
	// at request time).
	if len(cfg.MoaProposerModels) > 0 || len(cfg.MoaAggregatorModels) > 0 || cfg.MoaRefinerModel != "" {
		switch {
		case len(cfg.MoaProposerModels) == 0:
			logger.Warn("MOA_AGGREGATOR_MODELS or MOA_REFINER_MODEL set but MOA_PROPOSER_MODELS is empty; skipping registration of \"moa\" council type")
		case len(cfg.MoaAggregatorModels) == 0:
			logger.Warn("MOA_PROPOSER_MODELS set but MOA_AGGREGATOR_MODELS is empty; skipping registration of \"moa\" council type")
		case cfg.MoaRefinerModel == "":
			logger.Warn("MOA_PROPOSER_MODELS / MOA_AGGREGATOR_MODELS set but MOA_REFINER_MODEL is empty; skipping registration of \"moa\" council type")
		default:
			registry["moa"] = council.CouncilType{
				Name:             "moa",
				Strategy:         council.MixtureOfAgents,
				ProposerModels:   cfg.MoaProposerModels,
				AggregatorModels: cfg.MoaAggregatorModels,
				RefinerModel:     cfg.MoaRefinerModel,
				Temperature:      cfg.DefaultCouncilTemperature,
			}
		}
	}

	// RoleBased registration is opt-in AND requires both env vars. Stage 3
	// chairman always runs; no no-LLM path. Role content (names/instructions)
	// is fixed as council.DefaultRoles, not env-configurable — only the
	// per-role Model assignment comes from ROLE_BASED_MODELS, reproducing the
	// historical i % len(models) round-robin (env vars carry a flat model
	// list, not named role assignments; that's what the YAML council config
	// unlocks). QuorumMin is set to len(DefaultRoles) — every role is a
	// unique concern, so a missing role silently drops coverage rather than
	// degrading gracefully.
	if len(cfg.RoleBasedModels) > 0 {
		if cfg.RoleBasedChairmanModel == "" {
			logger.Warn("ROLE_BASED_MODELS set but ROLE_BASED_CHAIRMAN_MODEL is empty; skipping registration of \"role-based\" council type")
		} else {
			roleModels := make(map[string]string, len(council.DefaultRoleKeys))
			for i, key := range council.DefaultRoleKeys {
				roleModels[key] = cfg.RoleBasedModels[i%len(cfg.RoleBasedModels)]
			}
			roles, err := council.RolesWithModels(roleModels)
			if err != nil {
				logger.Warn("failed to assign models to role-based roles; skipping registration", "error", err)
			} else {
				registry["role-based"] = council.CouncilType{
					Name:          "role-based",
					Strategy:      council.RoleBased,
					Roles:         roles,
					ChairmanModel: cfg.RoleBasedChairmanModel,
					Temperature:   cfg.DefaultCouncilTemperature,
					QuorumMin:     len(council.DefaultRoles),
				}
			}
		}
	}

	// Delphi registration is opt-in AND requires both env vars. Stage 3
	// chairman always runs; no no-LLM path. DelphiMaxRounds=0 and
	// DelphiConvergenceThreshold=0 are sentinels for "use runner defaults"
	// (3 rounds, 0.1 threshold).
	if len(cfg.DelphiModels) > 0 {
		if cfg.DelphiChairmanModel == "" {
			logger.Warn("DELPHI_MODELS set but DELPHI_CHAIRMAN_MODEL is empty; skipping registration of \"delphi\" council type")
		} else {
			registry["delphi"] = council.CouncilType{
				Name:                       "delphi",
				Strategy:                   council.Delphi,
				Models:                     cfg.DelphiModels,
				ChairmanModel:              cfg.DelphiChairmanModel,
				Temperature:                cfg.DefaultCouncilTemperature,
				MaxDelphiRounds:            cfg.DelphiMaxRounds,
				DelphiConvergenceThreshold: cfg.DelphiConvergenceThreshold,
			}
		}
	}

	return registry
}
