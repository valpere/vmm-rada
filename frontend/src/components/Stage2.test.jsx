// Tests for Stage2.jsx — the dispatcher routes by `kind` to one of three
// sub-renderers (PeerRankingView, RoleStubView, UnknownKindView). These tests
// drive each branch without mounting the rest of App.

import { render, screen } from '@testing-library/react';
import Stage2 from './Stage2';

describe('Stage2 dispatcher', () => {
  it('routes kind="peer_ranking" to the peer-ranking view', () => {
    render(
      <Stage2
        kind="peer_ranking"
        rankings={[
          { reviewer_label: 'Response A', rankings: ['Response A', 'Response B'] },
        ]}
        labelToModel={{ 'Response A': 'openai/gpt-4o', 'Response B': 'anthropic/claude-haiku-4-5' }}
        aggregateRankings={[
          { model: 'openai/gpt-4o', score: 1.5 },
          { model: 'anthropic/claude-haiku-4-5', score: 2.5 },
        ]}
        consensusW={0.8}
        isLoading={false}
      />,
    );
    // Peer-ranking view renders the strong-consensus pill.
    expect(screen.getByText(/Stage 2: Peer Rankings/i)).toBeInTheDocument();
    expect(screen.getByText(/W=0.80/i)).toBeInTheDocument();
  });

  it('routes kind="role_stub" to the role-stub view', () => {
    render(<Stage2 kind="role_stub" isLoading={false} />);
    expect(
      screen.getByText(/roles are complementary — no peer ranking/i),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Stage 2: Peer Rankings/i)).not.toBeInTheDocument();
  });

  it('routes an unknown kind to the unknown-kind view, surfacing the kind name', () => {
    // Use one of the still-unimplemented reserved kinds. moa_aggregator and
    // delphi_round are reserved as of this PR; debate_round and vote_tally
    // are shipped, so they'd route to their proper views.
    render(<Stage2 kind="moa_aggregator" isLoading={false} />);
    expect(screen.getByText(/view not implemented yet/i)).toBeInTheDocument();
    expect(screen.getByText('moa_aggregator')).toBeInTheDocument();
  });

  it('defaults to peer_ranking when kind is undefined (old-backend safety net)', () => {
    render(
      <Stage2
        rankings={[
          { reviewer_label: 'Response A', rankings: ['Response A'] },
        ]}
        labelToModel={{ 'Response A': 'openai/gpt-4o' }}
        aggregateRankings={[]}
        consensusW={0.0}
        isLoading={false}
      />,
    );
    // Falls through to PeerRankingView even though kind is undefined.
    expect(screen.getByText(/Stage 2: Peer Rankings/i)).toBeInTheDocument();
  });

  describe('VoteTallyView (kind="vote_tally")', () => {
    it('renders all clusters with the winner highlighted', () => {
      render(
        <Stage2
          kind="vote_tally"
          voteTally={{
            winner_label: 'Response A',
            clusters: [
              { members: ['Response A', 'Response B'], representative: 'yes', votes: 2 },
              { members: ['Response C'], representative: 'no', votes: 1 },
              { members: ['Response D'], representative: 'maybe', votes: 1 },
            ],
          }}
          isLoading={false}
        />,
      );
      expect(screen.getByText(/Stage 2: Vote Tally/i)).toBeInTheDocument();
      // Winner cluster shows the ✓ marker.
      expect(screen.getByLabelText(/winner/i)).toBeInTheDocument();
      // Vote counts visible.
      expect(screen.getByText(/2 votes/i)).toBeInTheDocument();
      // 'maybe' and 'no' both at 1 vote — getAllByText handles plural.
      expect(screen.getAllByText(/1 vote(?!s)/i).length).toBeGreaterThanOrEqual(2);
      // All three representatives present.
      expect(screen.getByText('yes')).toBeInTheDocument();
      expect(screen.getByText('no')).toBeInTheDocument();
      expect(screen.getByText('maybe')).toBeInTheDocument();
    });

    it('renders a unanimous (single-cluster) tally', () => {
      render(
        <Stage2
          kind="vote_tally"
          voteTally={{
            winner_label: 'Response A',
            clusters: [
              { members: ['Response A', 'Response B', 'Response C'], representative: '42', votes: 3 },
            ],
          }}
          isLoading={false}
        />,
      );
      expect(screen.getByText(/Stage 2: Vote Tally/i)).toBeInTheDocument();
      expect(screen.getByText(/3 votes/i)).toBeInTheDocument();
      expect(screen.getByText('42')).toBeInTheDocument();
      // Exactly one winner marker on a unanimous tally.
      expect(screen.getAllByLabelText(/winner/i)).toHaveLength(1);
    });

    it('truncates long representative content with a Show full answer button', () => {
      const longText =
        'This is a long representative answer that exceeds the truncation threshold so the dispatcher exposes a Show full answer button — '.repeat(2);
      render(
        <Stage2
          kind="vote_tally"
          voteTally={{
            winner_label: 'Response A',
            clusters: [{ members: ['Response A'], representative: longText, votes: 3 }],
          }}
          isLoading={false}
        />,
      );
      expect(screen.getByRole('button', { name: /Show full answer/i })).toBeInTheDocument();
    });
  });

  describe('RankRefineView (kind="rank_refine")', () => {
    const fullRankRefine = {
      top_k: 3,
      criteria: ['correctness', 'clarity', 'completeness', 'originality'],
      rankings: [
        {
          label: 'Response A',
          scores: { correctness: 0.9, clarity: 0.9, completeness: 0.9, originality: 0.9 },
          total_score: 3.6,
          advancing: true,
        },
        {
          label: 'Response B',
          scores: { correctness: 0.7, clarity: 0.7, completeness: 0.7, originality: 0.7 },
          total_score: 2.8,
          advancing: true,
        },
        {
          label: 'Response C',
          scores: { correctness: 0.5, clarity: 0.5, completeness: 0.5, originality: 0.5 },
          total_score: 2.0,
          advancing: true,
        },
        {
          label: 'Response D',
          scores: { correctness: 0.3, clarity: 0.3, completeness: 0.3, originality: 0.3 },
          total_score: 1.2,
          advancing: false,
        },
        {
          label: 'Response E',
          scores: { correctness: 0.2, clarity: 0.2, completeness: 0.2, originality: 0.2 },
          total_score: 0.8,
          advancing: false,
        },
      ],
    };

    it('renders all candidates with the top-K marked advancing', () => {
      render(<Stage2 kind="rank_refine" rankRefine={fullRankRefine} isLoading={false} />);
      expect(screen.getByText(/Stage 2: Rank & Refine/i)).toBeInTheDocument();
      // 3 advancing badges (top-K).
      expect(screen.getAllByLabelText(/advancing/i)).toHaveLength(3);
      // All five labels rendered.
      for (const label of ['Response A', 'Response B', 'Response C', 'Response D', 'Response E']) {
        expect(screen.getByText(label)).toBeInTheDocument();
      }
      // Criterion names appear (capitalized in CSS but plain text in DOM).
      expect(screen.getAllByText('correctness').length).toBeGreaterThan(0);
      expect(screen.getAllByText('clarity').length).toBeGreaterThan(0);
    });

    it('renders a single advancing candidate when top-K is 1', () => {
      const oneOf = {
        top_k: 1,
        criteria: ['correctness', 'clarity', 'completeness', 'originality'],
        rankings: [
          {
            label: 'Response A',
            scores: { correctness: 1, clarity: 1, completeness: 1, originality: 1 },
            total_score: 4,
            advancing: true,
          },
          {
            label: 'Response B',
            scores: { correctness: 0.5, clarity: 0.5, completeness: 0.5, originality: 0.5 },
            total_score: 2,
            advancing: false,
          },
        ],
      };
      render(<Stage2 kind="rank_refine" rankRefine={oneOf} isLoading={false} />);
      expect(screen.getAllByLabelText(/advancing/i)).toHaveLength(1);
      // Header reports "1 advancing of 2".
      expect(screen.getByText(/1 advancing of 2/i)).toBeInTheDocument();
    });

    it('truncates long candidate content with a Show full answer button', () => {
      const longText =
        'A long candidate answer that exceeds the truncation threshold so the dispatcher exposes a Show full answer button — '.repeat(2);
      const stage1 = [
        { label: 'Response A', content: longText },
      ];
      const tally = {
        top_k: 1,
        criteria: ['correctness', 'clarity', 'completeness', 'originality'],
        rankings: [
          {
            label: 'Response A',
            scores: { correctness: 0.9, clarity: 0.9, completeness: 0.9, originality: 0.9 },
            total_score: 3.6,
            advancing: true,
          },
        ],
      };
      render(<Stage2 kind="rank_refine" rankRefine={tally} stage1={stage1} isLoading={false} />);
      expect(screen.getByRole('button', { name: /Show full answer/i })).toBeInTheDocument();
    });
  });

  describe('DebateView (kind="debate_round")', () => {
    const twoRoundDebate = {
      final_round: 2,
      rounds: [
        {
          round: 1,
          revisions: [
            { label: 'Response A', critique: 'A1 critique', content: 'A1 revision' },
            { label: 'Response B', critique: 'B1 critique', content: 'B1 revision' },
            { label: 'Response C', critique: 'C1 critique', content: 'C1 revision' },
          ],
        },
        {
          round: 2,
          revisions: [
            { label: 'Response A', critique: 'A2 critique', content: 'A2 revision' },
            { label: 'Response B', critique: 'B2 critique', content: 'B2 revision' },
            { label: 'Response C', critique: 'C2 critique', content: 'C2 revision' },
          ],
        },
      ],
    };

    it('renders rounds with all debater revisions', () => {
      render(<Stage2 kind="debate_round" debate={twoRoundDebate} isLoading={false} />);
      expect(screen.getByText(/Stage 2: Debate \(2 rounds\)/i)).toBeInTheDocument();
      // Round headers.
      expect(screen.getByText(/Round 1/i)).toBeInTheDocument();
      expect(screen.getByText(/Round 2/i)).toBeInTheDocument();
      // Both rounds × 3 debaters = 6 revisions visible.
      expect(screen.getByText('A1 revision')).toBeInTheDocument();
      expect(screen.getByText('A2 revision')).toBeInTheDocument();
      expect(screen.getByText('B1 revision')).toBeInTheDocument();
      expect(screen.getByText('C2 revision')).toBeInTheDocument();
      // Critiques visible too.
      expect(screen.getByText('A1 critique')).toBeInTheDocument();
    });

    it('appends a new round when state updates (live progressive UI)', () => {
      const oneRound = {
        final_round: 1,
        rounds: [
          {
            round: 1,
            revisions: [
              { label: 'Response A', content: 'first round answer' },
            ],
          },
        ],
      };
      const { rerender } = render(<Stage2 kind="debate_round" debate={oneRound} isLoading={false} />);
      expect(screen.getByText('first round answer')).toBeInTheDocument();
      expect(screen.queryByText('second round answer')).not.toBeInTheDocument();

      // Update to add round 2 — first round must remain visible.
      const twoRounds = {
        final_round: 2,
        rounds: [
          ...oneRound.rounds,
          {
            round: 2,
            revisions: [
              { label: 'Response A', content: 'second round answer' },
            ],
          },
        ],
      };
      rerender(<Stage2 kind="debate_round" debate={twoRounds} isLoading={false} />);
      expect(screen.getByText('first round answer')).toBeInTheDocument();
      expect(screen.getByText('second round answer')).toBeInTheDocument();
    });

    it('truncates long revision content with a Show full answer button', () => {
      const longText =
        'A long revision that exceeds the truncation threshold so the dispatcher exposes a Show full answer button — '.repeat(2);
      const debate = {
        final_round: 1,
        rounds: [
          {
            round: 1,
            revisions: [
              { label: 'Response A', critique: 'short critique', content: longText },
            ],
          },
        ],
      };
      render(<Stage2 kind="debate_round" debate={debate} isLoading={false} />);
      expect(screen.getByRole('button', { name: /Show full answer/i })).toBeInTheDocument();
    });
  });
});
