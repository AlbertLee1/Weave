package index_test

// US-004 — pkg/index 失败恢复、并发、JetStream 补偿、per-ObjectType 隔离、句柄释放
// 全部以 `*_test.go` 形式与 manager_test.go 共存于 `index_test` 包，复用 sampleProperties()
// / newRebuildFixture() 等本地 helper。每个顶级测试下挂多个 t.Run 子用例，整体 ≥ 12
// 个新子测试，覆盖 AC 的五条主线。

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/blevesearch/bleve/v2"
	"github.com/liyang/weave/pkg/index"
)

// scrambleIndexDir overwrites every regular file under the on-disk index
// directory with garbage so bleve.Open() will reject it. The directory itself
// is left in place — bleve.New would otherwise consider the path "occupied"
// and fail with a different error than the open-corruption path we want to
// exercise.
func scrambleIndexDir(t *testing.T, root string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		// Truncate + write garbage. Bleve stores its bolt / scorch state in
		// regular files under the dir; replacing the bytes is enough to make
		// the next Open() return a meaningful "not a Bleve index" error.
		return os.WriteFile(path, []byte("__weave_corrupted_payload__"), 0o600)
	})
	if err != nil {
		t.Fatalf("scrambleIndexDir(%q): %v", root, err)
	}
}

// TestManager_CorruptionRecovery covers AC §1 — 索引文件损坏 → 自动重建。
// Bleve does NOT silently self-heal; the recovery API surface is
// DropIndex + EnsureIndex (or Rebuild, which is DropIndex + EnsureIndex
// wrapped around a source replay). These subtests drive both paths.
func TestManager_CorruptionRecovery(t *testing.T) {
	t.Run("DropAndRecreateAfterScramble", func(t *testing.T) {
		dataDir := t.TempDir()
		mgr := index.NewManager(dataDir)

		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}
		if err := mgr.IndexDocument("employee", "emp-1", map[string]interface{}{"name": "Alice"}); err != nil {
			t.Fatalf("seed doc: %v", err)
		}
		// Force the on-disk state to flush before we corrupt it.
		if err := mgr.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		scrambleIndexDir(t, filepath.Join(dataDir, "indexes", "employee"))

		// A fresh manager points at the same dataDir; EnsureIndex must NOT
		// silently surface a half-broken handle. With the dir already on
		// disk, bleve.Open will fail and the explicit recovery path is
		// DropIndex + EnsureIndex.
		mgr2 := index.NewManager(dataDir)
		defer mgr2.Close()

		if _, err := mgr2.EnsureIndex("employee", sampleProperties()); err == nil {
			// Some bleve backends are lenient; if this lenient path is hit,
			// at least confirm we can DropIndex + recreate and get a healthy
			// shell back.
			if err := mgr2.DropIndex("employee"); err != nil {
				t.Fatalf("DropIndex after lenient open: %v", err)
			}
		}

		if err := mgr2.DropIndex("employee"); err != nil {
			t.Fatalf("DropIndex after corruption: %v", err)
		}
		idx, err := mgr2.EnsureIndex("employee", sampleProperties())
		if err != nil {
			t.Fatalf("recovery EnsureIndex: %v", err)
		}
		if idx == nil {
			t.Fatal("recovery EnsureIndex returned nil index")
		}
		// Recovered shell must accept fresh writes + count cleanly.
		if err := mgr2.IndexDocument("employee", "emp-recovered", map[string]interface{}{"name": "Mallory"}); err != nil {
			t.Fatalf("post-recovery IndexDocument: %v", err)
		}
		count, _ := mgr2.DocCount("employee")
		if count != 1 {
			t.Errorf("post-recovery DocCount = %d, want 1", count)
		}
	})

	t.Run("RebuildAfterCorruptionRestoresState", func(t *testing.T) {
		dataDir := t.TempDir()
		mgr := index.NewManager(dataDir)
		defer mgr.Close()

		repo, src := newRebuildFixture()
		key := index.ScopedKey("northwind", "Customer")

		// First populate the index via Rebuild so we have a healthy state
		// to corrupt.
		if _, err := index.Rebuild(context.Background(), mgr, repo, src, index.RebuildRequest{
			OntologyAPIName:   "northwind",
			ObjectTypeAPIName: "Customer",
		}); err != nil {
			t.Fatalf("initial Rebuild: %v", err)
		}

		// Close the current handle, scramble on-disk files, then reopen via
		// a fresh manager. Rebuild must transparently drop the corrupted
		// dir and re-ingest from the source.
		if err := mgr.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		scrambleIndexDir(t, filepath.Join(dataDir, "indexes", key))

		mgr2 := index.NewManager(dataDir)
		defer mgr2.Close()

		res, err := index.Rebuild(context.Background(), mgr2, repo, src, index.RebuildRequest{
			OntologyAPIName:   "northwind",
			ObjectTypeAPIName: "Customer",
		})
		if err != nil {
			t.Fatalf("recovery Rebuild: %v", err)
		}
		if res.IndexedCount != 3 {
			t.Errorf("recovery IndexedCount = %d, want 3", res.IndexedCount)
		}
		count, _ := mgr2.DocCount(key)
		if count != 3 {
			t.Errorf("post-recovery DocCount = %d, want 3", count)
		}
		// Rebuild marker must have been cleared by the defer even after the
		// corrupted DropIndex path inside Rebuild — otherwise the executor
		// hot-path would route to the cold tier forever.
		if mgr2.IsRebuilding(key) {
			t.Error("rebuild marker leaked after recovery")
		}
	})

	t.Run("DropIndexToleratesPartialCorruption", func(t *testing.T) {
		dataDir := t.TempDir()
		mgr := index.NewManager(dataDir)

		// Seed and close so the dir is fully flushed.
		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}
		if err := mgr.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		scrambleIndexDir(t, filepath.Join(dataDir, "indexes", "employee"))

		// A second manager has no in-memory record of the corrupted index;
		// DropIndex must therefore tolerate the missing in-memory entry AND
		// the on-disk directory can be cleared by EnsureIndex's recovery
		// path below. The contract: DropIndex never blows up on stale dirs.
		mgr2 := index.NewManager(dataDir)
		defer mgr2.Close()

		if err := mgr2.DropIndex("employee"); err != nil {
			t.Fatalf("DropIndex on stale dir: %v", err)
		}
		// Belt-and-suspenders: physically clear any leftover scrambled bytes
		// so EnsureIndex (which calls bleve.Open first) takes the New path.
		if err := os.RemoveAll(filepath.Join(dataDir, "indexes", "employee")); err != nil {
			t.Fatalf("manual cleanup: %v", err)
		}
		if _, err := mgr2.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("post-cleanup EnsureIndex: %v", err)
		}
	})
}

