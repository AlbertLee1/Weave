import { useState } from 'react';
import MDEditor from '@uiw/react-md-editor';
import type { MDEditorProps } from '@uiw/react-md-editor';

type PreviewMode = 'edit' | 'live' | 'preview';

const PREVIEW_MODES: { key: PreviewMode; label: string }[] = [
  { key: 'edit', label: 'Edit' },
  { key: 'live', label: 'Live' },
  { key: 'preview', label: 'Preview' },
];

export interface MarkdownEditorProps {
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
  height?: number;
  initialPreview?: PreviewMode;
  ariaLabel?: string;
  testId?: string;
}

export function MarkdownEditor({
  value,
  onChange,
  placeholder,
  height = 220,
  initialPreview = 'edit',
  ariaLabel,
  testId = 'markdown-editor',
}: MarkdownEditorProps) {
  const [preview, setPreview] = useState<PreviewMode>(initialPreview);

  const editorProps: MDEditorProps = {
    value,
    onChange: (next) => onChange(next ?? ''),
    height,
    preview,
    visibleDragbar: false,
    'data-color-mode': 'dark',
    textareaProps: {
      placeholder,
      'aria-label': ariaLabel ?? 'Markdown editor',
    } as MDEditorProps['textareaProps'],
  };

  return (
    <div
      className="rounded border overflow-hidden"
      style={{ borderColor: 'rgba(31,41,55,0.5)' }}
      data-testid={testId}
    >
      <div
        className="flex items-center justify-between gap-2 px-2 py-1.5 border-b"
        style={{
          borderColor: 'rgba(31,41,55,0.5)',
          background: 'rgba(13,17,23,0.6)',
        }}
        role="toolbar"
        aria-label="Markdown preview mode"
      >
        <div
          className="flex gap-1 text-[10px] uppercase tracking-widest"
          data-testid={`${testId}-mode-toggle`}
        >
          {PREVIEW_MODES.map((m) => (
            <button
              key={m.key}
              type="button"
              onClick={() => setPreview(m.key)}
              aria-pressed={preview === m.key}
              data-testid={`${testId}-mode-${m.key}`}
              className={`px-2 py-0.5 rounded font-mono transition-colors ${
                preview === m.key
                  ? 'bg-accent-cyan/20 text-accent-cyan border border-accent-cyan/40'
                  : 'text-text-secondary hover:text-text-primary border border-transparent'
              }`}
            >
              {m.label}
            </button>
          ))}
        </div>
        <span className="text-[10px] uppercase tracking-widest text-text-secondary font-mono">
          Markdown
        </span>
      </div>
      <MDEditor {...editorProps} />
    </div>
  );
}

export interface MarkdownPreviewProps {
  source: string;
  testId?: string;
}

export function MarkdownPreview({
  source,
  testId = 'markdown-preview',
}: MarkdownPreviewProps) {
  return (
    <div
      data-testid={testId}
      className="rounded border px-3 py-2 text-xs"
      style={{
        borderColor: 'rgba(31,41,55,0.5)',
        background: 'rgba(13,17,23,0.4)',
      }}
    >
      <MDEditor.Markdown source={source} data-color-mode="dark" />
    </div>
  );
}
