import { afterEach, beforeEach, describe, expect, it } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';

import { LanguageSwitcher } from '../LanguageSwitcher';
import { DEFAULT_LOCALE, LOCALE_STORAGE_KEY, i18n } from '../../../i18n';

describe('LanguageSwitcher', () => {
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

  it('renders the trigger with the active locale indicator', () => {
    render(<LanguageSwitcher />);
    const trigger = screen.getByTestId('language-menu-trigger');
    expect(trigger).toBeInTheDocument();
    expect(trigger).toHaveAttribute('data-locale', 'en');
    expect(trigger).toHaveTextContent('EN');
  });

  it('opens a menu listing both supported locales', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);
    await user.click(screen.getByTestId('language-menu-trigger'));
    expect(screen.getByTestId('language-menu')).toBeInTheDocument();
    expect(screen.getByTestId('language-option-en')).toBeInTheDocument();
    expect(screen.getByTestId('language-option-zh-CN')).toBeInTheDocument();
  });

  it('marks the active locale with aria-checked=true', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);
    await user.click(screen.getByTestId('language-menu-trigger'));
    expect(screen.getByTestId('language-option-en')).toHaveAttribute(
      'aria-checked',
      'true',
    );
    expect(screen.getByTestId('language-option-zh-CN')).toHaveAttribute(
      'aria-checked',
      'false',
    );
  });

  it('changes the active language and persists it on selection', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);
    await user.click(screen.getByTestId('language-menu-trigger'));
    await user.click(screen.getByTestId('language-option-zh-CN'));

    await waitFor(() => {
      expect(window.localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh-CN');
    });
    expect(i18n.language).toBe('zh-CN');
    expect(screen.getByTestId('language-menu-trigger')).toHaveAttribute(
      'data-locale',
      'zh-CN',
    );
    expect(screen.queryByTestId('language-menu')).not.toBeInTheDocument();
  });

  it('renders localised option labels (English by default)', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);
    await user.click(screen.getByTestId('language-menu-trigger'));
    expect(screen.getByTestId('language-option-en')).toHaveTextContent(
      'English',
    );
    expect(screen.getByTestId('language-option-zh-CN')).toHaveTextContent(
      '简体中文',
    );
  });

  it('switching to zh-CN translates the trigger title attribute', async () => {
    const user = userEvent.setup();
    render(<LanguageSwitcher />);
    await user.click(screen.getByTestId('language-menu-trigger'));
    await user.click(screen.getByTestId('language-option-zh-CN'));

    await waitFor(() => {
      expect(screen.getByTestId('language-menu-trigger')).toHaveAttribute(
        'title',
        '语言',
      );
    });
  });
});