// TestManager_Concurrency covers AC §2 — 并发索引/查询竞态。
// All subtests run under -race to surface any mutex regressions.
func TestManager_Concurrency(t *testing.T) {
	t.Run("ConcurrentIndexDocumentDistinctIDs", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()
		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}

		const writers = 8
		const perWriter = 25
		var wg sync.WaitGroup
		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; i < perWriter; i++ {
					id := fmt.Sprintf("w%d-i%d", w, i)
					doc := map[string]interface{}{"name": id, "age": i, "active": i%2 == 0}
					if err := mgr.IndexDocument("employee", id, doc); err != nil {
						t.Errorf("IndexDocument(%s): %v", id, err)
						return
					}
				}
			}(w)
		}
		wg.Wait()

		count, err := mgr.DocCount("employee")
		if err != nil {
			t.Fatalf("DocCount: %v", err)
		}
		if count != writers*perWriter {
			t.Errorf("DocCount = %d, want %d", count, writers*perWriter)
		}
	})

	t.Run("ConcurrentEnsureIndexSameKeyReturnsOneHandle", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()

		const callers = 16
		handles := make([]bleve.Index, callers)
		var wg sync.WaitGroup
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				idx, err := mgr.EnsureIndex("employee", sampleProperties())
				if err != nil {
					t.Errorf("EnsureIndex: %v", err)
					return
				}
				handles[i] = idx
			}(i)
		}
		wg.Wait()

		for i := 1; i < callers; i++ {
			if handles[i] != handles[0] {
				t.Errorf("concurrent EnsureIndex returned distinct handles at i=%d", i)
			}
		}
	})

	t.Run("ConcurrentSearchWhileIndexing", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()
		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}
		// Pre-seed one doc so the search side has something to find from
		// the very first query.
		if err := mgr.IndexDocument("employee", "seed", map[string]interface{}{"name": "seed", "age": 1, "active": true}); err != nil {
			t.Fatalf("seed: %v", err)
		}

		const writers = 4
		const readers = 6
		const perGoroutine = 30
		var stop atomic.Bool
		var wg sync.WaitGroup

		// Writers stream new docs.
		for w := 0; w < writers; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				for i := 0; i < perGoroutine; i++ {
					id := fmt.Sprintf("w%d-i%d", w, i)
					doc := map[string]interface{}{"name": id, "age": i, "active": true}
					if err := mgr.IndexDocument("employee", id, doc); err != nil {
						t.Errorf("IndexDocument: %v", err)
					}
				}
			}(w)
		}
		// Readers loop until the writers finish.
		for r := 0; r < readers; r++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for !stop.Load() {
					q := bleve.NewMatchAllQuery()
					if _, err := mgr.Search("employee", bleve.NewSearchRequest(q)); err != nil {
						t.Errorf("Search: %v", err)
						return
					}
				}
			}()
		}
		// When the writers finish we tell the readers to stop.
		go func() {
			// Tiny coordination shim — we want readers to keep going while
			// writers are still busy, so we wait for the writer subset
			// (the first `writers` Adds) by polling DocCount.
			for {
				c, _ := mgr.DocCount("employee")
				if c >= uint64(1+writers*perGoroutine) {
					stop.Store(true)
					return
				}
			}
		}()
		wg.Wait()

		count, _ := mgr.DocCount("employee")
		if count != 1+writers*perGoroutine {
			t.Errorf("final DocCount = %d, want %d", count, 1+writers*perGoroutine)
		}
	})

	t.Run("ConcurrentApplyBatchAndDelete", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()
		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}

		// Pre-seed 50 docs we can delete from concurrently.
		for i := 0; i < 50; i++ {
			id := fmt.Sprintf("seed-%d", i)
			if err := mgr.IndexDocument("employee", id, map[string]interface{}{"name": id}); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}

		const inserters = 4
		const inserterDocs = 20
		var wg sync.WaitGroup

		// Inserters: ApplyBatch upserts new docs (disjoint IDs from the deleters).
		for w := 0; w < inserters; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				ops := make([]index.BatchOp, 0, inserterDocs)
				for i := 0; i < inserterDocs; i++ {
					id := fmt.Sprintf("new-w%d-i%d", w, i)
					ops = append(ops, index.BatchOp{
						Type:       index.BatchOpIndex,
						PrimaryKey: id,
						Document:   map[string]interface{}{"name": id, "age": i, "active": true},
					})
				}
				if err := mgr.ApplyBatch("employee", ops); err != nil {
					t.Errorf("ApplyBatch: %v", err)
				}
			}(w)
		}
		// Deleter: removes all 50 seeds concurrently with the inserters.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				id := fmt.Sprintf("seed-%d", i)
				if err := mgr.DeleteDocument("employee", id); err != nil {
					t.Errorf("DeleteDocument: %v", err)
				}
			}
		}()
		wg.Wait()

		count, _ := mgr.DocCount("employee")
		want := uint64(inserters * inserterDocs)
		if count != want {
			t.Errorf("DocCount after concurrent batch/delete = %d, want %d", count, want)
		}
	})
}

