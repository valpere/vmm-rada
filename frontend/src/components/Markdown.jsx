import ReactMarkdown from 'react-markdown';
import rehypeHighlight from 'rehype-highlight';
import 'highlight.js/styles/github-dark-dimmed.css';

export default function Markdown({ children }) {
  return (
    <ReactMarkdown rehypePlugins={[rehypeHighlight]}>
      {children ?? ''}
    </ReactMarkdown>
  );
}
