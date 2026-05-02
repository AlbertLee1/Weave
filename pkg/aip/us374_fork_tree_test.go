package aip

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// seedLinearThread builds an alice-owned thread carrying a linear
// user → assistant → user → assistant chain rooted at the first user
// message. The store auto-links parent_message_id by default so the
// caller doesn't have to thread the chain manually.
func seedLinearThread(t *testing.T, store *MemoryStore, id string) []*Message {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateThread(ctx, &Thread{ID: id, Provider: ProviderMock, CreatedBy: "user:alice"}); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	var msgs []*Message
	for _, spec := range []struct{ role, content string }{
		{RoleUser, "hello"},
		{RoleAssistant, "hi back"},
		{RoleUser, "more please"},
		{RoleAssistant, "here you go"},
	} {
		m := &Message{ThreadID: id, Role: spec.role, Content: spec.content}
		if err := store.AppendMessage(ctx, m); err != nil {
			t.Fatalf("AppendMessage(%s): %v", spec.content, err)
		}
		msgs = append(msgs, m)
	}
	return msgs
}

func TestUS374_AppendMessage_AutoLinksParentOnDefaultBranch(t *testing.T) {
	store := NewMemoryStore()
	msgs := seedLinearThread(t, store, "thr_link")

	if msgs[0].ParentMessageID != nil {
		t.Errorf("first message should have nil parent, got %v", *msgs[0].ParentMessageID)
	}
	if msgs[0].BranchID != DefaultBranchID {
		t.Errorf("BranchID = %q want %q", msgs[0].BranchID, DefaultBranchID)
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i].ParentMessageID == nil {
			t.Fatalf("msg[%d] parent should be set", i)
		}
		if *msgs[i].ParentMessageID != msgs[i-1].ID {
			t.Errorf("msg[%d] parent=%d want %d", i, *msgs[i].ParentMessageID, msgs[i-1].ID)
		}
	}
}

func TestUS374_ForkThread_CopiesAncestorChain(t *testing.T) {
	store := NewMemoryStore()
	msgs := seedLinearThread(t, store, "thr_src")

	pivot := msgs[1] // assistant "hi back"
	newThread := &Thread{
		ID:        "thr_fork",
		Provider:  ProviderMock,
		CreatedBy: "user:alice",
		Title:     "branch",
	}
	stored, copied, err := store.ForkThread(context.Background(), "thr_src", pivot.ID, newThread)
	if err != nil {
		t.Fatalf("ForkThread: %v", err)
	}
	if stored.ID != "thr_fork" {
		t.Fatalf("stored.ID=%q", stored.ID)
	}
	if len(copied) != 2 {
		t.Fatalf("copied len=%d want 2 (root user + pivot assistant)", len(copied))
	}
	if copied[0].Role != RoleUser || copied[0].Content != "hello" {
		t.Errorf("copied[0]=%+v", copied[0])
	}
	if copied[1].Role != RoleAssistant || copied[1].Content != "hi back" {
		t.Errorf("copied[1]=%+v", copied[1])
	}
	if copied[0].ParentMessageID != nil {
		t.Errorf("copied root should have nil parent, got %v", *copied[0].ParentMessageID)
	}
	if copied[1].ParentMessageID == nil || *copied[1].ParentMessageID != copied[0].ID {
		t.Errorf("copied[1] parent expected %d got %v", copied[0].ID, copied[1].ParentMessageID)
	}
	if copied[0].ID == msgs[0].ID || copied[1].ID == msgs[1].ID {
		t.Errorf("copied messages must have fresh ids; got %d/%d", copied[0].ID, copied[1].ID)
	}
	for _, c := range copied {
		if c.ThreadID != "thr_fork" {
			t.Errorf("copied ThreadID=%q want thr_fork", c.ThreadID)
		}
		if c.BranchID != DefaultBranchID {
			t.Errorf("copied BranchID=%q want main", c.BranchID)
		}
	}
}

