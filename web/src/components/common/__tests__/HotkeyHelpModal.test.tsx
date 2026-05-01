import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { HotkeyHelpModal } from '../HotkeyHelpModal';
import { HOTKEYS } from '../../../hotkeys';
import { DEFAULT_LOCALE, i18n } from '../../../i18n';

describe('HotkeyHelpModal', () => {
  beforeEach(async () => {
    if (i18n.isInitialized && i18n.language !== 'en') {
      await i18n.changeLanguage('en');
    }
  });

  afterEach(async () => {
    if (i18n.isInitialized && i18n.language !== DEFAULT_LOCALE) {
      await i18n.changeLanguage(DEFAULT_LOCALE);
    }
  });

  it('renders nothing while closed', () => {
    render(<HotkeyHelpModal open={false} onClose={() => {}} />);
    expect(screen.queryByTestId('hotkey-help-modal')).not.toBeInTheDocument();
  });

  it('renders the localised title and the global / navigation groups when open', () => {
    render(<HotkeyHelpModal open={true} onClose={() => {}} />);
    expect(screen.getByTestId('hotkey-help-modal')).toBeInTheDocument();
    expect(screen.getByText('Keyboard Shortcuts')).toBeInTheDocument();
    expect(screen.getByTestId('hotkey-group-global')).toBeInTheDocument();
    expect(screen.getByTestId('hotkey-group-navigation')).toBeInTheDocument();
    expect(screen.getByText('Global')).toBeInTheDocument();
    expect(screen.getByText('Navigation')).toBeInTheDocument();
  });

  it('renders one row per registry entry with a description and key chips', () => {
    render(<HotkeyHelpModal open={true} onClose={() => {}} />);
    for (const def of HOTKEYS) {
      const row = screen.getByTestId(`hotkey-row-${def.id}`);
      expect(row).toBeInTheDocument();
    }

    // Spot-check a couple of key-chip renders. The command palette ships
    // two alternatives separated by an "or" label.
    const commandPaletteRow = screen.getByTestId('hotkey-row-commandPalette');
    expect(commandPaletteRow.textContent).toContain('⌘');
    expect(commandPaletteRow.textContent).toContain('K');
    expect(commandPaletteRow.textContent).toContain('Ctrl');
    expect(commandPaletteRow.textContent).toContain('or');

    // Sequence shortcut renders a "then" separator between the two steps.
    const goDashboardRow = screen.getByTestId('hotkey-row-goDashboard');
    expect(goDashboardRow.textContent).toContain('then');
    expect(goDashboardRow.textContent).toContain('G');
    expect(goDashboardRow.textContent).toContain('D');

    // Help shortcut shows the Shift + slash combo.
    const helpRow = screen.getByTestId('hotkey-row-showHelp');
    expect(helpRow.textContent).toContain('⇧');
    expect(helpRow.textContent).toContain('/');
  });

  it('uses zh-CN strings when the active locale is zh-CN', async () => {
    await i18n.changeLanguage('zh-CN');
    render(<HotkeyHelpModal open={true} onClose={() => {}} />);
    expect(screen.getByText('键盘快捷键')).toBeInTheDocument();
    expect(screen.getByText('全局')).toBeInTheDocument();
    expect(screen.getByText('导航')).toBeInTheDocument();
    expect(screen.getByTestId('hotkey-row-commandPalette').textContent).toContain('或');
    expect(screen.getByTestId('hotkey-row-goDashboard').textContent).toContain('然后');
  });

  it('calls onClose when the close button is clicked', async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(<HotkeyHelpModal open={true} onClose={onClose} />);
    await user.click(screen.getByRole('button', { name: /close/i }));
    expect(onClose).toHaveBeenCalled();
  });
});
