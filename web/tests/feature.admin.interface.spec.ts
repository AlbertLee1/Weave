import { expect, test, type Page, type Route } from '@playwright/test';
import {
  Given,
  InterfaceAdminPage,
  Then,
  When,
  describeFeature,
  signIn,
} from './support';

/**
 * BDD coverage of `/admin/:ontology/interfaces` — the Interface editor
 * rendered by `src/components/admin/InterfaceAdminPage.tsx`.
 *
 * Scenarios map the PRD AC for US-032 (frontend-backend-gap-coverage):
 *   AC mapping → scenario:
 *     create                 → create-flow-posts-expected-body-and-refreshes
 *     add method             → create-flow-roundtrips-shared-property-and-link-type
 *                              (Interfaces expose `sharedProperties` +
 *                              `outgoingLinkTypes` instead of "methods" —
 *                              they are the contract every implementer
 *                              must provide)
 *     implement by ObjectType → implementing-modal-attaches-and-detaches
 *     resolved view          → list-renders-extends-and-implementing-counts
 *                              (row data attrs document the resolved view:
 *                              parent apiName from extendsRid, shared/link
 *                              counts, and implementing rollup count)
 *     delete                 → delete-flow-confirms-and-removes-the-row
 *
 * All scenarios stub the admin endpoints through `page.route` so the page
 * renders deterministic fixtures without touching real backend data, and
 * mutations latch a `state` object so subsequent refetches reflect the
 * server transition (same convention as US-029/030/031 admin specs).
 */

const ONTOLOGY = 'northwind';

interface MockInterface {
  rid: string;
  apiName: string;
  displayName: string;
  description?: string;
  extendsRid?: string;
  sharedProperties: Array<{
    apiName: string;
    baseType: string;
    isArray: boolean;
  }>;
  outgoingLinkTypes: Array<{
    apiName: string;
    displayName: string;
    linkedEntityTypeApiName: string;
    cardinality: 'ONE' | 'MANY';
    required: boolean;
  }>;
}

const OBJECT_TYPES = [
  {
    rid: 'ri.ontology.main.object-type.emp',
    apiName: 'Employee',
    displayName: 'Employee',
    pluralDisplayName: 'Employees',
    primaryKey: 'employeeId',
    status: 'ACTIVE',
    visibility: 'PROMINENT',
  },
  {
    rid: 'ri.ontology.main.object-type.cust',
    apiName: 'Customer',
    displayName: 'Customer',
    pluralDisplayName: 'Customers',
    primaryKey: 'customerId',
    status: 'ACTIVE',
    visibility: 'NORMAL',
  },
  {
    rid: 'ri.ontology.main.object-type.dept',
    apiName: 'Department',
    displayName: 'Department',
    pluralDisplayName: 'Departments',
    primaryKey: 'departmentId',
    status: 'ACTIVE',
    visibility: 'NORMAL',
  },
];

const addressable: MockInterface = {
  rid: 'ri.ontology.main.interface.addressable',
  apiName: 'Addressable',
  displayName: 'Addressable',
  sharedProperties: [
    { apiName: 'address', baseType: 'string', isArray: false },
  ],
  outgoingLinkTypes: [],
};

// "Locatable" extends Addressable — exercises the resolved-view AC (a child
// interface's row should cite its parent's apiName in the data attrs even
// though the underlying field is the parent's rid).
const locatable: MockInterface = {
  rid: 'ri.ontology.main.interface.locatable',
  apiName: 'Locatable',
  displayName: 'Locatable',
  extendsRid: addressable.rid,
  sharedProperties: [
    { apiName: 'latitude', baseType: 'double', isArray: false },
    { apiName: 'longitude', baseType: 'double', isArray: false },
  ],
  outgoingLinkTypes: [
    {
      apiName: 'region',
      displayName: 'Region',
      linkedEntityTypeApiName: 'Department',
      cardinality: 'ONE',
      required: false,
    },
  ],
};

