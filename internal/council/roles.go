package council

// DefaultRoles is the generic role set used by the "role-based" council type
// registration. Roles are Generator/Critic/Verifier/Simplifier, per the
// Role-Based Rada shape proposed in docs/council-research-synthesis.md §2.7
// (Devil's Advocate and role rotation across rounds are explicitly left as
// future work there, not included here).
//
// Role content is fixed in code, not env-configurable — see the "role-based"
// registration in cmd/server/main.go and docs/strategies.md for the
// registration contract.
var DefaultRoles = []Role{
	{
		Name: "Generator",
		Instruction: "You are the Generator. Explore the question broadly and propose a " +
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
}
