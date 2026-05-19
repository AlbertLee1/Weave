import { describe, it, expect, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { ObjectSetComposer } from '../ObjectSetComposer';
import type { ObjectSetDefinition } from '../../../api/types';

function renderComposer(value: ObjectSetDefinition) {
  render(
    <ObjectSetComposer
      objectTypes={['Employee']}
      value={value}
      onChange={() => {}}
      onExecute={vi.fn()}
      onSaveAs={() => {}}
      savedObjectSets={[]}
      onLoadSaved={() => {}}
      onDeleteSaved={() => {}}
    />,
  );
}

describe('BDD: ObjectSet composer filter validation (SELF-440)', () => {
  it('Given an eq filter has no field or value, When validation runs, Then Execute is disabled with field and value errors', () => {
    renderComposer({
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Employee' },
      where: { type: 'eq', field: '', value: '' },
    });

    expect(screen.getByRole('button', { name: /execute/i })).toBeDisabled();
    expect(screen.getByText(/eq requires a field/i)).toBeInTheDocument();
    expect(screen.getByText(/eq requires a value/i)).toBeInTheDocument();
  });

  it('Given a shared filter uses neq, When the composer renders, Then Execute is disabled and neq is not offered for new filters', () => {
    renderComposer({
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Employee' },
      where: { type: 'neq', field: 'status', value: 'archived' },
    });

    expect(screen.getByRole('button', { name: /execute/i })).toBeDisabled();
    expect(screen.getByText(/unsupported operator "neq"/i)).toBeInTheDocument();

    const whereType = screen.getByRole('combobox', { name: /where type/i });
    expect(within(whereType).queryByRole('option', { name: 'neq' })).not.toBeInTheDocument();
  });

  it('Given an isNull filter has a field and no value, When validation runs, Then Execute remains enabled', () => {
    renderComposer({
      type: 'filter',
      objectSet: { type: 'base', objectType: 'Employee' },
      where: { type: 'isNull', field: 'archivedAt', value: '' },
    });

    expect(screen.getByRole('button', { name: /execute/i })).toBeEnabled();
    expect(screen.queryByText(/validation/i)).not.toBeInTheDocument();
  });
});