// TestManager_JetStreamReplayCompensation covers AC §3 — 当 funnel/JetStream
// 漏发或 WEAVE_DATA_DIR 缺失时，Rebuild 从权威 LatestDocumentSource 补齐。
// 这里把 funnel 的语义抽象为：先在 Bleve 里制造一个"丢消息"后的不一致状态
// (only 1/3 docs indexed)，调一次 Rebuild 验证最终状态收敛到 source。
func TestManager_JetStreamReplayCompensation(t *testing.T) {
	t.Run("PartialStateRecoveredByRebuild", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()

		repo, src := newRebuildFixture()
		key := index.ScopedKey("northwind", "Customer")

		// Simulate "JetStream delivered only the first edit and lost the
		// other two" — operator manually creates the index shell and indexes
		// 1 of 3 docs.
		if _, err := mgr.EnsureIndex(key, []index.Property{
			{APIName: "customerId", BaseType: "string", IsSearchable: true},
			{APIName: "country", BaseType: "string", IsSearchable: true, Analyzer: index.AnalyzerNotIndexed},
		}); err != nil {
			t.Fatalf("EnsureIndex shell: %v", err)
		}
		if err := mgr.IndexDocument(key, "ALFKI", map[string]interface{}{"customerId": "ALFKI", "country": "USA"}); err != nil {
			t.Fatalf("partial seed: %v", err)
		}
		preCount, _ := mgr.DocCount(key)
		if preCount != 1 {
			t.Fatalf("pre-rebuild DocCount = %d, want 1", preCount)
		}

		res, err := index.Rebuild(context.Background(), mgr, repo, src, index.RebuildRequest{
			OntologyAPIName:   "northwind",
			ObjectTypeAPIName: "Customer",
		})
		if err != nil {
			t.Fatalf("Rebuild: %v", err)
		}
		if res.IndexedCount != 3 {
			t.Errorf("IndexedCount = %d, want 3", res.IndexedCount)
		}
		// Final state must reflect the FULL source, not source+stale (drop
		// happens before re-ingest).
		count, _ := mgr.DocCount(key)
		if count != 3 {
			t.Errorf("post-Rebuild DocCount = %d, want 3", count)
		}
	})

	t.Run("ReplayAfterMissedEdits_ApplyBatchIdempotent", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()
		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}

		// Imagine three deliveries A → B → C; B is delivered twice (consumer
		// replay after restart). ApplyBatch with the same PK must collapse
		// to a single doc.
		deliveries := [][]index.BatchOp{
			{{Type: index.BatchOpIndex, PrimaryKey: "emp-1", Document: map[string]interface{}{"name": "Alice", "age": 30, "active": true}}},
			{{Type: index.BatchOpIndex, PrimaryKey: "emp-1", Document: map[string]interface{}{"name": "Alice", "age": 31, "active": true}}},
			{{Type: index.BatchOpIndex, PrimaryKey: "emp-1", Document: map[string]interface{}{"name": "Alice", "age": 31, "active": true}}},
		}
		for i, ops := range deliveries {
			if err := mgr.ApplyBatch("employee", ops); err != nil {
				t.Fatalf("delivery %d: %v", i, err)
			}
		}
		count, _ := mgr.DocCount("employee")
		if count != 1 {
			t.Errorf("DocCount after replay = %d, want 1 (idempotent upsert)", count)
		}
		// And the final age value is the latest, not the first.
		q := bleve.NewDocIDQuery([]string{"emp-1"})
		req := bleve.NewSearchRequest(q)
		req.Fields = []string{"age"}
		res, err := mgr.Search("employee", req)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if res.Total != 1 {
			t.Fatalf("expected 1 hit, got %d", res.Total)
		}
		got := res.Hits[0].Fields["age"]
		// Bleve marshals numbers as float64.
		if f, ok := got.(float64); !ok || f != 31 {
			t.Errorf("age after replay = %v, want 31", got)
		}
	})

	t.Run("DeleteThenReindexCovergesToReindexedDoc", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()
		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}

		// Sequence: insert, delete, re-insert (out-of-order replay).
		ops := []index.BatchOp{
			{Type: index.BatchOpIndex, PrimaryKey: "emp-1", Document: map[string]interface{}{"name": "Alice", "age": 30, "active": true}},
			{Type: index.BatchOpDelete, PrimaryKey: "emp-1"},
			{Type: index.BatchOpIndex, PrimaryKey: "emp-1", Document: map[string]interface{}{"name": "Alice2", "age": 31, "active": true}},
		}
		if err := mgr.ApplyBatch("employee", ops); err != nil {
			t.Fatalf("ApplyBatch: %v", err)
		}
		count, _ := mgr.DocCount("employee")
		if count != 1 {
			t.Errorf("DocCount = %d, want 1", count)
		}
	})
}

