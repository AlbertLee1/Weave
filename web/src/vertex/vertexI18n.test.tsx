// VTX-121 — Vertex i18n coverage gate.
//
// Every user-visible string emitted by the Vertex widget surface MUST come
// from the i18n resource tree under the `vertex.*` namespace, with both
// `zh-CN` and `en` translations defined. This suite renders each widget in
// both locales and asserts the locale-specific copy is wired through; that
// implicitly fails if the component still ships hard-coded English text.
//
// The suite uses an explicit `await changeLocale(...)` rather than mocking
// `useTranslation`, so a regression in the i18n bootstrap surfaces here
// instead of being masked by component-level stubs.

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router';

import { changeLocale, DEFAULT_LOCALE, i18n, resources, DEFAULT_NAMESPACE } from '../i18n';
import { VertexGraphWidget } from './VertexGraphWidget';
import { LayersDragPanel } from './LayersDragPanel';
import { MapOpenInVertexButton } from './MapOpenInVertexButton';
import { ScenarioCopilotButtons } from './ScenarioCopilotButtons';
import { ScenarioDebugDrawer, type ScenarioRunDebug } from './ScenarioDebugDrawer';
import { ScenarioPane } from './ScenarioPane';
import { ScenarioRetryPane, type ScenarioRetryEvent } from './ScenarioRetryPane';

afterEach(async () => {
  await changeLocale(DEFAULT_LOCALE);
});

function flatten(obj: Record<string, unknown>, prefix = '', acc: string[] = []): string[] {
  for (const [k, v] of Object.entries(obj)) {
    const next = prefix ? `${prefix}.${k}` : k;
    if (v && typeof v === 'object' && !Array.isArray(v)) {
      flatten(v as Record<string, unknown>, next, acc);
    } else {
      acc.push(next);
    }
  }
  return acc;
}

describe('Vertex i18n resource registration', () => {
  it('exposes a vertex.* namespace in both zh-CN and en', () => {
    const zhTree = resources['zh-CN'][DEFAULT_NAMESPACE] as Record<string, unknown>;
    const enTree = resources.en[DEFAULT_NAMESPACE] as Record<string, unknown>;
    expect(zhTree.vertex, 'vertex.* missing from zh-CN').toBeTruthy();
    expect(enTree.vertex, 'vertex.* missing from en').toBeTruthy();
  });

  it('zh-CN and en define the same set of vertex.* keys', () => {
    const zhTree = (resources['zh-CN'][DEFAULT_NAMESPACE] as Record<string, unknown>).vertex as
      | Record<string, unknown>
      | undefined;
    const enTree = (resources.en[DEFAULT_NAMESPACE] as Record<string, unknown>).vertex as
      | Record<string, unknown>
      | undefined;
    expect(zhTree).toBeTruthy();
    expect(enTree).toBeTruthy();
    const zhKeys = new Set(flatten(zhTree!));
    const enKeys = new Set(flatten(enTree!));
    expect([...zhKeys].filter((k) => !enKeys.has(k))).toEqual([]);
    expect([...enKeys].filter((k) => !zhKeys.has(k))).toEqual([]);
  });

  it('vertex.* keys are non-empty in both locales', () => {
    const zhTree = (resources['zh-CN'][DEFAULT_NAMESPACE] as Record<string, unknown>).vertex as
      | Record<string, unknown>
      | undefined;
    const enTree = (resources.en[DEFAULT_NAMESPACE] as Record<string, unknown>).vertex as
      | Record<string, unknown>
      | undefined;
    const walk = (obj: Record<string, unknown>) => {
      for (const v of Object.values(obj)) {
        if (v && typeof v === 'object' && !Array.isArray(v)) {
          walk(v as Record<string, unknown>);
        } else {
          expect(typeof v).toBe('string');
          expect((v as string).length).toBeGreaterThan(0);
        }
      }
    };
    walk(zhTree!);
    walk(enTree!);
  });

  it('does not retain stale diagramming placeholder keys', () => {
    const zhTree = (resources['zh-CN'][DEFAULT_NAMESPACE] as Record<string, unknown>).vertex as
      | Record<string, unknown>
      | undefined;
    const enTree = (resources.en[DEFAULT_NAMESPACE] as Record<string, unknown>).vertex as
      | Record<string, unknown>
      | undefined;
    expect(zhTree).toBeTruthy();
    expect(enTree).toBeTruthy();
    expect(flatten(zhTree!)).not.toEqual(expect.arrayContaining([expect.stringMatching(/^diagramming\./)]));
    expect(flatten(enTree!)).not.toEqual(expect.arrayContaining([expect.stringMatching(/^diagramming\./)]));
  });
});

describe('VertexGraphWidget — i18n', () => {
  it('renders English copy under en', async () => {
    await changeLocale('en');
    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        loader={async () => ({})}
      />,
    );
    await waitFor(() => {
      expect(screen.getByTestId('vertex-widget-save').textContent).toBe('Save');
    });
  });

  it('renders Chinese copy after switching to zh-CN', async () => {
    await changeLocale('zh-CN');
    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        loader={async () => ({})}
      />,
    );
    await waitFor(() => {
      expect(screen.getByTestId('vertex-widget-save').textContent).toBe('保存');
    });
  });
});

describe('LayersDragPanel — i18n', () => {
  beforeEach(async () => {
    await changeLocale('en');
  });

  it('Layers heading + drop-here placeholder in English', () => {
    render(<LayersDragPanel layers={[]} search={async () => []} onObjectsLoaded={() => {}} />);
    expect(screen.getByTestId('vertex-layers-panel').textContent).toContain('Layers');
    expect(screen.getByTestId('vertex-graph-canvas').textContent).toContain('Drop a layer here');
  });

  it('Layers heading + drop-here placeholder in Chinese', async () => {
    await changeLocale('zh-CN');
    render(<LayersDragPanel layers={[]} search={async () => []} onObjectsLoaded={() => {}} />);
    expect(screen.getByTestId('vertex-layers-panel').textContent).toContain('图层');
    expect(screen.getByTestId('vertex-graph-canvas').textContent).toContain('拖动图层到此');
  });
});

