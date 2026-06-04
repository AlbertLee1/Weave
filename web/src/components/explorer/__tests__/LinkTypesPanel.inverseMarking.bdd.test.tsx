import { describe, it, expect } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import { LinkTypesPanel } from '../LinkTypesPanel';
import type { LinkType } from '../../../api/types';

// LINK-INV-MARK: pkg/oms LinkType.InverseLinkRID and LinkType.PropagateMarkings
// (US-261) are serialised as `inverseLinkRid` / `propagateMarkings`, but the
// frontend LinkType interface dropped them and LinkTypesPanel never surfaced
// them. These scenarios pin the contract that the Explorer shows whether a link
// propagates Markings and whether it has a declared inverse (bidirectional).

const linkTypes: LinkType[] = [
  {
    rid: 'ri.link.full',
    apiName: 'primaryOwner',
    displayName: 'Primary Owner',
    description: 'The owning account',
    objectTypeApiName: 'Order',
    linkedObjectTypeApiName: 'Account',
    cardinality: 'MANY_TO_MANY',
    required: true,
    inverseLinkRid: 'ri.link.ownedOrders',
    propagateMarkings: true,
  },
  {
    rid: 'ri.link.plain',
    apiName: 'relatedTicket',
    displayName: 'Related Ticket',
    description: 'An optional support ticket',
    objectTypeApiName: 'Order',
    linkedObjectTypeApiName: 'Ticket',
    cardinality: 'ONE_TO_MANY',
    required: false,
  },
];

function rowForApiName(apiName: string) {
  const li = screen.getByText(apiName).closest('li');
  if (!li) throw new Error(`row for ${apiName} not found`);
  return within(li as HTMLElement);
}

describe('BDD: LinkTypesPanel surfaces inverse-link & marking-propagation status (LINK-INV-MARK)', () => {
  it('Given a link with propagateMarkings=true, When the panel renders, Then that row shows a "Propagates markings" badge', () => {
    render(<LinkTypesPanel linkTypes={linkTypes} />);
    const fullRow = rowForApiName('primaryOwner');
    expect(fullRow.getByText('Propagates markings')).toBeInTheDocument();
  });

  it('Given a link with inverseLinkRid set, When the panel renders, Then that row shows a bidirectional / has-inverse indicator referencing the inverse RID', () => {
    render(<LinkTypesPanel linkTypes={linkTypes} />);
    const fullRow = rowForApiName('primaryOwner');
    const indicator = fullRow.getByTestId('link-inverse-indicator');
    expect(indicator).toBeInTheDocument();
    expect(indicator).toHaveAttribute('title', expect.stringContaining('ri.link.ownedOrders'));
  });

  it('Given a link without propagateMarkings, When the panel renders, Then that row does not show a "Propagates markings" badge', () => {
    render(<LinkTypesPanel linkTypes={linkTypes} />);
    const plainRow = rowForApiName('relatedTicket');
    expect(plainRow.queryByText('Propagates markings')).not.toBeInTheDocument();
  });

  it('Given a link without inverseLinkRid, When the panel renders, Then that row does not show a bidirectional / has-inverse indicator', () => {
    render(<LinkTypesPanel linkTypes={linkTypes} />);
    const plainRow = rowForApiName('relatedTicket');
    expect(plainRow.queryByTestId('link-inverse-indicator')).not.toBeInTheDocument();
  });

  it('Given a link with both flags, When the panel renders, Then it keeps showing its existing cardinality and required markers', () => {
    render(<LinkTypesPanel linkTypes={linkTypes} />);
    const fullRow = rowForApiName('primaryOwner');
    expect(fullRow.getByText('Required')).toBeInTheDocument();
    expect(fullRow.getByText('MANY:TO:MANY')).toBeInTheDocument();
  });
});
