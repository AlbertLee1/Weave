import { useEffect, useMemo, useRef, useState } from 'react';
import {
  useComments,
  useCreateComment,
  useDeleteComment,
  useUpdateComment,
} from '../../hooks/useComments';
import type { Comment } from '../../api/comments';
import { LoadingSpinner } from '../common/LoadingSpinner';
import { Modal } from '../common/Modal';
import { useAuth } from '../../auth/useAuth';
import { MentionTextarea } from './MentionTextarea';
import { CommentBody } from './CommentBody';

interface CommentsTabProps {
  targetRid: string;
  // highlightCommentId opts the rendered thread into a one-shot
  // scroll-into-view + ring-glow on the matching comment row. Used by
  // the /mentions deep link (US-340) so a notification click lands on
  // the exact comment instead of the top of the thread.
  highlightCommentId?: string;
}

interface ThreadNode {
  comment: Comment;
  children: Comment[];
}

function formatTimestamp(value: string): string {
  if (!value) return '';
  try {
    return new Date(value).toLocaleString();
  } catch {
    return value;
  }
}

// Group flat comment list into a one-deep thread (root + replies). The
// backend enforces reply-of-reply rejection (US-334), so this is the only
// shape we need to render.
function buildThreads(comments: Comment[]): ThreadNode[] {
  const roots: Comment[] = [];
  const replies = new Map<string, Comment[]>();
  for (const c of comments) {
    if (c.parentId) {
      const list = replies.get(c.parentId) ?? [];
      list.push(c);
      replies.set(c.parentId, list);
    } else {
      roots.push(c);
    }
  }
  const byCreated = (a: Comment, b: Comment) =>
    a.createdAt.localeCompare(b.createdAt);
  roots.sort(byCreated);
  for (const list of replies.values()) list.sort(byCreated);
  return roots.map((c) => ({
    comment: c,
    children: replies.get(c.id) ?? [],
  }));
}

