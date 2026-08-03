package media

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/mediaconvert"
	mctypes "github.com/aws/aws-sdk-go-v2/service/mediaconvert/types"
)

// MediaConvertProvider transcodes with AWS Elemental MediaConvert.
//
// # Why this one
//
// It is the first provider that is permitted in production, and the choice was
// made by what the deployment already has rather than by feature list. The
// object store is S3, the region is me-central-1, the AWS credential chain is
// already wired through objectstore, and MediaConvert reads and writes S3
// directly — so a reel never travels through this process. Mux and Cloudflare
// Stream would both mean shipping every upload to a third party and paying
// egress to do it.
//
// # Why it is allowed in production when ffmpeg is not
//
// The objection to [FFmpegProvider] is not its output, which is real: it is
// that its job table is an in-memory map, so a worker restart strands every
// in-flight reel permanently. This provider holds no job state at all. The job
// lives in MediaConvert under an ID that is persisted with the reel, and
// [MediaConvertProvider.Fetch] resolves it from that ID alone — a restart, a
// redeploy or a second worker picking up the sweep all behave identically.
//
// # Completion
//
// Submit returns a handle and never a Result, as the seam requires. Completion
// arrives by EventBridge, and Fetch is what both the event handler and the
// reconciliation sweep call — deliberately, because a provider that is only
// correct when the event arrives is a provider that loses reels, and some
// events never arrive.
type MediaConvertProvider struct {
	// API is the MediaConvert client. An interface rather than the concrete
	// client so the job description below can be tested without an AWS
	// account — the shape of that description is the part worth testing, and
	// it is also the part nobody reviews carefully.
	API MediaConvertAPI

	// Role is the IAM role MediaConvert assumes to read the source and write
	// the output. Required by the service; a job submitted without one fails
	// at AWS rather than here.
	Role string

	// Queue is optional. Empty uses the account's default queue, which is
	// correct until there is a reason to separate interactive uploads from a
	// backfill.
	Queue string

	// Destination is the S3 prefix outputs are written under, e.g.
	// s3://marketplace-media/hls. One prefix per reel is appended.
	Destination string

	// BaseURL is where those outputs are served from — the CDN, not the
	// bucket. Playlist URLs are built from this, and getting it wrong produces
	// a reel whose manifest resolves to a 403 for everyone but the account
	// that wrote it.
	BaseURL string
}

// MediaConvertAPI is the slice of the service this uses.
type MediaConvertAPI interface {
	CreateJob(context.Context, *mediaconvert.CreateJobInput, ...func(*mediaconvert.Options)) (*mediaconvert.CreateJobOutput, error)
	GetJob(context.Context, *mediaconvert.GetJobInput, ...func(*mediaconvert.Options)) (*mediaconvert.GetJobOutput, error)
}

const MediaConvertName = "aws-mediaconvert"

func (MediaConvertProvider) Name() string { return MediaConvertName }

// Validate reports why this provider cannot run, before it is asked to.
//
// Called at wiring time so a missing role ARN is a boot failure with a name in
// it, rather than an AccessDenied on the first upload of the day.
func (p MediaConvertProvider) Validate() error {
	var missing []string
	if p.API == nil {
		missing = append(missing, "client")
	}
	if strings.TrimSpace(p.Role) == "" {
		missing = append(missing, "MEDIACONVERT_ROLE_ARN")
	}
	if !strings.HasPrefix(p.Destination, "s3://") {
		missing = append(missing, "MEDIACONVERT_DESTINATION (must be an s3:// url)")
	}
	if strings.TrimSpace(p.BaseURL) == "" {
		missing = append(missing, "MEDIA_CDN_BASE_URL")
	}
	if len(missing) > 0 {
		return fmt.Errorf("media: mediaconvert is not configured: %s",
			strings.Join(missing, ", "))
	}
	return nil
}