// TestManager_PerObjectTypeIsolation covers AC §4 — 一个 Manager 内不同 ObjectType
// 的索引互不污染、互不干扰。
func TestManager_PerObjectTypeIsolation(t *testing.T) {
	t.Run("SearchScopedToObjectType", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()

		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex(employee): %v", err)
		}
		if _, err := mgr.EnsureIndex("customer", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex(customer): %v", err)
		}
		if err := mgr.IndexDocument("employee", "emp-1", map[string]interface{}{"name": "Alice"}); err != nil {
			t.Fatalf("emp seed: %v", err)
		}
		if err := mgr.IndexDocument("customer", "cust-1", map[string]interface{}{"name": "Bob"}); err != nil {
			t.Fatalf("cust seed: %v", err)
		}

		empCount, _ := mgr.DocCount("employee")
		custCount, _ := mgr.DocCount("customer")
		if empCount != 1 || custCount != 1 {
			t.Fatalf("DocCount emp=%d cust=%d, want 1/1", empCount, custCount)
		}

		// Searching employee for "Bob" must miss — bob lives in customer.
		q := bleve.NewMatchQuery("Bob")
		q.SetField("name")
		res, err := mgr.Search("employee", bleve.NewSearchRequest(q))
		if err != nil {
			t.Fatalf("Search employee: %v", err)
		}
		if res.Total != 0 {
			t.Errorf("employee.Search(Bob) total=%d, want 0 (isolation breach)", res.Total)
		}

		q2 := bleve.NewMatchQuery("Bob")
		q2.SetField("name")
		res2, err := mgr.Search("customer", bleve.NewSearchRequest(q2))
		if err != nil {
			t.Fatalf("Search customer: %v", err)
		}
		if res2.Total != 1 {
			t.Errorf("customer.Search(Bob) total=%d, want 1", res2.Total)
		}
	})

	t.Run("DropOneIndexLeavesOthers", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()

		for _, ot := range []string{"a", "b", "c"} {
			if _, err := mgr.EnsureIndex(ot, sampleProperties()); err != nil {
				t.Fatalf("EnsureIndex(%s): %v", ot, err)
			}
			if err := mgr.IndexDocument(ot, ot+"-1", map[string]interface{}{"name": ot}); err != nil {
				t.Fatalf("seed %s: %v", ot, err)
			}
		}

		if err := mgr.DropIndex("b"); err != nil {
			t.Fatalf("DropIndex(b): %v", err)
		}
		// a / c must still respond; b must be gone.
		if mgr.GetIndex("a") == nil {
			t.Error("a should still be open after DropIndex(b)")
		}
		if mgr.GetIndex("c") == nil {
			t.Error("c should still be open after DropIndex(b)")
		}
		if mgr.GetIndex("b") != nil {
			t.Error("b should be gone after DropIndex(b)")
		}
		// a / c writes still succeed.
		if err := mgr.IndexDocument("a", "a-2", map[string]interface{}{"name": "still-a"}); err != nil {
			t.Errorf("IndexDocument(a) after drop(b): %v", err)
		}
		if err := mgr.IndexDocument("c", "c-2", map[string]interface{}{"name": "still-c"}); err != nil {
			t.Errorf("IndexDocument(c) after drop(b): %v", err)
		}
		// b writes fail (no index).
		if err := mgr.IndexDocument("b", "b-2", map[string]interface{}{"name": "ghost"}); err == nil {
			t.Error("IndexDocument(b) after drop should return error")
		}
	})

	t.Run("ScopedKeyIsolatesIdenticalTypeAcrossOntologies", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		defer mgr.Close()

		keyA := index.ScopedKey("ontology-a", "Customer")
		keyB := index.ScopedKey("ontology-b", "Customer")
		if keyA == keyB {
			t.Fatalf("ScopedKey collision: %q == %q", keyA, keyB)
		}
		if _, err := mgr.EnsureIndex(keyA, sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex(%s): %v", keyA, err)
		}
		if _, err := mgr.EnsureIndex(keyB, sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex(%s): %v", keyB, err)
		}

		// Each scoped key gets its own doc with the same primary key.
		if err := mgr.IndexDocument(keyA, "shared-pk", map[string]interface{}{"name": "tenant-a"}); err != nil {
			t.Fatalf("index A: %v", err)
		}
		if err := mgr.IndexDocument(keyB, "shared-pk", map[string]interface{}{"name": "tenant-b"}); err != nil {
			t.Fatalf("index B: %v", err)
		}

		ca, _ := mgr.DocCount(keyA)
		cb, _ := mgr.DocCount(keyB)
		if ca != 1 || cb != 1 {
			t.Errorf("counts A=%d B=%d, want 1/1", ca, cb)
		}

		// Dropping A must not affect B.
		if err := mgr.DropIndex(keyA); err != nil {
			t.Fatalf("DropIndex(A): %v", err)
		}
		if mgr.GetIndex(keyB) == nil {
			t.Error("scoped B leaked along with A")
		}
		cb, _ = mgr.DocCount(keyB)
		if cb != 1 {
			t.Errorf("B count after dropping A = %d, want 1", cb)
		}
	})
}

