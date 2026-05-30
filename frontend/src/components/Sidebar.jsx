import './Sidebar.css';
import { stripMarkdown } from '../utils';

function formatDate(iso) {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
}

export default function Sidebar({
  conversations,
  currentConversationId,
  onSelectConversation,
  onNewConversation,
  isOpen,
  onToggle,
  theme,
  onToggleTheme,
}) {
  return (
    <div className={`sidebar${isOpen ? '' : ' collapsed'}`}>
      <div className="sidebar-header">
        <span className="sidebar-title">VMM Rada</span>
        <button
          className="sidebar-toggle"
          onClick={onToggle}
          aria-label={isOpen ? 'Collapse sidebar' : 'Expand sidebar'}
        >
          {isOpen ? '‹' : '›'}
        </button>
      </div>

      <div className="sidebar-body">
        <button className="new-conversation-btn" onClick={onNewConversation}>
          + New Conversation
        </button>

        <div className="conversation-list">
          {conversations.length === 0 ? (
            <div className="no-conversations">No conversations yet</div>
          ) : (
            conversations.map((conv) => (
              <button
                key={conv.id}
                className={`conversation-item${conv.id === currentConversationId ? ' active' : ''}`}
                onClick={() => onSelectConversation(conv.id)}
              >
                <div className="conversation-title">
                  {stripMarkdown(conv.title || 'New Conversation')}
                </div>
                <div className="conversation-meta">
                  {formatDate(conv.created_at)}
                </div>
              </button>
            ))
          )}
        </div>
      </div>

      <div className="sidebar-footer">
        <button
          className="theme-toggle"
          onClick={onToggleTheme}
          aria-label={theme === 'dark' ? 'Switch to light theme' : 'Switch to dark theme'}
        >
          {theme === 'dark' ? '☀' : '☾'}
        </button>
      </div>
    </div>
  );
}
