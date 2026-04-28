import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { CommentBody } from '../CommentBody';

// US-341: comments support Markdown with XSS sanitisation. The renderer
// runs untrusted user input through react-markdown (no `rehype-raw`),
// so raw HTML is escaped, `javascript:` URLs are stripped, and mentions
// keep their existing visual treatment alongside markdown formatting.

describe('CommentBody (US-341)', () => {
  it('renders markdown bold + emphasis + inline code as the matching elements', () => {
    render(<CommentBody body="**bold** _italic_ `code`" testId="cb" />);
    const root = screen.getByTestId('cb');
    expect(root.querySelector('strong')?.textContent).toBe('bold');
    expect(root.querySelector('em')?.textContent).toBe('italic');
    expect(root.querySelector('code')?.textContent).toBe('code');
  });

  it('renders fenced code blocks via <pre><code>', () => {
    const body = '```\nlet x = 1\n```';
    render(<CommentBody body={body} testId="cb-fence" />);
    const root = screen.getByTestId('cb-fence');
    const pre = root.querySelector('pre');
    expect(pre).not.toBeNull();
    expect(pre!.querySelector('code')?.textContent).toContain('let x = 1');
  });

  it('renders external links with target=_blank + rel=noopener noreferrer', () => {
    render(
      <CommentBody body="[ack](https://example.com)" testId="cb-link" />,
    );
    const link = screen.getByTestId('cb-link').querySelector('a');
    expect(link).not.toBeNull();
    expect(link!.getAttribute('href')).toBe('https://example.com');
    expect(link!.getAttribute('target')).toBe('_blank');
    expect(link!.getAttribute('rel')).toMatch(/noopener/);
    expect(link!.getAttribute('rel')).toMatch(/noreferrer/);
  });

  it('escapes raw HTML in the source — <script> never reaches the DOM', () => {
    render(
      <CommentBody
        body={'hello <script>window.__pwn = 1</script> world'}
        testId="cb-xss"
      />,
    );
    const root = screen.getByTestId('cb-xss');
    // The literal "<script>" is rendered as visible text (escaped).
    expect(root.textContent).toContain('<script>');
    // No actual <script> element was injected into the DOM.
    expect(root.querySelector('script')).toBeNull();
    // And the side-effect from the would-be script body never fired.
    expect(
      (window as unknown as { __pwn?: number }).__pwn,
    ).toBeUndefined();
  });

  it('strips javascript: URLs from markdown links', () => {
    // eslint-disable-next-line no-script-url
    const body = '[click me](javascript:alert(1))';
    render(<CommentBody body={body} testId="cb-jsurl" />);
    const link = screen.getByTestId('cb-jsurl').querySelector('a');
    if (link) {
      expect(link.getAttribute('href') ?? '').not.toMatch(/^javascript:/i);
    }
  });

  it('drops <img onerror> handlers — the attribute never reaches the DOM', () => {
    render(
      <CommentBody
        body={'<img src="x" onerror="window.__pwnImg = 1" />'}
        testId="cb-img"
      />,
    );
    const img = screen.getByTestId('cb-img').querySelector('img');
    // Either the <img> is escaped to text (most common path with default
    // skipHtml=false + no rehype-raw → react-markdown escapes raw HTML),
    // or if it ever rendered, it must not carry the onerror handler.
    if (img) {
      expect(img.getAttribute('onerror')).toBeNull();
    }
    expect(
      (window as unknown as { __pwnImg?: number }).__pwnImg,
    ).toBeUndefined();
  });

  it('highlights @<email> mentions with the data-mention badge', () => {
    render(
      <CommentBody body="hello @alice@example.com !" testId="cb-m" />,
    );
    const badge = screen.getByTestId('mention-link-alice@example.com');
    expect(badge).toBeInTheDocument();
    expect(badge.getAttribute('data-mention')).toBe('alice@example.com');
    // Mention badge shows the @-prefixed handle as visible text.
    expect(badge.textContent).toContain('@alice@example.com');
  });

  it('keeps mention highlighting alongside markdown formatting', () => {
    render(
      <CommentBody
        body="**heads up** @bob@example.com please review"
        testId="cb-mm"
      />,
    );
    const root = screen.getByTestId('cb-mm');
    expect(root.querySelector('strong')?.textContent).toBe('heads up');
    expect(
      screen.getByTestId('mention-link-bob@example.com'),
    ).toBeInTheDocument();
  });
});
