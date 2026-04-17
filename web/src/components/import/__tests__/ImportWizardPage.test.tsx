import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { ImportWizardPage } from '../ImportWizardPage';

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp',
    apiName: 'Employee',
    displayName: 'Employee',
    primaryKey: 'id',
    status: 'ACTIVE',
    visibility: 'NORMAL',
    properties: {
      id: { dataType: { type: 'string' }, rid: 'ri.prop.id' },
      name: { dataType: { type: 'string' }, rid: 'ri.prop.name' },
      age: { dataType: { type: 'integer' }, rid: 'ri.prop.age' },
    },
  },
];

const ACTION_TYPES = [
  {
    rid: 'ri.action.create-emp',
    apiName: 'createEmployee',
    displayName: 'Create Employee',
    status: 'ACTIVE',
    parameters: {
      id: { dataType: { type: 'string' }, required: true },
      name: { dataType: { type: 'string' }, required: true },
      age: { dataType: { type: 'integer' }, required: false },
    },
    rules: [
      {
        type: 'createObject',
        objectType: 'Employee',
        propertyBindings: {
          id: { type: 'parameter', value: 'id' },
          name: { type: 'parameter', value: 'name' },
          age: { type: 'parameter', value: 'age' },
        },
      },
    ],
  },
];

interface StubState {
  applyCalls: Array<{ action: string; body: unknown }>;
  applyFailNext: number;
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function installFetch(state: StubState) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = typeof input === 'string' ? input : input.toString();
      const method = (init?.method ?? 'GET').toUpperCase();

      if (
        method === 'GET' &&
        url.endsWith('/api/v2/ontologies/northwind/objectTypes')
      ) {
        return jsonResponse({ data: OBJECT_TYPES });
      }
      if (
        method === 'GET' &&
        url.endsWith('/api/v2/ontologies/northwind/actionTypes')
      ) {
        return jsonResponse({ data: ACTION_TYPES });
      }

      const applyMatch = url.match(
        /\/api\/v2\/ontologies\/northwind\/actions\/([^/]+)\/apply$/,
      );
      if (applyMatch && method === 'POST') {
        const action = decodeURIComponent(applyMatch[1]);
        const body = init?.body ? JSON.parse(init.body as string) : {};
        state.applyCalls.push({ action, body });
        if (state.applyFailNext > 0) {
          state.applyFailNext -= 1;
          return jsonResponse(
            { errorCode: 'ApplyFailed', errorName: 'Apply failed' },
            400,
          );
        }
        return jsonResponse({ edits: { type: 'edits' } }, 200);
      }

      return new Response('{}', { status: 200 });
    }),
  );
}

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, refetchInterval: false },
      mutations: { retry: false },
    },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={['/import/northwind']}>
        <Routes>
          <Route path="/import/:ontology" element={<ImportWizardPage />} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function makeCsvFile(content: string, name = 'employees.csv') {
  return new File([content], name, { type: 'text/csv' });
}

