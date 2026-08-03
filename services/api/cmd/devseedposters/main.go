// Generates a poster for every seeded reel that has a poster_key pointing at
// nothing, uploads it, and records the blurhash the real transcoder would have
// produced.
//
// Dev fixture only. The seed data invented poster keys under `posters/` because
// the stub transcoder invents them; nothing ever wrote the objects, so every
// thumbnail in the app 404'd. This makes the dev stack show what production
// would show, and incidentally runs media.EncodeBlurHash over real JPEGs.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/marketplace/api/internal/modules/media"
	"github.com/marketplace/api/internal/platform/objectstore"
)

// A colour per subject type, so the feed is visually legible while scrolling.
var tint = map[string]string{
	"vehicle_listing":  "0x2E4057",
	"property_listing": "0x3A5A40",
	"flash_offer":      "0x7A3B2E",
	"meal_plan":        "0x5C4033",
}

func main() {
	ctx := context.Background()

	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer pool.Close()

	store, err := objectstore.New(ctx, objectstore.Config{
		Endpoint:       os.Getenv("S3_ENDPOINT"),
		PublicEndpoint: os.Getenv("S3_PUBLIC_ENDPOINT"),
		Region:         os.Getenv("S3_REGION"),
		Bucket:         os.Getenv("S3_BUCKET"),
		AccessKeyID:    os.Getenv("S3_ACCESS_KEY_ID"),
		SecretKey:      os.Getenv("S3_SECRET_ACCESS_KEY"),
		ForcePathStyle: true,
	})
	if err != nil {
		log.Fatalf("store: %v", err)
	}

	rows, err := pool.Query(ctx, `
		SELECT id, COALESCE(poster_key, ''), COALESCE(subject_type, '')
		  FROM reels
		 WHERE poster_key LIKE 'posters/%'`)
	if err != nil {
		log.Fatalf("query: %v", err)
	}
	type reel struct{ id, key, subject string }
	var reels []reel
	for rows.Next() {
		var r reel
		if err := rows.Scan(&r.id, &r.key, &r.subject); err != nil {
			log.Fatal(err)
		}
		reels = append(reels, r)
	}
	rows.Close()

	work, err := os.MkdirTemp("", "posters")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(work)

	for _, r := range reels {
		colour := tint[r.subject]
		if colour == "" {
			colour = "0x333333"
		}
		out := filepath.Join(work, "p.jpg")

		// A vertical gradient rather than a flat fill, so the blurhash has
		// something real to encode and the result is visibly a picture rather
		// than a swatch.
		cmd := exec.Command("ffmpeg", "-y", "-loglevel", "error",
			"-f", "lavfi", "-i", fmt.Sprintf("color=c=%s:s=720x1280", colour),
			"-vf", "geq=r='r(X,Y)*(0.5+0.7*Y/H)':g='g(X,Y)*(0.5+0.7*Y/H)':b='b(X,Y)*(0.5+0.9*Y/H)'",
			"-frames:v", "1", "-q:v", "3", out)
		if o, err := cmd.CombinedOutput(); err != nil {
			log.Fatalf("ffmpeg %s: %v: %s", r.id, err, strings.TrimSpace(string(o)))
		}

		hash, err := media.EncodeBlurHash(out, media.PosterComponentsX, media.PosterComponentsY)
		if err != nil {
			log.Fatalf("blurhash %s: %v", r.id, err)
		}

		f, err := os.Open(out)
		if err != nil {
			log.Fatal(err)
		}
		err = store.Put(ctx, r.key, "image/jpeg", f)
		f.Close()
		if err != nil {
			log.Fatalf("put %s: %v", r.key, err)
		}

		if _, err := pool.Exec(ctx,
			`UPDATE reels SET lqip = $2 WHERE id = $1`, r.id, hash); err != nil {
			log.Fatalf("update %s: %v", r.id, err)
		}
		fmt.Printf("%s  %-28s  %s  %s\n", r.id[:8], r.key, hash, r.subject)
	}
	fmt.Printf("\n%d posters written\n", len(reels))
}
