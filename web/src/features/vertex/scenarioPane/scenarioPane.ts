// VTX-036 — Scenario Pane（侧栏纵向表格 UI）的纯逻辑层。
//
// 状态模型驱动 Scenarios 按钮折叠/展开、Case Study + Scenarios 列、Models/
// Actions 行、Input/Output 列。React 接线层把 toolbar 按钮、TanStack Table
// 表头/单元格、对话框等串起来；本模块只提供 reducer 与派生 helper，可
// 在 vitest 里直接驱动。

export interface CaseStudyRef {
  rid: string;
  name: string;
}

export interface ScenarioRef {
  rid: string;
  name: string;
  immutable?: boolean;
}

export interface ScenarioPaneActionRow {
  kind: 'action';
  rid: string;
  label: string;
  actionTypeId: string;
}

export interface ScenarioPaneModelRow {
  kind: 'model';
  rid: string;
  label: string;
  modelRid: string;
  // VTX-054: live model deployments expose versioned dropdowns. The pane row
  // captures the operator's selection so re-running the scenario stays
  // deterministic. Both fields are optional to preserve the VTX-036 row shape
  // for non-versioned (e.g. simple/test) model rows.
  modelVersion?: string;
  configVersion?: string;
}

export type ScenarioPaneRow = ScenarioPaneActionRow | ScenarioPaneModelRow;

export interface ScenarioPaneInputOutputColumn {
  key: string;
  label: string;
}

export interface ScenarioPaneState {
  expanded: boolean;
  caseStudy: CaseStudyRef | null;
  scenarios: ScenarioRef[];
  rows: ScenarioPaneRow[];
  inputOutputColumns: ScenarioPaneInputOutputColumn[];
}

export interface ScenarioPaneInit {
  expanded?: boolean;
  caseStudy?: CaseStudyRef | null;
  scenarios?: ScenarioRef[];
  rows?: ScenarioPaneRow[];
  inputOutputColumns?: ScenarioPaneInputOutputColumn[];
}

export function createScenarioPaneState(init?: ScenarioPaneInit): ScenarioPaneState {
  return {
    expanded: init?.expanded ?? false,
    caseStudy: init?.caseStudy ?? null,
    scenarios: init?.scenarios ? [...init.scenarios] : [],
    rows: init?.rows ? [...init.rows] : [],
    inputOutputColumns: init?.inputOutputColumns ? [...init.inputOutputColumns] : [],
  };
}

export function togglePane(state: ScenarioPaneState): ScenarioPaneState {
  return { ...state, expanded: !state.expanded };
}

export function setPaneExpanded(
  state: ScenarioPaneState,
  expanded: boolean,
): ScenarioPaneState {
  return { ...state, expanded };
}

// setCaseStudy switches the active Case Study. When the rid changes (or is
// cleared) the dependent state — scenarios, rows, input/output columns — is
// reset, since those records are owned by the previous Case Study. Re-setting
// the same rid is a no-op on dependent state so React callers can refresh the
// payload (e.g. after rename) without losing scenario/row context.
export function setCaseStudy(
  state: ScenarioPaneState,
  caseStudy: CaseStudyRef | null,
): ScenarioPaneState {
  const sameRid =
    state.caseStudy !== null &&
    caseStudy !== null &&
    state.caseStudy.rid === caseStudy.rid;
  if (sameRid) {
    return { ...state, caseStudy };
  }
  return {
    ...state,
    caseStudy,
    scenarios: [],
    rows: [],
    inputOutputColumns: [],
  };
}

export function addScenario(
  state: ScenarioPaneState,
  scenario: ScenarioRef,
): ScenarioPaneState {
  if (state.caseStudy === null) {
    throw new Error('addScenario requires an active case study');
  }
  if (state.scenarios.some(s => s.rid === scenario.rid)) {
    return state;
  }
  return { ...state, scenarios: [...state.scenarios, scenario] };
}

export function removeScenario(
  state: ScenarioPaneState,
  rid: string,
): ScenarioPaneState {
  if (!state.scenarios.some(s => s.rid === rid)) {
    return state;
  }
  return { ...state, scenarios: state.scenarios.filter(s => s.rid !== rid) };
}

export function addActionRow(
  state: ScenarioPaneState,
  row: ScenarioPaneActionRow,
): ScenarioPaneState {
  if (state.rows.some(r => r.rid === row.rid)) {
    return state;
  }
  return { ...state, rows: [...state.rows, row] };
}

export function addModelRow(
  state: ScenarioPaneState,
  row: ScenarioPaneModelRow,
): ScenarioPaneState {
  if (state.rows.some(r => r.rid === row.rid)) {
    return state;
  }
  return { ...state, rows: [...state.rows, row] };
}

export function removeRow(
  state: ScenarioPaneState,
  rid: string,
): ScenarioPaneState {
  if (!state.rows.some(r => r.rid === rid)) {
    return state;
  }
  return { ...state, rows: state.rows.filter(r => r.rid !== rid) };
}

export function addInputOutputColumn(
  state: ScenarioPaneState,
  column: ScenarioPaneInputOutputColumn,
): ScenarioPaneState {
  if (state.inputOutputColumns.some(c => c.key === column.key)) {
    return state;
  }
  return {
    ...state,
    inputOutputColumns: [...state.inputOutputColumns, column],
  };
}

export function removeInputOutputColumn(
  state: ScenarioPaneState,
  key: string,
): ScenarioPaneState {
  if (!state.inputOutputColumns.some(c => c.key === key)) {
    return state;
  }
  return {
    ...state,
    inputOutputColumns: state.inputOutputColumns.filter(c => c.key !== key),
  };
}

export type ScenarioPaneToolbarButton =
  | 'addCaseStudy'
  | 'addScenario'
  | 'addAction'
  | 'addInputOrOutput'
  | 'run';

// getToolbarButtons returns the toolbar button set for the current state.
// Empty (no case study) → only the "+ Add Case Study" action is meaningful.
// Otherwise the full Scenario toolbar is exposed; the React layer can disable
// individual buttons based on stricter rules (e.g. Run requires ≥1 scenario)
// but the Pane spec exposes them all the moment a case study is loaded.
export function getToolbarButtons(state: ScenarioPaneState): ScenarioPaneToolbarButton[] {
  if (state.caseStudy === null) {
    return ['addCaseStudy'];
  }
  return ['addScenario', 'addAction', 'addInputOrOutput', 'run'];
}

// getColumnHeaders returns the Pane table header row.
// No case study → no columns. Otherwise: Baseline always leads, then each
// Scenario in insertion order. Input/Output columns are *rows* in the Pane
// orientation (vertical table per the spec), so they are not returned here.
export function getColumnHeaders(state: ScenarioPaneState): string[] {
  if (state.caseStudy === null) return [];
  return ['Baseline', ...state.scenarios.map(s => s.name)];
}

export interface ScenarioPaneRowGroups {
  models: ScenarioPaneModelRow[];
  actions: ScenarioPaneActionRow[];
}

// getRowGroups splits Pane rows by kind for the "Models / Actions" sectioned
// rendering. Insertion order is preserved within each group so the React layer
// can show stable row indexes for keyboard nav.
export function getRowGroups(state: ScenarioPaneState): ScenarioPaneRowGroups {
  const models: ScenarioPaneModelRow[] = [];
  const actions: ScenarioPaneActionRow[] = [];
  for (const row of state.rows) {
    if (row.kind === 'model') models.push(row);
    else actions.push(row);
  }
  return { models, actions };
}

export function isEmpty(state: ScenarioPaneState): boolean {
  return state.caseStudy === null;
}
