package support

import (
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/notify"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newUser(t *testing.T, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(context.Background(), `INSERT INTO users (phone) VALUES ($1) RETURNING id`,
		fmt.Sprintf("+97157%07d", rand.IntN(10_000_000))).Scan(&id); err != nil {
		t.Fatalf("user: %v", err)
	}
	return id
}

type captured struct{ msgs []notify.Message }

func (c *captured) Enqueue(_ context.Context, _ pgx.Tx, m notify.Message) (uuid.UUID, error) {
	c.msgs = append(c.msgs, m)
	return uuid.New(), nil
}

// "Show older" reaches every ticket: pages don't overlap and the last page has
// no cursor.
func TestMinePagesThroughEveryTicket(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	svc := NewService(pool, nil)
	user := newUser(t, pool)
	for i := range 5 {
		if _, err := svc.Open(ctx, user, fmt.Sprintf("Ticket %d", i), "body", nil); err != nil {
			t.Fatal(err)
		}
	}
	defer func(n int) { minePageSize = n }(minePageSize)
	minePageSize = 2

	seen := map[uuid.UUID]bool{}
	page, next, err := svc.Mine(ctx, user, nil)
	for pages := 1; ; pages++ {
		if err != nil {
			t.Fatal(err)
		}
		for _, tk := range page {
			if seen[tk.ID] {
				t.Fatalf("ticket %s on two pages", tk.ID)
			}
			seen[tk.ID] = true
		}
		if next == nil {
			if pages != 3 {
				t.Fatalf("got %d pages, want 3", pages)
			}
			break
		}
		page, next, err = svc.Mine(ctx, user, next)
	}
	if len(seen) != 5 {
		t.Fatalf("saw %d tickets, want 5", len(seen))
	}
}

// A staff reply marks the ticket answered and queues one support.reply push
// whose link is a locale-prefixed web path MealsLinks maps to Help (notify
// rewrites the prefix to the recipient's language); a customer reply reopens.
func TestStaffReplyNotifiesAndCustomerReplyReopens(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	n := &captured{}
	svc := NewService(pool, n)
	user, staff := newUser(t, pool), newUser(t, pool)
	th, err := svc.Open(ctx, user, "Lunch arrived cold", "It was cold.", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.Reply(ctx, th.ID, staff, nil, "Sorry — refunded.")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "answered" || len(n.msgs) != 1 {
		t.Fatalf("status %s, %d pushes", got.Status, len(n.msgs))
	}
	if link := n.msgs[0].Params["link"]; link != "/en/support/"+th.ID.String() {
		t.Fatalf("link %q", link)
	}
	if l := notify.LocalizeLink(n.msgs[0].Params["link"], "ar"); l != "/ar/support/"+th.ID.String() {
		t.Fatalf("arabic link %q", l)
	}
	got, err = svc.Reply(ctx, th.ID, user, &user, "Thanks")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "open" || len(n.msgs) != 1 {
		t.Fatalf("customer reply: status %s, %d pushes", got.Status, len(n.msgs))
	}
	if _, err := svc.Thread(ctx, th.ID, &staff); err == nil {
		t.Fatal("someone else's ticket was readable")
	}
}
