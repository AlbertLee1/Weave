import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import {
  LinkTypeAdminPage,
  ActionTypeAdminPage,
  InterfaceAdminPage,
  SchemaGraphPage,
  AuditHistoryPage,
} from '../AdminPlaceholderPage';

function renderAt(path: string, Component: React.ComponentType) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/admin/:ontology/:section" element={<Component />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('Admin placeholder pages', () => {
  it('LinkTypeAdminPage renders section header + coming soon', () => {
    renderAt('/admin/northwind/linkTypes', LinkTypeAdminPage);
    expect(screen.getByText(/Link Types/)).toBeInTheDocument();
    expect(screen.getByText(/Coming soon/i)).toBeInTheDocument();
    expect(screen.getByText('northwind')).toBeInTheDocument();
  });

  it('ActionTypeAdminPage renders the Action Types section', () => {
    renderAt('/admin/northwind/actionTypes', ActionTypeAdminPage);
    expect(screen.getByText(/Action Types/)).toBeInTheDocument();
  });

  it('InterfaceAdminPage renders the Interfaces section', () => {
    renderAt('/admin/northwind/interfaces', InterfaceAdminPage);
    expect(screen.getByText(/Interfaces/)).toBeInTheDocument();
  });

  it('SchemaGraphPage renders the Schema Graph section', () => {
    renderAt('/admin/northwind/graph', SchemaGraphPage);
    expect(screen.getByText(/Schema Graph/)).toBeInTheDocument();
  });

  it('AuditHistoryPage renders the Audit History section', () => {
    renderAt('/admin/northwind/history', AuditHistoryPage);
    expect(screen.getByText(/Audit History/)).toBeInTheDocument();
  });
});
