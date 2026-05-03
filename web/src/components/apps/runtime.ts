import type { AppEvent, AppVariable } from '../../api/apps';
import { coerceVariableValue, substituteVariables } from './layout';

// US-393: App runtime engine. The runtime owns:
//   1. The current value of every declared variable (a name → value map)
//   2. A dispatcher that interprets a single AppEvent (the only kind
//      consumed today: `setVariable` / `runAction` / `navigate`).
//
// The editor's Preview mode wires the dispatcher into button onClicks
// and uses `substituteVariables` to render `{{varName}}` references in
// component props. Edit mode bypasses the runtime entirely so authors
// always see the literal `{{var}}` placeholder + the unsubstituted
// values.

export type VariableValue = string | number | boolean;

export type VariableState = Record<string, VariableValue>;

export function initialVariableState(
  variables: AppVariable[],
): VariableState {
  const state: VariableState = {};
  for (const v of variables) {
    state[v.name] = coerceVariableValue(v.default ?? '', v.type);
  }
  return state;
}

export interface RuntimeContext {
  variables: AppVariable[];
  state: VariableState;
  setState: React.Dispatch<React.SetStateAction<VariableState>>;
  navigate: (to: string) => void;
  runAction: (
    actionType: string,
    params: Record<string, unknown>,
  ) => Promise<void> | void;
}

// dispatchEvent applies a single onClick action against the runtime
// context. setVariable is synchronous (in-memory state only); runAction
// and navigate are wrapped so async failures bubble up via the returned
// Promise without forcing every call site to await.
export async function dispatchEvent(
  event: AppEvent,
  ctx: RuntimeContext,
): Promise<void> {
  switch (event.kind) {
    case 'setVariable': {
      const variable = ctx.variables.find((v) => v.name === event.name);
      if (!variable) return;
      const expanded = substituteVariables(event.value ?? '', ctx.state);
      const next = coerceVariableValue(expanded, variable.type);
      ctx.setState((prev) => ({ ...prev, [variable.name]: next }));
      return;
    }
    case 'runAction': {
      const params: Record<string, unknown> = {};
      if (event.params) {
        for (const [k, raw] of Object.entries(event.params)) {
          params[k] = substituteVariables(String(raw ?? ''), ctx.state);
        }
      }
      await ctx.runAction(event.actionType, params);
      return;
    }
    case 'navigate': {
      const to = substituteVariables(event.to ?? '', ctx.state);
      ctx.navigate(to);
      return;
    }
  }
}
