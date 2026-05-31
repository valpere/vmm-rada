/**
 * API client for the VMM Rada backend.
 *
 * Adapter pattern: this module is the sole point of contact between the React
 * frontend and the Go backend.  All network requests (`fetch` calls) and SSE
 * stream parsing live here.  Components and App.jsx never call `fetch` or
 * perform network requests directly — they only call methods on this `api`
 * object and receive plain JS values, or provide an `onEvent(eventType, event)`
 * callback that `sendMessageStream` calls.
 *
 * SSE boundary: `sendMessageStream` reads the raw ReadableStream and fires
 * `onEvent` once per parsed SSE data line.  App.jsx owns all state mutations
 * in response to those events; this module remains stateless.
 */

// In development the Vite dev server proxies /api → backend (see vite.config.js).
// VITE_API_BASE is only needed for production builds served from a different
// origin than the API (e.g. a CDN). Leave it unset for local development.
const API_BASE = (() => {
  const raw = import.meta.env.VITE_API_BASE;
  if (!raw || typeof raw !== 'string') {
    return ''; // relative URLs — Vite proxy in dev, same-origin in prod
  }
  const trimmed = raw.trim().replace(/\/+$/, '');
  return trimmed || '';
})();

export const api = {
  /**
   * List all conversations.
   */
  async listConversations() {
    const response = await fetch(`${API_BASE}/api/conversations`);
    if (!response.ok) {
      throw new Error('Failed to list conversations');
    }
    return response.json();
  },

  /**
   * Create a new conversation.
   */
  async createConversation() {
    const response = await fetch(`${API_BASE}/api/conversations`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify({}),
    });
    if (!response.ok) {
      throw new Error('Failed to create conversation');
    }
    return response.json();
  },

  /**
   * Get a specific conversation.
   */
  async getConversation(conversationId) {
    const response = await fetch(
      `${API_BASE}/api/conversations/${conversationId}`
    );
    if (!response.ok) {
      throw new Error('Failed to get conversation');
    }
    return response.json();
  },

  async deleteConversation(conversationId) {
    const response = await fetch(`${API_BASE}/api/conversations/${conversationId}`, {
      method: 'DELETE',
    });
    if (!response.ok) {
      throw new Error('Failed to delete conversation');
    }
  },

  async renameConversation(conversationId, title) {
    const response = await fetch(`${API_BASE}/api/conversations/${conversationId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ title }),
    });
    if (!response.ok) {
      throw new Error('Failed to rename conversation');
    }
    return response.json();
  },

  /**
   * Send a message in a conversation.
   */
  async sendMessage(conversationId, content) {
    const response = await fetch(
      `${API_BASE}/api/conversations/${conversationId}/message`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify({ content }),
      }
    );
    if (!response.ok) {
      throw new Error('Failed to send message');
    }
    return response.json();
  },

  /**
   * Send a message or clarification answers and receive streaming updates.
   * @param {string} conversationId - The conversation ID
   * @param {Object} body - `{content, council_type}` for a new message, or `{answers:[...]}` for a clarification round
   * @param {function} onEvent - Callback function for each event: (eventType, data) => void
   * @returns {Promise<void>}
   */
  async sendMessageStream(conversationId, body, onEvent) {
    const response = await fetch(
      `${API_BASE}/api/conversations/${conversationId}/message/stream`,
      {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
        },
        body: JSON.stringify(body),
      }
    );

    if (!response.ok) {
      throw new Error('Failed to send message');
    }

    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    let buffer = '';

    while (true) {
      const { done, value } = await reader.read();

      // On done, flush the decoder; otherwise decode with stream:true to
      // handle multi-byte characters split across chunk boundaries.
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true });
      const lines = buffer.split('\n');
      // When not done, keep the last (potentially incomplete) line in the
      // buffer. When done, process everything (no more chunks will arrive).
      buffer = done ? '' : lines.pop();

      for (const line of lines) {
        if (line.startsWith('data: ')) {
          const data = line.slice(6);
          try {
            const event = JSON.parse(data);
            onEvent(event.type, event);
          } catch (e) {
            console.error('Failed to parse SSE event:', e);
          }
        }
      }

      if (done) break;
    }
  },
};