const named: MockInterface = {
  rid: 'ri.ontology.main.interface.named',
  apiName: 'Named',
  displayName: 'Named',
  sharedProperties: [],
  outgoingLinkTypes: [],
};

interface StubState {
  rows: MockInterface[];
  // objectTypeRid → list of {objectTypeRid, interfaceRid}
  attachments: Record<
    string,
    Array<{ objectTypeRid: string; interfaceRid: string }>
  >;
  listCalls: number;
  createCalls: number;
  createBody: unknown;
  updateCalls: Array<{ rid: string; body: unknown }>;
  deleteCalls: string[];
  attachCalls: Array<{ objectTypeRid: string; body: unknown }>;
  detachCalls: Array<{ objectTypeRid: string; interfaceRid: string }>;
}

function makeState(
  initial: MockInterface[],
  initialAttachments: Record<
    string,
    Array<{ objectTypeRid: string; interfaceRid: string }>
  > = {},
): StubState {
  const attachments: Record<
    string,
    Array<{ objectTypeRid: string; interfaceRid: string }>
  > = {};
  for (const ot of OBJECT_TYPES) {
    attachments[ot.rid] = (initialAttachments[ot.rid] ?? []).map((row) => ({
      ...row,
    }));
  }
  return {
    rows: initial.map((row) => structuredClone(row)),
    attachments,
    listCalls: 0,
    createCalls: 0,
    createBody: null,
    updateCalls: [],
    deleteCalls: [],
    attachCalls: [],
    detachCalls: [],
  };
}

/**
 * Installs all Interface admin endpoints needed by InterfaceAdminPage +
 * the implementing-modal's per-ObjectType attachment fanout. Mutations
 * latch into `state` so a refetch after submit reflects the change,
 * mirroring real server transitions (template established by US-029 /
 * US-030 / US-031).
 *
 * The list endpoint hits `/interfacesAdmin` — the admin route returns the
 * full Interface records. `/interfaceTypes` is the runtime read-only
 * projection used elsewhere and is not stubbed here (InterfaceAdminPage
 * does not call it).
 */
