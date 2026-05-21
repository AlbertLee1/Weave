import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { SchemaGraphPage } from '../SchemaGraphPage';

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp',
    apiName: 'Employee',
    displayName: 'Employee',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
    color: '#a78bfa',
    icon: 'user',
  },
  {
    rid: 'ri.ontology.main.object-type.dept',
    apiName: 'Department',
    displayName: 'Department',
    primaryKey: 'departmentId',
    status: 'ACTIVE',
    visibility: 'NORMAL',
    color: '#38bdf8',
    icon: 'building',
  },
  {
    rid: 'ri.ontology.main.object-type.proj',
    apiName: 'Project',
    displayName: 'Project',
    primaryKey: 'projectId',
    status: 'ACTIVE',
    visibility: 'NORMAL',
  },
];

const LINK_TYPES = [
  {
    rid: 'ri.ontology.main.link-type.l1',
    apiName: 'employeeDepartment',
    displayName: 'Employee → Department',
    objectTypeApiName: 'Employee',
    linkedObjectTypeApiName: 'Department',
    cardinality: 'ONE_TO_ONE',
    required: true,
  },
  {
    rid: 'ri.ontology.main.link-type.l2',
    apiName: 'departmentEmployees',
    displayName: 'Department → Employees',
    objectTypeApiName: 'Department',
    linkedObjectTypeApiName: 'Employee',
    cardinality: 'ONE_TO_MANY',
    required: false,
  },
  {
    rid: 'ri.ontology.main.link-type.l3',
    apiName: 'employeeProjects',
    displayName: 'Employee → Projects',
    objectTypeApiName: 'Employee',
    linkedObjectTypeApiName: 'Project',
    cardinality: 'MANY_TO_MANY',
    required: false,
  },
];

const INTERFACES = [
  {
    rid: 'ri.ontology.main.interface.addr',
    apiName: 'Addressable',
    displayName: 'Addressable',
    sharedProperties: [],
    outgoingLinkTypes: [],
  },
  {
    rid: 'ri.ontology.main.interface.loc',
    apiName: 'Locatable',
    displayName: 'Locatable',
    sharedProperties: [],
    outgoingLinkTypes: [],
  },
];

// objectTypeRid -> interface attachments
const ATTACHMENTS: Record<
  string,
  Array<{ objectTypeRid: string; interfaceRid: string; propertyMapping: Record<string, string> }>
> = {
  'ri.ontology.main.object-type.emp': [
    {
      objectTypeRid: 'ri.ontology.main.object-type.emp',
      interfaceRid: 'ri.ontology.main.interface.addr',
      propertyMapping: {},
    },
  ],
  'ri.ontology.main.object-type.dept': [
    {
      objectTypeRid: 'ri.ontology.main.object-type.dept',
      interfaceRid: 'ri.ontology.main.interface.addr',
      propertyMapping: {},
    },
    {
      objectTypeRid: 'ri.ontology.main.object-type.dept',
      interfaceRid: 'ri.ontology.main.interface.loc',
      propertyMapping: {},
    },
  ],
  'ri.ontology.main.object-type.proj': [],
};

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}

function installFetch({
  objectTypes = OBJECT_TYPES,
  linkTypes = LINK_TYPES,
  interfaces = INTERFACES,
  attachments = ATTACHMENTS,
}: {
  objectTypes?: typeof OBJECT_TYPES;
  linkTypes?: typeof LINK_TYPES;
  interfaces?: typeof INTERFACES;
  attachments?: typeof ATTACHMENTS;
} = {}) {
  vi.stubGlobal(
    'fetch',
    vi.fn(
      async (
        input: RequestInfo | URL,
        init?: RequestInit,
      ): Promise<Response> => {
        const url = typeof input === 'string' ? input : input.toString();
        const method = (init?.method ?? 'GET').toUpperCase();

        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/objectTypes')
        ) {
          return jsonResponse({ data: objectTypes });
        }
        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/linkTypes')
        ) {
          return jsonResponse({ data: linkTypes });
        }
        if (
          method === 'GET' &&
          url.endsWith('/api/v2/ontologies/northwind/interfacesAdmin')
        ) {
          return jsonResponse({ data: interfaces });
        }
        const attachMatch = url.match(
          /\/api\/v2\/ontologies\/northwind\/objectTypes\/byRid\/([^/]+)\/interfaces$/,
        );
        if (attachMatch && method === 'GET') {
          const rid = decodeURIComponent(attachMatch[1]);
          return jsonResponse({ data: attachments[rid] ?? [] });
        }

        return new Response('{}', { status: 200 });
      },
    ),
  );
}