func TestUS374_ForkedBranch_AppendsIndependentlyOfSource(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	srcMsgs := seedLinearThread(t, store, "thr_src2")

	pivot := srcMsgs[1]
	if _, _, err := store.ForkThread(ctx, "thr_src2", pivot.ID, &Thread{
		ID: "thr_fork2", Provider: ProviderMock, CreatedBy: "user:alice",
	}); err != nil {
		t.Fatalf("ForkThread: %v", err)
	}

	// Append on the new branch — the source thread should not see it.
	if err := store.AppendMessage(ctx, &Message{ThreadID: "thr_fork2", Role: RoleUser, Content: "branch-only"}); err != nil {
		t.Fatalf("AppendMessage on fork: %v", err)
	}
	srcAfter, err := store.ListMessages(ctx, "thr_src2")
	if err != nil {
		t.Fatalf("ListMessages src: %v", err)
	}
	if len(srcAfter) != len(srcMsgs) {
		t.Errorf("source thread message count changed: before=%d after=%d", len(srcMsgs), len(srcAfter))
	}
	for _, m := range srcAfter {
		if m.Content == "branch-only" {
			t.Errorf("branch-only message leaked into source thread")
		}
	}
	forkAfter, err := store.ListMessages(ctx, "thr_fork2")
	if err != nil {
		t.Fatalf("ListMessages fork: %v", err)
	}
	if len(forkAfter) != 3 {
		t.Fatalf("fork should now have 3 messages (2 copied + 1 new); got %d", len(forkAfter))
	}
	last := forkAfter[len(forkAfter)-1]
	if last.Content != "branch-only" {
		t.Errorf("last fork message content=%q", last.Content)
	}
	if last.ParentMessageID == nil || *last.ParentMessageID != forkAfter[len(forkAfter)-2].ID {
		t.Errorf("new fork message parent chain broken")
	}

	// Append on the source thread — the fork should not see the update.
	if err := store.AppendMessage(ctx, &Message{ThreadID: "thr_src2", Role: RoleUser, Content: "src-extension"}); err != nil {
		t.Fatalf("AppendMessage on src: %v", err)
	}
	forkAgain, _ := store.ListMessages(ctx, "thr_fork2")
	for _, m := range forkAgain {
		if m.Content == "src-extension" {
			t.Errorf("src-extension leaked into fork")
		}
	}
}

