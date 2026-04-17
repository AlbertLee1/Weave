import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter, Routes, Route } from 'react-router';
import { AuditHistoryPage } from '../AdminPlaceholderPage';

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
  it('AuditHistoryPage renders the Audit History section', () => {
    renderAt('/admin/northwind/history', AuditHistoryPage);
    expect(screen.getByText(/Audit History/)).toBeInTheDocument();
  });
});
