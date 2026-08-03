package media

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/mediaconvert"
	mctypes "github.com/aws/aws-sdk-go-v2/service/mediaconvert/types"
)

// A MediaConvert job description is a hundred lines of nested structs that
// nobody reviews carefully and AWS validates only at submission — by which
// point the feedback loop is an IAM role, a queue and several minutes. The
// shape of that description is therefore exactly the thing worth testing here,
// and it is testable without an account because the client is an interface.

type fakeMC struct {
	created *mediaconvert.CreateJobInput
	job     *mctypes.Job
	err     error
}

func (f *fakeMC) CreateJob(_ context.Context, in *mediaconvert.CreateJobInput, _ ...func(*mediaconvert.Options)) (*mediaconvert.CreateJobOutput, error) {
	f.created = in
	if f.err != nil {
		return nil, f.err
	}
	return &mediaconvert.CreateJobOutput{Job: &mctypes.Job{Id: aws.String("job-1234")}}, nil
}

func (f *fakeMC) GetJob(_ context.Context, _ *mediaconvert.GetJobInput, _ ...func(*mediaconvert.Options)) (*mediaconvert.GetJobOutput, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &mediaconvert.GetJobOutput{Job: f.job}, nil
}

func provider(api MediaConvertAPI) MediaConvertProvider {
	return MediaConvertProvider{
		API:         api,
		Role:        "arn:aws:iam::123456789012:role/MediaConvertRole",
		Destination: "s3://marketplace-media/hls",
		BaseURL:     "https://cdn.example.ae",
	}
}

func TestSubmitDescribesTheLadderTheCostModelAssumes(t *testing.T) {
	api := &fakeMC{}
	ref, err := provider(api).Submit(context.Background(),
		"s3://marketplace-uploads/reels/abc123.mp4", DefaultProfile())
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if ref.Provider != MediaConvertName || ref.ID != "job-1234" {
		t.Fatalf("got %+v", ref)
	}

	in := api.created
	if aws.ToString(in.Role) == "" {
		t.Error("no role: MediaConvert cannot read the source or write the output")
	}

	var hls *mctypes.OutputGroup
	for i, g := range in.Settings.OutputGroups {
		if g.OutputGroupSettings.Type == mctypes.OutputGroupTypeHlsGroupSettings {
			hls = &in.Settings.OutputGroups[i]
		}
	}
	if hls == nil {
		t.Fatal("no HLS output group")
	}

	// Three rungs, 720p at the top. §2.2 caps the ladder there because 1080p
	// roughly doubles egress for a difference nobody perceives in a
	// phone-sized autoplay feed, and egress is the largest line in the cost
	// model. A fourth rung added here would not fail anything at runtime — it
	// would just quietly double the bill.
	if len(hls.Outputs) != len(Ladder()) {
		t.Fatalf("ladder has %d rungs, job describes %d", len(Ladder()), len(hls.Outputs))
	}
	for i, want := range Ladder() {
		got := hls.Outputs[i]
		h := got.VideoDescription.CodecSettings.H264Settings
		if aws.ToInt32(got.VideoDescription.Height) != int32(want.Height) {
			t.Errorf("rung %s: height %d, want %d",
				want.Label, aws.ToInt32(got.VideoDescription.Height), want.Height)
		}
		if aws.ToInt32(h.MaxBitrate) != int32(want.BitrateKbps)*1000 {
			t.Errorf("rung %s: max bitrate %d, want %d",
				want.Label, aws.ToInt32(h.MaxBitrate), want.BitrateKbps*1000)
		}
	}

	// Two-second segments, because startup latency is the entire experience in
	// a feed where someone swipes every four seconds.
	if got := aws.ToInt32(hls.OutputGroupSettings.HlsGroupSettings.SegmentLength); got != 2 {
		t.Errorf("segment length %ds, want 2s", got)
	}

	// A silent source is normal — listings are often filmed muted, and the
	// recorder ships a silent clip when the seller declines the microphone.
	if len(in.Settings.Inputs[0].AudioSelectors) == 0 {
		t.Error("no audio selector: a source without an audio track fails the job")
	}
}

