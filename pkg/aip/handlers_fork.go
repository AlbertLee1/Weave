package aip

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/liyang/weave/pkg/apierror"
	"github.com/liyang/weave/pkg/httputil"
)

// forkThreadRequest is the body shape accepted by POST
// /api/v2/aip/threads/{threadId}/fork. messageId names the pivot
// message in the source thread; the new thread copies every ancestor
// from the root through (and including) the pivot.
//
// Optional title / model / systemPrompt overrides give the caller a
// chance to relabel the new branch at fork time. Provider is inherited
// from the source thread by design — switching providers mid-branch
// would invalidate the message history's tool-call references.
type forkThreadRequest struct {
	MessageID    int64   `json:"messageId"`
	NewThreadID  string  `json:"newThreadId,omitempty"`
	Title        *string `json:"title,omitempty"`
	Model        *string `json:"model,omitempty"`
	SystemPrompt *string `json:"systemPrompt,omitempty"`
}

// forkThreadResponse echoes the new thread plus the copied messages
// inline so the SPA can render the fresh branch without a second
// ListMessages round-trip.
type forkThreadResponse struct {
	Thread   *Thread    `json:"thread"`
	Messages []*Message `json:"messages"`
}

// MessageTreeNode is the recursive node the /tree endpoint emits. The
// node carries every Message field (flat fields cleaner than embedding
// for SDK serialisers) plus a Children slice ordered by message id asc.
type MessageTreeNode struct {
	*Message
	Children []*MessageTreeNode `json:"children,omitempty"`
}

// threadTreeResponse is the wire shape of GET .../tree. Roots are the
// messages whose ParentMessageID is nil; ordering is by id asc so the
// linear default branch comes first when multiple forks coexist in the
// same thread.
type threadTreeResponse struct {
	ThreadID string             `json:"threadId"`
	Roots    []*MessageTreeNode `json:"roots"`
}

// ForkThread POST /api/v2/aip/threads/{threadId}/fork.
func (h *Handler) ForkThread(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.threadIDParam(w, r)
	if !ok {
		return
	}
	src, ok := h.lookupThreadOwned(r.Context(), w, id, user)
	if !ok {
		return
	}
	var req forkThreadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidRequestBody", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if req.MessageID <= 0 {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("MissingMessageID", map[string]string{
			"reason": "messageId is required",
		}))
		return
	}

	pivot, err := h.store.GetMessage(r.Context(), req.MessageID)
	if err != nil {
		if errors.Is(err, ErrMessageNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPMessageNotFound", map[string]string{
				"messageId": strconv.FormatInt(req.MessageID, 10),
			}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPMessageLookupFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	if pivot.ThreadID != id {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("PivotThreadMismatch", map[string]string{
			"reason":     "pivot message does not belong to source thread",
			"messageId":  strconv.FormatInt(req.MessageID, 10),
			"sourceId":   id,
			"messageRef": pivot.ThreadID,
		}))
		return
	}

	newID := strings.TrimSpace(req.NewThreadID)
	if newID == "" {
		newID = newThreadID()
	} else if err := ValidateThreadID(newID); err != nil {
		apierror.WriteJSON(w, apierror.NewInvalidParameter("InvalidThreadID", map[string]string{
			"reason": err.Error(),
			"id":     req.NewThreadID,
		}))
		return
	}

	now := time.Now().UTC()
	newThread := &Thread{
		ID:           newID,
		Title:        deriveForkTitle(req.Title, src.Title),
		Provider:     src.Provider,
		Model:        derefOr(req.Model, src.Model),
		SystemPrompt: derefOr(req.SystemPrompt, src.SystemPrompt),
		CreatedBy:    user.ID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	stored, msgs, err := h.store.ForkThread(r.Context(), id, req.MessageID, newThread)
	if err != nil {
		switch {
		case errors.Is(err, ErrThreadAlreadyExists):
			apierror.WriteJSON(w, apierror.NewConflict("AIPThreadAlreadyExists", map[string]string{
				"id": newID,
			}))
		case errors.Is(err, ErrMessageNotFound):
			apierror.WriteJSON(w, apierror.NewNotFound("AIPMessageNotFound", map[string]string{
				"messageId": strconv.FormatInt(req.MessageID, 10),
			}))
		case errors.Is(err, ErrPivotThreadMismatch):
			apierror.WriteJSON(w, apierror.NewInvalidParameter("PivotThreadMismatch", map[string]string{
				"reason":    err.Error(),
				"messageId": strconv.FormatInt(req.MessageID, 10),
			}))
		case errors.Is(err, ErrThreadNotFound):
			apierror.WriteJSON(w, apierror.NewNotFound("AIPThreadNotFound", map[string]string{"id": id}))
		default:
			apierror.WriteJSON(w, apierror.NewInternal("AIPThreadForkFailed", map[string]string{
				"reason": err.Error(),
			}))
		}
		return
	}
	if msgs == nil {
		msgs = []*Message{}
	}
	httputil.WriteJSON(w, http.StatusCreated, forkThreadResponse{
		Thread:   stored,
		Messages: msgs,
	})
}

