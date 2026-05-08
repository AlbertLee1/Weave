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

  it('renders the localised title and the navigation / editing / search groups when open', () => {
    render(<HotkeyHelpModal open={true} onClose={() => {}} />);
    expect(screen.getByTestId('hotkey-help-modal')).toBeInTheDocument();
    expect(screen.getByText('Keyboard Shortcuts')).toBeInTheDocument();
    expect(screen.getByTestId('hotkey-group-navigation')).toBeInTheDocument();
    expect(screen.getByTestId('hotkey-group-editing')).toBeInTheDocument();
    expect(screen.getByTestId('hotkey-group-search')).toBeInTheDocument();
    expect(screen.getByText('Navigation')).toBeInTheDocument();
    expect(screen.getByText('Editing')).toBeInTheDocument();
    expect(screen.getByText('Search')).toBeInTheDocument();
  });

  it('renders groups in PRD-mandated order (Navigation → Editing → Search)', () => {
    render(<HotkeyHelpModal open={true} onClose={() => {}} />);
    const headings = screen
      .getAllByRole('heading', { level: 3 })
      .map((h) => h.textContent ?? '');
    expect(headings).toEqual(['Navigation', 'Editing', 'Search']);
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

    // US-458: editing-group additions render too.
    const submitRow = screen.getByTestId('hotkey-row-submitForm');
    expect(submitRow.textContent).toContain('⌘');
    expect(submitRow.textContent).toContain('Enter');
    const cancelRow = screen.getByTestId('hotkey-row-cancelEdit');
    expect(cancelRow.textContent).toContain('Esc');
  });

  it('uses zh-CN strings when the active locale is zh-CN', async () => {
    await i18n.changeLanguage('zh-CN');
    render(<HotkeyHelpModal open={true} onClose={() => {}} />);
    expect(screen.getByText('键盘快捷键')).toBeInTheDocument();
    expect(screen.getByText('导航')).toBeInTheDocument();
    expect(screen.getByText('编辑')).toBeInTheDocument();
    expect(screen.getByText('搜索')).toBeInTheDocument();
    expect(screen.getByTestId('hotkey-row-commandPalette').textContent).toContain('或');
    expect(screen.getByTestId('hotkey-row-goDashboard').textContent).toContain('然后');
    expect(screen.getByTestId('hotkey-row-submitForm').textContent).toContain('提交');
  });

  it('calls onClose when the close button is clicked', async () => {
    const onClose = vi.fn();
    const user = userEvent.setup();
    render(<HotkeyHelpModal open={true} onClose={onClose} />);
    await user.click(screen.getByRole('button', { name: /close/i }));
    expect(onClose).toHaveBeenCalled();
  });
});
