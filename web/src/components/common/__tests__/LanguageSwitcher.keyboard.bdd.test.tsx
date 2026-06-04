import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { LanguageSwitcher } from '../LanguageSwitcher';
import { DEFAULT_LOCALE, i18n } from '../../../i18n';

// BDD: WAI-ARIA menu keyboard contract for LanguageSwitcher.
//
// The dropdown declares role="menu", so screen readers announce "menu" and
// keyboard users expect arrow-key navigation between items and Escape to
// close. These scenarios pin that observable behavior.
//
// SUPPORTED_LOCALES order is ['zh-CN', 'en'], so in DOM order:
//   item[0] = language-option-zh-CN
//   item[1] = language-option-en
// beforeEach forces the active locale to 'en', so on open the *current*
// (checked) item — language-option-en — receives focus.
describe('LanguageSwitcher keyboard navigation (a11y)', () => {
  beforeEach(async () => {
    window.localStorage.clear();
    if (i18n.isInitialized && i18n.language !== 'en') {
      await i18n.changeLanguage('en');
    }
  });

  afterEach(async () => {
    window.localStorage.clear();
    if (i18n.isInitialized && i18n.language !== DEFAULT_LOCALE) {
      await i18n.changeLanguage(DEFAULT_LOCALE);
    }
  });

  it('Given a closed menu, When opened, Then focus moves to the currently selected item', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    await user.click(screen.getByTestId('language-menu-trigger'));

    // Active locale is 'en' → its menu item receives focus on open.
    await waitFor(() => {
      expect(screen.getByTestId('language-option-en')).toHaveFocus();
    });
  });

  it('Given an open menu, When pressing ArrowDown, Then focus cycles forward and wraps', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    await user.click(screen.getByTestId('language-menu-trigger'));
    // Opens with the 'en' item (last item, index 1) focused.
    await waitFor(() => {
      expect(screen.getByTestId('language-option-en')).toHaveFocus();
    });

    // last item -> wraps back to first item
    await user.keyboard('{ArrowDown}');
    expect(screen.getByTestId('language-option-zh-CN')).toHaveFocus();

    // first item -> second item
    await user.keyboard('{ArrowDown}');
    expect(screen.getByTestId('language-option-en')).toHaveFocus();
  });

  it('Given an open menu, When pressing ArrowUp, Then focus cycles backward and wraps', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    await user.click(screen.getByTestId('language-menu-trigger'));
    // Opens with the 'en' item (last item, index 1) focused.
    await waitFor(() => {
      expect(screen.getByTestId('language-option-en')).toHaveFocus();
    });

    // last item -> first item
    await user.keyboard('{ArrowUp}');
    expect(screen.getByTestId('language-option-zh-CN')).toHaveFocus();

    // first item -> wraps to last item
    await user.keyboard('{ArrowUp}');
    expect(screen.getByTestId('language-option-en')).toHaveFocus();
  });

  it('Given an open menu, When pressing Home/End, Then focus jumps to first/last item', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    await user.click(screen.getByTestId('language-menu-trigger'));
    await waitFor(() => {
      expect(screen.getByTestId('language-option-en')).toHaveFocus();
    });

    await user.keyboard('{Home}');
    expect(screen.getByTestId('language-option-zh-CN')).toHaveFocus();

    await user.keyboard('{End}');
    expect(screen.getByTestId('language-option-en')).toHaveFocus();
  });

  it('Given an open menu, When pressing Escape, Then the menu closes and focus returns to the trigger', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    const trigger = screen.getByTestId('language-menu-trigger');
    await user.click(trigger);
    expect(screen.getByTestId('language-menu')).toBeInTheDocument();

    await user.keyboard('{Escape}');

    await waitFor(() => {
      expect(screen.queryByTestId('language-menu')).not.toBeInTheDocument();
    });
    expect(trigger).toHaveFocus();
  });

  it('Given an open menu, When pressing Enter on a focused item, Then the language is selected and the menu closes', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);

    await user.click(screen.getByTestId('language-menu-trigger'));
    await waitFor(() => {
      expect(screen.getByTestId('language-option-en')).toHaveFocus();
    });

    // Move focus to the zh-CN item, then activate it via the keyboard.
    await user.keyboard('{ArrowDown}');
    expect(screen.getByTestId('language-option-zh-CN')).toHaveFocus();
    await user.keyboard('{Enter}');

    await waitFor(() => {
      expect(i18n.language).toBe('zh-CN');
    });
    expect(screen.queryByTestId('language-menu')).not.toBeInTheDocument();
  });
});
