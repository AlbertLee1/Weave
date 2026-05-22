import { afterEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { changeLocale, type SupportedLocale } from '../../../i18n';
import { OntologyCard } from '../OntologyCard';

const mockOntology = {
  rid: 'ri.ontology.main.ontology.i18n',
  apiName: 'i18n-demo',
  displayName: 'I18n Demo',
  description: 'Ontology card copy contract',
};

async function renderCard(locale: SupportedLocale, objectTypeCount: number) {
  await changeLocale(locale);
  render(
    <OntologyCard
      ontology={mockOntology}
      objectTypeCount={objectTypeCount}
      onClick={() => {}}
    />,
  );
}

describe('BDD: Dashboard ontology card object type count localization', () => {
  afterEach(async () => {
    await changeLocale('zh-CN');
  });

  it('Given one object type, When English dashboard cards render, Then the count uses singular copy', async () => {
    await renderCard('en', 1);

    expect(screen.getByText('1 object type')).toBeInTheDocument();
    expect(screen.queryByText('1 types')).not.toBeInTheDocument();
  });

  it('Given multiple object types, When English dashboard cards render, Then the count uses plural copy', async () => {
    await renderCard('en', 3);

    expect(screen.getByText('3 object types')).toBeInTheDocument();
  });

  it('Given zh-CN is active, When dashboard cards render, Then the count uses the localized object-type label', async () => {
    await renderCard('zh-CN', 3);

    expect(screen.getByText('3 个对象类型')).toBeInTheDocument();
    expect(screen.queryByText('3 types')).not.toBeInTheDocument();
  });
});