describe('ImportWizardPage', () => {
  let state: StubState;

  beforeEach(() => {
    state = { applyCalls: [], applyFailNext: 0 };
    installFetch(state);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders heading and step indicator', () => {
    renderPage();
    expect(screen.getByTestId('import-wizard-heading')).toHaveTextContent(
      /Import CSV Data/i,
    );
    expect(screen.getByTestId('step-indicator')).toBeInTheDocument();
    expect(screen.getByTestId('step-1')).toHaveAttribute('data-state', 'active');
  });

  it('parses an uploaded CSV and enables Next', async () => {
    const user = userEvent.setup();
    renderPage();
    const fileInput = screen.getByTestId('file-input');
    await user.upload(
      fileInput as HTMLInputElement,
      makeCsvFile('id,name,age\r\n1,Alice,30\r\n2,Bob,25'),
    );
    await waitFor(() => {
      expect(screen.getByTestId('parse-summary')).toHaveTextContent(
        /2 rows, 3 columns/,
      );
    });
    expect(screen.getByTestId('next-2')).toBeEnabled();
  });

  it('auto-maps columns after selecting the target object type', async () => {
    const user = userEvent.setup();
    renderPage();
    await user.upload(
      screen.getByTestId('file-input') as HTMLInputElement,
      makeCsvFile('id,name,age\r\n1,Alice,30'),
    );
    await waitFor(() =>
      expect(screen.getByTestId('next-2')).toBeEnabled(),
    );
    await user.click(screen.getByTestId('next-2'));

    // object type select becomes visible
    const otSelect = (await screen.findByTestId(
      'object-type-select',
    )) as HTMLSelectElement;
    await user.selectOptions(otSelect, 'Employee');

    // mapping dropdowns render and have auto-mapped values
    const idMap = (await screen.findByTestId('map-id')) as HTMLSelectElement;
    expect(idMap.value).toBe('id');
    const nameMap = screen.getByTestId('map-name') as HTMLSelectElement;
    expect(nameMap.value).toBe('name');
    const ageMap = screen.getByTestId('map-age') as HTMLSelectElement;
    expect(ageMap.value).toBe('age');
    expect(screen.getByTestId('next-3')).toBeEnabled();
  });

  it('shows a yellow warning badge for values that cannot be converted', async () => {
    const user = userEvent.setup();
    renderPage();
    await user.upload(
      screen.getByTestId('file-input') as HTMLInputElement,
      makeCsvFile('id,name,age\r\n1,Alice,not-a-number'),
    );
    await waitFor(() =>
      expect(screen.getByTestId('next-2')).toBeEnabled(),
    );
    await user.click(screen.getByTestId('next-2'));
    const otSelect = (await screen.findByTestId(
      'object-type-select',
    )) as HTMLSelectElement;
    await user.selectOptions(otSelect, 'Employee');
    await user.click(await screen.findByTestId('next-3'));

    // preview table is now visible
    expect(await screen.findByTestId('warn-0-age')).toBeInTheDocument();
  });

  it('executes apply per row and reports progress', async () => {
    const user = userEvent.setup();
    renderPage();
    await user.upload(
      screen.getByTestId('file-input') as HTMLInputElement,
      makeCsvFile('id,name,age\r\n1,Alice,30\r\n2,Bob,25'),
    );
    await waitFor(() =>
      expect(screen.getByTestId('next-2')).toBeEnabled(),
    );
    await user.click(screen.getByTestId('next-2'));
    await user.selectOptions(
      (await screen.findByTestId('object-type-select')) as HTMLSelectElement,
      'Employee',
    );
    await user.click(await screen.findByTestId('next-3'));
    await user.click(await screen.findByTestId('next-4'));
    expect(await screen.findByTestId('row-count')).toHaveTextContent('2');

    await user.click(screen.getByTestId('start-import'));

    await waitFor(
      () => {
        expect(screen.getByTestId('processed-count')).toHaveTextContent('2');
        expect(screen.getByTestId('success-count')).toHaveTextContent('2');
        expect(screen.getByTestId('failure-count')).toHaveTextContent('0');
      },
      { timeout: 3000 },
    );

    expect(state.applyCalls).toHaveLength(2);
    expect(state.applyCalls[0].action).toBe('createEmployee');
    expect(state.applyCalls[0].body).toEqual({
      parameters: { id: '1', name: 'Alice', age: 30 },
    });
  });

  it('lists failed rows when apply returns an error', async () => {
    state.applyFailNext = 1;
    const user = userEvent.setup();
    renderPage();
    await user.upload(
      screen.getByTestId('file-input') as HTMLInputElement,
      makeCsvFile('id,name,age\r\n1,Alice,30\r\n2,Bob,25'),
    );
    await waitFor(() =>
      expect(screen.getByTestId('next-2')).toBeEnabled(),
    );
    await user.click(screen.getByTestId('next-2'));
    await user.selectOptions(
      (await screen.findByTestId('object-type-select')) as HTMLSelectElement,
      'Employee',
    );
    await user.click(await screen.findByTestId('next-3'));
    await user.click(await screen.findByTestId('next-4'));
    await user.click(screen.getByTestId('start-import'));

    await waitFor(
      () => {
        expect(screen.getByTestId('failure-count')).toHaveTextContent('1');
      },
      { timeout: 3000 },
    );
    const summary = screen.getByTestId('failure-summary');
    expect(summary).toHaveTextContent(/Row 1/);
  });
});
