// English translations. Mirror the shape of `zh-CN.ts` exactly — the
// key-extraction script (`web/scripts/extract-i18n.mjs`) compares both
// resource trees and reports drift. Missing keys fall back to zh-CN.
const en = {
  common: {
    cancel: 'Cancel',
    confirm: 'Confirm',
    save: 'Save',
    delete: 'Delete',
    edit: 'Edit',
    close: 'Close',
    loading: 'Loading…',
    retry: 'Retry',
    search: 'Search',
    create: 'Create',
    apply: 'Apply',
    reset: 'Reset',
    yes: 'Yes',
    no: 'No',
  },
  nav: {
    dashboard: 'Dashboard',
    explorer: 'Explorer',
    browser: 'Browser',
    actions: 'Actions',
    threads: 'Threads',
    pipelines: 'Pipelines',
    lineage: 'Lineage',
    dashboards: 'Dashboards',
    approvals: 'Approvals',
    permissionRequests: 'Permission Requests',
    mentions: 'Mentions',
    aggregation: 'Aggregation',
    objectsets: 'Object Sets',
    admin: 'Admin',
    developer: 'Developer',
  },
  auth: {
    signIn: 'Sign in',
    signOut: 'Sign out',
    email: 'Email',
    password: 'Password',
    emailRequired: 'Email and password are required',
    invalidCredentials: 'Invalid email or password',
    tooManyAttempts: 'Too many attempts, try again in {{seconds}}s',
  },
  dashboard: {
    title: 'WEAVE',
    subtitle:
      'Define your data universe. Model objects, relationships, and actions in a unified ontology layer.',
    eyebrow: 'Ontology Layer Engine',
    ontologyCount_one: '{{count}} ontology',
    ontologyCount_other: '{{count}} ontologies',
    objectTypeCount_one: '{{count}} object type',
    objectTypeCount_other: '{{count}} object types',
  },
  theme: {
    label: 'Theme',
    light: 'Light',
    dark: 'Dark',
    system: 'System',
  },
  language: {
    label: 'Language',
    'zh-CN': '简体中文',
    en: 'English',
  },
  dashboardPage: {
    sectionOntologies: 'Ontologies',
    emptyTitle: 'No ontologies yet',
    emptyDescription:
      'Ontologies are managed through the Foundry API. Use the SDK or CLI to create ontologies.',
    failedToLoad: 'Failed to load ontologies: {{message}}',
    statOntologies: 'Ontologies',
    statObjectTypes: 'Object Types',
  },
  errors: {
    loadFailed: 'Failed to load: {{message}}',
    networkError: 'Network error, please check your connection and retry.',
    unknownError: 'Unknown error',
  },
} as const;

export default en;
