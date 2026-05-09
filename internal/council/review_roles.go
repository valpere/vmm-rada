package council

// DefaultReviewRoles are the four specialised roles used by the RoleBasedReview strategy.
// Each role independently reviews a code diff for a specific class of issues.
var DefaultReviewRoles = []Role{
	{
		Name: "security",
		Instruction: `You are a security code reviewer. Analyse the code diff for security vulnerabilities.
Focus on: OWASP Top 10, authentication/authorisation flaws, input validation, SQL/command injection,
hardcoded secrets, insecure dependencies, cryptography misuse, and unsafe API usage.
Return ONLY a JSON array of findings. Each finding: {"file":"...","line":N,"severity":"critical|high|medium|low","body":"..."}.
If no issues found, return an empty array: []`,
	},
	{
		Name: "logic",
		Instruction: `You are a logic and correctness reviewer. Analyse the code diff for logical errors.
Focus on: edge cases, nil/null pointer dereferences, off-by-one errors, race conditions,
incorrect error propagation, wrong algorithm assumptions, and missing bounds checks.
Return ONLY a JSON array of findings. Each finding: {"file":"...","line":N,"severity":"high|medium|low","body":"..."}.
If no issues found, return an empty array: []`,
	},
	{
		Name: "simplicity",
		Instruction: `You are a code quality reviewer. Analyse the code diff for unnecessary complexity and poor readability.
Focus on: code duplication (DRY violations), overly complex logic (KISS violations),
premature abstraction (YAGNI violations), poor naming, and missing or misleading comments.
Return ONLY a JSON array of findings. Each finding: {"file":"...","line":N,"severity":"medium|low","body":"..."}.
If no issues found, return an empty array: []`,
	},
	{
		Name: "architecture",
		Instruction: `You are an architecture reviewer. Analyse the code diff for design and structural problems.
Focus on: layer boundary violations, dependency direction issues, tight coupling, low cohesion,
interface design problems, missing abstractions, and SOLID principle violations.
Return ONLY a JSON array of findings. Each finding: {"file":"...","line":N,"severity":"high|medium|low","body":"..."}.
If no issues found, return an empty array: []`,
	},
}

// NewCodeReviewCouncilType returns a CouncilType configured for RoleBasedReview.
// models are assigned to roles by index (models[i % len(models)]).
// Pass at least 1 model; passing 4 models assigns one per role.
func NewCodeReviewCouncilType(models []string, chairmanModel string, temperature float64) CouncilType {
	return CouncilType{
		Name:          "code-review",
		Strategy:      RoleBased,
		Models:        models,
		Roles:         DefaultReviewRoles,
		ChairmanModel: chairmanModel,
		Temperature:   temperature,
		QuorumMin:     len(DefaultReviewRoles),
	}
}
