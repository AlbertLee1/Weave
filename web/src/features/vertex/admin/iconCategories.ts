export interface IconDef {
  name: string;
  category: string;
}

export function filterIconsByActiveCategories(
  icons: IconDef[],
  activeCategories: string[],
): IconDef[] {
  if (activeCategories.length === 0) return [...icons];
  const active = new Set(activeCategories);
  return icons.filter((i) => active.has(i.category));
}
