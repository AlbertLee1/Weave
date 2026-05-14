import { describe, it, expect, vi } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
import { SlidePanel } from '../SlidePanel';

describe('SlidePanel', () => {
  it('renders title and content when open', () => {
    render(
      <SlidePanel open={true} onClose={() => {}} title="Detail Panel">
        <p>Panel content</p>
      </SlidePanel>,
    );
    expect(screen.getByText('Detail Panel')).toBeInTheDocument();
    expect(screen.getByText('Panel content')).toBeInTheDocument();
  });

  it('applies translate-x-full when closed', () => {
    render(
      <SlidePanel open={false} onClose={() => {}} title="Panel">
        <p>Content</p>
      </SlidePanel>,
    );
    const panel = screen.getByTestId('slide-panel');
    expect(panel.className).toContain('translate-x-full');
  });

  // Dogfood Round 3 #1: closed panels still rendered in the DOM (only
  // translated off-screen) caused visual / e2e probes to mark the drawer
  // as "open" across routes. The panel root must declare its hidden state
  // semantically (aria-hidden) and refuse pointer events while closed so
  // it can't intercept clicks on the underlying page.
  it('marks the panel aria-hidden and disables pointer events when closed', () => {
    render(
      <SlidePanel open={false} onClose={() => {}} title="Panel">
        <p>Content</p>
      </SlidePanel>,
    );
    const panel = screen.getByTestId('slide-panel');
    expect(panel.getAttribute('aria-hidden')).toBe('true');
    expect(panel.className).toContain('pointer-events-none');
  });

  it('marks the panel aria-hidden=false and allows pointer events when open', () => {
    render(
      <SlidePanel open={true} onClose={() => {}} title="Panel">
        <p>Content</p>
      </SlidePanel>,
    );
    const panel = screen.getByTestId('slide-panel');
    expect(panel.getAttribute('aria-hidden')).toBe('false');
    expect(panel.className).not.toContain('pointer-events-none');
  });

  it('calls onClose when close button is clicked', () => {
    const onClose = vi.fn();
    render(
      <SlidePanel open={true} onClose={onClose} title="Panel">
        <p>Content</p>
      </SlidePanel>,
    );

    fireEvent.click(screen.getByRole('button', { name: /close panel/i }));
    expect(onClose).toHaveBeenCalled();
  });

  it('calls onClose on Escape key when open', () => {
    const onClose = vi.fn();
    render(
      <SlidePanel open={true} onClose={onClose} title="Panel">
        <p>Content</p>
      </SlidePanel>,
    );

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });
});
