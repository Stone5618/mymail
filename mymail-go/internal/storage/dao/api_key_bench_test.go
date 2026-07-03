// api_key_bench_test.go 测试 API Key prefix 索引查找性能。
//
// 验收标准 9.6 第 1 项：API Key 用 prefix 索引查找（性能测试 < 1ms）。
//
// 包含：
//   - BenchmarkAPIKeyDAO_FindByPrefix：基准测试（Hit / Miss）
//   - TestAPIKeyDAO_FindByPrefix_Performance：显式断言 < 1ms
package dao

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/mymail/mymail-go/internal/storage/db"
)

// setupBenchDB 创建基准测试用 DB（不依赖 newTestDB，因后者接受 *testing.T）。
func setupBenchDB(tb testing.TB) *db.DB {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "bench.db")
	database, err := db.Open(path)
	if err != nil {
		tb.Fatalf("打开测试 DB 失败: %v", err)
	}
	if err := database.Migrate(); err != nil {
		tb.Fatalf("迁移失败: %v", err)
	}
	tb.Cleanup(func() { database.Close() })
	return database
}

// setupBenchAPIKeys 创建 100 个 API Key，返回第 50 个的 prefix（用于命中测试）。
func setupBenchAPIKeys(tb testing.TB, database *db.DB) (apiKeyDAO *APIKeyDAO, targetPrefix string) {
	tb.Helper()
	userDAO := NewUserDAO(database)
	uid, err := userDAO.Create(context.Background(), CreateUserInput{
		Username: "benchuser", Email: "bench@example.com", PasswordHash: "h",
	})
	if err != nil {
		tb.Fatalf("创建测试用户失败: %v", err)
	}

	apiKeyDAO = NewAPIKeyDAO(database)
	ctx := context.Background()
	for i := 0; i < 100; i++ {
		prefix := fmt.Sprintf("mk_bench%03dxx", i)
		if i == 50 {
			targetPrefix = prefix
		}
		_, err := apiKeyDAO.Create(ctx, CreateAPIKeyInput{
			UserID:    uid,
			Name:      fmt.Sprintf("bench-key-%d", i),
			KeyHash:   "$2a$12$somehashvalue",
			KeyPrefix: prefix,
			Scopes:    []string{"send"},
			RateLimit: 60,
		})
		if err != nil {
			tb.Fatalf("创建 API Key %d 失败: %v", i, err)
		}
	}
	return apiKeyDAO, targetPrefix
}

// TestAPIKeyDAO_FindByPrefix_Performance 验收标准 9.6 第 1 项：
// API Key prefix 索引查找 < 1ms。
func TestAPIKeyDAO_FindByPrefix_Performance(t *testing.T) {
	database := setupBenchDB(t)
	apiKeyDAO, targetPrefix := setupBenchAPIKeys(t, database)
	ctx := context.Background()

	// 预热（首次查询可能触发 SQLite page cache 加载）
	_, _ = apiKeyDAO.FindByPrefix(ctx, targetPrefix)

	// 正式测量
	start := time.Now()
	keys, err := apiKeyDAO.FindByPrefix(ctx, targetPrefix)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("FindByPrefix 失败: %v", err)
	}
	if len(keys) == 0 {
		t.Fatal("FindByPrefix 应返回非空结果")
	}

	if elapsed >= time.Millisecond {
		t.Errorf("验收标准：FindByPrefix 应 < 1ms，实际 %v", elapsed)
	}
	t.Logf("FindByPrefix 耗时: %v（100 条数据中命中 %d 条）", elapsed, len(keys))
}

// BenchmarkAPIKeyDAO_FindByPrefix 基准测试 prefix 索引查找。
func BenchmarkAPIKeyDAO_FindByPrefix(b *testing.B) {
	database := setupBenchDB(b)
	apiKeyDAO, targetPrefix := setupBenchAPIKeys(b, database)
	ctx := context.Background()

	b.ResetTimer()

	b.Run("Hit", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, err := apiKeyDAO.FindByPrefix(ctx, targetPrefix)
			if err != nil {
				b.Fatalf("FindByPrefix 失败: %v", err)
			}
		}
	})

	b.Run("Miss", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = apiKeyDAO.FindByPrefix(ctx, "mk_nonexist00")
		}
	})
}