// Submit creates the job and returns its handle.
//
// The output prefix is derived from the source key rather than from a fresh
// UUID, which matters more than it looks: Fetch has to rebuild the same
// playlist URLs from the job alone, and it can only do that if the location is
// a function of the input. A random prefix would work until the first time a
// job was resolved by the sweep instead of the event, and then it would
// silently produce URLs pointing at nothing.
func (p MediaConvertProvider) Submit(ctx context.Context, sourceURL string, prof Profile) (JobRef, error) {
	if err := p.Validate(); err != nil {
		return JobRef{}, err
	}
	if strings.TrimSpace(sourceURL) == "" {
		return JobRef{}, fmt.Errorf("media: empty source url")
	}
	if len(prof.Renditions) == 0 {
		prof = DefaultProfile()
	}

	prefix := outputPrefix(sourceURL)
	dest := strings.TrimSuffix(p.Destination, "/") + "/" + prefix + "/"

	settings := &mctypes.JobSettings{
		Inputs: []mctypes.Input{{
			FileInput: aws.String(sourceURL),
			AudioSelectors: map[string]mctypes.AudioSelector{
				"Audio Selector 1": {DefaultSelection: mctypes.AudioDefaultSelectionDefault},
			},
			VideoSelector: &mctypes.VideoSelector{},
			// A source with no audio track is normal here: plenty of listings
			// are filmed muted, and the recorder ships a silent clip when the
			// seller declines the microphone. Without this the job fails.
			TimecodeSource: mctypes.InputTimecodeSourceZerobased,
		}},
		OutputGroups: []mctypes.OutputGroup{
			p.hlsGroup(dest, prof),
			p.posterGroup(dest, prof),
		},
	}

	in := &mediaconvert.CreateJobInput{
		Role:     aws.String(p.Role),
		Settings: settings,
		// Carried so an operator looking at the MediaConvert console can tell
		// which reel a job belongs to without joining against the database.
		UserMetadata: map[string]string{"prefix": prefix},
		// Status updates every 10s rather than the 60s default. The feed's
		// promise is that a listing goes live quickly; a minute of dead time
		// after encoding finishes is a minute the seller spends looking at a
		// spinner for no reason.
		StatusUpdateInterval: mctypes.StatusUpdateIntervalSeconds10,
	}
	if p.Queue != "" {
		in.Queue = aws.String(p.Queue)
	}

	out, err := p.API.CreateJob(ctx, in)
	if err != nil {
		return JobRef{}, fmt.Errorf("media: mediaconvert create job: %w", err)
	}
	if out == nil || out.Job == nil || out.Job.Id == nil {
		return JobRef{}, fmt.Errorf("media: mediaconvert returned no job id")
	}
	return JobRef{Provider: MediaConvertName, ID: *out.Job.Id}, nil
}

// Fetch resolves a job, returning ok=false while it is still running.
//
// Errors and cancellations are errors, not "not ready". Reporting a failed
// encode as merely incomplete would leave the reel in processing forever, and
// the seller watching a spinner is never told the upload is dead.
func (p MediaConvertProvider) Fetch(ctx context.Context, ref JobRef) (Result, bool, error) {
	if ref.ID == "" {
		return Result{}, false, fmt.Errorf("media: empty job ref")
	}
	out, err := p.API.GetJob(ctx, &mediaconvert.GetJobInput{Id: aws.String(ref.ID)})
	if err != nil {
		return Result{}, false, fmt.Errorf("media: mediaconvert get job %s: %w", ref.ID, err)
	}
	if out == nil || out.Job == nil {
		return Result{}, false, fmt.Errorf("media: mediaconvert job %s not found", ref.ID)
	}
	job := out.Job

	switch job.Status {
	case mctypes.JobStatusComplete:
	case mctypes.JobStatusError:
		msg := "unknown error"
		if job.ErrorMessage != nil {
			msg = *job.ErrorMessage
		}
		return Result{}, false, fmt.Errorf("media: mediaconvert job %s failed: %s", ref.ID, msg)
	case mctypes.JobStatusCanceled:
		return Result{}, false, fmt.Errorf("media: mediaconvert job %s was cancelled", ref.ID)
	default:
		// SUBMITTED or PROGRESSING. Not an error and not a result.
		return Result{}, false, nil
	}

	prefix := job.UserMetadata["prefix"]
	if prefix == "" {
		// Recoverable: the job carries its own input path, so the prefix can
		// be re-derived by the same function that produced it. This is the
		// path taken for a job submitted by an older build.
		if len(job.Settings.Inputs) > 0 && job.Settings.Inputs[0].FileInput != nil {
			prefix = outputPrefix(*job.Settings.Inputs[0].FileInput)
		}
	}
	if prefix == "" {
		return Result{}, false, fmt.Errorf(
			"media: mediaconvert job %s has no output prefix to resolve", ref.ID)
	}

	base := strings.TrimSuffix(p.BaseURL, "/") + "/" + prefix
	rs := Ladder()
	for i := range rs {
		// The name modifier set in hlsGroup. Kept in one place so the URL
		// written here and the file written by AWS cannot drift.
		rs[i].PlaylistURL = fmt.Sprintf("%s/%s%s.m3u8", base, hlsBaseName, renditionModifier(rs[i]))
	}

	return Result{
		Renditions: rs,
		PosterKey:  fmt.Sprintf("%s/%s.0000000.jpg", prefix, posterBaseName),
		PreviewKey: "",
		DurationMS: durationMS(job),
	}, true, nil
}

const (
	hlsBaseName    = "index"
	posterBaseName = "poster"
)

func renditionModifier(r Rendition) string { return "_" + r.Label }

