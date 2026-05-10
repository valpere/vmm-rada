package council

import (
	"fmt"
	"sort"
	"strings"
)

// BuildStage0GeneratorPrompt returns the prompt for Stage 0 generator queries.
// Generators must return JSON: {"questions": [{"text": "..."}]}
func BuildStage0GeneratorPrompt(query string, history []ClarificationRound) []ChatMessage {
	var sb strings.Builder
	sb.WriteString("You are helping clarify a question before a council of AI models answers it.\n")
	sb.WriteString("Original question: ")
	sb.WriteString(query)
	sb.WriteString("\n")

	if len(history) > 0 {
		sb.WriteString("\nPrior clarification Q&A:\n")
		for _, r := range history {
			for i, q := range r.Questions {
				sb.WriteString("Q: ")
				sb.WriteString(q.Text)
				sb.WriteString("\n")
				answer := "(no answer)"
				for _, a := range r.Answers {
					if a.ID == q.ID && a.Text != "" {
						answer = a.Text
						break
					}
				}
				// Also check positional match if IDs don't line up
				if answer == "(no answer)" && i < len(r.Answers) && r.Answers[i].Text != "" {
					answer = r.Answers[i].Text
				}
				sb.WriteString("A: ")
				sb.WriteString(answer)
				sb.WriteString("\n\n")
			}
		}
	}

	sb.WriteString("\nIdentify contradictions, ambiguities, or missing context in the question.\n")
	sb.WriteString("Return ONLY a JSON object: {\"questions\": [{\"text\": \"...\"}]}\n")
	sb.WriteString("Return an empty questions array if the question is already clear enough.")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildStage0ChairmanPrompt returns the prompt for the Stage 0 chairman decision.
// Chairman must return JSON: {"questions": [{"id": "q1", "text": "..."}], "enough": true/false}
func BuildStage0ChairmanPrompt(query string, candidates []string, round, maxRounds, maxPerRound, accumulated, maxTotal int) []ChatMessage {
	var sb strings.Builder
	sb.WriteString("You are deciding whether to ask the user for clarification before answering.\n")
	sb.WriteString("Original question: ")
	sb.WriteString(query)
	sb.WriteString("\n\n")

	if len(candidates) > 0 {
		sb.WriteString("Proposed clarification questions:\n")
		for _, c := range candidates {
			sb.WriteString("- ")
			sb.WriteString(c)
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	} else {
		sb.WriteString("No clarification questions were proposed.\n\n")
	}

	fmt.Fprintf(&sb, "Current round: %d/%d, Questions asked so far: %d/%d\n", round, maxRounds, accumulated, maxTotal)
	fmt.Fprintf(&sb, "Select up to %d most important questions, merge duplicates.\n", maxPerRound)
	sb.WriteString("If the question is clear enough or more clarification would not significantly improve the answer, set 'enough': true.\n")
	sb.WriteString("Return ONLY JSON: {\"questions\": [{\"id\": \"q1\", \"text\": \"...\"}, ...], \"enough\": false}\n")
	fmt.Fprintf(&sb, "Use sequential IDs starting from q%d.", accumulated+1)

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildAugmentedQuery builds the full query passed to RunFull when clarification history exists.
func BuildAugmentedQuery(query string, history []ClarificationRound) string {
	if len(history) == 0 {
		return query
	}

	// Check if any round has at least one non-empty answer
	hasAnswers := false
	for _, r := range history {
		for _, a := range r.Answers {
			if a.Text != "" {
				hasAnswers = true
				break
			}
		}
		if hasAnswers {
			break
		}
	}
	if !hasAnswers {
		return query
	}

	var sb strings.Builder
	sb.WriteString(query)
	sb.WriteString("\n\n## User clarifications\n")

	for _, r := range history {
		if len(r.Answers) == 0 {
			continue
		}
		// Check if this round has any non-empty answer
		roundHasAnswers := false
		for _, a := range r.Answers {
			if a.Text != "" {
				roundHasAnswers = true
				break
			}
		}
		if !roundHasAnswers {
			continue
		}
		for _, q := range r.Questions {
			answer := "(no answer)"
			for _, a := range r.Answers {
				if a.ID == q.ID {
					if a.Text != "" {
						answer = a.Text
					}
					break
				}
			}
			sb.WriteString("Q: ")
			sb.WriteString(q.Text)
			sb.WriteString("\nA: ")
			sb.WriteString(answer)
			sb.WriteString("\n\n")
		}
	}

	return sb.String()
}

// BuildStage1Prompt returns the messages for a Stage 1 generation request.
func BuildStage1Prompt(query string) []ChatMessage {
	return []ChatMessage{
		{Role: "user", Content: query},
	}
}

// BuildStage2Prompt returns the messages for a Stage 2 peer-review request.
// labeledResponses maps anonymous label → response text (e.g. "Response A" → "...").
// The prompt requests JSON output with schema {"rankings": ["Response X", ...]}.
func BuildStage2Prompt(query string, labeledResponses map[string]string) []ChatMessage {
	// Sort labels for a deterministic, readable prompt.
	labels := make([]string, 0, len(labeledResponses))
	for l := range labeledResponses {
		labels = append(labels, l)
	}
	sort.Strings(labels)

	var sb strings.Builder
	sb.WriteString("You were asked the following question:\n\n")
	sb.WriteString(query)
	sb.WriteString("\n\nHere are the responses given by different assistants:\n\n")
	for _, label := range labels {
		sb.WriteString("## ")
		sb.WriteString(label)
		sb.WriteString("\n")
		sb.WriteString(labeledResponses[label])
		sb.WriteString("\n\n")
	}
	sb.WriteString("Rank these responses from best to worst based on accuracy, clarity, and completeness.\n")
	sb.WriteString("Return ONLY a JSON object with this exact schema — no additional text:\n")
	sb.WriteString(`{"rankings": ["Response X", "Response Y", ...]}`)
	sb.WriteString("\n\nList all response labels in order from best (first) to worst (last).")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildStage3Prompt returns the messages for the Stage 3 Chairman synthesis request.
// labeledResponses contains the Stage 1 candidate answers (label → content).
// Rankings are built from Go structs — Stage 2 reviewer prose is never passed through,
// preventing prompt injection from Stage 2 model output.
// Kendall's W drives the synthesis guidance injected into the prompt.
func BuildStage3Prompt(query string, rankings []StageTwoResult, labelToModel map[string]string, consensusW float64, labeledResponses map[string]string) []ChatMessage {
	var guidance string
	switch {
	case consensusW >= 0.70:
		guidance = fmt.Sprintf(
			"The peer reviewers reached strong consensus (W=%.2f). "+
				"Synthesize the responses confidently, drawing on the most highly-ranked insights.",
			consensusW,
		)
	case consensusW >= 0.40:
		guidance = fmt.Sprintf(
			"The peer reviewers reached moderate consensus (W=%.2f). "+
				"Synthesize the best insights while acknowledging where reviewers diverged.",
			consensusW,
		)
	default:
		guidance = fmt.Sprintf(
			"The peer reviewers did not reach consensus (W=%.2f). "+
				"Present the main perspectives fairly, surface well-reasoned minority views, "+
				"and help the user understand the range of expert opinion.",
			consensusW,
		)
	}

	var sb strings.Builder
	sb.WriteString("You were asked to answer:\n\n")
	sb.WriteString(query)

	// Include Stage 1 candidate responses so the Chairman can synthesize their content.
	if len(labeledResponses) > 0 {
		labels := make([]string, 0, len(labeledResponses))
		for l := range labeledResponses {
			labels = append(labels, l)
		}
		sort.Strings(labels)
		sb.WriteString("\n\nCandidate responses:\n")
		for _, label := range labels {
			sb.WriteString("\n## ")
			sb.WriteString(label)
			sb.WriteString("\n")
			sb.WriteString(labeledResponses[label])
		}
	}

	sb.WriteString("\n\n")
	sb.WriteString(guidance)
	sb.WriteString("\n\nPeer review rankings (structured attribution — best to worst):\n")

	for _, r := range rankings {
		if len(r.Rankings) == 0 {
			continue
		}
		sb.WriteString("\nReviewer ")
		sb.WriteString(r.ReviewerLabel)
		sb.WriteString(":\n")
		for i, label := range r.Rankings {
			model := labelToModel[label]
			fmt.Fprintf(&sb, "  %d. %s (%s)\n", i+1, label, model)
		}
	}

	sb.WriteString("\nProvide a comprehensive, well-reasoned answer that synthesizes the best insights from all responses.")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildRoleStage1Prompt returns messages for a role participant.
// The system message carries the role instruction; the user message carries the query.
func BuildRoleStage1Prompt(role Role, query string) []ChatMessage {
	return []ChatMessage{
		{Role: "system", Content: role.Instruction},
		{Role: "user", Content: query},
	}
}

// BuildRoleChairmanPrompt returns messages for the chairman to synthesise role findings.
// Each role's findings appear in a labelled section. The chairman produces the final review.
func BuildRoleChairmanPrompt(query string, results []StageOneResult) []ChatMessage {
	var sb strings.Builder
	sb.WriteString("You are the lead reviewer. Synthesise the findings below into a clear, ")
	sb.WriteString("prioritised review. Remove duplicates. Group by file. Order by severity ")
	sb.WriteString("(critical → high → medium → low). Note which role(s) flagged each issue.\n\n")
	sb.WriteString("ORIGINAL QUERY:\n")
	sb.WriteString(query)
	sb.WriteString("\n\n")

	for _, r := range results {
		sb.WriteString("=== ")
		sb.WriteString(r.Label)
		sb.WriteString(" REVIEWER FINDINGS ===\n")
		sb.WriteString(r.Content)
		sb.WriteString("\n\n")
	}

	sb.WriteString("Write your synthesised review in Markdown. ")
	sb.WriteString("If there are no findings across all reviewers, state that explicitly.")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildMajorityPolishPrompt asks the chairman to polish the winning answer
// from the Majority strategy. The chairman MUST refine prose only — it must
// not change the substance of the answer the council voted for.
//
// Discriminator prefix: "You polish the council's winning answer." — used
// by tests to classify the call as a polish call vs. a tiebreak call.
func BuildMajorityPolishPrompt(query string, winner string) []ChatMessage {
	var sb strings.Builder
	sb.WriteString("You polish the council's winning answer. ")
	sb.WriteString("The council of LLMs voted on the question below; the answer with the most votes is given.\n\n")
	sb.WriteString("Question: ")
	sb.WriteString(query)
	sb.WriteString("\n\nWinning answer:\n")
	sb.WriteString(winner)
	sb.WriteString("\n\n")
	sb.WriteString("Polish the winning answer for clarity, grammar, and prose quality. ")
	sb.WriteString("DO NOT change the substance of the answer. ")
	sb.WriteString("If the answer is already clear, return it verbatim. ")
	sb.WriteString("Reply with the polished answer only — no preamble, no commentary.")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildMajorityTiebreakPrompt asks the chairman to break a tie among the
// top-voted clusters in the Majority strategy. Each tied cluster's
// representative content is shown; the chairman picks one as the final answer.
//
// Discriminator prefix: "You arbitrate a tie among the council's top answers."
// — used by tests to classify the call as a tiebreak call.
func BuildMajorityTiebreakPrompt(query string, tied []VoteCluster) []ChatMessage {
	var sb strings.Builder
	sb.WriteString("You arbitrate a tie among the council's top answers. ")
	sb.WriteString("The council of LLMs voted on the question below; ")
	fmt.Fprintf(&sb, "%d answers tied for the most votes.\n\n", len(tied))
	sb.WriteString("Question: ")
	sb.WriteString(query)
	sb.WriteString("\n\nTied answers:\n")
	for i, cl := range tied {
		fmt.Fprintf(&sb, "\n[Candidate %d] (%d votes):\n", i+1, cl.Votes)
		sb.WriteString(cl.Representative)
		sb.WriteString("\n")
	}
	sb.WriteString("\nPick the answer that best addresses the question. ")
	sb.WriteString("If you must blend, blend conservatively — prefer one of the candidates over a synthesis. ")
	sb.WriteString("Reply with the chosen answer only — no preamble, no commentary on the tiebreak process.")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildRankPrompt asks the GenerateRankRefine arbiter to score each candidate
// answer against a fixed set of criteria. Each criterion is scored on [0.0, 1.0];
// total_score is the sum across criteria.
//
// Discriminator prefix: "You rank council answers." — used by tests to classify
// the call as the rank step (vs the refine step's "You refine the top-K").
func BuildRankPrompt(query string, candidates []StageOneResult, criteria []string, k int) []ChatMessage {
	var sb strings.Builder
	sb.WriteString("You rank council answers. ")
	fmt.Fprintf(&sb, "%d candidate answers were generated independently for the question below; ", len(candidates))
	fmt.Fprintf(&sb, "score each one on the criteria below, then identify the top %d that should advance to refinement.\n\n", k)
	sb.WriteString("Question: ")
	sb.WriteString(query)
	sb.WriteString("\n\nCriteria (each scored 0.0–1.0):\n")
	for _, c := range criteria {
		fmt.Fprintf(&sb, "- %s\n", c)
	}
	sb.WriteString("\nCandidates:\n")
	for _, cand := range candidates {
		fmt.Fprintf(&sb, "\n[%s]\n%s\n", cand.Label, cand.Content)
	}
	sb.WriteString("\nReturn ONLY a JSON object with this shape:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n  \"rankings\": [\n    {\n")
	sb.WriteString("      \"label\": \"<exact label from above>\",\n")
	sb.WriteString("      \"scores\": { ")
	for i, c := range criteria {
		if i > 0 {
			sb.WriteString(", ")
		}
		fmt.Fprintf(&sb, "\"%s\": <0.0..1.0>", c)
	}
	sb.WriteString(" },\n")
	sb.WriteString("      \"total_score\": <sum of scores>\n")
	sb.WriteString("    }\n  ]\n}\n```\n")
	sb.WriteString("Score every candidate. Use the exact label string. ")
	sb.WriteString("Be discriminating — spread scores across the range so the top-K cut is meaningful.")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildDebateRoundPrompt asks a single debater to critique the OTHER
// debaters' previous-round answers and produce a revised answer of their own.
//
// Anonymisation contract: `others` shows OTHER debaters' content with labels
// only — model names MUST NOT appear in the body. `selfPrev` is the
// debater's own previous-round output (round-0 answer in round 1; prior
// revision in subsequent rounds) so the model can revise rather than start
// from scratch.
//
// Discriminator prefix: "You debate council answers." — used by tests to
// classify the call as a debate round.
func BuildDebateRoundPrompt(query string, selfPrev DebaterRevision, others []DebaterRevision, round, totalRounds int) []ChatMessage {
	var sb strings.Builder
	sb.WriteString("You debate council answers. ")
	fmt.Fprintf(&sb, "This is round %d of %d in a multi-agent debate.\n\n", round, totalRounds)
	sb.WriteString("Question: ")
	sb.WriteString(query)
	sb.WriteString("\n\n")

	sb.WriteString("Your previous-round answer (revise it, don't start from scratch):\n")
	sb.WriteString(selfPrev.Content)
	sb.WriteString("\n\n")

	sb.WriteString("Other debaters' previous-round answers (anonymous; you don't know which model wrote which):\n")
	if len(others) == 0 {
		sb.WriteString("(no other debaters in this round)\n")
	} else {
		for _, o := range others {
			fmt.Fprintf(&sb, "\n[%s]\n%s\n", o.Label, o.Content)
		}
	}

	sb.WriteString("\nProduce a JSON object with this shape:\n")
	sb.WriteString("```json\n")
	sb.WriteString("{\n  \"critique\": \"<your critique of the other debaters' answers — what they got right, what they got wrong>\",\n")
	sb.WriteString("  \"revision\": \"<your revised answer to the question, taking the others' arguments into account>\"\n}\n")
	sb.WriteString("```\n")
	sb.WriteString("Be specific in critique. Be substantive in revision — don't just defend your previous answer if the others raised valid points; revise. ")
	sb.WriteString("If you genuinely think the others are wrong on every point, your revision can match your previous answer, but explain why in critique.")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildDebateChairmanPrompt asks the chairman to synthesise a final answer
// from the full debate transcript: round-0 initial answers (from Stage 1)
// plus all subsequent rounds' revisions, plus any dropout markers.
//
// The chairman receives the LabelToModel map so it can attribute model
// provenance in its synthesis. Dropouts are surfaced explicitly so the
// chairman can reason about an evolving cast.
func BuildDebateChairmanPrompt(query string, stage1 []StageOneResult, debate *Debate, labelToModel map[string]string) []ChatMessage {
	var sb strings.Builder
	sb.WriteString("You synthesise the final answer from a multi-agent debate. ")
	fmt.Fprintf(&sb, "%d debaters argued the question below across %d rounds.\n\n", len(stage1), debate.FinalRound)

	sb.WriteString("Question: ")
	sb.WriteString(query)
	sb.WriteString("\n\n")

	sb.WriteString("Round 0 (initial answers):\n")
	for _, s1 := range stage1 {
		modelName := labelToModel[s1.Label]
		if modelName == "" {
			modelName = s1.Model
		}
		fmt.Fprintf(&sb, "\n[%s — %s]\n%s\n", s1.Label, modelName, s1.Content)
	}

	for _, round := range debate.Rounds {
		fmt.Fprintf(&sb, "\nRound %d:\n", round.Round)
		for _, rev := range round.Revisions {
			modelName := labelToModel[rev.Label]
			if modelName == "" {
				modelName = rev.Model
			}
			if rev.Critique != "" {
				fmt.Fprintf(&sb, "\n[%s — %s] Critique: %s\n", rev.Label, modelName, rev.Critique)
			}
			fmt.Fprintf(&sb, "\n[%s — %s] Revision: %s\n", rev.Label, modelName, rev.Content)
		}
	}

	if len(debate.Dropouts) > 0 {
		sb.WriteString("\nDropouts (debaters who stopped revising mid-debate):\n")
		for _, d := range debate.Dropouts {
			fmt.Fprintf(&sb, "- [%s] dropped after round %d (reason: %s)\n", d.Label, d.LastRound, d.Reason)
		}
	}

	sb.WriteString("\nSynthesise the final answer. Use the strongest threads from each debater's arguments — don't just copy one position. ")
	sb.WriteString("If the debaters converged on a position, take that as a strong signal. ")
	sb.WriteString("If they diverged, weigh the critiques and pick the most defensible synthesis. ")
	sb.WriteString("Reply with the final answer only — no preamble, no commentary on the debate process.")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}

// BuildRankRefinePrompt asks the GenerateRankRefine refiner (the chairman) to
// produce a final answer from the top-K advancing candidates. The instruction
// emphasises picking strong threads over averaging — bland blends are the
// failure mode this strategy is most prone to.
//
// Discriminator prefix: "You refine the top-K council answers." — used by
// tests to classify the call as the refine step.
func BuildRankRefinePrompt(query string, advancing []StageOneResult, criteria []string) []ChatMessage {
	var sb strings.Builder
	sb.WriteString("You refine the top-K council answers. ")
	fmt.Fprintf(&sb, "An arbiter ranked %d council answers for the question below against these criteria: %s. ", len(advancing), strings.Join(criteria, ", "))
	sb.WriteString("The top candidates are shown below; produce one refined final answer.\n\n")
	sb.WriteString("Question: ")
	sb.WriteString(query)
	sb.WriteString("\n\nTop candidates:\n")
	for i, cand := range advancing {
		fmt.Fprintf(&sb, "\n[Candidate %d] (%s)\n%s\n", i+1, cand.Label, cand.Content)
	}
	sb.WriteString("\nRefine — do NOT produce a bland blend or averaged synthesis. ")
	sb.WriteString("Pick the strongest threads from each candidate and weave them into one answer that is more accurate, clearer, and more complete than any individual candidate. ")
	sb.WriteString("If one candidate is clearly best on every criterion, prefer it over an unnecessary synthesis. ")
	sb.WriteString("Reply with the refined answer only — no preamble, no commentary on the ranking process.")

	return []ChatMessage{
		{Role: "user", Content: sb.String()},
	}
}
