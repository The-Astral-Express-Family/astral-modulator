// retention_test.go 是版本保留窗口清扫的内务测试（sqlite 直调
// sweepVersionsOnce）：每文档上限只留最近 N 版、TTL 剪除窗口外版本、
// 双 0 不动任何行。
package document

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/model"
	"github.com/The-Astral-Express-Family/astral-modulator/server/internal/testsupport"
)

func TestSweepVersionsOnce(t *testing.T) {
	db := testsupport.NewTestDB(t)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()

	// 两个文档各 5 版；每文档 rev1-2 的 created_at 推到 48h 前（TTL 候选），
	// rev3-5 留在当前时刻。doc_b 的 revision 从 11 起验证上限按 revision 而非
	// 行序裁剪。
	seed := func(docID string, baseRev int64) {
		t.Helper()
		for i := int64(1); i <= 5; i++ {
			age := now
			if i <= 2 {
				age = now.Add(-48 * time.Hour)
			}
			if err := db.Create(&model.DocumentVersion{
				ID: fmt.Sprintf("dvh_test_%s_%02d", docID, i), WorkspaceID: "ws_test",
				DocumentID: docID, Path: "docs/" + docID + ".md",
				Revision: baseRev + i, ContentHash: fmt.Sprintf("sha256:%064d", i), Content: "c",
				Kind: versionKindPush, ActorID: "agt_test", CreatedAt: age,
			}).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	seed("doc_a", 0)
	seed("doc_b", 10)

	count := func(q string, args ...any) int64 {
		var n int64
		if err := db.Model(&model.DocumentVersion{}).Where(q, args...).Count(&n).Error; err != nil {
			t.Fatal(err)
		}
		return n
	}

	// 维度全关：一行不删。
	sweepVersionsOnce(t.Context(), db, log, 0, 0)
	if n := count("1=1"); n != 10 {
		t.Fatalf("disabled sweep removed rows: %d", n)
	}

	// TTL = 24h：剪掉 48h 前的 4 行（每文档 2 行）。
	sweepVersionsOnce(t.Context(), db, log, 0, 24*time.Hour)
	if n := count("1=1"); n != 6 {
		t.Fatalf("ttl sweep: %d rows remain, want 6", n)
	}

	// 每文档上限 = 2：每文档仅留 rev 最高的 2 版（doc_a: 5,4；doc_b: 15,14）。
	sweepVersionsOnce(t.Context(), db, log, 2, 0)
	if n := count("document_id = ?", "doc_a"); n != 2 {
		t.Fatalf("doc_a cap: %d rows remain, want 2", n)
	}
	if n := count("document_id = ?", "doc_b"); n != 2 {
		t.Fatalf("doc_b cap: %d rows remain, want 2", n)
	}
	var kept []int64
	if err := db.Model(&model.DocumentVersion{}).Where("document_id = ?", "doc_b").
		Order("revision DESC").Pluck("revision", &kept).Error; err != nil {
		t.Fatal(err)
	}
	if len(kept) != 2 || kept[0] != 15 || kept[1] != 14 {
		t.Fatalf("doc_b kept revisions = %v, want [15 14]", kept)
	}
}
