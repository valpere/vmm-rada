import { useState } from 'react';
import './Stage2.css';

function modelShortName(model) {
  return model.split('/')[1] || model;
}

function consensusLabel(w) {
  if (w >= 0.70) return 'strong';
  if (w >= 0.40) return 'moderate';
  return 'weak';
}

// PeerRankingView renders the 3-tab ranking + aggregate panel for the
// PeerReview strategy. Unchanged from the pre-dispatcher Stage 2 body.
function PeerRankingView({ rankings, labelToModel, aggregateRankings, consensusW, isLoading }) {
  const [expanded, setExpanded] = useState(false);
  const [activeTab, setActiveTab] = useState(0);

  if (!isLoading && (!rankings || rankings.length === 0)) {
    return null;
  }

  const hasConsensus = consensusW != null && consensusW > 0;
  const label = hasConsensus ? consensusLabel(consensusW) : null;

  return (
    <div className="stage stage2">
      <button
        className="stage-accordion"
        onClick={() => setExpanded((e) => !e)}
        aria-expanded={expanded}
      >
        <span className="stage-accordion-label">
          {isLoading ? (
            <>
              <span className="spinner-sm" />
              Running peer rankings…
            </>
          ) : (
            <>
              Stage 2: Peer Rankings
              {hasConsensus && (
                <span className={`consensus-pill consensus-${label}`}>
                  W={consensusW.toFixed(2)} {label}
                </span>
              )}
            </>
          )}
        </span>
        {!isLoading && (
          <span className="stage-accordion-chevron">{expanded ? '▲' : '▼'}</span>
        )}
      </button>

      {expanded && rankings && rankings.length > 0 && (
        <div className="stage-body">
          <div className="tabs">
            {rankings.map((rank, index) => {
              const reviewerModel = labelToModel?.[rank.reviewer_label] ?? rank.reviewer_label;
              return (
                <button
                  key={index}
                  className={`tab${activeTab === index ? ' active' : ''}`}
                  onClick={() => setActiveTab(index)}
                >
                  {modelShortName(reviewerModel)}
                </button>
              );
            })}
          </div>

          <div className="tab-content">
            <div className="model-name">
              {labelToModel?.[rankings[activeTab].reviewer_label] ?? rankings[activeTab].reviewer_label}
            </div>
            {rankings[activeTab].rankings && rankings[activeTab].rankings.length > 0 ? (
              <div className="parsed-ranking">
                <strong>Ranking (best → worst):</strong>
                <ol>
                  {rankings[activeTab].rankings.map((lbl, i) => (
                    <li key={i}>
                      {labelToModel?.[lbl] ? modelShortName(labelToModel[lbl]) : lbl}
                    </li>
                  ))}
                </ol>
              </div>
            ) : (
              <p className="ranking-missing">No rankings submitted by this reviewer.</p>
            )}
          </div>

          {aggregateRankings && aggregateRankings.length > 0 && (
            <div className="aggregate-rankings">
              <div className="aggregate-title">Aggregate Rankings</div>
              <div className="aggregate-list">
                {aggregateRankings.map((agg, index) => (
                  <div key={index} className="aggregate-item">
                    <span className="rank-position">#{index + 1}</span>
                    <span className="rank-model">{modelShortName(agg.model)}</span>
                    <span className="rank-score">{agg.score.toFixed(2)}</span>
                  </div>
                ))}
              </div>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// VoteTallyView renders the Majority strategy's Stage 2 payload as a vertical
// list of clusters, each with a horizontal bar proportional to vote count.
// The winning cluster (the first in the sorted list, by buildVoteTally
// invariant) is highlighted with an accent border and ✓ marker. Long cluster
// representatives are truncated to two lines with click-to-expand.
const REPRESENTATIVE_TRUNCATE_THRESHOLD = 140;

function VoteCluster({ cluster, isWinner, totalVotes }) {
  const [expanded, setExpanded] = useState(false);
  const ratio = totalVotes > 0 ? cluster.votes / totalVotes : 0;
  const widthPct = Math.max(2, Math.round(ratio * 100));
  const longText = (cluster.representative ?? '').length > REPRESENTATIVE_TRUNCATE_THRESHOLD;

  return (
    <div className={`vote-cluster${isWinner ? ' winner' : ''}`}>
      <div className="vote-cluster-header">
        {isWinner && <span className="vote-winner-mark" aria-label="winner">✓</span>}
        <div className="vote-bar-track" aria-hidden="true">
          <div className="vote-bar-fill" style={{ width: `${widthPct}%` }} />
        </div>
        <div className="vote-count">
          {cluster.votes} {cluster.votes === 1 ? 'vote' : 'votes'}
        </div>
      </div>
      <div className={`vote-representative${longText && !expanded ? ' collapsed' : ''}`}>
        {cluster.representative}
      </div>
      {longText && (
        <button
          type="button"
          className="vote-expand-btn"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
        >
          {expanded ? 'Show less' : 'Show full answer'}
        </button>
      )}
    </div>
  );
}

function VoteTallyView({ voteTally, isLoading }) {
  if (isLoading) {
    return (
      <div className="stage stage2">
        <div className="stage-accordion" aria-disabled="true">
          <span className="stage-accordion-label">
            <span className="spinner-sm" />
            Tallying votes…
          </span>
        </div>
      </div>
    );
  }
  if (!voteTally || !voteTally.clusters || voteTally.clusters.length === 0) {
    return null;
  }
  const totalVotes = voteTally.clusters.reduce((sum, c) => sum + (c.votes ?? 0), 0);
  const winnerLabel = voteTally.winner_label;

  return (
    <div className="stage stage2">
      <div className="stage-accordion" aria-disabled="true">
        <span className="stage-accordion-label">Stage 2: Vote Tally</span>
      </div>
      <div className="stage-body">
        <div className="vote-tally">
          {voteTally.clusters.map((cluster, index) => {
            const isWinner =
              index === 0 || (winnerLabel && cluster.members?.includes(winnerLabel));
            return (
              <VoteCluster
                key={index}
                cluster={cluster}
                isWinner={Boolean(isWinner) && index === 0}
                totalVotes={totalVotes}
              />
            );
          })}
        </div>
      </div>
    </div>
  );
}

// RankRefineView renders the GenerateRankRefine strategy's Stage 2 payload:
// a vertical list of ranked candidates, each row showing the label, total
// score, and a horizontal bar per criterion (4 mini-bars per row, each
// 0.0–1.0). Top-K rows have an "Advancing to refinement" badge + accent
// border. Long candidate content is truncated with click-to-expand, same
// pattern as VoteTallyView.
function RankRefineCandidate({ candidate, criteria, candidateContent }) {
  const [expanded, setExpanded] = useState(false);
  const longText = (candidateContent ?? '').length > REPRESENTATIVE_TRUNCATE_THRESHOLD;
  const total = candidate.total_score?.toFixed?.(2) ?? '0.00';

  return (
    <div className={`rank-candidate${candidate.advancing ? ' advancing' : ''}`}>
      <div className="rank-candidate-header">
        <span className="rank-candidate-label">{candidate.label}</span>
        {candidate.advancing && (
          <span className="rank-advancing-badge" aria-label="advancing to refinement">
            ↑ advancing
          </span>
        )}
        <span className="rank-total-score">total {total}</span>
      </div>
      <div className="rank-criteria-bars">
        {criteria.map((name) => {
          const score = candidate.scores?.[name] ?? 0;
          const widthPct = Math.max(2, Math.round(score * 100));
          return (
            <div className="rank-criterion-row" key={name}>
              <span className="rank-criterion-name">{name}</span>
              <div className="rank-criterion-track" aria-hidden="true">
                <div className="rank-criterion-fill" style={{ width: `${widthPct}%` }} />
              </div>
              <span className="rank-criterion-score">{score.toFixed(2)}</span>
            </div>
          );
        })}
      </div>
      {candidateContent && (
        <>
          <div className={`rank-candidate-content${longText && !expanded ? ' collapsed' : ''}`}>
            {candidateContent}
          </div>
          {longText && (
            <button
              type="button"
              className="vote-expand-btn"
              onClick={() => setExpanded((v) => !v)}
              aria-expanded={expanded}
            >
              {expanded ? 'Show less' : 'Show full answer'}
            </button>
          )}
        </>
      )}
    </div>
  );
}

function RankRefineView({ rankRefine, rankings: stage1Rankings, isLoading }) {
  if (isLoading) {
    return (
      <div className="stage stage2">
        <div className="stage-accordion" aria-disabled="true">
          <span className="stage-accordion-label">
            <span className="spinner-sm" />
            Ranking candidates…
          </span>
        </div>
      </div>
    );
  }
  if (!rankRefine || !rankRefine.rankings || rankRefine.rankings.length === 0) {
    return null;
  }
  // Stage 1 results are passed through `rankings` for content lookup by label.
  const contentByLabel = {};
  if (Array.isArray(stage1Rankings)) {
    for (const r of stage1Rankings) {
      if (r?.label) contentByLabel[r.label] = r.content ?? '';
    }
  }
  const criteria = Array.isArray(rankRefine.criteria) ? rankRefine.criteria : [];

  return (
    <div className="stage stage2">
      <div className="stage-accordion" aria-disabled="true">
        <span className="stage-accordion-label">
          Stage 2: Rank &amp; Refine ({rankRefine.top_k ?? 0} advancing of {rankRefine.rankings.length})
        </span>
      </div>
      <div className="stage-body">
        <div className="rank-refine-list">
          {rankRefine.rankings.map((c, i) => (
            <RankRefineCandidate
              key={`${c.label}-${i}`}
              candidate={c}
              criteria={criteria}
              candidateContent={contentByLabel[c.label]}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

// DebateView renders the MultiAgentDebate strategy's Stage 2 payload as a
// vertical timeline: one section per round, with each surviving debater's
// critique + revision shown as a row, plus dropout markers in muted style.
// Long content truncated with click-to-expand (same pattern as
// VoteTallyView and RankRefineView).
function DebaterRow({ revision, isDropout }) {
  const [expanded, setExpanded] = useState(false);
  const longText = (revision?.content ?? '').length > REPRESENTATIVE_TRUNCATE_THRESHOLD;

  if (isDropout) {
    return (
      <div className="debate-row dropout" title={`reason: ${revision.reason}`}>
        <span className="debate-row-label">{revision.label}</span>
        <span className="debate-dropout-mark">
          ✗ dropped after round {revision.last_round} ({revision.reason})
        </span>
      </div>
    );
  }

  return (
    <div className="debate-row">
      <div className="debate-row-header">
        <span className="debate-row-label">{revision.label}</span>
      </div>
      {revision.critique && (
        <div className="debate-critique">
          <span className="debate-critique-label">Critique:</span>
          <span className="debate-critique-text">{revision.critique}</span>
        </div>
      )}
      <div className={`debate-revision${longText && !expanded ? ' collapsed' : ''}`}>
        {revision.content}
      </div>
      {longText && (
        <button
          type="button"
          className="vote-expand-btn"
          onClick={() => setExpanded((v) => !v)}
          aria-expanded={expanded}
        >
          {expanded ? 'Show less' : 'Show full answer'}
        </button>
      )}
    </div>
  );
}

function DebateView({ debate, isLoading }) {
  if (isLoading && (!debate || !debate.rounds || debate.rounds.length === 0)) {
    return (
      <div className="stage stage2">
        <div className="stage-accordion" aria-disabled="true">
          <span className="stage-accordion-label">
            <span className="spinner-sm" />
            Debate in progress…
          </span>
        </div>
      </div>
    );
  }
  if (!debate || !debate.rounds || debate.rounds.length === 0) {
    return null;
  }

  // Build per-round dropout map: which debaters dropped at the END of round N
  // (i.e., LastRound = N, and they no longer appear in round N+1).
  const dropoutsByRound = {};
  if (Array.isArray(debate.dropouts)) {
    for (const d of debate.dropouts) {
      const r = d.last_round;
      if (!dropoutsByRound[r]) dropoutsByRound[r] = [];
      dropoutsByRound[r].push(d);
    }
  }

  const finalRound = debate.final_round ?? debate.rounds.length;

  return (
    <div className="stage stage2">
      <div className="stage-accordion" aria-disabled="true">
        <span className="stage-accordion-label">
          Stage 2: Debate ({finalRound} {finalRound === 1 ? 'round' : 'rounds'})
        </span>
      </div>
      <div className="stage-body">
        <div className="debate-timeline">
          {debate.rounds.map((round) => {
            // Dropouts that occurred BEFORE this round (LastRound = round - 1).
            const before = dropoutsByRound[round.round - 1] || [];
            return (
              <div className="debate-round" key={round.round}>
                <div className="debate-round-header">Round {round.round}</div>
                {before.map((d) => (
                  <DebaterRow key={`drop-${d.label}-${round.round}`} revision={d} isDropout />
                ))}
                {round.revisions.map((rev, i) => (
                  <DebaterRow key={`${rev.label}-${round.round}-${i}`} revision={rev} />
                ))}
              </div>
            );
          })}
        </div>
      </div>
    </div>
  );
}

// RoleStubView renders a minimal placeholder for the RoleBased strategy,
// where Stage 2 has no peer-ranking content (roles are complementary).
function RoleStubView({ isLoading }) {
  if (isLoading) {
    return (
      <div className="stage stage2">
        <div className="stage-accordion" aria-disabled="true">
          <span className="stage-accordion-label">
            <span className="spinner-sm" />
            Stage 2…
          </span>
        </div>
      </div>
    );
  }
  return (
    <div className="stage stage2">
      <div className="stage-accordion" aria-disabled="true">
        <span className="stage-accordion-label">
          Stage 2: roles are complementary — no peer ranking.
        </span>
      </div>
    </div>
  );
}

// UnknownKindView is the safety net for strategy kinds the frontend doesn't
// know how to render yet. It surfaces the kind name so the gap is visible
// in development without crashing the UI.
function UnknownKindView({ kind }) {
  return (
    <div className="stage stage2">
      <div className="stage-accordion" aria-disabled="true">
        <span className="stage-accordion-label">
          Stage 2 — kind: <code>{kind}</code> (view not implemented yet)
        </span>
      </div>
    </div>
  );
}

// Stage2 dispatches to the right sub-renderer based on `kind`. The dispatcher
// is the only public component; the views above are private to this module.
//
// `kind` propagates from the SSE stage2_complete event (or, for replayed
// historical conversations, is derived in App.jsx). When it is null /
// undefined / empty / whitespace-only (e.g. an older backend that doesn't
// emit kind, or a malformed event), we default to peer_ranking because that
// was the only persisted Stage 2 shape before this PR.
export default function Stage2({
  kind,
  rankings,
  labelToModel,
  aggregateRankings,
  consensusW,
  voteTally,
  rankRefine,
  debate,
  stage1,
  isLoading,
}) {
  const trimmed = typeof kind === 'string' ? kind.trim() : '';
  const effectiveKind = trimmed || 'peer_ranking';

  switch (effectiveKind) {
    case 'peer_ranking':
      return (
        <PeerRankingView
          rankings={rankings}
          labelToModel={labelToModel}
          aggregateRankings={aggregateRankings}
          consensusW={consensusW}
          isLoading={isLoading}
        />
      );
    case 'role_stub':
      return <RoleStubView isLoading={isLoading} />;
    case 'vote_tally':
      return <VoteTallyView voteTally={voteTally} isLoading={isLoading} />;
    case 'rank_refine':
      return <RankRefineView rankRefine={rankRefine} rankings={stage1} isLoading={isLoading} />;
    case 'debate_round':
      return <DebateView debate={debate} isLoading={isLoading} />;
    default:
      return <UnknownKindView kind={effectiveKind} />;
  }
}