describe('MapOpenInVertexButton — i18n', () => {
  it('renders "Open in Vertex" in English', async () => {
    await changeLocale('en');
    render(
      <MemoryRouter>
        <MapOpenInVertexButton selected={null} />
      </MemoryRouter>,
    );
    expect(screen.getByTestId('map-open-in-vertex').textContent).toBe('Open in Vertex');
  });

  it('renders 在 Vertex 中打开 in Chinese', async () => {
    await changeLocale('zh-CN');
    render(
      <MemoryRouter>
        <MapOpenInVertexButton selected={null} />
      </MemoryRouter>,
    );
    expect(screen.getByTestId('map-open-in-vertex').textContent).toContain('Vertex');
    expect(screen.getByTestId('map-open-in-vertex').textContent).toContain('打开');
  });
});

describe('ScenarioCopilotButtons — i18n', () => {
  it('English: Suggest Override + Explain Result', async () => {
    await changeLocale('en');
    render(<ScenarioCopilotButtons scenarioRid="ri.s1" hasResult={false} />);
    expect(screen.getByTestId('copilot-suggest').textContent).toBe('Suggest Override');
    expect(screen.getByTestId('copilot-explain').textContent).toBe('Explain Result');
  });

  it('Chinese: 推荐覆盖 + 解释结果', async () => {
    await changeLocale('zh-CN');
    render(<ScenarioCopilotButtons scenarioRid="ri.s1" hasResult={false} />);
    expect(screen.getByTestId('copilot-suggest').textContent).toBe('推荐覆盖');
    expect(screen.getByTestId('copilot-explain').textContent).toBe('解释结果');
  });
});

describe('ScenarioDebugDrawer — i18n', () => {
  const sample: ScenarioRunDebug = {
    scenarioRunRid: 'ri.run.x',
    inputSnapshot: { ok: true },
    functionLogs: ['log1'],
    partialEdits: [],
  };

  it('English: Close + Input snapshot + Function logs', async () => {
    await changeLocale('en');
    render(<ScenarioDebugDrawer scenarioRunRid={sample.scenarioRunRid} onClose={() => {}} fetcher={async () => sample} />);
    await waitFor(() => {
      expect(screen.getByTestId('scenario-debug-input').textContent).toContain('Input snapshot');
    });
    expect(screen.getByTestId('scenario-debug-close').textContent).toBe('Close');
    expect(screen.getByTestId('scenario-debug-logs').textContent).toContain('Function logs');
  });

  it('Chinese: 关闭 + 输入快照 + 函数日志', async () => {
    await changeLocale('zh-CN');
    render(<ScenarioDebugDrawer scenarioRunRid={sample.scenarioRunRid} onClose={() => {}} fetcher={async () => sample} />);
    await waitFor(() => {
      expect(screen.getByTestId('scenario-debug-input').textContent).toContain('输入快照');
    });
    expect(screen.getByTestId('scenario-debug-close').textContent).toBe('关闭');
    expect(screen.getByTestId('scenario-debug-logs').textContent).toContain('函数日志');
  });
});

describe('ScenarioPane — i18n', () => {
  it('English Run button', async () => {
    await changeLocale('en');
    render(<ScenarioPane onRun={() => {}} />);
    expect(screen.getByTestId('scenario-pane-run').textContent).toBe('Run');
  });

  it('Chinese 运行 button', async () => {
    await changeLocale('zh-CN');
    render(<ScenarioPane onRun={() => {}} />);
    expect(screen.getByTestId('scenario-pane-run').textContent).toBe('运行');
  });
});

describe('ScenarioRetryPane — i18n', () => {
  const events: ScenarioRetryEvent[] = [
    { activityId: 'a', attempt: 1, error: 'e', occurredAt: '2026-05-15T10:00:00Z' },
  ];

  it('English empty + counter copy', async () => {
    await changeLocale('en');
    const { unmount } = render(<ScenarioRetryPane events={[]} />);
    expect(screen.getByTestId('scenario-retry-empty').textContent).toBe('No retries yet.');
    unmount();
    render(<ScenarioRetryPane events={events} />);
    expect(screen.getByTestId('scenario-retry-counter-a').textContent).toContain('retries: 1');
  });

  it('Chinese empty + counter copy', async () => {
    await changeLocale('zh-CN');
    const { unmount } = render(<ScenarioRetryPane events={[]} />);
    expect(screen.getByTestId('scenario-retry-empty').textContent).toContain('暂无重试');
    unmount();
    render(<ScenarioRetryPane events={events} />);
    expect(screen.getByTestId('scenario-retry-counter-a').textContent).toContain('重试次数');
  });
});

describe('i18n smoke — runtime language change re-renders Vertex copy', () => {
  it('VertexGraphWidget Save label flips when locale changes', async () => {
    await changeLocale('en');
    render(
      <VertexGraphWidget
        graphRid="ri.vertex.main.graph.alpha"
        loader={async () => ({})}
      />,
    );
    await waitFor(() => {
      expect(screen.getByTestId('vertex-widget-save').textContent).toBe('Save');
    });
    await act(async () => {
      await changeLocale('zh-CN');
    });
    await waitFor(() => {
      expect(screen.getByTestId('vertex-widget-save').textContent).toBe('保存');
    });
    // Sanity: i18n instance reports the new language.
    expect(i18n.language).toBe('zh-CN');
  });
});
