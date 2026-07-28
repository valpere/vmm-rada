package config

import (
	"errors"
	"fmt"
	"os"
	"sort"

	"github.com/valpere/vmm-rada/internal/council"
	"gopkg.in/yaml.v3"
)

// councilFile is the top-level shape of a council registry YAML file.
type councilFile struct {
	Councils map[string]councilEntry `yaml:"councils"`
}

// councilEntry is one registration. Which fields apply depends on the
// declared Strategy — see validateEntry. Optional numeric overrides use
// pointers so "field omitted" (use the strategy's default) is distinguishable
// from "field explicitly set to zero".
type councilEntry struct {
	Strategy    string            `yaml:"strategy"`
	Arbiter     string            `yaml:"arbiter"`
	Members     []string          `yaml:"members"`
	Roles       map[string]string `yaml:"roles"`
	Refiner     string            `yaml:"refiner"`
	Proposers   []string          `yaml:"proposers"`
	Aggregators []string          `yaml:"aggregators"`
	Temperature *float64          `yaml:"temperature"`
	Quorum      *int              `yaml:"quorum"`

	RefineTopK                 int     `yaml:"refine_top_k"`
	MaxDebateRounds            int     `yaml:"max_debate_rounds"`
	MaxDelphiRounds            int     `yaml:"max_delphi_rounds"`
	DelphiConvergenceThreshold float64 `yaml:"delphi_convergence_threshold"`
}

// LoadCouncilRegistryYAML reads and validates a council registry YAML file at
// path, returning the fully-built registry. Unlike the env-var fallback
// (buildRegistryFromEnv), validation failures here are hard errors, not
// warn-and-skip: a YAML file at a configured path is a deliberate authored
// artifact, not ambient environment a deployment might inherit unintentionally.
// All validation errors are collected via errors.Join before returning, so a
// single run surfaces every problem rather than one at a time across repeated
// startup attempts.
//
// The returned error wraps fs.ErrNotExist when path doesn't exist — callers
// (BuildRegistry) use errors.Is to distinguish "no YAML file, fall back to
// env" from "YAML file exists but is broken, fail startup".
func LoadCouncilRegistryYAML(path string, defaultTemperature float64) (map[string]council.CouncilType, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec := yaml.NewDecoder(f)
	dec.KnownFields(true)
	var cf councilFile
	if err := dec.Decode(&cf); err != nil {
		return nil, fmt.Errorf("%s: parse: %w", path, err)
	}

	if len(cf.Councils) == 0 {
		return nil, fmt.Errorf("%s: no councils defined", path)
	}

	// Deterministic iteration order so validation errors (and the eventual
	// success log line) are reproducible across runs — map iteration order
	// is randomized in Go, which would make "which key failed first" flaky
	// in test output and startup logs alike.
	keys := make([]string, 0, len(cf.Councils))
	for k := range cf.Councils {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var errs []error
	usedStrategies := make(map[council.Strategy]string, len(keys))
	registry := make(map[string]council.CouncilType, len(keys))

	for _, key := range keys {
		entry := cf.Councils[key]

		strat, ok := council.ParseStrategy(entry.Strategy)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: council %q: unknown strategy %q", path, key, entry.Strategy))
			continue
		}
		if prior, dup := usedStrategies[strat]; dup {
			errs = append(errs, fmt.Errorf("%s: council %q and %q both declare strategy %q — at most one registration per strategy is allowed", path, prior, key, strat))
			continue
		}
		usedStrategies[strat] = key

		ct, entryErrs := buildCouncilType(key, strat, entry, defaultTemperature)
		if len(entryErrs) > 0 {
			for _, e := range entryErrs {
				errs = append(errs, fmt.Errorf("%s: council %q: %w", path, key, e))
			}
			continue
		}
		registry[key] = ct
	}

	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return registry, nil
}

