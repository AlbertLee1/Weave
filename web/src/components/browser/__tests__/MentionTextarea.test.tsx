import { describe, it, expect, vi } from 'vitest';
import { useState } from 'react';
import { render, screen, fireEvent, act, waitFor } from '@testing-library/react';
import { MentionTextarea } from '../MentionTextarea';
import type { MentionUser } from '../../../api/mentions';

function ControlledHarness({
  initial = '',
  searchUsers,
  onChangeSpy,
}: {
  initial?: string;
  searchUsers?: (q: string) => Promise<MentionUser[]>;
  onChangeSpy?: (next: string) => void;
}) {
  const [value, setValue] = useState(initial);
  return (
    <MentionTextarea
      value={value}
      onChange={(next) => {
        setValue(next);
        onChangeSpy?.(next);
      }}
      data-testid="mention-input"
      searchUsers={searchUsers}
      rows={3}
    />
  );
}

describe('MentionTextarea (US-336)', () => {
  it('does not show suggestions until @ is typed', async () => {
    render(<ControlledHarness searchUsers={vi.fn()} />);
    const input = screen.getByTestId('mention-input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'hello world' } });
    // No `@` ⇒ no list mounted.
    expect(screen.queryByTestId('mention-suggestions')).not.toBeInTheDocument();
  });

  it('opens the dropdown on @<query> and lists matching users', async () => {
    const search = vi.fn(async (q: string) => {
      expect(q).toBe('al');
      return [
        { id: 'user:alice@example.com', email: 'alice@example.com', name: 'Alice' },
        { id: 'user:alan@example.com', email: 'alan@example.com', name: 'Alan' },
      ];
    });
    render(<ControlledHarness searchUsers={search} />);
    const input = screen.getByTestId('mention-input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'hi @al' } });
    // Trigger selectionStart update — jsdom keeps caret at the end after
    // change, which is what we want.
    await act(async () => {
      await new Promise((r) => setTimeout(r, 200));
    });
    await waitFor(() => {
      expect(screen.getByTestId('mention-suggestions')).toBeInTheDocument();
    });
    expect(screen.getByTestId('mention-suggestion-alice@example.com')).toBeInTheDocument();
    expect(screen.getByTestId('mention-suggestion-alan@example.com')).toBeInTheDocument();
    expect(search).toHaveBeenCalledWith('al');
  });

  it('inserts the chosen user as `@<email> ` on click', async () => {
    const search = vi.fn(async () => [
      { id: 'user:alice@example.com', email: 'alice@example.com', name: 'Alice' },
    ]);
    const spy = vi.fn();
    render(<ControlledHarness searchUsers={search} onChangeSpy={spy} />);
    const input = screen.getByTestId('mention-input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'cc @al' } });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 200));
    });
    await waitFor(() => {
      expect(screen.getByTestId('mention-suggestion-alice@example.com')).toBeInTheDocument();
    });
    fireEvent.mouseDown(screen.getByTestId('mention-suggestion-alice@example.com'));
    await waitFor(() => {
      const last = spy.mock.calls.at(-1)?.[0];
      expect(last).toBe('cc @alice@example.com ');
    });
  });

  it('does not call search for an empty query (just `@` with no chars)', async () => {
    const search = vi.fn();
    render(<ControlledHarness searchUsers={search} />);
    const input = screen.getByTestId('mention-input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: 'hello @' } });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 200));
    });
    expect(search).not.toHaveBeenCalled();
    expect(screen.queryByTestId('mention-suggestions')).not.toBeInTheDocument();
  });

  it('closes the dropdown on Escape', async () => {
    const search = vi.fn(async () => [
      { id: 'user:alice@example.com', email: 'alice@example.com', name: 'Alice' },
    ]);
    render(<ControlledHarness searchUsers={search} />);
    const input = screen.getByTestId('mention-input') as HTMLTextAreaElement;
    fireEvent.change(input, { target: { value: '@al' } });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 200));
    });
    await waitFor(() => {
      expect(screen.getByTestId('mention-suggestions')).toBeInTheDocument();
    });
    fireEvent.keyDown(input, { key: 'Escape' });
    await waitFor(() => {
      expect(screen.queryByTestId('mention-suggestions')).not.toBeInTheDocument();
    });
  });
});
