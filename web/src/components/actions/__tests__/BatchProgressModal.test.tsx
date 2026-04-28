import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { BatchProgressModal } from '../BatchProgressModal';
import * as actionsApi from '../../../api/actions';
import type { ActionJobProgressEvent } from '../../../hooks/useActionJobProgress';

type ProgressOptions = {
  ontologyApiName: string;
  jobId: string | null;
  enabled?: boolean;
  onTerminal?: (evt: ActionJobProgressEvent) => void;
};
type ProgressState = {
  percent: number;
  message: string;
  status: 'CONNECTING' | 'PENDING' | 'RUNNING' | 'SUCCEEDED' | 'FAILED' | 'CANCELED';
  connected: boolean;
  hasProgress: boolean;
};

// Spy slot for the most recent useActionJobProgress invocation. Each test
// reads the latest options and pushes events through the stub's setEvent.
let progressInvocation: {
  options: ProgressOptions | null;
  setEvent: (state: Partial<ProgressState>) => void;
} = { options: null, setEvent: () => {} };

vi.mock('../../../hooks/useActionJobProgress', async () => {
  const { useState } = await import('react');
  return {
    useActionJobProgress: (opts: ProgressOptions) => {
      const [state, setState] = useState({
        percent: 0,
        message: '',
        status: 'CONNECTING' as const,
        connected: false,
        hasProgress: false,
      });
      progressInvocation.options = opts;
      progressInvocation.setEvent = (next) =>
        setState((prev) => ({ ...prev, ...next } as typeof prev));
      return state;
    },
  };
});

describe('BatchProgressModal', () => {
  beforeEach(() => {
    progressInvocation = { options: null, setEvent: () => {} };
    vi.spyOn(actionsApi, 'cancelActionJob').mockResolvedValue({
      jobId: 'job-1',
      ontologyApiName: 'main',
      actionType: 'deleteEmployee',
      status: 'RUNNING',
      progress: 50,
      createdAt: '',
      updatedAt: '',
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('shows scheduling placeholder when jobId is not yet available', () => {
    render(
      <BatchProgressModal
        ontologyApiName="main"
        jobId={null}
        open={true}
        onClose={() => {}}
      />,
    );
    expect(screen.getByTestId('batch-scheduling')).toBeInTheDocument();
  });

  it('renders progress bar and percent on incoming events', async () => {
    render(
      <BatchProgressModal
        ontologyApiName="main"
        jobId="job-1"
        open={true}
        onClose={() => {}}
      />,
    );
    expect(screen.getByTestId('batch-progress-bar')).toHaveAttribute(
      'aria-valuenow',
      '0',
    );

    act(() => {
      progressInvocation.setEvent({
        percent: 42,
        message: 'half',
        status: 'RUNNING',
        connected: true,
        hasProgress: true,
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId('batch-percent')).toHaveTextContent('42%');
    });
    expect(screen.getByTestId('batch-progress-bar')).toHaveAttribute(
      'aria-valuenow',
      '42',
    );
    expect(screen.getByTestId('batch-message')).toHaveTextContent('half');
  });

  it('cancel button POSTs cancelActionJob and disappears on terminal status', async () => {
    const onClose = vi.fn();
    render(
      <BatchProgressModal
        ontologyApiName="main"
        jobId="job-2"
        open={true}
        onClose={onClose}
      />,
    );

    // Move into RUNNING so the cancel button renders.
    act(() => {
      progressInvocation.setEvent({
        percent: 25,
        status: 'RUNNING',
        connected: true,
        hasProgress: true,
      });
    });
    const cancelBtn = await screen.findByTestId('batch-cancel');
    fireEvent.click(cancelBtn);

    await waitFor(() => {
      expect(actionsApi.cancelActionJob).toHaveBeenCalledWith('main', 'job-2');
    });

    // Server confirms — the WS event the consumer would observe drives the
    // status flip.
    act(() => {
      progressInvocation.setEvent({
        percent: 25,
        status: 'CANCELED',
        connected: true,
        hasProgress: true,
      });
    });
    await waitFor(() => {
      expect(screen.queryByTestId('batch-cancel')).not.toBeInTheDocument();
    });
    expect(screen.getByTestId('batch-canceled')).toBeInTheDocument();
    // Close button is enabled once terminal.
    fireEvent.click(screen.getByTestId('batch-close'));
    expect(onClose).toHaveBeenCalled();
  });

  it('surfaces failure status with error tile', async () => {
    render(
      <BatchProgressModal
        ontologyApiName="main"
        jobId="job-3"
        open={true}
        onClose={() => {}}
      />,
    );

    act(() => {
      progressInvocation.setEvent({
        percent: 60,
        status: 'FAILED',
        connected: true,
        hasProgress: true,
      });
    });

    await waitFor(() => {
      expect(screen.getByTestId('batch-error')).toBeInTheDocument();
    });
    // Cancel button must be hidden once terminal.
    expect(screen.queryByTestId('batch-cancel')).not.toBeInTheDocument();
  });

  it('shows cancel error inline when the API rejects', async () => {
    vi.spyOn(actionsApi, 'cancelActionJob').mockRejectedValue(
      new Error('cancel rejected'),
    );
    render(
      <BatchProgressModal
        ontologyApiName="main"
        jobId="job-4"
        open={true}
        onClose={() => {}}
      />,
    );

    act(() => {
      progressInvocation.setEvent({
        percent: 50,
        status: 'RUNNING',
        connected: true,
        hasProgress: true,
      });
    });

    fireEvent.click(await screen.findByTestId('batch-cancel'));

    await waitFor(() => {
      expect(screen.getByTestId('batch-cancel-error')).toHaveTextContent(
        'cancel rejected',
      );
    });
  });

  it('disables Close button while job is in flight', async () => {
    render(
      <BatchProgressModal
        ontologyApiName="main"
        jobId="job-5"
        open={true}
        onClose={() => {}}
      />,
    );

    act(() => {
      progressInvocation.setEvent({
        percent: 40,
        status: 'RUNNING',
        connected: true,
        hasProgress: true,
      });
    });
    const closeBtn = await screen.findByTestId('batch-close');
    expect(closeBtn).toBeDisabled();

    act(() => {
      progressInvocation.setEvent({ percent: 100, status: 'SUCCEEDED' });
    });
    await waitFor(() => {
      expect(screen.getByTestId('batch-close')).not.toBeDisabled();
    });
  });
});
