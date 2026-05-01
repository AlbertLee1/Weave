import { Fragment, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import { Modal } from './Modal';
import { HOTKEYS, type HotkeyDef, type HotkeyGroup } from '../../hotkeys';

interface HotkeyHelpModalProps {
  open: boolean;
  onClose: () => void;
}

const GROUP_ORDER: ReadonlyArray<HotkeyGroup> = ['global', 'navigation'];

const GROUP_I18N: Record<HotkeyGroup, string> = {
  global: 'hotkeys.groupGlobal',
  navigation: 'hotkeys.groupNavigation',
};

const KEY_GLYPH: Record<string, string> = {
  meta: '⌘',
  ctrl: 'Ctrl',
  control: 'Ctrl',
  shift: '⇧',
  alt: '⌥',
  mod: 'Mod',
  slash: '/',
  space: 'Space',
  escape: 'Esc',
  enter: 'Enter',
  tab: 'Tab',
  backspace: 'Bksp',
  arrowup: '↑',
  arrowdown: '↓',
  arrowleft: '←',
  arrowright: '→',
};

function prettifyKey(key: string): string {
  const lower = key.trim().toLowerCase();
  if (KEY_GLYPH[lower]) return KEY_GLYPH[lower];
  return lower.length === 1 ? lower.toUpperCase() : lower;
}

export function HotkeyHelpModal({ open, onClose }: HotkeyHelpModalProps) {
  const { t } = useTranslation();

  const grouped = useMemo(() => {
    const map = new Map<HotkeyGroup, HotkeyDef[]>();
    for (const group of GROUP_ORDER) map.set(group, []);
    for (const def of HOTKEYS) {
      const list = map.get(def.group);
      if (list) list.push(def);
    }
    return map;
  }, []);

  return (
    <Modal open={open} onClose={onClose} title={t('hotkeys.helpTitle')} size="lg">
      <div data-testid="hotkey-help-modal" className="space-y-6">
        {GROUP_ORDER.map((group) => {
          const entries = grouped.get(group) ?? [];
          if (entries.length === 0) return null;
          return (
            <section
              key={group}
              data-testid={`hotkey-group-${group}`}
              aria-labelledby={`hotkey-group-${group}-title`}
            >
              <h3
                id={`hotkey-group-${group}-title`}
                className="mb-3 text-xs font-semibold uppercase tracking-wider text-text-muted"
              >
                {t(GROUP_I18N[group])}
              </h3>
              <ul className="divide-y divide-border/40 rounded-md border border-border/40 bg-bg-secondary/40">
                {entries.map((def) => (
                  <li
                    key={def.id}
                    data-testid={`hotkey-row-${def.id}`}
                    className="flex items-center justify-between gap-4 px-4 py-2.5"
                  >
                    <span className="text-sm text-text-primary">
                      {t(def.i18nKey)}
                    </span>
                    <KeyPattern pattern={def.keys} orLabel={t('hotkeys.or')} thenLabel={t('hotkeys.then')} />
                  </li>
                ))}
              </ul>
            </section>
          );
        })}
      </div>
    </Modal>
  );
}

interface KeyPatternProps {
  pattern: string;
  orLabel: string;
  thenLabel: string;
}

function KeyPattern({ pattern, orLabel, thenLabel }: KeyPatternProps) {
  const alternatives = pattern
    .split(',')
    .map((s) => s.trim())
    .filter(Boolean);
  return (
    <span className="flex flex-wrap items-center justify-end gap-1.5 text-xs">
      {alternatives.map((alt, i) => (
        <Fragment key={`${alt}-${i}`}>
          {i > 0 && <Separator label={orLabel} />}
          <KeySequence sequence={alt} thenLabel={thenLabel} />
        </Fragment>
      ))}
    </span>
  );
}

function KeySequence({ sequence, thenLabel }: { sequence: string; thenLabel: string }) {
  const steps = sequence.split('>').map((s) => s.trim()).filter(Boolean);
  return (
    <span className="inline-flex items-center gap-1">
      {steps.map((step, i) => (
        <Fragment key={`${step}-${i}`}>
          {i > 0 && <Separator label={thenLabel} />}
          <KeyCombo combo={step} />
        </Fragment>
      ))}
    </span>
  );
}

function KeyCombo({ combo }: { combo: string }) {
  const parts = combo.split('+').map((s) => s.trim()).filter(Boolean);
  return (
    <span className="inline-flex items-center gap-0.5">
      {parts.map((p, i) => (
        <kbd
          key={`${p}-${i}`}
          className="inline-flex min-w-[1.5rem] items-center justify-center rounded border border-border/60 bg-bg-elevated px-1.5 py-0.5 font-mono text-[0.7rem] font-medium text-text-primary shadow-sm"
        >
          {prettifyKey(p)}
        </kbd>
      ))}
    </span>
  );
}

function Separator({ label }: { label: string }) {
  return <span className="text-text-muted">{label}</span>;
}
