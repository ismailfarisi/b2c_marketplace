package interests

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestActiveViewersQueryRuns(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)

	if _, err := NewService(pool).activeViewers(context.Background(), time.Now().Add(-24*time.Hour), 10); err != nil {
		t.Fatalf("active viewers: %v", err)
	}
}
