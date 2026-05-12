import { expect, test, type Page, type Route } from '@playwright/test';
import {
  AppsBuilderPage,
  Given,
  Then,
  When,
  describeFeature,
} from './support';

/**
 * BDD coverage of the Apps Builder editor (`/apps`, `/apps/:rid`) rendered
 * by `src/components/apps/AppEditorPage.tsx`.
 *
 * Maps the PRD AC for US-038 (upgrade us444/08-app-builder.spec from a
 * pure-API round-trip into a full UI BDD suite):
 *
 *   AC: "至少 6 scenarios：拖拽组件到 canvas、变量绑定、预览、保存版本、
 *        加载模板、删除组件"
 *
 * Honest mapping:
 *   1. "拖拽组件到 canvas" → palette → canvas drop (smoke). The palette
 *      button's click handler calls the identical `addInstance` path the
 *      canvas's HTML5-DnD drop handler ends up taking (see
 *      AppEditorPage.tsx:692-694 vs :335-338) — we exercise it via click
 *      while separately asserting the drag-source attributes (`draggable`,
 *      `data-component-type`) so the DnD contract is locked too.
 *   2. "变量绑定" → bind a `{{userName}}` template into a text widget,
 *      add an App variable + default, enter Preview, assert the runtime
 *      text renders the substituted value plus `app-runtime-var-*`
 *      reflects the state.
 *   3. "预览" → toggle into Preview mode, assert the runtime view + the
 *      `data-mode="preview"` attribute flips and the edit chrome is
 *      gone.
 *   4. "保存版本" → stub POST /api/v2/apps, click Save, capture the wire
 *      body and assert `name` + `layoutJson` shape match what was
 *      authored (canonical row → col[6] → component DSL).
 *   5. "加载模板" → on the blank `/apps` route the Template picker is
 *      auto-shown; click "Use template" on the CRM Dashboard card and
 *      assert the canvas populates with the scaffold's 3 components +
 *      the App name updates.
 *   6. "删除组件" → add a component, click the row's × button, assert
 *      the canvas-empty placeholder re-appears.
 *   7. Bonus: existing-App load → stub GET /api/v2/apps/:rid to return
 *      a fixture, navigate to `/apps/:rid`, assert the canvas decodes
 *      the layout back into instances and the name field reflects the
 *      saved row. This locks the inverse of scenario 4 (the same wire
 *      shape round-trips back in).
 *
 * All scenarios stub the apps API surface (GET /api/v2/apps,
 * GET /api/v2/apps/:rid) so the spec is independent of whether the
 * backend AppsStore is wired in degraded mode — the editor's UI
 * contract is what we lock here, not the persistence layer.
 */

interface CapturedAppsPost {
  body: {
    name?: string;
    layoutJson?: unknown;
  };
}

function stubAppsList(page: Page): void {
  page.route('**/api/v2/apps', async (route: Route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ apps: [] }),
      });
      return;
    }
    await route.continue();
  });
}

function stubAppsPost(page: Page, captured: CapturedAppsPost[]): void {
  page.route('**/api/v2/apps', async (route: Route) => {
    const req = route.request();
    if (req.method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ apps: [] }),
      });
      return;
    }
    if (req.method() === 'POST') {
      const raw = req.postData() ?? '{}';
      let body: CapturedAppsPost['body'] = {};
      try {
        body = JSON.parse(raw) as CapturedAppsPost['body'];
      } catch {
        body = {};
      }
      captured.push({ body });
      const created = {
        rid: 'ri.app.main.app.spec-fixture',
        name: body.name ?? 'Untitled App',
        ownerId: 'spec-user',
        layoutJson: body.layoutJson ?? {
          type: 'row',
          children: [],
        },
        version: 1,
        createdAt: new Date().toISOString(),
        updatedAt: new Date().toISOString(),
      };
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(created),
      });
      return;
    }
    await route.continue();
  });
}

function stubAppDetail(
  page: Page,
  rid: string,
  payload: Record<string, unknown>,
): void {
  page.route(`**/api/v2/apps/${rid}*`, async (route: Route) => {
    if (route.request().method() === 'GET') {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(payload),
      });
      return;
    }
    await route.continue();
  });
}