// The one that would have shipped broken.
//
// Fetch rebuilds playlist URLs from the job alone, because it is called by the
// reconciliation sweep as well as by the completion event. If the output
// location is not a pure function of the input, the URLs it produces point at
// nothing — and only on the sweep path, which is the path taken when the event
// is lost, which is the path nobody exercises before production.
func TestTheURLsFetchBuildsAreWhereSubmitWroteThem(t *testing.T) {
	const source = "s3://marketplace-uploads/reels/abc123.mp4"
	api := &fakeMC{}
	p := provider(api)

	if _, err := p.Submit(context.Background(), source, DefaultProfile()); err != nil {
		t.Fatalf("submit: %v", err)
	}
	dest := aws.ToString(
		api.created.Settings.OutputGroups[0].OutputGroupSettings.HlsGroupSettings.Destination)

	// A completed job carrying no UserMetadata, which is the older-build case
	// and forces Fetch down the re-derivation path rather than the easy one.
	api.job = &mctypes.Job{
		Id:     aws.String("job-1234"),
		Status: mctypes.JobStatusComplete,
		Settings: &mctypes.JobSettings{
			Inputs: []mctypes.Input{{FileInput: aws.String(source)}},
		},
	}

	res, ok, err := p.Fetch(context.Background(), JobRef{ID: "job-1234"})
	if err != nil || !ok {
		t.Fatalf("fetch: ok=%v err=%v", ok, err)
	}

	// s3://marketplace-media/hls/reels/abc123/index -> .../reels/abc123/index
	suffix := strings.TrimPrefix(dest, "s3://marketplace-media/hls/")
	for _, r := range res.Renditions {
		want := "https://cdn.example.ae/" + suffix + "_" + r.Label + ".m3u8"
		if r.PlaylistURL != want {
			t.Errorf("rendition %s\n got: %s\nwant: %s", r.Label, r.PlaylistURL, want)
		}
	}
}

func TestFetchDistinguishesRunningFromFailed(t *testing.T) {
	p := provider(nil)

	for _, tc := range []struct {
		status  mctypes.JobStatus
		wantOK  bool
		wantErr bool
		why     string
	}{
		{mctypes.JobStatusSubmitted, false, false, "queued is not an error"},
		{mctypes.JobStatusProgressing, false, false, "encoding is not an error"},
		{mctypes.JobStatusComplete, true, false, "done"},
		{
			mctypes.JobStatusError, false, true,
			"a failed encode reported as merely incomplete leaves the reel " +
				"processing forever and never tells the seller",
		},
		{mctypes.JobStatusCanceled, false, true, "same, for a cancelled job"},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			api := &fakeMC{job: &mctypes.Job{
				Id:           aws.String("j"),
				Status:       tc.status,
				ErrorMessage: aws.String("codec not supported"),
				UserMetadata: map[string]string{"prefix": "reels/abc"},
			}}
			p.API = api

			_, ok, err := p.Fetch(context.Background(), JobRef{ID: "j"})
			if ok != tc.wantOK {
				t.Errorf("ok=%v, want %v — %s", ok, tc.wantOK, tc.why)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err=%v, want error=%v — %s", err, tc.wantErr, tc.why)
			}
			if tc.wantErr && err != nil && tc.status == mctypes.JobStatusError {
				// The operator needs the reason, not just the status.
				if !strings.Contains(err.Error(), "codec not supported") {
					t.Errorf("error should carry AWS's message; got %v", err)
				}
			}
		})
	}
}

// Deriving the output location from the source is what makes the round trip
// above possible, and re-submitting the same source must land in the same
// place rather than accumulating orphaned copies nobody can find.
func TestOutputPrefixIsStableAndDerivedFromTheSource(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"s3://bucket/reels/abc123.mp4", "reels/abc123"},
		{"s3://other-bucket/reels/abc123.mov", "reels/abc123"},
		{"https://cdn.example.ae/uploads/x/y.webm", "uploads/x/y"},
		{"s3://bucket/noextension", "noextension"},
		{"", "unknown"},
	} {
		if got := outputPrefix(tc.in); got != tc.want {
			t.Errorf("outputPrefix(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	if a, b := outputPrefix("s3://b/reels/x.mp4"), outputPrefix("s3://b/reels/x.mp4"); a != b {
		t.Error("not stable across calls")
	}
}

func TestSubmitRefusesBeforeCallingAWS(t *testing.T) {
	api := &fakeMC{}
	p := provider(api)
	p.Role = ""

	if _, err := p.Submit(context.Background(), "s3://b/k.mp4", DefaultProfile()); err == nil {
		t.Fatal("expected a refusal")
	}
	// Not merely refused — refused without spending a round trip, and without
	// creating a job that will fail at AWS minutes later.
	if api.created != nil {
		t.Error("a job was submitted despite an invalid configuration")
	}
}

func TestDurationIsWallClockAndBounded(t *testing.T) {
	start := time.Date(2026, 8, 1, 10, 0, 0, 0, time.UTC)
	job := &mctypes.Job{Timing: &mctypes.Timing{
		StartTime:  aws.Time(start),
		FinishTime: aws.Time(start.Add(90 * time.Second)),
	}}
	if got := durationMS(job); got != 90_000 {
		t.Errorf("durationMS = %d, want 90000", got)
	}
	// A clock skew between AWS's timestamps must not produce a negative or
	// absurd duration on a reel.
	job.Timing.FinishTime = aws.Time(start.Add(-time.Hour))
	if got := durationMS(job); got != 0 {
		t.Errorf("a negative interval should be 0, got %d", got)
	}
	if got := durationMS(&mctypes.Job{}); got != 0 {
		t.Errorf("missing timing should be 0, got %d", got)
	}
}