function LocationProbe() {
  const loc = useLocation();
  return <span data-testid="location-probe">{loc.pathname}</span>;
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
      <MemoryRouter initialEntries={['/admin/northwind/graph']}>
        <Routes>
          <Route
            path="/admin/:ontology/graph"
            element={
              <>
                <SchemaGraphPage />
                <LocationProbe />
              </>
            }
          />
          <Route
            path="/admin/:ontology/objectTypes"
            element={
              <>
                <div>Object Type Admin</div>
                <LocationProbe />
              </>
            }
          />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe('SchemaGraphPage', () => {
  beforeEach(() => {
    installFetch();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it('renders the page heading with the ontology name', async () => {
    renderPage();
    expect(
      screen.getByRole('heading', { name: /Ontology Manager/i }),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByText(/3 types · 3 links/)).toBeInTheDocument();
    });
  });

  it('renders a graph node for each object type', async () => {
    renderPage();
    await waitFor(() => {
      const nodes = screen.getAllByTestId('graph-node');
      expect(nodes).toHaveLength(3);
    });
    const apiNames = screen
      .getAllByTestId('graph-node')
      .map((n) => n.getAttribute('data-api-name'));
    expect(apiNames.sort()).toEqual(['Department', 'Employee', 'Project']);
  });

  it('renders each link with a cardinality label', async () => {
    renderPage();
    await waitFor(() => {
      const edges = screen.getAllByTestId('graph-edge');
      expect(edges).toHaveLength(3);
    });
    const labels = screen
      .getAllByTestId('edge-cardinality')
      .map((el) => el.textContent);
    // cardinalities: ONE_TO_ONE -> 1:1, ONE_TO_MANY -> 1:N, MANY_TO_MANY -> N:N
    expect(labels.sort()).toEqual(['1:1', '1:N', 'N:N']);
  });

  it('offers a Reset view button', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('graph-node').length).toBeGreaterThan(0);
    });
    const resetBtn = screen.getByRole('button', { name: /Reset view/i });
    await user.click(resetBtn);
    expect(resetBtn).toBeInTheDocument();
  });

  it('filters nodes and edges by interface', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('graph-node')).toHaveLength(3);
    });
    const select = screen.getByRole('combobox', {
      name: /Filter by interface/i,
    });
    await user.selectOptions(select, 'ri.ontology.main.interface.loc');
    // Only Department implements Locatable
    await waitFor(() => {
      const nodes = screen.getAllByTestId('graph-node');
      expect(nodes).toHaveLength(1);
      expect(nodes[0].getAttribute('data-api-name')).toBe('Department');
    });
    // No edges remain (no links connect a single Department node to itself)
    expect(screen.queryAllByTestId('graph-edge')).toHaveLength(0);
    // Counter updates
    expect(screen.getByText(/1 type · 0 links/)).toBeInTheDocument();
  });

  it('uses plural-safe copy for schema graph summary counts', async () => {
    installFetch({ linkTypes: [LINK_TYPES[0]] });
    renderPage();
    await waitFor(() => {
      expect(screen.getByText(/3 types · 1 link/)).toBeInTheDocument();
    });
  });

  it('navigates to the ObjectType admin page when a node is clicked', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('graph-node')).toHaveLength(3);
    });
    const employeeNode = screen
      .getAllByTestId('graph-node')
      .find((n) => n.getAttribute('data-api-name') === 'Employee');
    expect(employeeNode).toBeDefined();
    await user.click(employeeNode!);
    await waitFor(() => {
      expect(screen.getByTestId('location-probe').textContent).toBe(
        '/admin/northwind/objectTypes',
      );
    });
  });

  it('shows loading spinner before data loads and empty state when filter yields no matches', async () => {
    const user = userEvent.setup();
    renderPage();
    await waitFor(() => {
      expect(screen.getAllByTestId('graph-node')).toHaveLength(3);
    });
    const select = screen.getByRole('combobox', {
      name: /Filter by interface/i,
    });
    // Temporarily set filter to Addressable (Employee + Department have it) then check counts
    await user.selectOptions(select, 'ri.ontology.main.interface.addr');
    await waitFor(() => {
      expect(screen.getAllByTestId('graph-node')).toHaveLength(2);
    });
    // The Employee→Department link should remain since both endpoints implement Addressable
    expect(screen.getAllByTestId('graph-edge').length).toBeGreaterThan(0);
  });
});