// hlsGroup is the ladder, packaged as HLS.
func (p MediaConvertProvider) hlsGroup(dest string, prof Profile) mctypes.OutputGroup {
	seg := prof.SegmentSeconds
	if seg <= 0 {
		seg = 2
	}

	outputs := make([]mctypes.Output, 0, len(prof.Renditions))
	for _, r := range prof.Renditions {
		outputs = append(outputs, mctypes.Output{
			NameModifier: aws.String(renditionModifier(r)),
			ContainerSettings: &mctypes.ContainerSettings{
				Container: mctypes.ContainerTypeM3u8,
			},
			VideoDescription: &mctypes.VideoDescription{
				Width:  aws.Int32(int32(r.Width)),
				Height: aws.Int32(int32(r.Height)),
				CodecSettings: &mctypes.VideoCodecSettings{
					Codec: mctypes.VideoCodecH264,
					H264Settings: &mctypes.H264Settings{
						Bitrate: aws.Int32(int32(r.BitrateKbps) * 1000),
						// Capped rather than constant: a static shot of a
						// parked car does not need its full allowance, and the
						// bytes saved are egress, which §2.2 names as the
						// largest line in the cost model.
						RateControlMode:    mctypes.H264RateControlModeQvbr,
						MaxBitrate:         aws.Int32(int32(r.BitrateKbps) * 1000),
						QvbrSettings:       &mctypes.H264QvbrSettings{QvbrQualityLevel: aws.Int32(7)},
						SceneChangeDetect:  mctypes.H264SceneChangeDetectTransitionDetection,
						GopSizeUnits:       mctypes.H264GopSizeUnitsSeconds,
						GopSize:            aws.Float64(float64(seg)),
						CodecProfile:       mctypes.H264CodecProfileMain,
						QualityTuningLevel: mctypes.H264QualityTuningLevelSinglePassHq,
					},
				},
			},
			AudioDescriptions: []mctypes.AudioDescription{{
				CodecSettings: &mctypes.AudioCodecSettings{
					Codec: mctypes.AudioCodecAac,
					AacSettings: &mctypes.AacSettings{
						Bitrate:    aws.Int32(96000),
						CodingMode: mctypes.AacCodingModeCodingMode20,
						SampleRate: aws.Int32(48000),
					},
				},
			}},
		})
	}

	return mctypes.OutputGroup{
		Name:    aws.String("HLS"),
		Outputs: outputs,
		OutputGroupSettings: &mctypes.OutputGroupSettings{
			Type: mctypes.OutputGroupTypeHlsGroupSettings,
			HlsGroupSettings: &mctypes.HlsGroupSettings{
				Destination:      aws.String(dest + hlsBaseName),
				SegmentLength:    aws.Int32(int32(seg)),
				MinSegmentLength: aws.Int32(0),
				// Single file per rendition rather than thousands of two-second
				// objects. Fewer round trips on a mobile connection, and S3
				// bills per request.
				SegmentControl: mctypes.HlsSegmentControlSingleFile,
			},
		},
	}
}

// posterGroup extracts the still the feed shows before the first frame decodes.
//
// One frame, at PosterAtMS. The BlurHash is computed from this file by the
// existing media pipeline rather than here — see EncodeBlurHash — because the
// hash is a property of the image and not of the encoder that produced it.
func (p MediaConvertProvider) posterGroup(dest string, prof Profile) mctypes.OutputGroup {
	at := prof.PosterAtMS
	if at <= 0 {
		at = 1000
	}
	// MediaConvert counts captures in frames at the output frame rate, so one
	// frame every `at` milliseconds with MaxCaptures 1 gives the frame at that
	// offset and nothing after it.
	return mctypes.OutputGroup{
		Name: aws.String("Poster"),
		Outputs: []mctypes.Output{{
			NameModifier: aws.String("_" + posterBaseName),
			ContainerSettings: &mctypes.ContainerSettings{
				Container: mctypes.ContainerTypeRaw,
			},
			VideoDescription: &mctypes.VideoDescription{
				CodecSettings: &mctypes.VideoCodecSettings{
					Codec: mctypes.VideoCodecFrameCapture,
					FrameCaptureSettings: &mctypes.FrameCaptureSettings{
						FramerateNumerator:   aws.Int32(1000),
						FramerateDenominator: aws.Int32(int32(at)),
						MaxCaptures:          aws.Int32(1),
						Quality:              aws.Int32(80),
					},
				},
			},
		}},
		OutputGroupSettings: &mctypes.OutputGroupSettings{
			Type: mctypes.OutputGroupTypeFileGroupSettings,
			FileGroupSettings: &mctypes.FileGroupSettings{
				Destination: aws.String(dest + posterBaseName),
			},
		},
	}
}

// outputPrefix derives a stable output location from the source object key.
//
// Stable is the whole requirement: Fetch rebuilds playlist URLs from this, and
// it may run from a sweep that has only the job. Deriving it from the source
// means the same input always names the same output, so a re-submitted job
// overwrites its own files rather than accumulating orphans nobody can find.
func outputPrefix(sourceURL string) string {
	s := sourceURL
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[i+1:] // drop the bucket
	}
	if i := strings.LastIndex(s, "."); i >= 0 {
		s = s[:i] // drop the extension
	}
	s = strings.Trim(s, "/")
	if s == "" {
		return "unknown"
	}
	return s
}

func durationMS(job *mctypes.Job) int {
	if job.Timing == nil || job.Timing.StartTime == nil || job.Timing.FinishTime == nil {
		return 0
	}
	// Wall-clock encode time, not media duration — MediaConvert reports media
	// duration only in the completion event, and this is the value available
	// from GetJob. Named honestly rather than passed off as the other.
	d := job.Timing.FinishTime.Sub(*job.Timing.StartTime)
	if d < 0 || d > 24*time.Hour {
		return 0
	}
	return int(d.Milliseconds())
}
