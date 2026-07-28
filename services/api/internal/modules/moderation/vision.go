package moderation

import (
	"context"
	"fmt"
)

// VisionModerator covers the poster-frame and video-frame layers.
//
// Both are Rekognition in production, and they are one interface rather than
// two because the only difference between them is what you point at and how
// long it takes — the label vocabulary and the threshold table are identical.
type VisionModerator interface {
	Name() string
	// ModerateImage is the cheap, early-exiting layer: one poster frame, cents,
	// a few hundred milliseconds. It runs before transcode finishes and catches
	// the blatant cases before any transcode money is spent.
	ModerateImage(ctx context.Context, imageURL string) ([]Label, error)
	// ModerateVideo is the expensive one. It runs only after the poster and
	// caption layers have passed, and only against transcode output.
	ModerateVideo(ctx context.Context, videoURL string) ([]Label, error)
}

// StubVisionModerator returns no labels, which Decide reads as approve.
//
// The honest characterisation: with this wired, the vision layers are not
// moderating anything. It exists so the pipeline and the state machine can be
// exercised without AWS credentials, and cmd/worker refuses it in production
// rather than letting a misconfiguration quietly disable half the gate.
type StubVisionModerator struct{}

func (StubVisionModerator) Name() string { return "stub" }

func (StubVisionModerator) ModerateImage(_ context.Context, imageURL string) ([]Label, error) {
	if imageURL == "" {
		return nil, fmt.Errorf("moderation: empty image url")
	}
	return nil, nil
}

func (StubVisionModerator) ModerateVideo(_ context.Context, videoURL string) ([]Label, error) {
	if videoURL == "" {
		return nil, fmt.Errorf("moderation: empty video url")
	}
	return nil, nil
}
