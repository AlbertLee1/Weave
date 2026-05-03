import type { LayoutNode } from '../../api/apps';

// US-399: built-in App templates surfaced in the new-App flow so authors
// can start from a sensible scaffold rather than an empty canvas.
//
// The wire shape is the canonical Layout DSL accepted by
// pkg/apps/layout.go::ValidateLayout — every template's `layoutJson`
// round-trips through the same wire validator the editor's Save path
// uses, so a "create from template" pick is functionally identical to
// a manually authored layout posted via PUT.
//
// Templates are intentionally stored as JSON objects rather than .json
// files: TypeScript's structural type-checking against `LayoutNode`
// catches schema drift at compile time, and the SPA bundles them into
// the same chunk as the editor (no additional fetch round-trip).
//
// The PRD asked for three templates — CRM Dashboard, Approval Console,
// and Object Browser. Each one demonstrates a different combination of
// the existing Component Palette entries (table / form / chart / button
// / objectCard / text) so authors can see how the available primitives
// compose without first having to read the docs.

export type AppTemplateId = 'crm-dashboard' | 'approval-console' | 'object-browser';

export interface AppTemplate {
  id: AppTemplateId;
  name: string;
  description: string;
  defaultAppName: string;
  layoutJson: LayoutNode;
}

const crmDashboard: AppTemplate = {
  id: 'crm-dashboard',
  name: 'CRM Dashboard',
  description:
    'Customer overview with a metrics chart, a directory table, and a quick-action button.',
  defaultAppName: 'CRM Dashboard',
  layoutJson: {
    type: 'row',
    variables: [
      { name: 'selectedAccount', type: 'string', default: '' },
    ],
    children: [
      {
        type: 'col',
        width: 4,
        child: {
          type: 'component',
          componentType: 'chart',
          props: {
            chartType: 'bar',
            title: 'Pipeline by Stage',
          },
        },
      },
      {
        type: 'col',
        width: 6,
        child: {
          type: 'component',
          componentType: 'table',
          props: {
            objectSet: 'Account',
            columns: ['name', 'industry', 'arr'],
            pageSize: 25,
            orderByField: 'arr',
            orderByDirection: 'desc',
            filterField: '',
            filterOp: 'eq',
            filterValue: '',
          },
        },
      },
      {
        type: 'col',
        width: 2,
        child: {
          type: 'component',
          componentType: 'button',
          props: {
            label: 'New Account',
            actionType: 'createAccount',
          },
        },
      },
    ],
  },
};

const approvalConsole: AppTemplate = {
  id: 'approval-console',
  name: 'Approval Console',
  description:
    'Approval queue table paired with an inline form that submits the configured ActionType.',
  defaultAppName: 'Approval Console',
  layoutJson: {
    type: 'row',
    variables: [
      { name: 'requestId', type: 'string', default: '' },
      { name: 'decision', type: 'string', default: 'approve' },
    ],
    children: [
      {
        type: 'col',
        width: 7,
        child: {
          type: 'component',
          componentType: 'table',
          props: {
            objectSet: 'ApprovalRequest',
            columns: ['id', 'submittedBy', 'submittedAt', 'status'],
            pageSize: 50,
            orderByField: 'submittedAt',
            orderByDirection: 'desc',
            filterField: 'status',
            filterOp: 'eq',
            filterValue: 'pending',
          },
        },
      },
      {
        type: 'col',
        width: 5,
        child: {
          type: 'component',
          componentType: 'form',
          props: {
            actionType: 'decideApprovalRequest',
          },
        },
      },
    ],
  },
};

const objectBrowser: AppTemplate = {
  id: 'object-browser',
  name: 'Object Browser',
  description:
    'Single-object detail card next to a table of related objects — the canonical drill-down layout.',
  defaultAppName: 'Object Browser',
  layoutJson: {
    type: 'row',
    variables: [
      { name: 'objectType', type: 'string', default: '' },
      { name: 'objectId', type: 'string', default: '' },
    ],
    children: [
      {
        type: 'col',
        width: 4,
        child: {
          type: 'component',
          componentType: 'objectCard',
          props: {
            objectType: '{{objectType}}',
            objectId: '{{objectId}}',
          },
        },
      },
      {
        type: 'col',
        width: 6,
        child: {
          type: 'component',
          componentType: 'table',
          props: {
            objectSet: '{{objectType}}',
            columns: ['id', 'name', 'updatedAt'],
            pageSize: 25,
            orderByField: 'updatedAt',
            orderByDirection: 'desc',
            filterField: '',
            filterOp: 'eq',
            filterValue: '',
          },
        },
      },
      {
        type: 'col',
        width: 2,
        child: {
          type: 'component',
          componentType: 'text',
          props: {
            content:
              'Pick an objectType + objectId variable to drive the card and the related-objects table.',
          },
        },
      },
    ],
  },
};

// APP_TEMPLATES is the ordered catalogue rendered by the new-App
// template picker. Order is significant — it drives the picker's
// presentation order, not template identity, so adding a fourth
// template appends to the end without renumbering.
export const APP_TEMPLATES: readonly AppTemplate[] = [
  crmDashboard,
  approvalConsole,
  objectBrowser,
];

// findTemplate returns the catalogue entry for an id, or undefined if
// the caller passed an unknown / stale id. Kept as a helper rather than
// a `Record` lookup so future runtime-loaded templates (US-414+) can
// share the same lookup surface.
export function findTemplate(id: string): AppTemplate | undefined {
  return APP_TEMPLATES.find((t) => t.id === id);
}
