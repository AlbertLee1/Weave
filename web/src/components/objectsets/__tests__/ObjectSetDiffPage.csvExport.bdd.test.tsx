import {
  describe,
  it,
  expect,
  beforeAll,
  afterAll,
  afterEach,
  beforeEach,
  vi,
} from 'vitest';
import {
  render,
  screen,
  fireEvent,
  waitFor,
} from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { http, HttpResponse } from 'msw';
import { setupServer } from 'msw/node';
import { ObjectSetDiffPage } from '../ObjectSetDiffPage';
import { localStorageKey } from '../../../lib/objectSetBuilder';

vi.mock('../../../hooks/useObjectTypes', () => ({
  useObjectTypes: () => ({
    data: [{ apiName: 'Employee', displayName: 'Employee' }],
    isLoading: false,
  }),
  useObjectType: (_ontology: string, apiName: string) => ({
    data: apiName
      ? {
          rid: 'ri.ot.employee',
          apiName,
          displayName: apiName,
          primaryKey: 'employeeId',
          status: 'ACTIVE',
          visibility: 'NORMAL',
          properties: {
            employeeId: { dataType: { type: 'string' }, rid: 'ri.p.id' },
            name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
            title: { dataType: { type: 'string' }, rid: 'ri.p.title' },
            metadata: { dataType: { type: 'string' }, rid: 'ri.p.metadata' },
          },
        }
      : undefined,
    isLoading: false,
  }),
  useOutgoingLinkTypes: () => ({ data: [], isLoading: false }),
}));

const ONTOLOGY = 'test';

const rowsA = [
  {
    __rid: 'ri.employee.alice',
    __primaryKey: 'EMP-1',
    __apiName: 'Employee',
    employeeId: 'EMP-1',
    name: 'Alice, A',
    title: 'Engineer',
    metadata: { region: 'North' },
  },
  {
    __rid: 'ri.employee.carol',
    __primaryKey: 'EMP-3',
    __apiName: 'Employee',
    employeeId: 'EMP-3',
    name: 'Carol',
    title: 'Analyst',
    metadata: null,
  },
];

const rowsB = [
  {
    __rid: 'ri.employee.carol.b',
    __primaryKey: 'EMP-3',
    __apiName: 'Employee',
    employeeId: 'EMP-3',
    name: 'Caroline',
    title: 'Senior "Analyst"',
    metadata: null,
  },
  {
    __rid: 'ri.employee.dave',
    __primaryKey: 'EMP-4',
    __apiName: 'Employee',
    employeeId: 'EMP-4',
    name: 'Dave',
    title: 'Designer',
    metadata: ['new', 'remote'],
  },
];

const server = setupServer();

beforeAll(() => server.listen());
afterEach(() => {
  server.resetHandlers();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  window.localStorage.clear();
});
afterAll(() => server.close());

beforeEach(() => {
  const savedSets = [
    {
      id: 'sa-1',
      name: 'Snapshot A',
      def: { type: 'base', objectType: 'Employee' },
      createdAt: new Date().toISOString(),
    },
    {
      id: 'sb-1',
      name: 'Snapshot B',
      def: { type: 'base', objectType: 'Employee' },
      createdAt: new Date().toISOString(),
    },
  ];
  window.localStorage.setItem(localStorageKey(ONTOLOGY), JSON.stringify(savedSets));
});

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[`/objectsets/${ONTOLOGY}/diff`]}>
        <Routes>
          <Route
            path="/objectsets/:ontology/diff"
            element={<ObjectSetDiffPage />}
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

function stubDiffRows() {
  let requestOrdinal = 0;
  server.use(
    http.get('/api/v2/ontologies/test/objectTypes/Employee', () =>
      HttpResponse.json({
        rid: 'ri.ot.employee',
        apiName: 'Employee',
        displayName: 'Employee',
        primaryKey: 'employeeId',
        status: 'ACTIVE',
        visibility: 'NORMAL',
        properties: {
          employeeId: { dataType: { type: 'string' }, rid: 'ri.p.id' },
          name: { dataType: { type: 'string' }, rid: 'ri.p.name' },
          title: { dataType: { type: 'string' }, rid: 'ri.p.title' },
          metadata: { dataType: { type: 'string' }, rid: 'ri.p.metadata' },
        },
      }),
    ),
    http.post('/api/v2/ontologies/test/objectSets/loadObjects', () => {
      const data = requestOrdinal === 0 ? rowsA : rowsB;
      requestOrdinal += 1;
      return HttpResponse.json({
        data,
        totalCount: String(data.length),
      });
    }),
  );
}

async function computeDiff() {
  stubDiffRows();
  renderPage();
  fireEvent.change(screen.getByLabelText(/object set a/i), {
    target: { value: 'sa-1' },
  });
  fireEvent.change(screen.getByLabelText(/object set b/i), {
    target: { value: 'sb-1' },
  });
  fireEvent.click(screen.getByRole('button', { name: /compute diff/i }));
  await screen.findByTestId('objectset-diff-results');
}

describe('BDD: ObjectSetDiffPage CSV export (P2B-003)', () => {
  it('Given a computed diff, When Export CSV is clicked, Then section and row metadata are downloaded', async () => {
    const blobs: Blob[] = [];
    vi.spyOn(URL, 'createObjectURL').mockImplementation((obj: Blob | MediaSource) => {
      if (obj instanceof Blob) blobs.push(obj);
      return `blob:objectset-diff-${blobs.length}`;
    });
    vi.spyOn(URL, 'revokeObjectURL').mockImplementation(() => {});

    const clickedDownloads: string[] = [];
    const originalCreateElement = document.createElement.bind(document);
    vi.spyOn(document, 'createElement').mockImplementation((tagName: string) => {
      const el = originalCreateElement(tagName);
      if (tagName.toLowerCase() === 'a') {
        const anchor = el as HTMLAnchorElement;
        anchor.click = () => {
          clickedDownloads.push(anchor.download);
        };
      }
      return el;
    });

    await computeDiff();

    fireEvent.click(screen.getByRole('button', { name: /export csv/i }));

    await waitFor(() => {
      expect(blobs).toHaveLength(1);
      expect(clickedDownloads).toEqual([
        'test-snapshot-a-vs-snapshot-b-objectset-diff.csv',
      ]);
    });
    expect(blobs[0]!.type).toBe('text/csv;charset=utf-8');
    await expect(blobs[0]!.text()).resolves.toBe(
      [
        'section,primaryKey,field,valueA,valueB',
        'Only in A,EMP-1,employeeId,EMP-1,',
        'Only in A,EMP-1,name,"Alice, A",',
        'Only in A,EMP-1,title,Engineer,',
        'Only in A,EMP-1,metadata,"{""region"":""North""}",',
        'Changed,EMP-3,name,Carol,Caroline',
        'Changed,EMP-3,title,Analyst,"Senior ""Analyst"""',
        'Only in B,EMP-4,employeeId,,EMP-4',
        'Only in B,EMP-4,name,,Dave',
        'Only in B,EMP-4,title,,Designer',
        'Only in B,EMP-4,metadata,,"[""new"",""remote""]"',
        '',
      ].join('\n'),
    );
  });

  it('Given no diff has been computed, When the page renders, Then Export CSV is hidden', () => {
    renderPage();

    expect(
      screen.queryByRole('button', { name: /export csv/i }),
    ).not.toBeInTheDocument();
  });
});
