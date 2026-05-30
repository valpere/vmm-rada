import './EmptyState.css';

const SUGGESTED_PROMPTS = [
  'Explain the trolley problem',
  'How does TCP differ from UDP?',
  'What is quantum entanglement?',
  'Compare merge sort and quicksort',
];

export default function EmptyState({ onSendMessage, isLoading }) {
  return (
    <div className="empty-state-container">
      <div className="empty-state-hero">
        <h2 className="empty-state-title">VMM Rada</h2>
        <p className="empty-state-subtitle">
          Ask a question — multiple models answer independently, peer-review each other,
          and a chairman synthesises the final answer.
        </p>
      </div>
      <div className="prompt-chips">
        {SUGGESTED_PROMPTS.map((prompt) => (
          <button
            key={prompt}
            className="prompt-chip"
            onClick={() => onSendMessage(prompt)}
            disabled={isLoading}
          >
            {prompt}
          </button>
        ))}
      </div>
    </div>
  );
}
