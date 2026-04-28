import { describe, it, expect, vi } from 'vitest';
import { createElement } from 'react';
import { render, screen, fireEvent } from '@testing-library/react';

// Stub the heavy @uiw/react-md-editor so jsdom can render without pulling in
// the whole CodeMirror surface. The mock surfaces the props we actually rely
// on (value/onChange/preview/textareaProps) so behavioural assertions stay
// honest without exercising the real editor internals.
vi.mock('@uiw/react-md-editor', () => {
  const Editor = (props: {
    value?: string;
    onChange?: (next: string | undefined) => void;
    preview?: string;
    textareaProps?: Record<string, unknown>;
  }) => {
    const { value, onChange, preview, textareaProps } = props;
    return createElement(
      'div',
      { 'data-testid': 'mde-mock', 'data-preview': preview },
      createElement('textarea', {
        'data-testid': 'mde-textarea',
        value: value ?? '',
        onChange: (e: { target: { value: string } }) => onChange?.(e.target.value),
        ...(textareaProps ?? {}),
      }),
    );
  };
  // MDEditor.Markdown is used by MarkdownPreview.
  (Editor as unknown as {
    Markdown: (props: { source: string }) => ReturnType<typeof createElement>;
  }).Markdown = (props: { source: string }) =>
    createElement('div', { 'data-testid': 'mde-mock-preview' }, props.source);
  return { default: Editor };
});

// Imports that depend on the mock have to come AFTER the mock registration.
import { MarkdownEditor, MarkdownPreview } from '../MarkdownEditor';

describe('MarkdownEditor', () => {
  it('renders preview-mode toggle defaulting to edit', () => {
    render(<MarkdownEditor value="" onChange={() => {}} />);
    const editBtn = screen.getByTestId('markdown-editor-mode-edit');
    const liveBtn = screen.getByTestId('markdown-editor-mode-live');
    const previewBtn = screen.getByTestId('markdown-editor-mode-preview');
    expect(editBtn).toHaveAttribute('aria-pressed', 'true');
    expect(liveBtn).toHaveAttribute('aria-pressed', 'false');
    expect(previewBtn).toHaveAttribute('aria-pressed', 'false');
    expect(screen.getByTestId('mde-mock')).toHaveAttribute(
      'data-preview',
      'edit',
    );
  });

  it('switches preview mode when toggle is clicked', () => {
    render(<MarkdownEditor value="" onChange={() => {}} />);
    fireEvent.click(screen.getByTestId('markdown-editor-mode-live'));
    expect(screen.getByTestId('markdown-editor-mode-live')).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByTestId('mde-mock')).toHaveAttribute(
      'data-preview',
      'live',
    );
    fireEvent.click(screen.getByTestId('markdown-editor-mode-preview'));
    expect(screen.getByTestId('mde-mock')).toHaveAttribute(
      'data-preview',
      'preview',
    );
  });

  it('honours initialPreview prop', () => {
    render(
      <MarkdownEditor value="" onChange={() => {}} initialPreview="live" />,
    );
    expect(screen.getByTestId('markdown-editor-mode-live')).toHaveAttribute(
      'aria-pressed',
      'true',
    );
    expect(screen.getByTestId('mde-mock')).toHaveAttribute(
      'data-preview',
      'live',
    );
  });

  it('forwards changes from the underlying textarea', () => {
    const onChange = vi.fn();
    render(<MarkdownEditor value="initial" onChange={onChange} />);
    const ta = screen.getByTestId('mde-textarea') as HTMLTextAreaElement;
    expect(ta).toHaveValue('initial');
    fireEvent.change(ta, { target: { value: '# Hello' } });
    expect(onChange).toHaveBeenCalledWith('# Hello');
  });

  it('passes placeholder + ariaLabel through to the underlying textarea', () => {
    render(
      <MarkdownEditor
        value=""
        onChange={() => {}}
        placeholder="Type Markdown here"
        ariaLabel="Description (Markdown)"
      />,
    );
    const ta = screen.getByTestId('mde-textarea');
    expect(ta).toHaveAttribute('placeholder', 'Type Markdown here');
    expect(ta).toHaveAttribute('aria-label', 'Description (Markdown)');
  });
});

describe('MarkdownPreview', () => {
  it('renders the source string through the markdown preview', () => {
    render(<MarkdownPreview source="# Heading" />);
    const root = screen.getByTestId('markdown-preview');
    expect(root).toBeInTheDocument();
    expect(screen.getByTestId('mde-mock-preview')).toHaveTextContent(
      '# Heading',
    );
  });

  it('honours custom testId', () => {
    render(<MarkdownPreview source="hi" testId="my-md" />);
    expect(screen.getByTestId('my-md')).toBeInTheDocument();
  });
});
