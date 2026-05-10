import { useState, useEffect } from 'react';
import Sidebar from './components/Sidebar';
import ChatInterface from './components/ChatInterface';
import { api } from './api';
import './App.css';

// Maps a persisted council_type (or a strategy-emitted Stage 2 kind) to the
// kind value the Stage2 dispatcher expects. Today only PeerReview is
// persistable, so "default" → "peer_ranking". Future strategies extend the
// switch when their registrations begin persisting. Empty / whitespace / null
// inputs all collapse to "peer_ranking" — the safe pre-this-PR default.
function deriveStage2Kind(councilType) {
  const ct = (councilType ?? '').trim();
  if (!ct) return 'peer_ranking';
  switch (ct) {
    case 'default':
      return 'peer_ranking';
    default:
      return 'peer_ranking';
  }
}

// normaliseStage2Kind treats null/undefined/empty/whitespace as missing and
// falls back to "peer_ranking". A non-empty value is returned as-is so the
// dispatcher routes to UnknownKindView when the backend emits a kind the
// frontend genuinely doesn't recognise.
function normaliseStage2Kind(raw) {
  const trimmed = (raw ?? '').trim();
  return trimmed || 'peer_ranking';
}

function App() {
  const [conversations, setConversations] = useState([]);
  const [currentConversationId, setCurrentConversationId] = useState(null);
  const [currentConversation, setCurrentConversation] = useState(null);
  const [isLoading, setIsLoading] = useState(false);
  const [theme, setTheme] = useState(() => localStorage.getItem('theme') ?? 'dark');
  const [sidebarOpen, setSidebarOpen] = useState(() => window.innerWidth >= 768);

  useEffect(() => {
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('theme', theme);
  }, [theme]);

  const toggleTheme = () => setTheme((t) => (t === 'dark' ? 'light' : 'dark'));
  const toggleSidebar = () => setSidebarOpen((o) => !o);

  const loadConversations = async () => {
    try {
      const convs = await api.listConversations();
      setConversations(convs);
    } catch (error) {
      console.error('Failed to load conversations:', error);
    }
  };

  const loadConversation = async (id) => {
    try {
      const conv = await api.getConversation(id);
      let pendingClarification = null;

      const messages = (conv.messages ?? [])
        .filter((msg) => {
          if (msg.role !== 'clarification') return true;
          const isPending =
            !msg.answers || msg.answers.length === 0 ||
            msg.answers.every((a) => !a.text);
          if (isPending) pendingClarification = { round: msg.round, questions: msg.questions };
          return false;
        })
        .map((msg) => {
          if (msg.role !== 'assistant') return msg;
          // AssistantMessage doesn't persist Kind, so derive stage2Kind from
          // metadata.council_type for replay. Falls back to "peer_ranking" for
          // missing council_type — matches the pre-this-PR rendering default.
          return {
            loading: { stage0: false, stage1: false, stage2: false, stage3: false },
            error: null,
            pendingClarification: null,
            stage2Kind: deriveStage2Kind(msg.metadata?.council_type),
            ...msg,
          };
        });

      if (pendingClarification) {
        const lastMsg = messages[messages.length - 1];
        if (lastMsg?.role === 'assistant') {
          lastMsg.pendingClarification = pendingClarification;
        } else {
          messages.push({
            role: 'assistant',
            stage1: null, stage2: null, stage2Kind: null, stage3: null, metadata: null,
            loading: { stage0: false, stage1: false, stage2: false, stage3: false },
            error: null,
            pendingClarification,
          });
        }
      }

      setCurrentConversation({ ...conv, messages, closed: conv.closed ?? false });
    } catch (error) {
      console.error('Failed to load conversation:', error);
    }
  };

  // Load conversations on mount
  useEffect(() => {
    loadConversations();
  }, []);

  // Sync currentConversation title from conversations list (updated after title_complete)
  useEffect(() => {
    if (!currentConversationId || !conversations.length) return;
    const conv = conversations.find((c) => c.id === currentConversationId);
    if (!conv?.title) return;
    setCurrentConversation((prev) => {
      if (!prev || prev.title === conv.title) return prev;
      return { ...prev, title: conv.title };
    });
  }, [conversations, currentConversationId]);

  // Load conversation details when selected
  useEffect(() => {
    if (currentConversationId) {
      loadConversation(currentConversationId);
    }
  }, [currentConversationId]);

  const handleNewConversation = async () => {
    try {
      const newConv = await api.createConversation();
      setConversations([
        { id: newConv.id, created_at: newConv.created_at, message_count: 0 },
        ...conversations,
      ]);
      setCurrentConversationId(newConv.id);
    } catch (error) {
      console.error('Failed to create conversation:', error);
    }
  };

  const handleSelectConversation = (id) => {
    setCurrentConversationId(id);
  };

  // Returns the SSE event handlers for an active stream, sharing updateLast.
  const makeStreamHandlers = (updateLast) => ({
    stage0_round_complete: (event) => {
      updateLast((msg) => {
        msg.pendingClarification = { round: event.data.round, questions: event.data.questions };
        msg.loading.stage0 = false;
        msg.loading.stage1 = false;
      });
      setIsLoading(false);
    },
    stage0_done: () => updateLast((msg) => {
      msg.pendingClarification = null;
      msg.loading.stage0 = false;
      msg.loading.stage1 = true;
    }),
    stage1_start: () => updateLast((msg) => { msg.loading.stage1 = true; }),
    stage1_complete: (event) => updateLast((msg) => {
      msg.stage1 = event.data;
      msg.loading.stage1 = false;
    }),
    stage2_start: () => updateLast((msg) => { msg.loading.stage2 = true; }),
    // Per-round events (currently only from MultiAgentDebate). Single source
    // of truth: msg.metadata.debate.rounds. Each event APPENDS its round's
    // revisions to the existing transcript; the terminal stage2_complete
    // overwrites with the canonical state. No separate `debateRounds` field —
    // dual state is a drift hazard.
    stage2_round_complete: (event) => updateLast((msg) => {
      if (!msg.metadata) {
        msg.metadata = event.metadata ? { ...event.metadata } : {};
      }
      if (!msg.metadata.debate) {
        msg.metadata.debate = { rounds: [], final_round: 0 };
      }
      const incoming = event.metadata?.debate?.rounds ?? [];
      msg.metadata.debate.rounds.push(...incoming);
      msg.metadata.debate.final_round = event.round ?? msg.metadata.debate.rounds.length;
      // Surface the kind so the dispatcher renders DebateView during streaming
      // (otherwise the view stays at peer_ranking until the terminal event).
      msg.stage2Kind = normaliseStage2Kind(event.kind);
    }),
    stage2_complete: (event) => updateLast((msg) => {
      msg.stage2 = event.data;
      // Treat null / undefined / empty / whitespace-only as missing and
      // fall back to "peer_ranking". Without this, an older backend or a
      // malformed event would route the dispatcher to UnknownKindView.
      msg.stage2Kind = normaliseStage2Kind(event.kind);
      // Terminal event is authoritative — overwrite metadata wholesale, which
      // includes the canonical debate transcript (with dropouts) for replay.
      msg.metadata = event.metadata;
      msg.loading.stage2 = false;
    }),
    stage3_start: () => updateLast((msg) => { msg.loading.stage3 = true; }),
    stage3_complete: (event) => {
      updateLast((msg) => {
        msg.stage3 = event.data;
        msg.loading.stage3 = false;
      });
      setCurrentConversation((prev) => prev ? { ...prev, closed: true } : prev);
      loadConversations();
    },
    title_complete: () => loadConversations(),
    complete: () => { loadConversations(); setIsLoading(false); },
    error: (event) => {
      updateLast((msg) => {
        msg.error = event.message;
        msg.loading = { stage0: false, stage1: false, stage2: false, stage3: false };
      });
      setIsLoading(false);
    },
  });

  const handleSendMessage = async (content) => {
    if (!currentConversationId) return;

    setIsLoading(true);
    try {
      const userMessage = { role: 'user', content };
      setCurrentConversation((prev) => ({
        ...prev,
        messages: [...prev.messages, userMessage],
      }));

      const assistantMessage = {
        role: 'assistant',
        stage1: null, stage2: null, stage2Kind: null, stage3: null, metadata: null,
        loading: { stage0: false, stage1: true, stage2: false, stage3: false },
        error: null,
        pendingClarification: null,
      };

      setCurrentConversation((prev) => ({
        ...prev,
        messages: [...prev.messages, assistantMessage],
      }));

      const updateLast = (updater) =>
        setCurrentConversation((prev) => {
          const messages = [...prev.messages];
          updater(messages[messages.length - 1]);
          return { ...prev, messages };
        });

      const handlers = makeStreamHandlers(updateLast);
      await api.sendMessageStream(
        currentConversationId,
        { content, council_type: 'default' },
        (eventType, event) => handlers[eventType]?.(event)
      );
    } catch (error) {
      console.error('Failed to send message:', error);
      setCurrentConversation((prev) => ({
        ...prev,
        messages: prev.messages.slice(0, -2),
      }));
      setIsLoading(false);
    }
  };

  const handleAnswerSubmit = async (answers) => {
    if (!currentConversationId) return;

    setIsLoading(true);

    const updateLast = (updater) =>
      setCurrentConversation((prev) => {
        const messages = [...prev.messages];
        updater(messages[messages.length - 1]);
        return { ...prev, messages };
      });

    updateLast((msg) => {
      msg.pendingClarification = null;
      msg.loading.stage1 = true;
    });

    try {
      const handlers = makeStreamHandlers(updateLast);
      await api.sendMessageStream(
        currentConversationId,
        { answers },
        (eventType, event) => handlers[eventType]?.(event)
      );
    } catch (error) {
      console.error('Failed to submit answers:', error);
      setIsLoading(false);
    }
  };

  return (
    <div className="app">
      <Sidebar
        conversations={conversations}
        currentConversationId={currentConversationId}
        onSelectConversation={handleSelectConversation}
        onNewConversation={handleNewConversation}
        isOpen={sidebarOpen}
        onToggle={toggleSidebar}
        theme={theme}
        onToggleTheme={toggleTheme}
      />
      <ChatInterface
        conversation={currentConversation}
        onSendMessage={handleSendMessage}
        onAnswerSubmit={handleAnswerSubmit}
        isConversationClosed={!!currentConversation?.closed}
        isLoading={isLoading}
        sidebarOpen={sidebarOpen}
        onToggleSidebar={toggleSidebar}
      />
    </div>
  );
}

export default App;
