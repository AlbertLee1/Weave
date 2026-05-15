// ScenarioPane — minimal Scenario container with Cmd+Enter run hotkey (VTX-120).
//
// The pane is a focusable region (tabIndex=0). When focus lands inside it
// (the pane or any descendant), Cmd+Enter / Ctrl+Enter triggers `onRun`.
// We mirror useShortcut's enableOnFormTags=false behaviour by additionally
// gating on a focus flag, so the global `submitForm` shortcut (which
// shares the same key pattern) keeps winning whenever the pane is not the
// active scope. This keeps form Submit-on-Enter intact everywhere else.
//
// The pane is intentionally thin: it does not own scenario state or
// rendering of the run output — callers slot their own children in. Tests
// inject a synchronous `onRun` to assert the hotkey fires exactly once.

import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { useShortcut } from '../hotkeys';

export interface ScenarioPaneProps {
  /** Invoked when the Run shortcut fires or the Run button is clicked. */
  onRun: () => void;
  /** Disable the Run binding (e.g. while a scenario is already running). */
  disabled?: boolean;
  children?: ReactNode;
}

export function ScenarioPane({ onRun, disabled = false, children }: ScenarioPaneProps) {
  const { t } = useTranslation();
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [focused, setFocused] = useState(false);

  // Track focus on the pane subtree by listening for focusin/focusout on
  // the container. The pair fires for every focus change inside, so a
  // simple `contains(activeElement)` check stays accurate even when focus
  // walks between descendants.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const recompute = () => {
      const active = document.activeElement;
      setFocused(!!active && el.contains(active));
    };
    el.addEventListener('focusin', recompute);
    el.addEventListener('focusout', recompute);
    recompute();
    return () => {
      el.removeEventListener('focusin', recompute);
      el.removeEventListener('focusout', recompute);
    };
  }, []);

  const handleRun = useCallback(() => {
    if (disabled) return;
    onRun();
  }, [disabled, onRun]);

  useShortcut('runScenario', handleRun, { enabled: focused && !disabled });

  return (
    <div
      ref={containerRef}
      data-testid="scenario-pane"
      data-focused={focused ? 'true' : 'false'}
      tabIndex={0}
      role="region"
      aria-label={t('hotkeys.runScenario')}
      className="rounded border border-border/40 bg-bg-secondary/40 p-3 outline-none focus:ring-2 focus:ring-amber-500/40"
    >
      <div className="mb-2 flex items-center justify-between">
        <span className="text-xs uppercase tracking-wider text-text-muted">
          {t('hotkeys.runScenario')}
        </span>
        <button
          type="button"
          data-testid="scenario-pane-run"
          onClick={handleRun}
          disabled={disabled}
          className="rounded bg-amber-600 px-2 py-1 text-xs text-white disabled:opacity-40"
        >
          Run
        </button>
      </div>
      {children}
    </div>
  );
}
