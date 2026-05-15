import { describe, it, expect } from 'vitest';
import {
  computeAlertedNodes,
  isAlerted,
  type AlertableNode,
  type AlertConfig,
} from './alertBadge';

const nodes: AlertableNode[] = [
  { id: 'n1', properties: { alertScore: 5 } },
  { id: 'n2', properties: { alertScore: 80 } },
  { id: 'n3', properties: { alertScore: 200 } },
  { id: 'n4', properties: { alertScore: null } },
  { id: 'n5', properties: {} },
];

describe('VTX-097 isAlerted', () => {
  const cfg: AlertConfig = { property: 'alertScore', threshold: 50 };

  it('given_ValueAboveThreshold_then_True', () => {
    expect(isAlerted(nodes[1], cfg)).toBe(true);
  });

  it('given_ValueAtThreshold_then_False', () => {
    expect(isAlerted({ id: 'x', properties: { alertScore: 50 } }, cfg)).toBe(false);
  });

  it('given_ValueBelowThreshold_then_False', () => {
    expect(isAlerted(nodes[0], cfg)).toBe(false);
  });

  it('given_NullValue_then_False', () => {
    expect(isAlerted(nodes[3], cfg)).toBe(false);
  });

  it('given_MissingProperty_then_False', () => {
    expect(isAlerted(nodes[4], cfg)).toBe(false);
  });
});

describe('VTX-097 computeAlertedNodes', () => {
  const cfg: AlertConfig = { property: 'alertScore', threshold: 50 };

  it('given_LiveModeOn_when_Compute_then_OnlyAboveThresholdAlerted', () => {
    const r = computeAlertedNodes(nodes, cfg, { liveMode: true });
    expect([...r.alerted].sort()).toEqual(['n2', 'n3']);
  });

  it('given_LiveModeOff_when_Compute_then_NoAlerts', () => {
    const r = computeAlertedNodes(nodes, cfg, { liveMode: false });
    expect(r.alerted.size).toBe(0);
  });

  it('given_PausedDuringTransition_when_Compute_then_NoAlerts', () => {
    const r = computeAlertedNodes(nodes, cfg, { liveMode: true, paused: true });
    expect(r.alerted.size).toBe(0);
  });
});
