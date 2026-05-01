// Canonical zh-CN baseline. Keys follow the structure documented in
// `web/src/i18n/README.md`. zh-CN is the project's source-of-truth locale —
// every key MUST be present here; `en.ts` is the translation surface.
//
// Convention: dot-separated dotted-namespace keys (e.g. `common.cancel`,
// `nav.dashboard`). Plurals use i18next's `_one` / `_other` suffix
// (e.g. `objects.count_one`, `objects.count_other`). Interpolation uses
// `{{var}}` placeholders.
const zhCN = {
  common: {
    cancel: '取消',
    confirm: '确认',
    save: '保存',
    delete: '删除',
    edit: '编辑',
    close: '关闭',
    loading: '加载中…',
    retry: '重试',
    search: '搜索',
    create: '新建',
    apply: '应用',
    reset: '重置',
    yes: '是',
    no: '否',
  },
  nav: {
    dashboard: '仪表盘',
    explorer: '本体浏览',
    browser: '对象表',
    actions: '动作',
    threads: '会话',
    pipelines: '数据管道',
    lineage: '血缘',
    dashboards: '可视化',
    approvals: '审批',
    permissionRequests: '权限申请',
    mentions: '@ 提及',
    aggregation: '聚合',
    objectsets: '对象集',
    admin: '管理',
    developer: '开发者',
  },
  auth: {
    signIn: '登录',
    signOut: '退出',
    email: '邮箱',
    password: '密码',
    emailRequired: '请输入邮箱和密码',
    invalidCredentials: '邮箱或密码错误',
    tooManyAttempts: '尝试次数过多，请在 {{seconds}} 秒后重试',
  },
  dashboard: {
    title: 'WEAVE',
    subtitle: '定义你的数据宇宙——以统一的本体层建模对象、关系与动作。',
    eyebrow: 'Ontology Layer Engine',
    // Chinese (zh-CN) only has the CLDR `other` plural form, so a single
    // `_other` entry covers every count. The corresponding English file
    // ships both `_one` and `_other`. The extraction script normalises
    // plural suffixes before comparing so the trees stay in parity despite
    // the per-locale shape difference.
    ontologyCount_other: '{{count}} 个本体',
    objectTypeCount_other: '{{count}} 个对象类型',
  },
  theme: {
    label: '主题',
    light: '浅色',
    dark: '深色',
    system: '跟随系统',
  },
  language: {
    label: '语言',
    'zh-CN': '简体中文',
    en: 'English',
  },
  dashboardPage: {
    sectionOntologies: '本体',
    emptyTitle: '暂无本体',
    emptyDescription: '本体通过 Foundry API 管理，请使用 SDK 或 CLI 创建本体。',
    failedToLoad: '加载本体失败：{{message}}',
    statOntologies: '本体',
    statObjectTypes: '对象类型',
  },
  errors: {
    loadFailed: '加载失败：{{message}}',
    networkError: '网络异常，请检查连接后重试',
    unknownError: '未知错误',
  },
  hotkeys: {
    groupGlobal: '全局',
    groupNavigation: '导航',
    commandPalette: '打开命令面板',
    goDashboard: '前往仪表盘',
    goObjectsets: '前往对象集',
    goPipelines: '前往数据管道',
    goApprovals: '前往审批',
  },
} as const;

export default zhCN;
