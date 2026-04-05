import { Outlet } from 'react-router';
import { Sidebar } from './Sidebar';
import { Topbar } from './Topbar';

export function Shell() {
  return (
    <div className="flex h-screen bg-bg-primary text-text-primary font-sans">
      {/* Top accent line: 1px gradient amber → teal → transparent */}
      <div
        className="fixed top-0 left-0 right-0 z-50 h-px"
        style={{
          background:
            'linear-gradient(90deg, #F59E0B 0%, #14B8A6 50%, transparent 100%)',
        }}
      />
      <Sidebar />
      <div className="flex flex-col flex-1 min-w-0">
        <Topbar />
        <main
          className="flex-1 overflow-auto p-6"
          style={{
            background:
              'linear-gradient(135deg, rgba(245,158,11,0.02) 0%, transparent 40%, rgba(20,184,166,0.02) 100%)',
          }}
        >
          <Outlet />
        </main>
      </div>
    </div>
  );
}
