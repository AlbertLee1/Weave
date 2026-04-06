import { cloneElement, isValidElement, type ReactElement, type ReactNode } from 'react';
import { useAuth } from '../../auth/useAuth';

interface PermissionGateProps {
  /** The permission code required to enable the wrapped action. */
  permission: string;
  /**
   * If set, do a scoped check on this ontology RID rather than a global one.
   * Useful for ontology-owner who is owner of one ontology but viewer of others.
   */
  ontologyRid?: string;
  /** A custom tooltip message; defaults to "You do not have permission ...". */
  deniedMessage?: string;
  /**
   * The wrapped element. If it is a single React element with a `disabled`
   * prop (e.g. a button), it will be cloned with `disabled={true}` and
   * pointer-events-none when the user lacks the permission. Otherwise the
   * children render unchanged inside a wrapper that signals denial via
   * aria-disabled.
   */
  children: ReactNode;
}

/**
 * PermissionGate wraps an action (typically a button) and disables it with a
 * tooltip when the current user lacks the required permission. The action is
 * NOT hidden — disabling preserves discoverability and lets reviewers ask
 * admins for access.
 */
export function PermissionGate({
  permission,
  ontologyRid,
  deniedMessage,
  children,
}: PermissionGateProps) {
  const { user, can, canOnOntology } = useAuth();

  const allowed =
    user !== null &&
    (ontologyRid ? canOnOntology(ontologyRid, permission) : can(permission));

  const tooltip = allowed
    ? undefined
    : deniedMessage ?? `You do not have permission (${permission}) to perform this action.`;

  let renderedChild: ReactNode = children;
  if (!allowed && isValidElement(children)) {
    const childProps = (children as ReactElement<{ disabled?: boolean }>).props;
    renderedChild = cloneElement(children as ReactElement<{ disabled?: boolean }>, {
      ...childProps,
      disabled: true,
    });
  }

  return (
    <span
      data-testid="permission-gate"
      title={tooltip}
      aria-disabled={!allowed}
      style={{ display: 'inline-block' }}
    >
      {renderedChild}
    </span>
  );
}
