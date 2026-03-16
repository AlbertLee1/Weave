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
