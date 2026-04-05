// Convert display name to camelCase apiName
// "Employee Department" → "employeeDepartment"
// "My Cool Type" → "myCoolType"
export function toApiName(displayName: string): string {
  if (!displayName) return '';
  const words = displayName.trim().split(/\s+/);
  return words
    .map((word, i) => {
      const lower = word.toLowerCase();
      if (i === 0) return lower;
      return lower.charAt(0).toUpperCase() + lower.slice(1);
    })
    .join('');
}

// Generate basic English plural
// "Employee" → "Employees"
// "Company" → "Companies"
// "Address" → "Addresses"
// "Status" → "Statuses"
export function toPluralName(singular: string): string {
  if (!singular) return '';
  if (/(?:s|x|z|ch|sh)$/i.test(singular)) {
    return singular + 'es';
  }
  if (/[^aeiou]y$/i.test(singular)) {
    return singular.slice(0, -1) + 'ies';
  }
  return singular + 's';
}
