import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  InterfaceMethodsPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/methods/:ontology/:objectType/:primaryKey` — the
 * Interface Methods Console rendered by
 * `src/components/browser/InterfaceMethodsConsolePage.tsx` (US-047,
 * PC-A04).
 *
 * Scenarios map to the US-047 acceptance criteria:
 *
 *   - "Browser 对象详情面板新增 Interface Methods 抽屉" → entry-point
 *     button on ObjectDetail links to `/methods/{o}/{ot}/{pk}`; the
 *     console itself is a focused page so the param form + result
 *     panel have room.
 *   - "列出对象类型实现的所有接口及方法" → render scenario seeds two
 *     attached interfaces with two methods each, asserts the rail +
 *     method list render with stable data-* attributes.
 *   - "点方法 → 弹参数表单 → 调 interfaces/{iface}/methods/{m}/execute"
 *     → invoke scenario fills the param form, submits, asserts the
 *     POST body shape (objectType + parameters map) hits
 *     `/interfaces/methods/{methodRid}/invoke`. (Honest mapping: the
 *     real backend path is `/invoke`, not `/execute` as the AC
 *     phrased it — see pkg/oms/admin_handlers_interface_method.go.)
 *   - "返回值/错误/审计日志展示" → result panel surfaces the resolved
 *     ActionType apiName + the JSON result body; the audit link
 *     navigates to `/actions/{ontology}/history?actionType=<resolved>`
 *     so operators can audit the underlying ActionType run.
 *
 * Every scenario stubs the backend through `page.route` so the page
 * renders deterministic fixtures without touching real PG.
 */

const ONTOLOGY = 'northwind';
const OBJECT_TYPE = 'employee';
const PRIMARY_KEY = '17';

interface CapturedRequest {
  url: string;
  method: string;
  body: unknown;
}

interface MockInterface {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
}

interface MockInterfaceMethod {
  rid: string;
  interfaceRid: string;
  name: string;
  params: { name: string; type: string; required?: boolean }[];
  returns: { type: string };
  description?: string;
}

interface MockInvokeResponse {
  actionTypeRid: string;
  actionTypeApiName: string;
  objectType: string;
  methodRid: string;
  result?: unknown;
}

interface Stubs {
  objectTypeRid: string;
  interfaces: MockInterface[];
  attachments: { objectTypeRid: string; interfaceRid: string }[];
  methodsByInterface: Record<string, MockInterfaceMethod[]>;
  invokes: CapturedRequest[];
  invokeResponseByMethod: Record<string, MockInvokeResponse>;
  failInvokeWith: { errorName: string; reason: string } | null;
}

function newStubs(): Stubs {
  return {
    objectTypeRid: 'ri.oms.main.object-type.northwind-employee',
    interfaces: [],
    attachments: [],
    methodsByInterface: {},
    invokes: [],
    invokeResponseByMethod: {},
    failInvokeWith: null,
  };
}

async function stubEndpoints(page: Page, stubs: Stubs): Promise<void> {
  // GET /api/v2/ontologies/{o}/objectTypes/{ot}
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/${OBJECT_TYPE}*`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          rid: stubs.objectTypeRid,
          apiName: OBJECT_TYPE,
          displayName: 'Employee',
          primaryKey: 'employeeId',
          primaryKeys: ['employeeId'],
          status: 'ACTIVE',
          visibility: 'PROMINENT',
          properties: {},
        }),
      });
    },
  );

  // GET /api/v2/ontologies/{o}/interfacesAdmin
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/interfacesAdmin*`,
    async (route: Route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: stubs.interfaces }),
      });
    },
  );

  // GET /api/v2/ontologies/{o}/objectTypes/byRid/{rid}/interfaces
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/byRid/*/interfaces*`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      const url = route.request().url();
      const m = url.match(/\/objectTypes\/byRid\/([^/?#]+)\/interfaces/);
      const otRid = m ? decodeURIComponent(m[1]) : '';
      const data = stubs.attachments
        .filter((a) => a.objectTypeRid === otRid)
        .map((a) => ({
          objectTypeRid: a.objectTypeRid,
          interfaceRid: a.interfaceRid,
          propertyMapping: {},
        }));
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data }),
      });
    },
  );

  // POST /api/v2/ontologies/{o}/interfaces/methods/{methodRid}/invoke
  // — register BEFORE the methods list pattern so Playwright's LIFO
  // resolution prefers the more-specific path. (See US-023 pattern.)
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/interfaces/methods/*/invoke`,
    async (route: Route) => {
      const url = route.request().url();
      const body = route.request().postDataJSON();
      stubs.invokes.push({ url, method: route.request().method(), body });
      if (stubs.failInvokeWith) {
        const { errorName, reason } = stubs.failInvokeWith;
        stubs.failInvokeWith = null;
        await route.fulfill({
          status: 500,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'INTERNAL',
            errorName,
            errorInstanceId: 'spec',
            parameters: { reason },
          }),
        });
        return;
      }
      const m = url.match(/\/interfaces\/methods\/([^/?#]+)\/invoke/);
      const methodRid = m ? decodeURIComponent(m[1]) : '';
      const resp = stubs.invokeResponseByMethod[methodRid];
      if (!resp) {
        await route.fulfill({
          status: 404,
          contentType: 'application/json',
          body: JSON.stringify({
            errorCode: 'NOT_FOUND',
            errorName: 'InterfaceMethodNotFound',
            errorInstanceId: 'spec',
            parameters: { methodRid },
          }),
        });
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify(resp),
      });
    },
  );

  // GET /api/v2/ontologies/{o}/interfaces/{interfaceRid}/methods
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/interfaces/*/methods*`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      const url = route.request().url();
      const m = url.match(/\/interfaces\/([^/?#]+)\/methods/);
      const interfaceRid = m ? decodeURIComponent(m[1]) : '';
      const data = stubs.methodsByInterface[interfaceRid] ?? [];
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data }),
      });
    },
  );
}

describeFeature('Interface Methods Console', () => {
  test('Scenario: renders attached interfaces and their methods @smoke', async ({
    page,
    request,
  }) => {
    const stubs = newStubs();
    stubs.interfaces = [
      {
        rid: 'ri.oms.main.interface.payable',
        apiName: 'Payable',
        displayName: 'Payable',
        description: 'Anything that can be paid.',
      },
      {
        rid: 'ri.oms.main.interface.addressable',
        apiName: 'Addressable',
        displayName: 'Addressable',
        description: 'Anything that has an address.',
      },
    ];
    stubs.attachments = [
      {
        objectTypeRid: stubs.objectTypeRid,
        interfaceRid: 'ri.oms.main.interface.payable',
      },
      {
        objectTypeRid: stubs.objectTypeRid,
        interfaceRid: 'ri.oms.main.interface.addressable',
      },
    ];
    stubs.methodsByInterface = {
      'ri.oms.main.interface.payable': [
        {
          rid: 'ri.oms.main.interface-method.pay',
          interfaceRid: 'ri.oms.main.interface.payable',
          name: 'pay',
          params: [
            { name: 'amount', type: 'double', required: true },
            { name: 'currency', type: 'string', required: false },
          ],
          returns: { type: 'string' },
          description: 'Pay the entity.',
        },
        {
          rid: 'ri.oms.main.interface-method.refund',
          interfaceRid: 'ri.oms.main.interface.payable',
          name: 'refund',
          params: [{ name: 'amount', type: 'double', required: true }],
          returns: { type: 'boolean' },
        },
      ],
      'ri.oms.main.interface.addressable': [
        {
          rid: 'ri.oms.main.interface-method.updateAddress',
          interfaceRid: 'ri.oms.main.interface.addressable',
          name: 'updateAddress',
          params: [{ name: 'address', type: 'string', required: true }],
          returns: { type: 'boolean' },
        },
      ],
    };

    const console_ = new InterfaceMethodsPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('two interfaces are attached with declared methods', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the Interface Methods console for employee #17', async () => {
      await console_.goto(ONTOLOGY, OBJECT_TYPE, PRIMARY_KEY);
      await expect(console_.root).toBeVisible();
    });

    await Then('the header lists the ontology / object type / primary key', async () => {
      await expect(console_.header).toHaveAttribute(
        'data-ontology-api-name',
        ONTOLOGY,
      );
      await expect(console_.header).toHaveAttribute(
        'data-object-type-api-name',
        OBJECT_TYPE,
      );
      await expect(console_.header).toHaveAttribute(
        'data-primary-key',
        PRIMARY_KEY,
      );
    });

    await Then('the rail renders one row per attached interface', async () => {
      await expect(console_.interfaceRows()).toHaveCount(2);
      await expect(
        console_.interfaceRowByApiName('Addressable'),
      ).toBeVisible();
      await expect(console_.interfaceRowByApiName('Payable')).toBeVisible();
    });

    await Then('the first interface auto-selects and its methods render', async () => {
      // Rail rows are sorted by display name (Addressable < Payable).
      const addressableRow = console_.interfaceRowByApiName('Addressable');
      await expect(addressableRow).toHaveAttribute(
        'data-interface-selected',
        'true',
      );
      await expect(console_.methodRows()).toHaveCount(1);
      await expect(console_.methodRowByName('updateAddress')).toBeVisible();
      await expect(console_.methodRowByName('updateAddress')).toHaveAttribute(
        'data-method-param-count',
        '1',
      );
    });

    await When('the user picks the Payable interface', async () => {
      await console_.interfaceButtonByApiName('Payable').click();
    });

    await Then('the methods list updates to Payable methods', async () => {
      await expect(console_.methodRows()).toHaveCount(2);
      await expect(console_.methodRowByName('pay')).toBeVisible();
      await expect(console_.methodRowByName('refund')).toBeVisible();
      await expect(console_.methodRowByName('pay')).toHaveAttribute(
        'data-method-param-count',
        '2',
      );
    });
  });

  test('Scenario: invoking a method posts to /invoke and renders the result + audit link @smoke', async ({
    page,
    request,
  }) => {
    const stubs = newStubs();
    stubs.interfaces = [
      {
        rid: 'ri.oms.main.interface.payable',
        apiName: 'Payable',
        displayName: 'Payable',
      },
    ];
    stubs.attachments = [
      {
        objectTypeRid: stubs.objectTypeRid,
        interfaceRid: 'ri.oms.main.interface.payable',
      },
    ];
    const payMethodRid = 'ri.oms.main.interface-method.pay';
    stubs.methodsByInterface = {
      'ri.oms.main.interface.payable': [
        {
          rid: payMethodRid,
          interfaceRid: 'ri.oms.main.interface.payable',
          name: 'pay',
          params: [
            { name: 'amount', type: 'double', required: true },
            { name: 'currency', type: 'string', required: false },
          ],
          returns: { type: 'string' },
        },
      ],
    };
    stubs.invokeResponseByMethod = {
      [payMethodRid]: {
        actionTypeRid: 'ri.oms.main.action-type.payEmployee',
        actionTypeApiName: 'payEmployee',
        objectType: OBJECT_TYPE,
        methodRid: payMethodRid,
        result: {
          ok: true,
          transactionId: 'tx-9001',
          amount: 250.5,
          currency: 'USD',
        },
      },
    };

    const console_ = new InterfaceMethodsPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('one interface with one method is attached', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user opens the console and selects the pay method', async () => {
      await console_.goto(ONTOLOGY, OBJECT_TYPE, PRIMARY_KEY);
      await expect(console_.root).toBeVisible();
      await expect(console_.methodRowByName('pay')).toBeVisible();
      // Auto-selected because it's the first (and only) method.
      await expect(console_.invokeForm).toBeVisible();
    });

    await When('they fill the parameter form and submit', async () => {
      await console_.paramInput('amount').fill('250.5');
      await console_.paramInput('currency').fill('USD');
      // Use form.requestSubmit() to sidestep the "submit button might
      // be below the viewport" trap covered by the US-029 pattern.
      await console_.invokeForm.evaluate((f) =>
        (f as HTMLFormElement).requestSubmit(),
      );
    });

    await Then('the backend receives a single POST with the right body', async () => {
      await expect.poll(() => stubs.invokes.length).toBe(1);
      const invoke = stubs.invokes[0]!;
      expect(invoke.method).toBe('POST');
      expect(invoke.url).toContain(`/interfaces/methods/${payMethodRid}/invoke`);
      expect(invoke.body).toMatchObject({
        objectType: OBJECT_TYPE,
        parameters: { amount: 250.5, currency: 'USD' },
      });
    });

    await Then('the result panel surfaces the dispatched ActionType + payload', async () => {
      await expect(console_.result).toBeVisible();
      await expect(console_.result).toHaveAttribute(
        'data-action-type-api-name',
        'payEmployee',
      );
      await expect(console_.resultAction).toContainText('payEmployee');
      await expect(console_.resultBody).toContainText('tx-9001');
      await expect(console_.resultBody).toContainText('250.5');
    });

    await Then('an audit-trail link navigates to the action history view', async () => {
      await expect(console_.auditLink).toBeVisible();
      await expect(console_.auditLink).toHaveAttribute(
        'data-action-type-api-name',
        'payEmployee',
      );
      await console_.auditLink.click();
      await expect(page).toHaveURL(
        new RegExp(
          `/actions/${ONTOLOGY}/history\\?actionType=payEmployee$`,
        ),
      );
    });
  });

  test('Scenario: backend invocation error surfaces an inline banner and skips the result panel', async ({
    page,
    request,
  }) => {
    const stubs = newStubs();
    stubs.interfaces = [
      {
        rid: 'ri.oms.main.interface.payable',
        apiName: 'Payable',
        displayName: 'Payable',
      },
    ];
    stubs.attachments = [
      {
        objectTypeRid: stubs.objectTypeRid,
        interfaceRid: 'ri.oms.main.interface.payable',
      },
    ];
    const payMethodRid = 'ri.oms.main.interface-method.pay';
    stubs.methodsByInterface = {
      'ri.oms.main.interface.payable': [
        {
          rid: payMethodRid,
          interfaceRid: 'ri.oms.main.interface.payable',
          name: 'pay',
          params: [{ name: 'amount', type: 'double', required: true }],
          returns: { type: 'string' },
        },
      ],
    };
    stubs.failInvokeWith = {
      errorName: 'NoImplementingAction',
      reason: 'No ActionType implements pay for employee.',
    };

    const console_ = new InterfaceMethodsPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('the backend will reject the next invoke', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user submits the invoke form', async () => {
      await console_.goto(ONTOLOGY, OBJECT_TYPE, PRIMARY_KEY);
      await expect(console_.invokeForm).toBeVisible();
      await console_.paramInput('amount').fill('99');
      await console_.invokeForm.evaluate((f) =>
        (f as HTMLFormElement).requestSubmit(),
      );
    });

    await Then('a server-error banner surfaces the apierror', async () => {
      await expect(console_.serverError).toBeVisible();
      await expect(console_.serverError).toContainText('NoImplementingAction');
      await expect(console_.serverError).toContainText(
        'No ActionType implements pay for employee.',
      );
    });

    await Then('the result panel is not rendered', async () => {
      await expect(console_.result).toHaveCount(0);
      await expect(console_.auditLink).toHaveCount(0);
    });

    await Then('the form remains interactable for a retry', async () => {
      await expect(console_.invokeForm).toBeVisible();
      await expect(console_.invokeSubmit).toBeEnabled();
    });
  });

  test('Scenario: required-param validation short-circuits zero POST', async ({
    page,
    request,
  }) => {
    const stubs = newStubs();
    stubs.interfaces = [
      {
        rid: 'ri.oms.main.interface.payable',
        apiName: 'Payable',
        displayName: 'Payable',
      },
    ];
    stubs.attachments = [
      {
        objectTypeRid: stubs.objectTypeRid,
        interfaceRid: 'ri.oms.main.interface.payable',
      },
    ];
    stubs.methodsByInterface = {
      'ri.oms.main.interface.payable': [
        {
          rid: 'ri.oms.main.interface-method.pay',
          interfaceRid: 'ri.oms.main.interface.payable',
          name: 'pay',
          params: [{ name: 'amount', type: 'double', required: true }],
          returns: { type: 'string' },
        },
      ],
    };

    const console_ = new InterfaceMethodsPage(page);

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given('a method with a required amount parameter is shown', async () => {
      await stubEndpoints(page, stubs);
    });

    await When('the user submits without filling the required field', async () => {
      await console_.goto(ONTOLOGY, OBJECT_TYPE, PRIMARY_KEY);
      await expect(console_.invokeForm).toBeVisible();
      await console_.invokeForm.evaluate((f) =>
        (f as HTMLFormElement).requestSubmit(),
      );
    });

    await Then('a client-side validation banner appears', async () => {
      await expect(console_.paramError).toBeVisible();
      await expect(console_.paramError).toContainText('amount');
      await expect(console_.paramError).toContainText('required');
    });

    await Then('no POST is sent to /invoke', async () => {
      expect(stubs.invokes).toHaveLength(0);
      await expect(console_.result).toHaveCount(0);
    });
  });
});
