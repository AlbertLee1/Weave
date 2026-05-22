import { describe, expect, it } from 'vitest';
import readmeSource from '../../../README.md?raw';

describe('web README contract', () => {
  it('documents the Weave Web UI setup, verification, E2E, and browser dogfood surfaces', () => {
    expect(readmeSource).toContain('# Weave Web UI');
    expect(readmeSource).not.toContain('# React + TypeScript + Vite');
    expect(readmeSource).not.toContain('This template provides a minimal setup');

    const requiredCommands = [
      'npm install',
      'npm run dev',
      'npm test',
      'npm run typecheck',
      'npm run build',
      'make e2e-up',
      'npm run test:e2e',
      'make e2e-down',
    ];

    for (const command of requiredCommands) {
      expect(readmeSource).toContain(command);
    }

    const requiredTestLocations = ['web/tests/', 'web/tests/support/', 'web/e2e/'];

    for (const location of requiredTestLocations) {
      expect(readmeSource).toContain(location);
    }

    const primarySurfaces = [
      'Dashboard',
      'Browser',
      'ObjectSets',
      'Quiver',
      'Import Data',
      'Admin',
      'Command Palette',
    ];

    for (const surface of primarySurfaces) {
      expect(readmeSource).toContain(surface);
    }
  });
});
