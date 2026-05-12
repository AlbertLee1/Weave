import { test } from '@playwright/test';

/**
 * BDD-flavoured step helpers built on top of Playwright's `test.step()`.
 *
 * `Given` / `When` / `Then` / `And` wrap the block body in a labelled step
 * so the Playwright HTML report and trace viewer point directly at the
 * failing Gherkin clause. Calling them is purely a labelling concern —
 * the body still has full access to the surrounding test's fixtures via
 * closure. They are intentionally thin (no playwright-bdd / cucumber
 * dependency per US-002 AC).
 *
 * Usage inside a Scenario:
 *
 *   await Given('the user is on the Browser page', async () => {
 *     await browser.goto('northwind', 'employee');
 *   });
 *   await When('they search for "alice"', async () => {
 *     await browser.typeSearch('alice');
 *   });
 *   await Then('the search input reflects the value', async () => {
 *     await expect(browser.searchInput).toHaveValue('alice');
 *   });
 */

type StepBody<T> = () => Promise<T> | T;

function step<T>(label: string, body: StepBody<T>): Promise<T> {
  return test.step(label, async () => body());
}

export function Given<T>(name: string, body: StepBody<T>): Promise<T> {
  return step(`Given ${name}`, body);
}

export function When<T>(name: string, body: StepBody<T>): Promise<T> {
  return step(`When ${name}`, body);
}

export function Then<T>(name: string, body: StepBody<T>): Promise<T> {
  return step(`Then ${name}`, body);
}

export function And<T>(name: string, body: StepBody<T>): Promise<T> {
  return step(`And ${name}`, body);
}

/**
 * `describeFeature(name, body)` is a thin wrapper around `test.describe`
 * that prefixes the suite title with "Feature: " so spec output mirrors
 * Gherkin terminology. Scenarios inside the body should keep using the
 * standard `test('Scenario: ...', ...)` form.
 */
export function describeFeature(name: string, body: () => void): void {
  test.describe(`Feature: ${name}`, body);
}
