import { useState } from 'react';
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query';
import {
  listSecurityPolicies,
  createSecurityPolicy,
  deleteSecurityPolicy,
  type CreateSecurityPolicyInput,
} from '../../api/admin';
import { useObjectTypes } from '../../hooks/useObjectTypes';
import { Modal } from '../common/Modal';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { EmptyState } from '../common/EmptyState';
import { Badge } from '../common/Badge';

interface SecurityPolicyListPageProps {
  ontologyApiName: string;
}

export function SecurityPolicyListPage({ ontologyApiName }: SecurityPolicyListPageProps) {
  const queryClient = useQueryClient();
  const [selectedObjectTypeRid, setSelectedObjectTypeRid] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<{ rid: string } | null>(null);

  // Form state
  const [policyType, setPolicyType] = useState<'OBJECT' | 'PROPERTY'>('OBJECT');
  const [rulesRaw, setRulesRaw] = useState('');
  const [rulesError, setRulesError] = useState('');

  const { data: objectTypes, isLoading: objectTypesLoading } = useObjectTypes(ontologyApiName);

  const { data, isLoading: policiesLoading } = useQuery({
    queryKey: ['securityPolicies', selectedObjectTypeRid],
    queryFn: () => listSecurityPolicies(selectedObjectTypeRid),
    enabled: !!selectedObjectTypeRid,
  });

  const createMutation = useMutation({
    mutationFn: (input: CreateSecurityPolicyInput) =>
      createSecurityPolicy(selectedObjectTypeRid, input),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['securityPolicies', selectedObjectTypeRid] });
      setShowCreate(false);
      resetForm();
    },
  });

  const deleteMutation = useMutation({
    mutationFn: (rid: string) => deleteSecurityPolicy(rid),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ['securityPolicies', selectedObjectTypeRid] });
      setDeleteTarget(null);
    },
  });

  function resetForm() {
    setPolicyType('OBJECT');
    setRulesRaw('');
    setRulesError('');
  }

  function handleCreate(e: React.FormEvent) {
    e.preventDefault();
    setRulesError('');

    let rules: unknown;
    try {
      rules = JSON.parse(rulesRaw);
    } catch {
      setRulesError('Invalid JSON');
      return;
    }

    createMutation.mutate({ policyType, rules });
  }

  function handleCloseCreate() {
    setShowCreate(false);
    resetForm();
  }

  const policies = data?.data ?? [];
  const selectedObjectType = objectTypes?.find((ot) => ot.rid === selectedObjectTypeRid);

  const inputClass =
    'bg-bg-tertiary border border-border rounded px-3 py-2 text-sm text-text-primary font-mono focus:border-accent-cyan focus:outline-none w-full';
  const labelClass = 'text-xs text-text-secondary font-sans mb-1';

  return (
    <div>
      {/* Object Type selector */}
      <div className="mb-4">
        <label className={labelClass}>Select Object Type</label>
        {objectTypesLoading ? (
          <LoadingSpinner size="sm" />
        ) : !objectTypes || objectTypes.length === 0 ? (
          <div className="text-xs text-text-secondary">No object types in this ontology.</div>
        ) : (
          <select
            value={selectedObjectTypeRid}
            onChange={(e) => setSelectedObjectTypeRid(e.target.value)}
            className={inputClass}
          >
            <option value="">— choose an object type —</option>
            {objectTypes.map((ot) => (
              <option key={ot.rid} value={ot.rid}>
                {ot.apiName} ({ot.displayName})
              </option>
            ))}
          </select>
        )}
      </div>

      {!selectedObjectTypeRid ? (
        <EmptyState
          title="Select an Object Type"
          description="Choose an object type above to view and manage its security policies."
        />
      ) : (
        <>
          <div className="flex items-center justify-between mb-4">
            <h3 className="text-sm font-medium text-text-primary">
              Security Policies for{' '}
              <span className="font-mono">{selectedObjectType?.apiName ?? selectedObjectTypeRid}</span>
              {policies.length > 0 && ` (${policies.length})`}
            </h3>
            <button
              onClick={() => setShowCreate(true)}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
            >
              + Create Policy
            </button>
          </div>

          {policiesLoading ? (
            <LoadingSpinner />
          ) : policies.length === 0 ? (
            <EmptyState
              title="No Security Policies"
              description="Create a security policy to control access to this object type or its properties."
              action={
                <button
                  onClick={() => setShowCreate(true)}
                  className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80"
                >
                  + Create Policy
                </button>
              }
            />
          ) : (
            <div className="flex flex-col gap-2">
              {policies.map((sp) => (
                <div
                  key={sp.rid}
                  className="flex items-center justify-between p-3 bg-bg-tertiary border border-border rounded"
                >
                  <div>
                    <div className="text-sm font-mono text-text-primary">{sp.rid}</div>
                    <div className="text-xs text-text-secondary mt-0.5 font-mono">
                      {JSON.stringify(sp.rules).slice(0, 80)}
                      {JSON.stringify(sp.rules).length > 80 && '…'}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant={sp.policyType === 'OBJECT' ? 'info' : 'warning'}>
                      {sp.policyType}
                    </Badge>
                    <button
                      onClick={() => setDeleteTarget({ rid: sp.rid })}
                      className="p-1 text-text-muted hover:text-red-400 transition-colors"
                      title="Delete"
                    >
                      <svg className="w-4 h-4" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                        <path d="M3 6h18M19 6v14a2 2 0 01-2 2H7a2 2 0 01-2-2V6m3 0V4a2 2 0 012-2h4a2 2 0 012 2v2" />
                      </svg>
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </>
      )}

      {/* Create Policy Modal */}
      <Modal open={showCreate} onClose={handleCloseCreate} title="Create Security Policy">
        <form onSubmit={handleCreate} className="flex flex-col gap-4">
          <div className="flex flex-col">
            <label className={labelClass}>Policy Type</label>
            <select
              value={policyType}
              onChange={(e) => setPolicyType(e.target.value as 'OBJECT' | 'PROPERTY')}
              className={inputClass}
            >
              <option value="OBJECT">OBJECT</option>
              <option value="PROPERTY">PROPERTY</option>
            </select>
          </div>
          <div className="flex flex-col">
            <label className={labelClass}>Rules (JSON)</label>
            <textarea
              value={rulesRaw}
              onChange={(e) => {
                setRulesRaw(e.target.value);
                setRulesError('');
              }}
              className={`${inputClass} h-32 resize-y font-mono`}
              placeholder={'{\n  "principals": ["group:analysts"],\n  "permissions": ["READ"]\n}'}
              required
            />
            {rulesError && (
              <span className="text-xs text-red-400 mt-1">{rulesError}</span>
            )}
          </div>
          <div className="flex justify-end gap-3">
            <button
              type="button"
              onClick={handleCloseCreate}
              className="px-4 py-2 rounded text-sm text-text-secondary hover:text-text-primary border border-border"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={createMutation.isPending || !rulesRaw.trim()}
              className="bg-accent-cyan text-bg-primary px-4 py-2 rounded text-sm font-medium hover:bg-accent-cyan/80 disabled:opacity-50"
            >
              {createMutation.isPending ? 'Creating...' : 'Create'}
            </button>
          </div>
        </form>
      </Modal>

      {/* Delete Confirmation Modal */}
      <Modal
        open={deleteTarget !== null}
        onClose={() => setDeleteTarget(null)}
        title="Delete Security Policy"
      >
        <div className="flex flex-col gap-4">
          <p className="text-sm text-text-secondary">
            Are you sure you want to delete this security policy? This action cannot be undone.
          </p>
          <div className="flex justify-end gap-3">
            <button
              onClick={() => setDeleteTarget(null)}
              className="px-4 py-2 rounded text-sm text-text-secondary hover:text-text-primary border border-border"
            >
              Cancel
            </button>
            <button
              onClick={() => deleteTarget && deleteMutation.mutate(deleteTarget.rid)}
              disabled={deleteMutation.isPending}
              className="bg-red-600 text-white px-4 py-2 rounded text-sm font-medium hover:bg-red-700 disabled:opacity-50"
            >
              {deleteMutation.isPending ? 'Deleting...' : 'Delete'}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}
