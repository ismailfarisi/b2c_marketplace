package moderation

import (
	"context"
	"errors"
	"testing"
)

type fakeVision struct {
	labels []Label
	err    error
}

func (fakeVision) Name() string { return "fake" }
func (f fakeVision) ModerateImage(context.Context, string) ([]Label, error) {
	return f.labels, f.err
}
func (f fakeVision) ModerateVideo(context.Context, string) ([]Label, error) {
	return f.labels, f.err
}

type fakeText struct {
	result TextResult
	err    error
}

func (fakeText) Name() string { return "fake" }
func (f fakeText) ModerateText(context.Context, string) (TextResult, error) {
	return f.result, f.err
}

func TestPipelineFailsTowardHumanWhenALayerErrors(t *testing.T) {
	// The failure that matters: an AWS incident must not put unmoderated
	// content in the feed. Failing toward approve would do exactly that, and
	// nothing in the logs would say so.
	p := Pipeline{
		Vision: fakeVision{err: errors.New("rekognition unavailable")},
		Text:   fakeText{result: TextResult{Verdict: VerdictApprove}},
	}
	got := p.Run(context.Background(), Input{PosterURL: "s3://poster", VideoURL: "s3://video"})
	if got.Verdict != VerdictReview {
		t.Fatalf("verdict = %s, want review when a moderator is down", got.Verdict)
	}
}

func TestPipelineSkipsExpensiveLayerAfterRejection(t *testing.T) {
	// §3.1's cost argument: video moderation is priced per minute analysed.
	// Running it on content the poster frame already condemned is money spent
	// on a conclusion you already have.
	p := Pipeline{
		Vision: fakeVision{labels: []Label{{Category: "Explicit Nudity", Confidence: 95}}},
		Text:   fakeText{result: TextResult{Verdict: VerdictApprove}},
	}
	got := p.Run(context.Background(), Input{PosterURL: "s3://poster", VideoURL: "s3://video"})

	if got.Verdict != VerdictReject {
		t.Fatalf("verdict = %s, want reject", got.Verdict)
	}
	for _, l := range got.Layers {
		if l.Layer == LayerVideo {
			t.Fatal("video layer ran after the poster frame already rejected")
		}
	}
}

func TestTextCategoriesSurviveWithoutBeingRescored(t *testing.T) {
	// Text categories carry no confidence. Forcing them through the numeric
	// table would mean inventing a number, and no number works: 50 is
	// simultaneously tobacco's review line and hate symbols' reject line.
	p := Pipeline{
		Vision: fakeVision{},
		Text: fakeText{result: TextResult{
			Verdict:    VerdictReview,
			Categories: []string{"hate_speech"},
			Language:   "ar-Latn",
		}},
	}
	got := p.Run(context.Background(), Input{Caption: "something borderline"})

	if got.Verdict != VerdictReview {
		t.Fatalf("verdict = %s, want review — a text review must not be promoted to reject", got.Verdict)
	}
	if len(got.Decision.Categories) == 0 {
		t.Fatal("text categories were dropped from the decision")
	}
	if got.Language != "ar-Latn" {
		t.Errorf("detected language = %q, want ar-Latn", got.Language)
	}
}

func TestCleanContentApproves(t *testing.T) {
	p := Pipeline{
		Vision: fakeVision{labels: []Label{{Category: "Alcohol", Confidence: 5}}},
		Text:   fakeText{result: TextResult{Verdict: VerdictApprove, Language: "ar"}},
	}
	got := p.Run(context.Background(), Input{
		PosterURL: "s3://poster", VideoURL: "s3://video", Caption: "غسيل وكي خلال ٢٤ ساعة",
	})
	if got.Verdict != VerdictApprove {
		t.Fatalf("clean content produced %s: %+v", got.Verdict, got.Decision.Categories)
	}
}

func TestEmptyPosterIsNotAFailure(t *testing.T) {
	// On the fast path the poster frame does not exist yet — it comes out of
	// transcode. Treating its absence as a moderation failure would send every
	// reel to review.
	p := Pipeline{
		Vision: fakeVision{},
		Text:   fakeText{result: TextResult{Verdict: VerdictApprove}},
	}
	got := p.Run(context.Background(), Input{Caption: "hello"})
	if got.Verdict != VerdictApprove {
		t.Fatalf("missing poster produced %s", got.Verdict)
	}
}