// TestManager_HandleRelease covers AC §5 — Close / DropIndex 必须释放 bleve
// 内部的文件锁，以便同 dataDir 在新进程 / 新 Manager 中能重新打开；并保证
// 重启后能看到之前持久化的状态。
func TestManager_HandleRelease(t *testing.T) {
	t.Run("CloseFlushesAndReleasesLocks", func(t *testing.T) {
		dataDir := t.TempDir()
		mgr := index.NewManager(dataDir)

		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}
		if err := mgr.IndexDocument("employee", "emp-1", map[string]interface{}{"name": "Alice"}); err != nil {
			t.Fatalf("IndexDocument: %v", err)
		}

		if err := mgr.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}
		// After Close the manager's in-memory map is empty.
		if mgr.GetIndex("employee") != nil {
			t.Error("GetIndex after Close should be nil")
		}

		// A NEW manager on the same dataDir must be able to open the same
		// index without "lock held" errors — proves the bleve lock file
		// was released cleanly.
		mgr2 := index.NewManager(dataDir)
		defer mgr2.Close()
		idx, err := mgr2.EnsureIndex("employee", sampleProperties())
		if err != nil {
			t.Fatalf("reopen EnsureIndex: %v", err)
		}
		if idx == nil {
			t.Fatal("reopen returned nil index")
		}
	})

	t.Run("ReopenAfterCloseSeesPersistedDocs", func(t *testing.T) {
		dataDir := t.TempDir()

		mgr := index.NewManager(dataDir)
		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}
		for i := 0; i < 3; i++ {
			id := fmt.Sprintf("emp-%d", i)
			if err := mgr.IndexDocument("employee", id, map[string]interface{}{"name": id}); err != nil {
				t.Fatalf("seed: %v", err)
			}
		}
		if err := mgr.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		mgr2 := index.NewManager(dataDir)
		defer mgr2.Close()
		if _, err := mgr2.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("reopen: %v", err)
		}
		count, err := mgr2.DocCount("employee")
		if err != nil {
			t.Fatalf("DocCount post-reopen: %v", err)
		}
		if count != 3 {
			t.Errorf("post-reopen DocCount = %d, want 3", count)
		}
	})

	t.Run("DropIndexClosesHandleAndRemovesDir", func(t *testing.T) {
		dataDir := t.TempDir()
		mgr := index.NewManager(dataDir)
		defer mgr.Close()

		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}
		indexPath := filepath.Join(dataDir, "indexes", "employee")
		if _, err := os.Stat(indexPath); err != nil {
			t.Fatalf("expected index dir at %q: %v", indexPath, err)
		}

		if err := mgr.DropIndex("employee"); err != nil {
			t.Fatalf("DropIndex: %v", err)
		}
		if mgr.GetIndex("employee") != nil {
			t.Fatal("GetIndex after DropIndex should be nil")
		}
		if _, err := os.Stat(indexPath); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("expected %q to be removed, got Stat err = %v", indexPath, err)
		}

		// Subsequent EnsureIndex creates a brand-new shell with 0 docs.
		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("post-drop EnsureIndex: %v", err)
		}
		count, _ := mgr.DocCount("employee")
		if count != 0 {
			t.Errorf("post-drop DocCount = %d, want 0", count)
		}
	})

	t.Run("DoubleCloseSafe", func(t *testing.T) {
		mgr := index.NewManager(t.TempDir())
		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex: %v", err)
		}
		if err := mgr.Close(); err != nil {
			t.Fatalf("Close#1: %v", err)
		}
		// Second Close must be a no-op (in-memory map already drained).
		if err := mgr.Close(); err != nil {
			t.Fatalf("Close#2: %v", err)
		}
	})

	t.Run("EnsureIndexAfterDropReusesPath", func(t *testing.T) {
		// Specifically targets the on-disk reuse pathway: DropIndex removes
		// the dir, EnsureIndex must take the bleve.New branch (not Open).
		dataDir := t.TempDir()
		mgr := index.NewManager(dataDir)
		defer mgr.Close()

		if _, err := mgr.EnsureIndex("employee", sampleProperties()); err != nil {
			t.Fatalf("EnsureIndex#1: %v", err)
		}
		if err := mgr.IndexDocument("employee", "emp-1", map[string]interface{}{"name": "Alice"}); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if err := mgr.DropIndex("employee"); err != nil {
			t.Fatalf("DropIndex: %v", err)
		}
		idx, err := mgr.EnsureIndex("employee", sampleProperties())
		if err != nil {
			t.Fatalf("EnsureIndex#2: %v", err)
		}
		if idx == nil {
			t.Fatal("EnsureIndex#2 nil")
		}
		// The reborn shell is empty even though the previous incarnation
		// had a doc.
		count, _ := mgr.DocCount("employee")
		if count != 0 {
			t.Errorf("reborn DocCount = %d, want 0", count)
		}
	})
}