async function stubInterfaceAdmin(
  page: Page,
  state: StubState,
): Promise<void> {
  // List on the admin-only collection endpoint.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/interfacesAdmin`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() === 'GET') {
        state.listCalls += 1;
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: state.rows }),
        });
        return;
      }
      await route.continue();
    },
  );

  // Create on the collection endpoint.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/interfaces`,
    async (route: Route) => {
      const req = route.request();
      if (req.method() === 'POST') {
        state.createCalls += 1;
        const body = req.postDataJSON() as Record<string, unknown>;
        state.createBody = body;
        const apiName = String(body.apiName ?? 'newInterface');
        const created: MockInterface = {
          rid: `ri.ontology.main.interface.${apiName}`,
          apiName,
          displayName: String(body.displayName ?? apiName),
          description: body.description as string | undefined,
          extendsRid: body.extendsRid as string | undefined,
          sharedProperties:
            (body.sharedProperties as MockInterface['sharedProperties']) ?? [],
          outgoingLinkTypes:
            (body.outgoingLinkTypes as MockInterface['outgoingLinkTypes']) ?? [],
        };
        state.rows.push(created);
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(created),
        });
        return;
      }
      await route.continue();
    },
  );

  // Update + Delete on the per-rid endpoint.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/interfaces/byRid/*`,
    async (route: Route) => {
      const req = route.request();
      const url = new URL(req.url());
      const segments = url.pathname.split('/');
      const ridEncoded = segments[segments.length - 1] ?? '';
      const rid = decodeURIComponent(ridEncoded);
      if (req.method() === 'PUT') {
        const body = req.postDataJSON() as Record<string, unknown>;
        state.updateCalls.push({ rid, body });
        const idx = state.rows.findIndex((r) => r.rid === rid);
        if (idx >= 0) {
          state.rows[idx] = {
            ...state.rows[idx],
            displayName: String(
              body.displayName ?? state.rows[idx].displayName,
            ),
            extendsRid:
              (body.extendsRid as string | undefined) ??
              state.rows[idx].extendsRid,
            sharedProperties:
              (body.sharedProperties as MockInterface['sharedProperties']) ??
              state.rows[idx].sharedProperties,
            outgoingLinkTypes:
              (body.outgoingLinkTypes as MockInterface['outgoingLinkTypes']) ??
              state.rows[idx].outgoingLinkTypes,
          };
        }
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(state.rows[idx]),
        });
        return;
      }
      if (req.method() === 'DELETE') {
        state.deleteCalls.push(rid);
        state.rows = state.rows.filter((r) => r.rid !== rid);
        // Cascade: any attachments referencing this interface go away too
        // (mirrors the server-side cleanup that real Interface delete
        // performs — see backend handler).
        for (const otRid of Object.keys(state.attachments)) {
          state.attachments[otRid] = state.attachments[otRid].filter(
            (a) => a.interfaceRid !== rid,
          );
        }
        await route.fulfill({ status: 204, body: '' });
        return;
      }
      await route.continue();
    },
  );

  // objectTypes — needed to populate the implementing-modal target picker
  // and the per-Interface implementing-count rollup.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes`,
    async (route: Route) => {
      if (route.request().method() !== 'GET') {
        await route.continue();
        return;
      }
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({ data: OBJECT_TYPES }),
      });
    },
  );

  // Per-ObjectType attachments endpoint (list + attach + detach). Note:
  // InterfaceAdminPage fans out per ObjectType via useAttachmentsByInterface
  // and then filters on interfaceRid client-side, so the GET response must
  // be scoped by the ObjectType in the URL.
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/byRid/*/interfaces`,
    async (route: Route) => {
      const req = route.request();
      const url = new URL(req.url());
      // Path shape: /api/v2/ontologies/{ontology}/objectTypes/byRid/{rid}/interfaces
      const match = url.pathname.match(
        /\/objectTypes\/byRid\/([^/]+)\/interfaces$/,
      );
      const otRid = match ? decodeURIComponent(match[1]) : '';
      if (req.method() === 'GET') {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify({ data: state.attachments[otRid] ?? [] }),
        });
        return;
      }
      if (req.method() === 'POST') {
        const body = req.postDataJSON() as Record<string, unknown>;
        state.attachCalls.push({ objectTypeRid: otRid, body });
        const row = {
          objectTypeRid: otRid,
          interfaceRid: String(body.interfaceRid ?? ''),
        };
        state.attachments[otRid] = [
          ...(state.attachments[otRid] ?? []),
          row,
        ];
        await route.fulfill({
          status: 201,
          contentType: 'application/json',
          body: JSON.stringify(row),
        });
        return;
      }
      await route.continue();
    },
  );

  // Detach is a DELETE on a different path:
  // /objectTypes/byRid/{otRid}/interfaces/{ifaceRid}
  await page.route(
    `**/api/v2/ontologies/${ONTOLOGY}/objectTypes/byRid/*/interfaces/*`,
    async (route: Route) => {
      const req = route.request();
      const url = new URL(req.url());
      const match = url.pathname.match(
        /\/objectTypes\/byRid\/([^/]+)\/interfaces\/([^/]+)$/,
      );
      if (!match) {
        await route.continue();
        return;
      }
      const otRid = decodeURIComponent(match[1]);
      const ifaceRid = decodeURIComponent(match[2]);
      if (req.method() === 'DELETE') {
        state.detachCalls.push({
          objectTypeRid: otRid,
          interfaceRid: ifaceRid,
        });
        state.attachments[otRid] = (state.attachments[otRid] ?? []).filter(
          (a) => a.interfaceRid !== ifaceRid,
        );
        await route.fulfill({ status: 204, body: '' });
        return;
      }
      await route.continue();
    },
  );
}

describeFeature('Admin: Interface editor', () => {
  test('Scenario: the table renders interfaces with per-row extends + shared/link counts + implementing rollup @smoke', async ({
    page,
    request,
  }) => {
    const admin = new InterfaceAdminPage(page);
    // Initial attachments: Employee implements Addressable; Customer implements
    // Locatable. This lets us assert the resolved "implementing count" rolls
    // up correctly per row (Addressable=1, Locatable=1, Named=0).
    const state = makeState([addressable, locatable, named], {
      'ri.ontology.main.object-type.emp': [
        {
          objectTypeRid: 'ri.ontology.main.object-type.emp',
          interfaceRid: addressable.rid,
        },
      ],
      'ri.ontology.main.object-type.cust': [
        {
          objectTypeRid: 'ri.ontology.main.object-type.cust',
          interfaceRid: locatable.rid,
        },
      ],
    });

    await Given('the visitor is authenticated', async () => {
      await signIn(request);
    });

    await Given(
      'three Interfaces are seeded on northwind (one with extends + one without)',
      async () => {
        await stubInterfaceAdmin(page, state);
      },
    );

    await When('the user opens /admin/northwind/interfaces', async () => {
      await admin.goto(ONTOLOGY);
    });

    await Then(
      'the page root + table are visible with three rows',
      async () => {
        await expect(admin.root).toBeVisible();
        await expect(admin.loading).toBeHidden();
        await expect(admin.table).toBeVisible();
        await expect(admin.rows).toHaveCount(3);
      },
    );

    await Then(
      'each row carries the apiName + shared/link count + extends + implementing data attrs',
      async () => {
        // Rows are sorted by displayName asc:
        //   Addressable  (—)            1 shared 0 link    1 implementer
        //   Locatable    (Addressable)  2 shared 1 link    1 implementer
        //   Named        (—)            0 shared 0 link    0 implementers
        await expect(admin.rowByApiName('Addressable')).toBeVisible();
        await expect(admin.rowByApiName('Locatable')).toBeVisible();
        await expect(admin.rowByApiName('Named')).toBeVisible();

        // Shared property + link type counts are numeric data attrs so the
        // spec doesn't depend on rendered cell text (same template as US-031
        // data-action-type-parameter-count / -rule-count).
        await expect(admin.rowByApiName('Addressable')).toHaveAttribute(
          'data-interface-shared-property-count',
          '1',
        );
        await expect(admin.rowByApiName('Addressable')).toHaveAttribute(
          'data-interface-link-type-count',
          '0',
        );
        await expect(admin.rowByApiName('Locatable')).toHaveAttribute(
          'data-interface-shared-property-count',
          '2',
        );
        await expect(admin.rowByApiName('Locatable')).toHaveAttribute(
          'data-interface-link-type-count',
          '1',
        );
        await expect(admin.rowByApiName('Named')).toHaveAttribute(
          'data-interface-shared-property-count',
          '0',
        );

        // Resolved view: extends column shows the *parent apiName*, not the
        // raw extendsRid — the page resolves the rid → row lookup so users
        // see a human label. The data attr captures that resolved value.
        await expect(admin.rowByApiName('Addressable')).toHaveAttribute(
          'data-interface-extends-api-name',
          '',
        );
        await expect(admin.rowByApiName('Locatable')).toHaveAttribute(
          'data-interface-extends-api-name',
          'Addressable',
        );

        // Resolved view: the Manage (N) button surfaces the rolled-up
        // implementer count via data-interface-implementing-count. This
        // exercises the per-ObjectType fanout in useAttachmentsByInterface
        // — Employee→Addressable (1 implementer); Customer→Locatable (1);
        // Named has no implementers (0).
        await expect.poll(async () =>
          admin
            .manageButtonFor('Addressable')
            .getAttribute('data-interface-implementing-count'),
        ).toBe('1');
        await expect.poll(async () =>
          admin
            .manageButtonFor('Locatable')
            .getAttribute('data-interface-implementing-count'),
        ).toBe('1');
        await expect.poll(async () =>
          admin
            .manageButtonFor('Named')
            .getAttribute('data-interface-implementing-count'),
        ).toBe('0');
      },
    );
  });

  test('Scenario: creating an Interface POSTs the expected body with shared properties + outgoing link types and roundtrips into a new row @smoke', async ({
    page,
  }) => {
    const admin = new InterfaceAdminPage(page);
    const state = makeState([addressable, locatable, named]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubInterfaceAdmin(page, state);
    });

    await Given('the user is on the Interface admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the New Interface modal', async () => {
      await admin.newButton.click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.createForm).toBeVisible();
    });

    await When(
      'the user fills displayName, adds a shared property + a link type, and submits',
      async () => {
        await admin.displayNameInput.fill('Has Name');
        // apiName auto-populates from displayName — see autoApiName() and
        // updateDisplayName() in InterfaceAdminPage.tsx.
        await expect(admin.apiNameInput).toHaveValue('hasName');

        // Add one shared property — Interfaces' "methods" are the shared
        // properties + outgoing link types every implementer must provide.
        await admin.addSharedPropertyButton.click();
        await admin.sharedPropertiesSection
          .getByLabel(/Shared property 1 api name/i)
          .fill('fullName');
        // baseType select defaults to 'string'; pick explicitly to lock the
        // wire value rather than the default cascade.
        await admin.sharedPropertiesSection
          .getByLabel(/Shared property 1 base type/i)
          .selectOption('string');

        // Add one outgoing link type — covers AC "add method" (link types
        // on an Interface are the relational counterpart to methods).
        await admin.addLinkTypeButton.click();
        await admin.linkTypesSection
          .getByLabel(/Link type 1 api name/i)
          .fill('parent');
        await admin.linkTypesSection
          .getByLabel(/Link type 1 target type/i)
          .fill('Department');
        await admin.linkTypesSection
          .getByLabel(/Link type 1 display name/i)
          .fill('Parent');
        await admin.linkTypesSection
          .getByLabel(/Link type 1 cardinality/i)
          .selectOption('ONE');

        // The Builder modal is the same large-form layout established by
        // US-029 / US-030 / US-031 — Submit can fall outside a 720px
        // viewport when the form grows past ~10 fields. requestSubmit() is
        // the same React onSubmit code path that Enter-on-input triggers.
        await admin.createForm.evaluate((form) =>
          (form as HTMLFormElement).requestSubmit(),
        );
      },
    );

    await Then(
      'POST /interfaces was invoked exactly once with the expected payload',
      async () => {
        await expect.poll(() => state.createCalls).toBe(1);
        const body = state.createBody as Record<string, unknown>;
        expect(body).toMatchObject({
          apiName: 'hasName',
          displayName: 'Has Name',
        });
        // sharedProperties contract: {apiName, baseType, isArray}.
        const shared = body.sharedProperties as Array<Record<string, unknown>>;
        expect(shared).toHaveLength(1);
        expect(shared[0]).toMatchObject({
          apiName: 'fullName',
          baseType: 'string',
          isArray: false,
        });
        // outgoingLinkTypes contract: {apiName, displayName,
        // linkedEntityTypeApiName, cardinality, required}.
        const links = body.outgoingLinkTypes as Array<Record<string, unknown>>;
        expect(links).toHaveLength(1);
        expect(links[0]).toMatchObject({
          apiName: 'parent',
          displayName: 'Parent',
          linkedEntityTypeApiName: 'Department',
          cardinality: 'ONE',
          required: false,
        });
      },
    );

    await Then(
      'the modal closes and the new row joins the table with the right counts',
      async () => {
        await expect(admin.modalOverlay).toBeHidden();
        await expect(admin.rows).toHaveCount(4);
        await expect(admin.rowByApiName('hasName')).toBeVisible();
        await expect(admin.rowByApiName('hasName')).toHaveAttribute(
          'data-interface-shared-property-count',
          '1',
        );
        await expect(admin.rowByApiName('hasName')).toHaveAttribute(
          'data-interface-link-type-count',
          '1',
        );
      },
    );
  });

  test('Scenario: the implementing modal attaches and detaches ObjectTypes via the per-ObjectType endpoint @smoke', async ({
    page,
  }) => {
    const admin = new InterfaceAdminPage(page);
    // Seed Employee as an existing implementer of Addressable so we can
    // exercise both Detach (remove an existing attachment) and Attach (add
    // Customer in the same scenario).
    const state = makeState([addressable, locatable, named], {
      'ri.ontology.main.object-type.emp': [
        {
          objectTypeRid: 'ri.ontology.main.object-type.emp',
          interfaceRid: addressable.rid,
        },
      ],
    });

    await Given('the admin endpoints are stubbed', async () => {
      await stubInterfaceAdmin(page, state);
    });

    await Given('the user is on the Interface admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When(
      'the user opens the Implementing modal for Addressable',
      async () => {
        await admin.manageButtonFor('Addressable').click();
        await expect(admin.modalOverlay).toBeVisible();
        await expect(admin.implementingModal).toBeVisible();
      },
    );

    await Then(
      'Employee shows up as the existing implementer with a Detach affordance',
      async () => {
        await expect(admin.implementingRows).toHaveCount(1);
        await expect(
          admin.implementingRowByObjectType('Employee'),
        ).toBeVisible();
        await expect(admin.detachButtonFor('Employee')).toBeVisible();
      },
    );

    await When(
      'the user picks Customer in the attach select and clicks Attach',
      async () => {
        await admin.implementingAttachSelect.selectOption(
          'ri.ontology.main.object-type.cust',
        );
        await admin.implementingAttachButton.click();
      },
    );

    await Then(
      'POST /objectTypes/byRid/.../interfaces was captured with the right body',
      async () => {
        await expect.poll(() => state.attachCalls.length).toBe(1);
        expect(state.attachCalls[0]).toEqual({
          objectTypeRid: 'ri.ontology.main.object-type.cust',
          body: expect.objectContaining({
            interfaceRid: addressable.rid,
          }),
        });
      },
    );

    // PROD GAP (honest BDD per US-023): within the same modal session the
    // implementing list does NOT refresh after attach/detach. The reason
    // is `useAttachmentsByInterface` uses a local useState+useEffect
    // pipeline keyed on `objectTypes` identity (InterfaceAdminPage.tsx:996)
    // — it is NOT a React Query subscriber. The mutation hooks invalidate
    // `['objectTypeInterfaces', ...]` and `['interfacesAdmin', ...]`
    // (useInterfaces.ts:77-82, 95-100) which refreshes the row-level
    // implementing-count (table re-renders, OBJECT_TYPES key recomputed)
    // but the *modal's own* attachment list stays stale until the modal
    // is closed + reopened (effect re-runs on remount). Future work to
    // make the implementing list a React Query subscriber should add a
    // positive scenario that asserts in-modal refresh; for now we lock the
    // *persisted* state (close → reopen surfaces the latest list).
    await When(
      'the user closes and reopens the Implementing modal',
      async () => {
        await admin.implementingClose.click();
        await expect(admin.implementingModal).toBeHidden();
        await admin.manageButtonFor('Addressable').click();
        await expect(admin.implementingModal).toBeVisible();
      },
    );

    await Then(
      'the modal now lists both Employee and Customer as implementers',
      async () => {
        await expect.poll(async () =>
          (await admin.implementingRows.count()),
        ).toBe(2);
        await expect(
          admin.implementingRowByObjectType('Employee'),
        ).toBeVisible();
        await expect(
          admin.implementingRowByObjectType('Customer'),
        ).toBeVisible();
      },
    );

    await When('the user clicks Detach for Employee', async () => {
      await admin.detachButtonFor('Employee').click();
    });

    await Then(
      'DELETE /objectTypes/byRid/.../interfaces/{ifaceRid} was captured',
      async () => {
        await expect.poll(() => state.detachCalls.length).toBe(1);
        expect(state.detachCalls[0]).toEqual({
          objectTypeRid: 'ri.ontology.main.object-type.emp',
          interfaceRid: addressable.rid,
        });
      },
    );

    await When(
      'the user closes and reopens the Implementing modal again',
      async () => {
        await admin.implementingClose.click();
        await expect(admin.implementingModal).toBeHidden();
        await admin.manageButtonFor('Addressable').click();
        await expect(admin.implementingModal).toBeVisible();
      },
    );

    await Then(
      'Employee is gone from the persisted list and only Customer remains',
      async () => {
        await expect.poll(async () =>
          (await admin.implementingRows.count()),
        ).toBe(1);
        await expect(
          admin.implementingRowByObjectType('Employee'),
        ).toHaveCount(0);
        await expect(
          admin.implementingRowByObjectType('Customer'),
        ).toBeVisible();
      },
    );
  });

  test('Scenario: deleting an Interface confirms via the modal and removes the row from the table', async ({
    page,
  }) => {
    const admin = new InterfaceAdminPage(page);
    const state = makeState([addressable, locatable, named]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubInterfaceAdmin(page, state);
    });

    await Given('the user is on the Interface admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the Delete modal for Named', async () => {
      await admin.deleteButtonFor('Named').click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.deleteModal).toBeVisible();
    });

    await Then(
      'the delete modal cites the apiName + rid via data attributes',
      async () => {
        // The Delete modal locks the resource identity in data-* attrs so
        // the spec doesn't depend on i18n copy in the confirmation prose
        // (same template as US-031 Delete modal).
        await expect(admin.deleteModal).toHaveAttribute(
          'data-interface-api-name',
          'Named',
        );
        await expect(admin.deleteModal).toHaveAttribute(
          'data-interface-rid',
          named.rid,
        );
      },
    );

    await When('the user confirms the delete', async () => {
      await admin.deleteConfirm.click();
    });

    await Then(
      'DELETE /interfaces/byRid was captured and the row is gone',
      async () => {
        await expect.poll(() => state.deleteCalls.length).toBe(1);
        expect(state.deleteCalls[0]).toBe(named.rid);
        await expect(admin.modalOverlay).toBeHidden();
        await expect(admin.rows).toHaveCount(2);
        await expect(admin.rowByApiName('Named')).toHaveCount(0);
      },
    );
  });

  test('Scenario: the create form blocks duplicate apiNames with an inline alert and no POST', async ({
    page,
  }) => {
    const admin = new InterfaceAdminPage(page);
    const state = makeState([addressable, locatable, named]);

    await Given('the admin endpoints are stubbed', async () => {
      await stubInterfaceAdmin(page, state);
    });

    await Given('the user is on the Interface admin page', async () => {
      await admin.goto(ONTOLOGY);
      await expect(admin.table).toBeVisible();
    });

    await When('the user opens the New Interface modal', async () => {
      await admin.newButton.click();
      await expect(admin.modalOverlay).toBeVisible();
      await expect(admin.createForm).toBeVisible();
    });

    await When(
      'the user fills displayName then overrides apiName to an existing one',
      async () => {
        await admin.displayNameInput.fill('Another Addressable');
        // Manually overriding apiName flips apiNameDirty=true so the
        // auto-populate stays off — see InterfaceAdminPage.tsx apiName
        // onChange handler.
        await admin.apiNameInput.fill('Addressable');
      },
    );

    await Then(
      'the submit button is disabled and the duplicate alert surfaces',
      async () => {
        // duplicateApiName=true (computed via apiNameTaken set) drives
        // canSubmit=false → button disabled, and renders an inline error
        // on the API Name Field component. The UI contract is "the user
        // cannot submit via the visible affordance" — the inline alert is
        // the validator's user-facing surface, the disabled submit button
        // is the gate. Same template as US-031 duplicate-apiName scenario.
        await expect(admin.createSubmit).toBeDisabled();
        await expect(admin.createForm).toContainText(
          'An Interface with apiName "Addressable" already exists.',
        );
        // We deliberately do NOT exercise form.requestSubmit() here: that
        // API bypasses the button's `disabled` state and would mask the
        // UI-level contract by directly invoking the onSubmit handler.
        expect(state.createCalls).toBe(0);
      },
    );
  });
});