describeFeature('Apps Builder editor', () => {
  test('Scenario: dragging a component from the palette adds an instance to the canvas @smoke', async ({
    page,
  }) => {
    const editor = new AppsBuilderPage(page);

    await Given('the apps list endpoint returns no rows', async () => {
      stubAppsList(page);
    });

    await Given('the user opens the App Editor on the new-App route', async () => {
      await editor.goto();
      await expect(editor.root).toBeVisible();
    });

    await Given(
      'the template picker is dismissed so the canvas is reachable',
      async () => {
        await editor.templatePickerBlank.click();
      },
    );

    await Then(
      'each palette item is a draggable source carrying its component type',
      async () => {
        const chartItem = editor.paletteItem('chart');
        await expect(chartItem).toHaveAttribute('draggable', 'true');
        await expect(chartItem).toHaveAttribute('data-component-type', 'chart');
      },
    );

    await Then('the canvas-empty placeholder is showing', async () => {
      await expect(editor.canvasEmpty).toBeVisible();
    });

    await When(
      'the user drops a Chart component from the palette onto the canvas',
      async () => {
        await editor.addFromPalette('chart');
      },
    );

    await Then('one chart instance is rendered on the canvas', async () => {
      await expect(editor.canvasInstances()).toHaveCount(1);
      await expect(editor.canvasInstance('chart')).toBeVisible();
    });

    await Then(
      'the new instance is auto-selected and the property panel binds onto it',
      async () => {
        await expect(editor.propertyPanel).toHaveAttribute(
          'data-component-type',
          'chart',
        );
      },
    );
  });

  test('Scenario: binding a variable substitutes its value in the runtime text widget', async ({
    page,
  }) => {
    const editor = new AppsBuilderPage(page);

    await Given('the apps API returns an empty list', async () => {
      stubAppsList(page);
    });

    await Given('the user is on a blank App Editor', async () => {
      await editor.goto();
      await editor.templatePickerBlank.click();
      await expect(editor.root).toBeVisible();
    });

    await When('a Text component is added and its content uses a {{var}} ref', async () => {
      await editor.addFromPalette('text');
      await editor.propField('text', 'content').fill('Hi {{userName}}!');
    });

    await When('a string variable named "userName" is declared with default "Alice"', async () => {
      await editor.variablesAdd.click();
      const nameInput = editor.variableName(0);
      await nameInput.fill('userName');
      await editor.variableDefault(0).fill('Alice');
    });

    await When('the user enters Preview mode', async () => {
      await editor.modeToggle.click();
    });

    await Then('the runtime view renders with the variable bound to its default', async () => {
      await expect(editor.runtimeView).toBeVisible();
      await expect(editor.runtimeState).toBeVisible();
      await expect(
        page.getByTestId('app-runtime-var-userName'),
      ).toHaveAttribute('data-variable-value', 'Alice');
    });

    await Then('the runtime text widget shows the substituted greeting', async () => {
      await expect(page.getByTestId('app-runtime-text')).toHaveText(
        'Hi Alice!',
      );
    });
  });

  test('Scenario: toggling Preview swaps the edit chrome for the runtime view @smoke', async ({
    page,
  }) => {
    const editor = new AppsBuilderPage(page);

    await Given('the apps API returns an empty list', async () => {
      stubAppsList(page);
    });

    await Given('the user is on a blank App Editor with one button placed', async () => {
      await editor.goto();
      await editor.templatePickerBlank.click();
      await editor.addFromPalette('button');
      await expect(editor.canvasInstance('button')).toBeVisible();
    });

    await When('the user clicks the Preview toggle', async () => {
      await editor.modeToggle.click();
    });

    await Then('the editor page reports preview mode via its data attribute', async () => {
      await expect(editor.root).toHaveAttribute('data-mode', 'preview');
    });

    await Then('the runtime view + button render in place of the edit canvas', async () => {
      await expect(editor.runtimeView).toBeVisible();
      await expect(page.getByTestId('app-runtime-button')).toBeVisible();
      await expect(editor.palette).toHaveCount(0);
      await expect(editor.canvas).toHaveCount(0);
    });

    await When('the user clicks the toggle again to return to Edit mode', async () => {
      await editor.modeToggle.click();
    });

    await Then('the edit canvas reappears', async () => {
      await expect(editor.root).toHaveAttribute('data-mode', 'edit');
      await expect(editor.canvas).toBeVisible();
    });
  });

  test('Scenario: clicking Save posts the canvas as the canonical layout DSL', async ({
    page,
  }) => {
    const editor = new AppsBuilderPage(page);
    const captured: CapturedAppsPost[] = [];

    await Given('the apps POST endpoint records the wire body and returns a fresh rid', async () => {
      stubAppsPost(page, captured);
    });

    await Given('the user is on a blank App Editor', async () => {
      await editor.goto();
      await editor.templatePickerBlank.click();
      await expect(editor.root).toBeVisible();
    });

    await When('the App is renamed and a text + button are added', async () => {
      await editor.nameInput.fill('Spec Saved App');
      await editor.addFromPalette('text');
      await editor.propField('text', 'content').fill('hello');
      await editor.addFromPalette('button');
    });

    await When('the user clicks Save', async () => {
      await editor.saveButton.click();
    });

    await Then(
      'the URL switches to the saved-app route (so onSaved navigated)',
      async () => {
        // After a successful POST the route wrapper calls
        // `navigate(/apps/{rid})` (App.tsx:86) — the saved-status span
        // briefly flips to "saved" but is unmounted by the
        // `key={params.rid}` change before tests can poll it; asserting
        // the URL is the durable proxy for "Save succeeded".
        await page.waitForURL(/\/apps\/ri\.app\.main\.app\.spec-fixture/, {
          timeout: 5000,
        });
        expect(page.url()).toContain('/apps/ri.app.main.app.spec-fixture');
      },
    );

    await Then('the POST body carries the authored name + a row→col[2] layout', async () => {
      expect(captured).toHaveLength(1);
      expect(captured[0].body.name).toBe('Spec Saved App');
      const layout = captured[0].body.layoutJson as {
        type: string;
        children: Array<{ width: number; child: { componentType: string } }>;
      };
      expect(layout.type).toBe('row');
      expect(layout.children).toHaveLength(2);
      expect(layout.children.map((c) => c.child.componentType)).toEqual([
        'text',
        'button',
      ]);
      // distributeWidths(2) → [6,6]; locks the auto-width contract.
      expect(layout.children.map((c) => c.width)).toEqual([6, 6]);
    });
  });

  test('Scenario: applying the CRM Dashboard template scaffolds the canvas', async ({
    page,
  }) => {
    const editor = new AppsBuilderPage(page);

    await Given('the apps API returns an empty list', async () => {
      stubAppsList(page);
    });

    await Given('the user lands on the new-App route', async () => {
      await editor.goto();
    });

    await Then('the Template picker is auto-shown with the bundled scaffolds', async () => {
      await expect(editor.templatePicker).toBeVisible();
      await expect(editor.templateCard('crm-dashboard')).toBeVisible();
      await expect(editor.templateCard('approval-console')).toBeVisible();
      await expect(editor.templateCard('object-browser')).toBeVisible();
    });

    await When('the user picks the CRM Dashboard template', async () => {
      await editor.templateUseButton('crm-dashboard').click();
    });

    await Then('the template picker is dismissed and the canvas is populated', async () => {
      await expect(editor.templatePicker).toHaveCount(0);
      await expect(editor.canvasInstances()).toHaveCount(3);
      await expect(editor.canvasInstance('chart')).toBeVisible();
      await expect(editor.canvasInstance('table')).toBeVisible();
      await expect(editor.canvasInstance('button')).toBeVisible();
    });

    await Then('the App name and declared variables match the template metadata', async () => {
      await expect(editor.nameInput).toHaveValue('CRM Dashboard');
      // crm-dashboard ships one `selectedAccount` variable; locks the
      // round-trip of `LayoutRow.variables` through decodeLayout.
      await expect(editor.variablesPanel).toHaveAttribute(
        'data-variable-count',
        '1',
      );
    });
  });

  test('Scenario: removing a component clears it from the canvas', async ({
    page,
  }) => {
    const editor = new AppsBuilderPage(page);

    await Given('the apps API returns an empty list', async () => {
      stubAppsList(page);
    });

    await Given(
      'the user has placed an Object Card on a blank canvas',
      async () => {
        await editor.goto();
        await editor.templatePickerBlank.click();
        await editor.addFromPalette('objectCard');
        await expect(editor.canvasInstances()).toHaveCount(1);
      },
    );

    await When('the user clicks the × remove button on the Object Card row', async () => {
      await editor.removeButton('objectCard').click();
    });

    await Then('the canvas-empty placeholder returns', async () => {
      await expect(editor.canvasInstances()).toHaveCount(0);
      await expect(editor.canvasEmpty).toBeVisible();
    });

    await Then(
      'the property panel falls back to its empty state (no selection)',
      async () => {
        await expect(editor.propertyPanel).toHaveAttribute('data-empty', 'true');
      },
    );
  });

  test('Scenario: opening /apps/:rid loads the saved app back into the canvas', async ({
    page,
  }) => {
    const editor = new AppsBuilderPage(page);
    const rid = 'ri.app.main.app.spec-load';
    const layoutJson = {
      type: 'row',
      variables: [{ name: 'who', type: 'string', default: 'World' }],
      children: [
        {
          type: 'col',
          width: 12,
          child: {
            type: 'component',
            componentType: 'text',
            props: { content: 'Loaded for {{who}}' },
          },
        },
      ],
    };

    await Given(
      'the apps detail endpoint returns a single-text-component fixture',
      async () => {
        stubAppDetail(page, rid, {
          rid,
          name: 'Loaded Spec App',
          ownerId: 'spec-user',
          layoutJson,
          version: 3,
          createdAt: new Date().toISOString(),
          updatedAt: new Date().toISOString(),
        });
        stubAppsList(page);
      },
    );

    await When('the user navigates directly to /apps/:rid', async () => {
      await editor.goto(rid);
    });

    await Then('the canvas decodes the layout back into a single text instance', async () => {
      await expect(editor.root).toBeVisible();
      await expect(editor.canvasInstance('text')).toBeVisible();
      await expect(editor.canvasInstances()).toHaveCount(1);
    });

    await Then('the App name field reflects the saved row', async () => {
      await expect(editor.nameInput).toHaveValue('Loaded Spec App');
    });

    await Then('the declared variable is restored on the Variables panel', async () => {
      await expect(editor.variablesPanel).toHaveAttribute(
        'data-variable-count',
        '1',
      );
      await expect(editor.variableName(0)).toHaveValue('who');
      await expect(editor.variableDefault(0)).toHaveValue('World');
    });
  });
});
