import { describe, expect, it, vi } from 'vitest';
import { act, fireEvent, render, screen } from '@testing-library/react';
import { ScenarioPane } from './ScenarioPane';

// VTX-120: ScenarioPane wires Cmd+Enter / Ctrl+Enter to `onRun` when the
// pane (or any descendant) holds focus. The hotkey is intentionally
// scoped — when focus lives elsewhere, the global submitForm shortcut
// wins so plain form Submit-on-Enter still works.

describe('ScenarioPane (VTX-120)', () => {
  it('Cmd+Enter runs the scenario when the pane is focused', () => {
    const onRun = vi.fn();
    render(<ScenarioPane onRun={onRun} />);
    const pane = screen.getByTestId('scenario-pane');
    act(() => pane.focus());
    expect(pane).toHaveAttribute('data-focused', 'true');
    act(() => {
      fireEvent.keyDown(document, { key: 'Enter', code: 'Enter', metaKey: true });
    });
    expect(onRun).toHaveBeenCalledTimes(1);
  });

  it('Ctrl+Enter runs the scenario when the pane is focused', () => {
    const onRun = vi.fn();
    render(<ScenarioPane onRun={onRun} />);
    const pane = screen.getByTestId('scenario-pane');
    act(() => pane.focus());
    act(() => {
      fireEvent.keyDown(document, { key: 'Enter', code: 'Enter', ctrlKey: true });
    });
    expect(onRun).toHaveBeenCalledTimes(1);
  });

  it('does not run when the pane is not focused', () => {
    const onRun = vi.fn();
    render(
      <>
        <button data-testid="outside" type="button">
          outside
        </button>
        <ScenarioPane onRun={onRun} />
      </>,
    );
    act(() => (screen.getByTestId('outside') as HTMLButtonElement).focus());
    act(() => {
      fireEvent.keyDown(document, { key: 'Enter', code: 'Enter', metaKey: true });
    });
    expect(onRun).not.toHaveBeenCalled();
  });

  it('does not run when disabled even if the pane is focused', () => {
    const onRun = vi.fn();
    render(<ScenarioPane onRun={onRun} disabled />);
    const pane = screen.getByTestId('scenario-pane');
    act(() => pane.focus());
    act(() => {
      fireEvent.keyDown(document, { key: 'Enter', code: 'Enter', metaKey: true });
    });
    expect(onRun).not.toHaveBeenCalled();
  });

  it('Run button click also fires onRun', () => {
    const onRun = vi.fn();
    render(<ScenarioPane onRun={onRun} />);
    fireEvent.click(screen.getByTestId('scenario-pane-run'));
    expect(onRun).toHaveBeenCalledTimes(1);
  });

  it('focused state flips when focus moves into a descendant', () => {
    const onRun = vi.fn();
    render(
      <ScenarioPane onRun={onRun}>
        <input data-testid="inner" />
      </ScenarioPane>,
    );
    const pane = screen.getByTestId('scenario-pane');
    expect(pane).toHaveAttribute('data-focused', 'false');
    act(() => (screen.getByTestId('inner') as HTMLInputElement).focus());
    expect(pane).toHaveAttribute('data-focused', 'true');
  });
});
