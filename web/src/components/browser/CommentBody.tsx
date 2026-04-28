import { cloneElement, isValidElement } from 'react';
import type { ReactElement, ReactNode } from 'react';
import Markdown from 'react-markdown';
import type { Components } from 'react-markdown';

interface CommentBodyProps {
  body: string;
  testId?: string;
}

// MENTION_RENDER_REGEX matches `@<email>` mentions written by the
// MentionTextarea autocomplete or hand-typed by users. Mirrors the
// backend extractor's accepted shape (pkg/comments/mentions.go) — keep
// the two in sync so what we highlight matches what we notify on.
const MENTION_RENDER_REGEX =
  /@([A-Za-z0-9._+%-]+@[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?(?:\.[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?)+)/g;

// Walk a React node tree and replace `@email` substrings inside string
// children with mention badges. Markdown formatting (strong / em / a /
// code) is preserved because we only descend into element children and
// rebuild them with cloneElement.
function decorateMentions(node: ReactNode, keyPrefix = ''): ReactNode {
  if (typeof node === 'string') {
    return splitOnMentions(node, keyPrefix);
  }
  if (Array.isArray(node)) {
    return node.map((child, i) => decorateMentions(child, `${keyPrefix}.${i}`));
  }
  if (isValidElement(node)) {
    const el = node as ReactElement<{ children?: ReactNode }>;
    const children = el.props?.children;
    if (children == null) return el;
    return cloneElement(el, {
      ...el.props,
      children: decorateMentions(children, `${keyPrefix}.c`),
    });
  }
  return node;
}

function splitOnMentions(text: string, keyPrefix: string): ReactNode {
  if (!text) return text;
  const out: ReactNode[] = [];
  let lastIndex = 0;
  let match: RegExpExecArray | null;
  MENTION_RENDER_REGEX.lastIndex = 0;
  while ((match = MENTION_RENDER_REGEX.exec(text)) !== null) {
    if (match.index > lastIndex) {
      out.push(text.slice(lastIndex, match.index));
    }
    const email = match[1];
    out.push(
      <span
        key={`${keyPrefix}-m${match.index}`}
        className="text-accent-cyan font-medium"
        data-mention={email}
        data-testid={`mention-link-${email}`}
      >
        @{email}
      </span>,
    );
    lastIndex = match.index + match[0].length;
  }
  if (lastIndex < text.length) {
    out.push(text.slice(lastIndex));
  }
  return out.length === 1 ? out[0] : out;
}

// Components override: external links open in a new tab with rel safety,
// and every text-bearing element runs through decorateMentions so mention
// badges survive inside any markdown construct.
const COMPONENTS: Components = {
  a: ({ children, href, ...rest }) => (
    <a
      {...rest}
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      className="text-accent-cyan underline"
    >
      {decorateMentions(children)}
    </a>
  ),
  p: ({ children, ...rest }) => (
    <p {...rest} className="text-xs text-text-primary leading-relaxed">
      {decorateMentions(children)}
    </p>
  ),
  li: ({ children, ...rest }) => (
    <li {...rest} className="text-xs text-text-primary">
      {decorateMentions(children)}
    </li>
  ),
  // Don't run decorateMentions inside <code> — mentions in code spans
  // are intentional literal text and shouldn't get the cyan badge.
  code: ({ children, className, ...rest }) => (
    <code
      {...rest}
      className={`${className ?? ''} px-1 py-0.5 rounded bg-bg-elevated/80 font-mono text-[11px]`.trim()}
    >
      {children}
    </code>
  ),
  pre: ({ children, ...rest }) => (
    <pre
      {...rest}
      className="my-2 p-2 rounded border border-border bg-bg-elevated/60 text-[11px] font-mono overflow-x-auto"
    >
      {children}
    </pre>
  ),
  strong: ({ children, ...rest }) => (
    <strong {...rest} className="font-semibold">
      {decorateMentions(children)}
    </strong>
  ),
  em: ({ children, ...rest }) => (
    <em {...rest} className="italic">
      {decorateMentions(children)}
    </em>
  ),
};

// CommentBody renders a comment body as Markdown. Raw HTML in the source
// is escaped (react-markdown's default — we do NOT enable rehype-raw),
// `javascript:` URLs are stripped by `defaultUrlTransform`, and `@email`
// mentions retain the same visual badge as the plain-text path used
// before US-341.
export function CommentBody({ body, testId }: CommentBodyProps) {
  return (
    <div data-testid={testId} className="space-y-2 break-words">
      <Markdown components={COMPONENTS}>{body}</Markdown>
    </div>
  );
}
