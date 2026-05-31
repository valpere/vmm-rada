import { useState, useEffect, useRef } from 'react';
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
  onDeleteConversation,
  onRenameConversation,
  isOpen,
  onToggle,
  theme,
  onToggleTheme,
}) {
  const [openMenuId, setOpenMenuId] = useState(null);
  const [editingId, setEditingId] = useState(null);
  const [editValue, setEditValue] = useState('');
  const menuRef = useRef(null);

  useEffect(() => {
    if (!openMenuId) return;
    const handler = (e) => {
      if (menuRef.current && !menuRef.current.contains(e.target)) {
        setOpenMenuId(null);
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [openMenuId]);

  const startEdit = (conv, e) => {
    e.stopPropagation();
    setOpenMenuId(null);
    setEditingId(conv.id);
    setEditValue(stripMarkdown(conv.title || 'New Conversation'));
  };

  const commitEdit = (id) => {
    const trimmed = editValue.trim();
    if (trimmed && trimmed !== stripMarkdown(conversations.find((c) => c.id === id)?.title || '')) {
      onRenameConversation(id, trimmed);
    }
    setEditingId(null);
  };

  const handleEditKeyDown = (e, id) => {
    if (e.key === 'Enter') { e.preventDefault(); commitEdit(id); }
    if (e.key === 'Escape') setEditingId(null);
  };

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
              <div
                key={conv.id}
                className={`conversation-item${conv.id === currentConversationId ? ' active' : ''}`}
              >
                <button
                  className="conversation-item-btn"
                  onClick={() => { if (editingId !== conv.id) onSelectConversation(conv.id); }}
                >
                  {editingId === conv.id ? (
                    <input
                      className="conversation-rename-input"
                      value={editValue}
                      autoFocus
                      onChange={(e) => setEditValue(e.target.value)}
                      onBlur={() => commitEdit(conv.id)}
                      onKeyDown={(e) => handleEditKeyDown(e, conv.id)}
                      onClick={(e) => e.stopPropagation()}
                    />
                  ) : (
                    <div className="conversation-title">
                      {stripMarkdown(conv.title || 'New Conversation')}
                    </div>
                  )}
                  <div className="conversation-meta">{formatDate(conv.created_at)}</div>
                </button>

                <div className="conversation-menu-wrap" ref={openMenuId === conv.id ? menuRef : null}>
                  <button
                    className="conversation-menu-btn"
                    aria-label="Conversation actions"
                    onClick={(e) => {
                      e.stopPropagation();
                      setOpenMenuId(openMenuId === conv.id ? null : conv.id);
                    }}
                  >
                    ···
                  </button>
                  {openMenuId === conv.id && (
                    <div className="conversation-dropdown">
                      <button
                        className="conversation-dropdown-item"
                        onClick={(e) => startEdit(conv, e)}
                      >
                        Rename
                      </button>
                      <button
                        className="conversation-dropdown-item conversation-dropdown-item--danger"
                        onClick={(e) => {
                          e.stopPropagation();
                          setOpenMenuId(null);
                          onDeleteConversation(conv.id);
                        }}
                      >
                        Delete
                      </button>
                    </div>
                  )}
                </div>
              </div>
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