func TestUS374_ForkThread_PivotAtRoot_OnlyOneMessage(t *testing.T) {
	store := NewMemoryStore()
	msgs := seedLinearThread(t, store, "thr_root")
	_, copied, err := store.ForkThread(context.Background(), "thr_root", msgs[0].ID, &Thread{
		ID: "thr_root_fork", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	if err != nil {
		t.Fatalf("ForkThread: %v", err)
	}
	if len(copied) != 1 {
		t.Fatalf("copied len=%d want 1", len(copied))
	}
	if copied[0].ParentMessageID != nil {
		t.Errorf("root fork copy should have nil parent")
	}
}

func TestUS374_ForkThread_RejectsUnknownPivot(t *testing.T) {
	store := NewMemoryStore()
	seedLinearThread(t, store, "thr_x")
	_, _, err := store.ForkThread(context.Background(), "thr_x", 99_999, &Thread{
		ID: "thr_x_fork", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	if !errors.Is(err, ErrMessageNotFound) {
		t.Fatalf("ForkThread bogus pivot err=%v want ErrMessageNotFound", err)
	}
	if _, err := store.GetThread(context.Background(), "thr_x_fork"); !errors.Is(err, ErrThreadNotFound) {
		t.Errorf("failed fork should not have created destination thread")
	}
}

func TestUS374_ForkThread_RejectsCrossThreadPivot(t *testing.T) {
	store := NewMemoryStore()
	seedLinearThread(t, store, "thr_a1")
	bMsgs := seedLinearThread(t, store, "thr_b1")
	_, _, err := store.ForkThread(context.Background(), "thr_a1", bMsgs[0].ID, &Thread{
		ID: "thr_a1_fork", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	if !errors.Is(err, ErrPivotThreadMismatch) {
		t.Fatalf("err=%v want ErrPivotThreadMismatch", err)
	}
}

func TestUS374_ForkThread_RejectsDuplicateNewID(t *testing.T) {
	store := NewMemoryStore()
	msgs := seedLinearThread(t, store, "thr_dup")
	if err := store.CreateThread(context.Background(), &Thread{ID: "thr_taken", Provider: ProviderMock, CreatedBy: "user:alice"}); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	_, _, err := store.ForkThread(context.Background(), "thr_dup", msgs[0].ID, &Thread{
		ID: "thr_taken", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	if !errors.Is(err, ErrThreadAlreadyExists) {
		t.Fatalf("err=%v want ErrThreadAlreadyExists", err)
	}
}

func TestUS374_BuildMessageTree_LinearChain(t *testing.T) {
	store := NewMemoryStore()
	msgs := seedLinearThread(t, store, "thr_tree")
	listed, err := store.ListMessages(context.Background(), "thr_tree")
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	roots := BuildMessageTree(listed)
	if len(roots) != 1 {
		t.Fatalf("expected one root; got %d", len(roots))
	}
	depth := 0
	cur := roots[0]
	for cur != nil {
		depth++
		if len(cur.Children) > 1 {
			t.Errorf("linear chain should have at most 1 child per node; got %d", len(cur.Children))
		}
		if len(cur.Children) == 0 {
			break
		}
		cur = cur.Children[0]
	}
	if depth != len(msgs) {
		t.Errorf("tree depth=%d want %d", depth, len(msgs))
	}
}

func TestUS374_BuildMessageTree_ForkProducesTwoChildren(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	srcMsgs := seedLinearThread(t, store, "thr_t1")
	// Fork at second message (assistant) and append a new message.
	pivot := srcMsgs[1]
	_, copied, err := store.ForkThread(ctx, "thr_t1", pivot.ID, &Thread{
		ID: "thr_t1_fork", Provider: ProviderMock, CreatedBy: "user:alice",
	})
	if err != nil {
		t.Fatalf("ForkThread: %v", err)
	}
	if err := store.AppendMessage(ctx, &Message{ThreadID: "thr_t1_fork", Role: RoleUser, Content: "alt path"}); err != nil {
		t.Fatalf("AppendMessage on fork: %v", err)
	}
	listed, err := store.ListMessages(ctx, "thr_t1_fork")
	if err != nil {
		t.Fatalf("ListMessages fork: %v", err)
	}
	roots := BuildMessageTree(listed)
	if len(roots) != 1 {
		t.Fatalf("fork should have one root after copy, got %d", len(roots))
	}
	pivotCopyID := copied[len(copied)-1].ID
	// Walk to the pivot copy node — it should have one child (the new
	// "alt path" message). Confirms tree structure preserves linkage.
	var foundPivot *MessageTreeNode
	var walk func(n *MessageTreeNode)
	walk = func(n *MessageTreeNode) {
		if n.Message.ID == pivotCopyID {
			foundPivot = n
			return
		}
		for _, c := range n.Children {
			walk(c)
			if foundPivot != nil {
				return
			}
		}
	}
	walk(roots[0])
	if foundPivot == nil {
		t.Fatalf("pivot copy not found in tree")
	}
	if len(foundPivot.Children) != 1 {
		t.Fatalf("pivot copy should have one child; got %d", len(foundPivot.Children))
	}
	if foundPivot.Children[0].Message.Content != "alt path" {
		t.Errorf("child content=%q want alt path", foundPivot.Children[0].Message.Content)
	}
}

func TestUS374_BuildMessageTree_OrphanParentBecomesRoot(t *testing.T) {
	// Synthesise a slice where one message references a parent id that
	// is not in the slice — BuildMessageTree should treat it as a root
	// rather than dropping the row.
	dangling := int64(99999)
	msgs := []*Message{
		{ID: 1, ThreadID: "x", Role: RoleUser, Content: "a"},
		{ID: 2, ThreadID: "x", Role: RoleAssistant, Content: "b", ParentMessageID: &dangling},
	}
	roots := BuildMessageTree(msgs)
	if len(roots) != 2 {
		t.Fatalf("expected 2 roots (one with absent parent), got %d", len(roots))
	}
}

// HTTP-level integration coverage for the two new endpoints.

func TestHandler_US374_Fork_HappyPath(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	srcMsgs := seedLinearThread(t, store, "thr_h_src")

	body, _ := json.Marshal(map[string]interface{}{
		"messageId":   srcMsgs[1].ID,
		"newThreadId": "thr_h_fork",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads/thr_h_src/fork", bytes.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp forkThreadResponse
	decodeJSON(t, w.Body.Bytes(), &resp)
	if resp.Thread == nil || resp.Thread.ID != "thr_h_fork" {
		t.Fatalf("unexpected thread: %+v", resp.Thread)
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("messages len=%d want 2", len(resp.Messages))
	}
	if resp.Messages[0].ParentMessageID != nil {
		t.Errorf("first copy should have nil parent")
	}
	if resp.Messages[1].ParentMessageID == nil || *resp.Messages[1].ParentMessageID != resp.Messages[0].ID {
		t.Errorf("second copy parent chain broken")
	}
}

func TestHandler_US374_Fork_RejectsCrossOwnerSource(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	if err := store.CreateThread(context.Background(), &Thread{ID: "thr_alice_only", Provider: ProviderMock, CreatedBy: "user:alice"}); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	body := strings.NewReader(`{"messageId":1}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads/thr_alice_only/fork", body)
	req = withAuthContext(req, "user:bob")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestHandler_US374_Fork_RejectsMissingMessageID(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	seedLinearThread(t, store, "thr_h_missing")
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads/thr_h_missing/fork", strings.NewReader(`{}`))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "MissingMessageID") {
		t.Errorf("expected MissingMessageID; body=%s", w.Body.String())
	}
}

func TestHandler_US374_Fork_PivotMustBelongToSource(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	seedLinearThread(t, store, "thr_other_src")
	otherMsgs := seedLinearThread(t, store, "thr_other_dst")

	body, _ := json.Marshal(map[string]interface{}{
		"messageId":   otherMsgs[0].ID,
		"newThreadId": "thr_other_fork",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads/thr_other_src/fork", bytes.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PivotThreadMismatch") {
		t.Errorf("expected PivotThreadMismatch; body=%s", w.Body.String())
	}
}

func TestHandler_US374_Tree_ReturnsForestShape(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	srcMsgs := seedLinearThread(t, store, "thr_h_tree")

	// Fork at second message and append on the new thread.
	if _, _, err := store.ForkThread(context.Background(), "thr_h_tree", srcMsgs[1].ID, &Thread{
		ID: "thr_h_tree_b", Provider: ProviderMock, CreatedBy: "user:alice",
	}); err != nil {
		t.Fatalf("ForkThread: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/threads/thr_h_tree/tree", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp threadTreeResponse
	decodeJSON(t, w.Body.Bytes(), &resp)
	if resp.ThreadID != "thr_h_tree" {
		t.Errorf("ThreadID=%q", resp.ThreadID)
	}
	if len(resp.Roots) != 1 {
		t.Fatalf("expected 1 root in source thread tree, got %d", len(resp.Roots))
	}
	// The original source still has 4 messages on its trunk; the fork
	// is a separate thread and does not appear here.
	depth := 0
	cur := resp.Roots[0]
	for cur != nil {
		depth++
		if len(cur.Children) == 0 {
			break
		}
		cur = cur.Children[0]
	}
	if depth != len(srcMsgs) {
		t.Errorf("expected depth=%d got %d", len(srcMsgs), depth)
	}
}

func TestHandler_US374_Tree_ParentMessageIDExposed(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	msgs := seedLinearThread(t, store, "thr_h_pid")
	req := httptest.NewRequest(http.MethodGet, "/api/v2/aip/threads/thr_h_pid/tree", nil)
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	// Look for the second message's id encoded as parentMessageId on
	// the third — confirms the field is preserved through JSON.
	expectedParent := strconv.FormatInt(msgs[1].ID, 10)
	if !bytes.Contains(w.Body.Bytes(), []byte(`"parentMessageId":`+expectedParent)) {
		t.Errorf("expected parentMessageId=%s in response; body=%s", expectedParent, w.Body.String())
	}
}

func TestHandler_US374_Fork_InheritsProviderAndOverridesTitle(t *testing.T) {
	h, store := newTestHandler()
	r := newRouter(h)
	if err := store.CreateThread(context.Background(), &Thread{ID: "thr_titled", Title: "Original", Provider: ProviderMock, Model: "gpt", CreatedBy: "user:alice", SystemPrompt: "be brief"}); err != nil {
		t.Fatalf("CreateThread: %v", err)
	}
	m := &Message{ThreadID: "thr_titled", Role: RoleUser, Content: "hi"}
	if err := store.AppendMessage(context.Background(), m); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	body, _ := json.Marshal(map[string]interface{}{
		"messageId": m.ID,
		"title":     "Branch A",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v2/aip/threads/thr_titled/fork", bytes.NewReader(body))
	req = withAuthContext(req, "user:alice")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp forkThreadResponse
	decodeJSON(t, w.Body.Bytes(), &resp)
	if resp.Thread.Title != "Branch A" {
		t.Errorf("title override = %q want Branch A", resp.Thread.Title)
	}
	if resp.Thread.Provider != ProviderMock {
		t.Errorf("provider should be inherited; got %q", resp.Thread.Provider)
	}
	if resp.Thread.Model != "gpt" {
		t.Errorf("model should be inherited; got %q", resp.Thread.Model)
	}
	if resp.Thread.SystemPrompt != "be brief" {
		t.Errorf("system prompt should be inherited; got %q", resp.Thread.SystemPrompt)
	}
}
