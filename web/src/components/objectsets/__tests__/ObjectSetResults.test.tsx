import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ObjectSetResults } from '../ObjectSetResults';
import * as objectsetsApi from '../../../api/objectsets';
import * as ontologiesApi from '../../../api/ontologies';
import type { ObjectSetDefinition, ObjectType } from '../../../api/types';

function makeWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
}

const baseDef: ObjectSetDefinition = { type: 'base', objectType: 'Employee' };
const stubObjectType: ObjectType = {
  rid: 'ri.ot',
  apiName: 'Employee',
  displayName: 'Employee',
  primaryKey: 'id',
  status: 'ACTIVE',
  visibility: 'NORMAL',
  properties: {
    id: { dataType: { type: 'string' }, rid: 'ri.p.id' },
    name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
  },
};

describe('ObjectSetResults', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders Browse tab with object table from loadObjectSet', async () => {
    vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue({
      data: [
        { __rid: 'ri.1', __primaryKey: 'p1', __apiName: 'Employee', name: 'Alice' },
      ],
      totalCount: '1',
    });
    vi.spyOn(ontologiesApi, 'getObjectType').mockResolvedValue(stubObjectType);

    render(
      <ObjectSetResults ontologyApiName="test" def={baseDef} executeKey={1} />,
      { wrapper: makeWrapper() },
    );

    await waitFor(() =>
      expect(objectsetsApi.loadObjectSet).toHaveBeenCalled(),
    );
    await waitFor(() => expect(screen.getByText('p1')).toBeInTheDocument());
  });

  it('switches to Aggregate tab on tab click', async () => {
    vi.spyOn(objectsetsApi, 'loadObjectSet').mockResolvedValue({
      data: [],
      totalCount: '0',
    });
    vi.spyOn(ontologiesApi, 'getObjectType').mockResolvedValue(stubObjectType);

    render(
      <ObjectSetResults ontologyApiName="test" def={baseDef} executeKey={1} />,
      { wrapper: makeWrapper() },
    );

    fireEvent.click(screen.getByRole('button', { name: /aggregate/i }));
    expect(screen.getByText(/Metrics/i)).toBeInTheDocument();
  });

  it('shows empty state when no def is provided', () => {
    render(
      <ObjectSetResults ontologyApiName="test" def={null} executeKey={0} />,
      { wrapper: makeWrapper() },
    );
    expect(screen.getByText(/no results yet|no query/i)).toBeInTheDocument();
  });
});