export function CommentsTab({ targetRid, highlightCommentId }: CommentsTabProps) {
  const { user } = useAuth();
  const meId = user?.id ?? '';
  const { data, isLoading, error } = useComments({
    targetRid,
    limit: 200,
    enabled: !!targetRid,
  });
  const createMut = useCreateComment(targetRid);
  const updateMut = useUpdateComment(targetRid);
  const deleteMut = useDeleteComment(targetRid);

  const [draft, setDraft] = useState('');
  const [createError, setCreateError] = useState<string | null>(null);
  const [replyParent, setReplyParent] = useState<string | null>(null);
  const [replyDraft, setReplyDraft] = useState('');
  const [replyError, setReplyError] = useState<string | null>(null);
  const [editId, setEditId] = useState<string | null>(null);
  const [editDraft, setEditDraft] = useState('');
  const [updateError, setUpdateError] = useState<string | null>(null);
  const [pendingDeleteId, setPendingDeleteId] = useState<string | null>(null);
  const [deleteError, setDeleteError] = useState<string | null>(null);

  const threads = useMemo(
    () => (data ? buildThreads(data.comments) : []),
    [data],
  );

  // One-shot scroll-into-view when the deep-link target lands. Tracks the
  // latest highlightCommentId so callers can re-target without remounting.
  const scrolledForRef = useRef<string | null>(null);
  useEffect(() => {
    if (!highlightCommentId || !data) return;
    if (scrolledForRef.current === highlightCommentId) return;
    const el = document.querySelector(
      `[data-testid="comment-row-${highlightCommentId}"]`,
    );
    if (el && typeof (el as HTMLElement).scrollIntoView === 'function') {
      (el as HTMLElement).scrollIntoView({ block: 'center' });
    }
    scrolledForRef.current = highlightCommentId;
  }, [highlightCommentId, data]);

  if (!targetRid) {
    return (
      <p
        className="text-xs text-text-secondary py-6 text-center"
        data-testid="comments-no-target"
      >
        Comments are unavailable: object has no RID.
      </p>
    );
  }

  if (isLoading) {
    return (
      <div
        className="flex items-center justify-center py-12"
        data-testid="comments-loading"
      >
        <LoadingSpinner />
      </div>
    );
  }

  if (error) {
    return (
      <p className="text-xs text-accent-error" data-testid="comments-error">
        Failed to load comments: {(error as Error).message}
      </p>
    );
  }

  async function handleCreate() {
    const trimmed = draft.trim();
    if (!trimmed) {
      setCreateError('Comment body must not be empty');
      return;
    }
    setCreateError(null);
    try {
      await createMut.mutateAsync({ targetRid, body: trimmed });
      setDraft('');
    } catch (e) {
      setCreateError((e as Error).message);
    }
  }

  async function handleReply(parentId: string) {
    const trimmed = replyDraft.trim();
    if (!trimmed) return;
    setReplyError(null);
    try {
      await createMut.mutateAsync({ targetRid, body: trimmed, parentId });
      setReplyDraft('');
      setReplyParent(null);
    } catch (e) {
      // Keep the reply form + draft open so the user can retry without
      // retyping, and surface the failure inline.
      setReplyError((e as Error).message);
    }
  }

  async function handleUpdate(id: string) {
    const trimmed = editDraft.trim();
    if (!trimmed) return;
    setUpdateError(null);
    try {
      await updateMut.mutateAsync({ id, body: trimmed });
      setEditId(null);
      setEditDraft('');
    } catch (e) {
      // Keep the inline editor open and show why the save failed.
      setUpdateError((e as Error).message);
    }
  }

  async function handleConfirmDelete() {
    if (!pendingDeleteId) return;
    setDeleteError(null);
    try {
      await deleteMut.mutateAsync(pendingDeleteId);
      setPendingDeleteId(null);
    } catch (e) {
      // Keep the confirm modal open and surface the failure inside it.
      setDeleteError((e as Error).message);
    }
  }

  return (
    <div className="space-y-4" data-testid="comments-tab">
      <section data-testid="comments-list">
        {threads.length === 0 ? (
          <p
            className="text-xs text-text-secondary py-6 text-center"
            data-testid="comments-empty"
          >
            No comments yet — start the discussion below.
          </p>
        ) : (
          <ul className="space-y-3">
            {threads.map((node) => (
              <li
                key={node.comment.id}
                className="rounded border border-border bg-bg-elevated/60 p-3"
                data-testid={`comment-thread-${node.comment.id}`}
              >
                <CommentRow
                  comment={node.comment}
                  meId={meId}
                  onReplyClick={() => {
                    setReplyParent(node.comment.id);
                    setReplyDraft('');
                    setReplyError(null);
                  }}
                  onEditClick={() => {
                    setEditId(node.comment.id);
                    setEditDraft(node.comment.body);
                    setUpdateError(null);
                  }}
                  onDeleteClick={() => {
                    setPendingDeleteId(node.comment.id);
                    setDeleteError(null);
                  }}
                  isEditing={editId === node.comment.id}
                  editDraft={editDraft}
                  editError={editId === node.comment.id ? updateError : null}
                  onEditDraftChange={setEditDraft}
                  onEditSave={() => handleUpdate(node.comment.id)}
                  onEditCancel={() => {
                    setEditId(null);
                    setEditDraft('');
                    setUpdateError(null);
                  }}
                  updating={updateMut.isPending}
                  canReply
                  highlight={highlightCommentId === node.comment.id}
                />

                {node.children.length > 0 && (
                  <ul
                    className="mt-3 space-y-2 pl-4 border-l border-border"
                    data-testid={`comment-replies-${node.comment.id}`}
                  >
                    {node.children.map((reply) => (
                      <li
                        key={reply.id}
                        data-testid={`comment-reply-${reply.id}`}
                      >
                        <CommentRow
                          comment={reply}
                          meId={meId}
                          onReplyClick={null}
                          onEditClick={() => {
                            setEditId(reply.id);
                            setEditDraft(reply.body);
                            setUpdateError(null);
                          }}
                          onDeleteClick={() => {
                            setPendingDeleteId(reply.id);
                            setDeleteError(null);
                          }}
                          isEditing={editId === reply.id}
                          editDraft={editDraft}
                          editError={editId === reply.id ? updateError : null}
                          onEditDraftChange={setEditDraft}
                          onEditSave={() => handleUpdate(reply.id)}
                          onEditCancel={() => {
                            setEditId(null);
                            setEditDraft('');
                            setUpdateError(null);
                          }}
                          updating={updateMut.isPending}
                          canReply={false}
                          highlight={highlightCommentId === reply.id}
                        />
                      </li>
                    ))}
                  </ul>
                )}

                {replyParent === node.comment.id && (
                  <div
                    className="mt-3 space-y-2"
                    data-testid={`comment-reply-form-${node.comment.id}`}
                  >
                    <MentionTextarea
                      value={replyDraft}
                      onChange={setReplyDraft}
                      rows={2}
                      placeholder="Write a reply…"
                      aria-label="Reply body"
                      className="w-full text-xs font-mono px-2 py-1.5 rounded border border-border bg-bg-base text-text-primary"
                      data-testid={`comment-reply-input-${node.comment.id}`}
                    />
                    {replyError && (
                      <p
                        className="text-xs text-accent-error"
                        data-testid={`comment-reply-error-${node.comment.id}`}
                      >
                        {replyError}
                      </p>
                    )}
                    <div className="flex gap-2 justify-end">
                      <button
                        type="button"
                        onClick={() => {
                          setReplyParent(null);
                          setReplyDraft('');
                          setReplyError(null);
                        }}
                        className="text-xs px-2 py-1 rounded border border-border text-text-secondary hover:text-text-primary"
                        data-testid={`comment-reply-cancel-${node.comment.id}`}
                      >
                        Cancel
                      </button>
                      <button
                        type="button"
                        onClick={() => handleReply(node.comment.id)}
                        disabled={createMut.isPending || !replyDraft.trim()}
                        className="text-xs px-2 py-1 rounded bg-accent-cyan/15 text-accent-cyan border border-accent-cyan/40 disabled:opacity-50"
                        data-testid={`comment-reply-submit-${node.comment.id}`}
                      >
                        Reply
                      </button>
                    </div>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section
        className="space-y-2 border-t border-border pt-3"
        data-testid="comments-create-form"
      >
        <label
          htmlFor="comment-new-body"
          className="text-xs font-sans font-medium text-text-secondary uppercase tracking-wider"
        >
          Add a comment
        </label>
        <MentionTextarea
          id="comment-new-body"
          value={draft}
          onChange={setDraft}
          rows={3}
          placeholder="Share something about this object…  Type @ to mention a teammate."
          className="w-full text-xs font-mono px-2 py-1.5 rounded border border-border bg-bg-base text-text-primary"
          data-testid="comment-new-input"
        />
        {createError && (
          <p className="text-xs text-accent-error" data-testid="comment-new-error">
            {createError}
          </p>
        )}
        <div className="flex justify-end">
          <button
            type="button"
            onClick={handleCreate}
            disabled={createMut.isPending || !draft.trim()}
            className="text-xs px-3 py-1.5 rounded bg-accent-cyan/15 text-accent-cyan border border-accent-cyan/40 hover:bg-accent-cyan/25 disabled:opacity-50"
            data-testid="comment-new-submit"
          >
            {createMut.isPending ? 'Posting…' : 'Post comment'}
          </button>
        </div>
      </section>

      <Modal
        open={pendingDeleteId !== null}
        onClose={() => {
          setPendingDeleteId(null);
          setDeleteError(null);
        }}
        title="Delete this comment?"
      >
        <div className="space-y-4" data-testid="comment-delete-confirm">
          <p className="text-sm text-text-primary">
            The comment body will be cleared but the placeholder stays visible
            so reply chains keep their context.
          </p>
          {deleteError && (
            <p
              className="text-xs text-accent-error"
              data-testid="comment-delete-error"
            >
              {deleteError}
            </p>
          )}
          <div className="flex justify-end gap-2">
            <button
              type="button"
              onClick={() => {
                setPendingDeleteId(null);
                setDeleteError(null);
              }}
              className="text-xs px-3 py-1.5 rounded border border-border text-text-secondary"
              data-testid="comment-delete-cancel"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={handleConfirmDelete}
              disabled={deleteMut.isPending}
              className="text-xs px-3 py-1.5 rounded bg-accent-error/20 text-accent-error border border-accent-error/40 disabled:opacity-50"
              data-testid="comment-delete-submit"
            >
              {deleteMut.isPending ? 'Deleting…' : 'Delete'}
            </button>
          </div>
        </div>
      </Modal>
    </div>
  );
}

interface CommentRowProps {
  comment: Comment;
  meId: string;
  onReplyClick: (() => void) | null;
  onEditClick: () => void;
  onDeleteClick: () => void;
  isEditing: boolean;
  editDraft: string;
  editError?: string | null;
  onEditDraftChange: (value: string) => void;
  onEditSave: () => void;
  onEditCancel: () => void;
  updating: boolean;
  canReply: boolean;
  highlight?: boolean;
}

function CommentRow({
  comment,
  meId,
  onReplyClick,
  onEditClick,
  onDeleteClick,
  isEditing,
  editDraft,
  editError = null,
  onEditDraftChange,
  onEditSave,
  onEditCancel,
  updating,
  canReply,
  highlight = false,
}: CommentRowProps) {
  const tombstoned = !!comment.deletedAt;
  const isMine = !tombstoned && comment.author === meId && meId !== '';
  const highlightClass = highlight
    ? 'rounded-sm ring-2 ring-accent-cyan/60 ring-offset-2 ring-offset-bg-elevated'
    : '';

  return (
    <div
      data-testid={`comment-row-${comment.id}`}
      data-highlight={highlight ? 'true' : 'false'}
      className={highlightClass}
    >
      <div className="flex items-baseline justify-between gap-2">
        <span
          className="text-xs font-mono text-accent-cyan"
          data-testid={`comment-author-${comment.id}`}
        >
          {tombstoned ? '[deleted]' : comment.author || 'unknown'}
        </span>
        <span className="text-[10px] text-text-secondary font-mono">
          {formatTimestamp(comment.createdAt)}
        </span>
      </div>

      {isEditing ? (
        <div className="mt-2 space-y-2">
          <textarea
            value={editDraft}
            onChange={(e) => onEditDraftChange(e.target.value)}
            rows={2}
            aria-label="Edit comment body"
            className="w-full text-xs font-mono px-2 py-1.5 rounded border border-border bg-bg-base text-text-primary"
            data-testid={`comment-edit-input-${comment.id}`}
          />
          {editError && (
            <p
              className="text-xs text-accent-error"
              data-testid={`comment-edit-error-${comment.id}`}
            >
              {editError}
            </p>
          )}
          <div className="flex gap-2 justify-end">
            <button
              type="button"
              onClick={onEditCancel}
              className="text-xs px-2 py-1 rounded border border-border text-text-secondary"
              data-testid={`comment-edit-cancel-${comment.id}`}
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={onEditSave}
              disabled={updating || !editDraft.trim()}
              className="text-xs px-2 py-1 rounded bg-accent-cyan/15 text-accent-cyan border border-accent-cyan/40 disabled:opacity-50"
              data-testid={`comment-edit-submit-${comment.id}`}
            >
              Save
            </button>
          </div>
        </div>
      ) : tombstoned ? (
        <p
          className="mt-1 text-xs whitespace-pre-wrap break-words italic text-text-secondary"
          data-testid={`comment-body-${comment.id}`}
        >
          [comment deleted]
        </p>
      ) : (
        <div className="mt-1">
          <CommentBody
            body={comment.body}
            testId={`comment-body-${comment.id}`}
          />
        </div>
      )}

      {!isEditing && !tombstoned && (
        <div className="mt-2 flex gap-3 text-[11px] font-mono">
          {canReply && onReplyClick && (
            <button
              type="button"
              onClick={onReplyClick}
              className="text-text-secondary hover:text-accent-cyan"
              data-testid={`comment-reply-button-${comment.id}`}
            >
              Reply
            </button>
          )}
          {isMine && (
            <button
              type="button"
              onClick={onEditClick}
              className="text-text-secondary hover:text-accent-cyan"
              data-testid={`comment-edit-button-${comment.id}`}
            >
              Edit
            </button>
          )}
          {isMine && (
            <button
              type="button"
              onClick={onDeleteClick}
              className="text-text-secondary hover:text-accent-error"
              data-testid={`comment-delete-button-${comment.id}`}
            >
              Delete
            </button>
          )}
        </div>
      )}
    </div>
  );
}