// buildCouncilType validates entry against the field requirements of strat
// and, if valid, constructs the resulting CouncilType. Returns all validation
// errors found (not just the first) so LoadCouncilRegistryYAML can report a
// complete picture for this entry in one pass.
func buildCouncilType(name string, strat council.Strategy, e councilEntry, defaultTemperature float64) (council.CouncilType, []error) {
	var errs []error

	temperature := defaultTemperature
	if e.Temperature != nil {
		temperature = *e.Temperature
	}

	ct := council.CouncilType{
		Name:        name,
		Strategy:    strat,
		Temperature: temperature,
	}
	if e.Quorum != nil {
		ct.QuorumMin = *e.Quorum
	}

	// Fields every strategy family forbids unless it's the one that uses
	// them — checked per-strategy below via forbidField.
	forbidField := func(cond bool, field string) {
		if cond {
			errs = append(errs, fmt.Errorf("field %q is not used by strategy %q", field, strat))
		}
	}

	switch strat {
	case council.PeerReview, council.GenerateRankRefine, council.MultiAgentDebate, council.Delphi:
		if e.Arbiter == "" {
			errs = append(errs, fmt.Errorf("strategy %q requires \"arbiter\"", strat))
		}
		if len(e.Members) < 2 {
			errs = append(errs, fmt.Errorf("strategy %q requires at least 2 \"members\", got %d", strat, len(e.Members)))
		}
		forbidField(len(e.Roles) > 0, "roles")
		forbidField(e.Refiner != "", "refiner")
		forbidField(len(e.Proposers) > 0, "proposers")
		forbidField(len(e.Aggregators) > 0, "aggregators")
		if strat != council.GenerateRankRefine {
			forbidField(e.RefineTopK != 0, "refine_top_k")
		}
		if strat != council.MultiAgentDebate {
			forbidField(e.MaxDebateRounds != 0, "max_debate_rounds")
		}
		if strat != council.Delphi {
			forbidField(e.MaxDelphiRounds != 0, "max_delphi_rounds")
			forbidField(e.DelphiConvergenceThreshold != 0, "delphi_convergence_threshold")
		}

		ct.ChairmanModel = e.Arbiter
		ct.Models = e.Members
		// Only the strategy that actually owns each scalar override gets it
		// assigned — forbidField above already errors (and the caller
		// discards this ct entirely) if a field is set on the wrong
		// strategy, but this keeps ct itself correct-by-construction rather
		// than relying solely on that discard to prevent cross-strategy
		// field leakage.
		switch strat {
		case council.GenerateRankRefine:
			ct.RefineTopK = e.RefineTopK
		case council.MultiAgentDebate:
			ct.MaxDebateRounds = e.MaxDebateRounds
		case council.Delphi:
			ct.MaxDelphiRounds = e.MaxDelphiRounds
			ct.DelphiConvergenceThreshold = e.DelphiConvergenceThreshold
		}

	case council.Majority:
		if len(e.Members) < 2 {
			errs = append(errs, fmt.Errorf("strategy %q requires at least 2 \"members\", got %d", strat, len(e.Members)))
		}
		forbidField(len(e.Roles) > 0, "roles")
		forbidField(e.Refiner != "", "refiner")
		forbidField(len(e.Proposers) > 0, "proposers")
		forbidField(len(e.Aggregators) > 0, "aggregators")
		forbidField(e.RefineTopK != 0, "refine_top_k")
		forbidField(e.MaxDebateRounds != 0, "max_debate_rounds")
		forbidField(e.MaxDelphiRounds != 0, "max_delphi_rounds")
		forbidField(e.DelphiConvergenceThreshold != 0, "delphi_convergence_threshold")

		// Majority's chairman is genuinely optional — "" keeps the no-chairman
		// tie-error path reachable, matching the env-var fallback's behavior.
		ct.ChairmanModel = e.Arbiter
		ct.Models = e.Members

	case council.RoleBased:
		if e.Arbiter == "" {
			errs = append(errs, fmt.Errorf("strategy %q requires \"arbiter\"", strat))
		}
		forbidField(len(e.Members) > 0, "members")
		forbidField(e.Refiner != "", "refiner")
		forbidField(len(e.Proposers) > 0, "proposers")
		forbidField(len(e.Aggregators) > 0, "aggregators")
		forbidField(e.RefineTopK != 0, "refine_top_k")
		forbidField(e.MaxDebateRounds != 0, "max_debate_rounds")
		forbidField(e.MaxDelphiRounds != 0, "max_delphi_rounds")
		forbidField(e.DelphiConvergenceThreshold != 0, "delphi_convergence_threshold")

		roles, err := council.RolesWithModels(e.Roles)
		if err != nil {
			errs = append(errs, fmt.Errorf("\"roles\": %w", err))
		} else {
			ct.Roles = roles
		}
		ct.ChairmanModel = e.Arbiter
		if e.Quorum == nil {
			ct.QuorumMin = len(council.DefaultRoleKeys)
		}

	case council.MixtureOfAgents:
		if len(e.Proposers) < 1 {
			errs = append(errs, fmt.Errorf("strategy %q requires at least 1 \"proposers\" entry", strat))
		}
		if len(e.Aggregators) < 1 {
			errs = append(errs, fmt.Errorf("strategy %q requires at least 1 \"aggregators\" entry", strat))
		}
		if e.Refiner == "" {
			errs = append(errs, fmt.Errorf("strategy %q requires \"refiner\"", strat))
		}
		forbidField(e.Arbiter != "", "arbiter")
		forbidField(len(e.Members) > 0, "members")
		forbidField(len(e.Roles) > 0, "roles")
		forbidField(e.RefineTopK != 0, "refine_top_k")
		forbidField(e.MaxDebateRounds != 0, "max_debate_rounds")
		forbidField(e.MaxDelphiRounds != 0, "max_delphi_rounds")
		forbidField(e.DelphiConvergenceThreshold != 0, "delphi_convergence_threshold")

		ct.ProposerModels = e.Proposers
		ct.AggregatorModels = e.Aggregators
		ct.RefinerModel = e.Refiner

	default:
		// Unreachable: strat came from council.ParseStrategy, which only
		// returns values from the same table Strategy.String() reads.
		errs = append(errs, fmt.Errorf("internal error: unhandled strategy %q", strat))
	}

	return ct, errs
}
