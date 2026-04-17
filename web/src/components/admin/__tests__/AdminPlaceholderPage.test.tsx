import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import {
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
