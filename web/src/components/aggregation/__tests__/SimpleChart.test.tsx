import { describe, it, expect } from 'vitest';
import { render } from '@testing-library/react';
import { SimpleChart } from '../SimpleChart';

describe('SimpleChart', () => {
  it('renders correct number of bars', () => {
    const data = [
      { group: { dept: 'Eng' }, metrics: { count: 10 } },
      { group: { dept: 'Sales' }, metrics: { count: 5 } },
      { group: { dept: 'HR' }, metrics: { count: 3 } },
    ];

    const { container } = render(
      <SimpleChart data={data} metricKey="count" />,
    );
    const rects = container.querySelectorAll('rect[data-bar]');
    expect(rects).toHaveLength(3);
  });

  it('renders nothing for empty data', () => {
    const { container } = render(
      <SimpleChart data={[]} metricKey="count" />,
    );
    const rects = container.querySelectorAll('rect[data-bar]');
    expect(rects).toHaveLength(0);
  });

  it('Given one positive bucket, When pie chart renders, Then it draws a full circle', () => {
    const data = [
      { group: { dept: 'Eng' }, metrics: { count: 10 } },
    ];

    const { container } = render(
      <SimpleChart data={data} metricKey="count" chartType="pie" />,
    );

    expect(container.querySelectorAll('circle[data-pie-full-slice]')).toHaveLength(1);
    expect(container.querySelectorAll('path[data-pie-slice]')).toHaveLength(0);
  });

  it('Given multiple positive buckets, When pie chart renders, Then it keeps slice paths', () => {
    const data = [
      { group: { dept: 'Eng' }, metrics: { count: 10 } },
      { group: { dept: 'Sales' }, metrics: { count: 5 } },
    ];

    const { container } = render(
      <SimpleChart data={data} metricKey="count" chartType="pie" />,
    );

    expect(container.querySelectorAll('path[data-pie-slice]')).toHaveLength(2);
    expect(container.querySelectorAll('circle[data-pie-full-slice]')).toHaveLength(0);
  });
});
