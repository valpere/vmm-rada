package council

import "fmt"

// DefaultRoles is the generic role set used by the "role-based" council type
// registration. Roles are Creator/Critic/Verifier/Simplifier/DevilsAdvocate, per
// the Role-Based Rada shape proposed in docs/council-research-synthesis.md §2.7.
//
// This is a template: every Role's Model field is left empty here. A
// registration is responsible for producing a fully-populated []Role (with
// Model set per role) via RolesWithModels — see the "role-based" registration
// in cmd/server/main.go and docs/strategies.md for the registration contract.
var DefaultRoles = []Role{
	{
		Name: "Creator",
		Instruction: "You are the Creator. Explore the question broadly and propose a " +
			"substantive answer. Favor breadth of consideration over caution — surface " +
			"approaches or angles a narrower answer would miss. Do not hedge; commit to a " +
			"concrete answer.",
	},
	{
		Name: "Critic",
		Instruction: "You are the Critic. Answer the question, then actively look for flaws, " +
			"logical gaps, and unstated assumptions in your own reasoning as you write it. " +
			"Call out where you're uncertain and why, rather than presenting a smoothed-over " +
			"answer.",
	},
	{
		Name: "Verifier",
		Instruction: "You are the Verifier. Answer the question with an emphasis on factual " +
			"accuracy. Distinguish clearly between what you're confident is correct, what's " +
			"a reasonable inference, and what's speculative — do not present speculation as " +
			"fact.",
	},
	{
		Name: "Simplifier",
		Instruction: "You are the Simplifier. Answer the question as concisely as possible " +
			"while remaining correct and complete. Cut qualification, repetition, and " +
			"preamble. Prefer the shortest answer that fully addresses the question.",
	},
	{
		Name: "DevilsAdvocate",
		Instruction: "You are the Devil's Advocate. Take the strongest position against the " +
			"most obvious answer to this question. Argue the case that the consensus reading is " +
			"wrong, incomplete, or solving the wrong problem — even where you privately think it " +
			"is right. Do not hedge into agreement at the end.",
	},
}

// DefaultRoleKeys are the lowercase config keys used to assign a model to each
// DefaultRoles entry (e.g. in a YAML "roles:" map). Order-matched to
// DefaultRoles — DefaultRoleKeys[i] names DefaultRoles[i].
var DefaultRoleKeys = []string{"creator", "critic", "verifier", "simplifier", "devils_advocate"}

// RolesWithModels clones DefaultRoles and assigns each role's Model from
// models, keyed by the role's entry in DefaultRoleKeys. Returns an error if
// models is missing a key, has an unknown key, or maps a key to an empty
// string — a role silently left without a model would otherwise fail much
// later, inside a goroutine, with a less specific error.
//
// The length check plus the per-key presence check below are jointly
// sufficient to also catch unknown keys: if len(models) == len(DefaultRoleKeys)
// and every DefaultRoleKeys entry is present, there is no room left in models
// for a key that isn't one of DefaultRoleKeys.
func RolesWithModels(models map[string]string) ([]Role, error) {
	if len(models) != len(DefaultRoleKeys) {
		return nil, fmt.Errorf("expected exactly %d role models (%v), got %d", len(DefaultRoleKeys), DefaultRoleKeys, len(models))
	}
	roles := make([]Role, len(DefaultRoles))
	for i, key := range DefaultRoleKeys {
		model, ok := models[key]
		if !ok {
			return nil, fmt.Errorf("missing model for role %q", key)
		}
		if model == "" {
			return nil, fmt.Errorf("empty model for role %q", key)
		}
		roles[i] = DefaultRoles[i]
		roles[i].Model = model
	}
	return roles, nil
}
