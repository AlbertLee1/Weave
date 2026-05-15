import { describe, it, expect } from 'vitest';
import { filterIconsByActiveCategories, type IconDef } from './iconCategories';

const allIcons: IconDef[] = [
  { name: 'plane', category: 'aviation' },
  { name: 'airport', category: 'aviation' },
  { name: 'truck', category: 'logistics' },
  { name: 'ship', category: 'logistics' },
  { name: 'doctor', category: 'medical' },
  { name: 'pill', category: 'medical' },
];

describe('VTX-096 filterIconsByActiveCategories', () => {
  it('given_TwoActiveCategories_when_Filter_then_OnlyMatchingShown', () => {
    const got = filterIconsByActiveCategories(allIcons, ['aviation', 'logistics']);
    expect(got.map((i) => i.name).sort()).toEqual([
      'airport',
      'plane',
      'ship',
      'truck',
    ]);
  });

  it('given_NoActiveCategories_when_Filter_then_AllShown', () => {
    // Empty active list means "no override" — show all icons. This is
    // the default state when an admin has not pinned the picker.
    expect(filterIconsByActiveCategories(allIcons, [])).toHaveLength(allIcons.length);
  });

  it('given_ActiveCategoryNotMatchingAny_when_Filter_then_Empty', () => {
    expect(filterIconsByActiveCategories(allIcons, ['zzz'])).toEqual([]);
  });

  it('given_DuplicateActiveCategories_when_Filter_then_Deduplicated', () => {
    const got = filterIconsByActiveCategories(allIcons, ['aviation', 'aviation']);
    expect(got.map((i) => i.name).sort()).toEqual(['airport', 'plane']);
  });
});