// GetThreadTree GET /api/v2/aip/threads/{threadId}/tree.
func (h *Handler) GetThreadTree(w http.ResponseWriter, r *http.Request) {
	user := h.requireAuth(w, r)
	if user == nil || !h.requireStore(w) {
		return
	}
	id, ok := h.threadIDParam(w, r)
	if !ok {
		return
	}
	if _, ok := h.lookupThreadOwned(r.Context(), w, id, user); !ok {
		return
	}
	msgs, err := h.store.ListMessages(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrThreadNotFound) {
			apierror.WriteJSON(w, apierror.NewNotFound("AIPThreadNotFound", map[string]string{"id": id}))
			return
		}
		apierror.WriteJSON(w, apierror.NewInternal("AIPMessageListFailed", map[string]string{
			"reason": err.Error(),
		}))
		return
	}
	httputil.WriteJSON(w, http.StatusOK, threadTreeResponse{
		ThreadID: id,
		Roots:    BuildMessageTree(msgs),
	})
}

// BuildMessageTree assembles a parent→children forest from a flat slice
// of messages. Roots (ParentMessageID nil OR pointing outside the slice)
// are returned in ascending id order; siblings are sorted by id asc so
// the assistant reply consistently follows its user prompt under any
// store-provided ordering.
func BuildMessageTree(msgs []*Message) []*MessageTreeNode {
	if len(msgs) == 0 {
		return []*MessageTreeNode{}
	}
	nodeByID := make(map[int64]*MessageTreeNode, len(msgs))
	for _, m := range msgs {
		nodeByID[m.ID] = &MessageTreeNode{Message: m}
	}
	var roots []*MessageTreeNode
	for _, m := range msgs {
		node := nodeByID[m.ID]
		if m.ParentMessageID == nil {
			roots = append(roots, node)
			continue
		}
		parent, ok := nodeByID[*m.ParentMessageID]
		if !ok {
			roots = append(roots, node)
			continue
		}
		parent.Children = append(parent.Children, node)
	}
	sortNodesByID(roots)
	for _, n := range nodeByID {
		if len(n.Children) > 1 {
			sortNodesByID(n.Children)
		}
	}
	if roots == nil {
		return []*MessageTreeNode{}
	}
	return roots
}

func sortNodesByID(nodes []*MessageTreeNode) {
	for i := 1; i < len(nodes); i++ {
		for j := i; j > 0 && nodes[j-1].Message.ID > nodes[j].Message.ID; j-- {
			nodes[j-1], nodes[j] = nodes[j], nodes[j-1]
		}
	}
}

func derefOr(p *string, fallback string) string {
	if p == nil {
		return fallback
	}
	return *p
}

func deriveForkTitle(override *string, srcTitle string) string {
	if override != nil {
		return *override
	}
	if srcTitle == "" {
		return "Fork"
	}
	return srcTitle + " (fork)"
}
