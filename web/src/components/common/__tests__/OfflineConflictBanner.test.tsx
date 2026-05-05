import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { OfflineConflictBanner } from '../OfflineConflictBanner';
import type { ObjectSetConflict } from '../../../lib/objectSetSnapshotCache';
import '../../../i18n';

const baseConflict: ObjectSetConflict = {
  mineFingerprint: 'h1:aaa',
  serverFingerprint: 'h1:bbb',
  minePk: ['1', '2'],
  serverPk: ['2', '3'],
  added: ['3'],
  removed: ['1'],
};

describe('OfflineConflictBanner (US-451)', () => {
  it('renders nothing when conflict is null', () => {
    const { container } = render(
      <OfflineConflictBanner
        conflict={null}
        onKeepMine={() => {}}
        onUseServer={() => {}}
      />,
    );
    expect(container.firstChild).toBeNull();
  });

  it('shows the title and both action buttons when conflict is present', () => {
    render(
      <OfflineConflictBanner
        conflict={baseConflict}
        onKeepMine={() => {}}
        onUseServer={() => {}}
      />,
    );
    expect(screen.getByTestId('offline-conflict-banner')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /keep mine/i })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /use server/i })).toBeInTheDocument();
  });

  it('invokes onKeepMine when the keep-mine button is clicked', () => {
    const onKeepMine = vi.fn();
    render(
      <OfflineConflictBanner
        conflict={baseConflict}
        onKeepMine={onKeepMine}
        onUseServer={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /keep mine/i }));
    expect(onKeepMine).toHaveBeenCalledTimes(1);
  });

  it('invokes onUseServer when the use-server button is clicked', () => {
    const onUseServer = vi.fn();
    render(
      <OfflineConflictBanner
        conflict={baseConflict}
        onKeepMine={() => {}}
        onUseServer={onUseServer}
      />,
    );
    fireEvent.click(screen.getByRole('button', { name: /use server/i }));
    expect(onUseServer).toHaveBeenCalledTimes(1);
  });

  it('shows the added/removed counts in the summary', () => {
    render(
      <OfflineConflictBanner
        conflict={baseConflict}
        onKeepMine={() => {}}
        onUseServer={() => {}}
      />,
    );
    const banner = screen.getByTestId('offline-conflict-banner');
    expect(banner.textContent).toMatch(/1 added/i);
    expect(banner.textContent).toMatch(/1 removed/i);
  });
});
