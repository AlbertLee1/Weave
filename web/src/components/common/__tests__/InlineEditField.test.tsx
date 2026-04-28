import { describe, it, expect, vi } from 'vitest';
import { act } from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { InlineEditField } from '../InlineEditField';

function flush() {
  return new Promise((resolve) => setTimeout(resolve, 0));
}

describe('InlineEditField', () => {
  it('renders the value as a clickable display by default', () => {
    render(<InlineEditField value="Alice" onSave={vi.fn()} />);
    const display = screen.getByTestId('inline-edit-display');
    expect(display).toHaveTextContent('Alice');
    expect(screen.queryByTestId('inline-edit-input')).not.toBeInTheDocument();
  });

  it('enters edit mode on click and focuses the input with the current value', () => {
    render(<InlineEditField value="Alice" onSave={vi.fn()} />);
    fireEvent.click(screen.getByTestId('inline-edit-display'));
    const input = screen.getByTestId('inline-edit-input') as HTMLInputElement;
    expect(input).toBeInTheDocument();
    expect(input.value).toBe('Alice');
    expect(document.activeElement).toBe(input);
  });

  it('cancels edit on Esc and reverts to the original value', () => {
    render(<InlineEditField value="Alice" onSave={vi.fn()} />);
    fireEvent.click(screen.getByTestId('inline-edit-display'));
    const input = screen.getByTestId('inline-edit-input') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'Bob' } });
    fireEvent.keyDown(input, { key: 'Escape' });
    expect(screen.queryByTestId('inline-edit-input')).not.toBeInTheDocument();
    expect(screen.getByTestId('inline-edit-display')).toHaveTextContent('Alice');
  });

  it('saves on Enter with optimistic update', async () => {
    let resolve!: () => void;
    const pending = new Promise<void>((r) => {
      resolve = r;
    });
    const onSave = vi.fn(() => pending);
    render(<InlineEditField value="Alice" onSave={onSave} />);

    fireEvent.click(screen.getByTestId('inline-edit-display'));
    const input = screen.getByTestId('inline-edit-input') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'Bob' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    // Optimistic update: display shows the new value before onSave resolves.
    expect(onSave).toHaveBeenCalledWith('Bob');
    expect(screen.queryByTestId('inline-edit-input')).not.toBeInTheDocument();
    expect(screen.getByTestId('inline-edit-display')).toHaveTextContent('Bob');

    await act(async () => {
      resolve();
      await pending;
    });
    expect(screen.getByTestId('inline-edit-display')).toHaveTextContent('Bob');
  });

  it('rolls back to the original value when onSave rejects and surfaces error', async () => {
    const onSave = vi.fn(() => Promise.reject(new Error('boom')));
    render(<InlineEditField value="Alice" onSave={onSave} />);

    fireEvent.click(screen.getByTestId('inline-edit-display'));
    const input = screen.getByTestId('inline-edit-input') as HTMLInputElement;
    fireEvent.change(input, { target: { value: 'Bob' } });
    await act(async () => {
      fireEvent.keyDown(input, { key: 'Enter' });
      await flush();
    });

    await waitFor(() => {
      expect(screen.getByTestId('inline-edit-display')).toHaveTextContent(
        'Alice',
      );
    });
    expect(screen.getByTestId('inline-edit-error')).toHaveTextContent(/boom/);
  });

  it('does not call onSave when the value is unchanged', () => {
    const onSave = vi.fn(() => Promise.resolve());
    render(<InlineEditField value="Alice" onSave={onSave} />);
    fireEvent.click(screen.getByTestId('inline-edit-display'));
    const input = screen.getByTestId('inline-edit-input') as HTMLInputElement;
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.queryByTestId('inline-edit-input')).not.toBeInTheDocument();
  });

  it('renders read-only display when disabled', () => {
    render(<InlineEditField value="Alice" onSave={vi.fn()} disabled />);
    fireEvent.click(screen.getByTestId('inline-edit-display'));
    expect(screen.queryByTestId('inline-edit-input')).not.toBeInTheDocument();
  });

  it('honours custom testId', () => {
    render(<InlineEditField value="x" onSave={vi.fn()} testId="my-field" />);
    expect(screen.getByTestId('my-field-display')).toBeInTheDocument();
  });

  it('uses the placeholder string when value is empty', () => {
    render(
      <InlineEditField value="" onSave={vi.fn()} placeholder="(empty)" />,
    );
    expect(screen.getByTestId('inline-edit-display')).toHaveTextContent(
      '(empty)',
    );
  });

  it('cancels on blur without saving when nothing changed', async () => {
    const onSave = vi.fn(() => Promise.resolve());
    render(<InlineEditField value="Alice" onSave={onSave} />);
    fireEvent.click(screen.getByTestId('inline-edit-display'));
    const input = screen.getByTestId('inline-edit-input') as HTMLInputElement;
    fireEvent.blur(input);
    expect(onSave).not.toHaveBeenCalled();
    expect(screen.queryByTestId('inline-edit-input')).not.toBeInTheDocument();
    expect(screen.getByTestId('inline-edit-display')).toHaveTextContent('Alice');
  });
});
