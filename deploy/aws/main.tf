# The AWS resources the API refuses to start without.
#
# Every guardrail in internal/config and internal/app was written to fail at
# boot with the name of a missing variable. That is the right behaviour and it
# leaves an operator holding a list of names and no way to produce the values —
# "create a MediaConvert role" is a sentence that hides an IAM trust policy, a
# bucket policy and a PassRole grant, and getting any of the three wrong
# produces the same AccessDenied hours later.
#
# So the list is expressed here instead. `terraform apply` and the outputs are
# the environment, one to one, in the order config.Load validates them.
#
# What this does NOT create, deliberately:
#
#   - the Postgres instance. Managed Postgres with PostGIS is a decision about
#     cost, region, HA and backup retention that belongs to whoever pays for
#     it, and a default here would be either wastefully large or quietly
#     unsuitable. DATABASE_URL is an input.
#   - the SMS sender. The UAE requires a registered sender ID and the process
#     is manual and carrier-specific.
#   - the Anthropic key. Not an AWS resource.
#
# Region defaults to me-central-1 (UAE), matching S3_REGION's default and the
# market. MediaConvert is available there.

terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.region
}

variable "region" {
  description = "Must match S3_REGION. MediaConvert must be available in it."
  type        = string
  default     = "me-central-1"
}

variable "name" {
  description = "Prefix for every resource, so two environments can share an account."
  type        = string
  default     = "marketplace"
}

variable "environment" {
  description = "production | staging. Only affects naming and tags."
  type        = string
  default     = "production"
}

locals {
  prefix = "${var.name}-${var.environment}"
  tags = {
    Application = var.name
    Environment = var.environment
    ManagedBy   = "terraform"
  }
}

# ---------------------------------------------------------------- storage --

# One bucket, three prefixes: uploads/ (what the phone PUTs), hls/ (what
# MediaConvert writes) and posters/. One bucket rather than three because the
# CDN, the bucket policy and the CORS rule would otherwise be triplicated, and
# the prefixes are what the lifecycle rules key on anyway.
resource "aws_s3_bucket" "media" {
  bucket = "${local.prefix}-media"
  tags   = local.tags
}

resource "aws_s3_bucket_public_access_block" "media" {
  bucket = aws_s3_bucket.media.id

  # Public access is blocked at every level. Playback is served through
  # CloudFront with an origin access control, so nothing needs the bucket to be
  # readable by the internet — and a bucket that is readable by the internet is
  # how a private mulkiya ends up indexed.
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

resource "aws_s3_bucket_server_side_encryption_configuration" "media" {
  bucket = aws_s3_bucket.media.id
  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

resource "aws_s3_bucket_versioning" "media" {
  bucket = aws_s3_bucket.media.id
  versioning_configuration {
    # Trust documents live here. An erasure request has to be able to remove
    # one, and versioning would leave the removed copy recoverable — see the
    # account-lifecycle work. Suspended, not enabled.
    status = "Suspended"
  }
}

resource "aws_s3_bucket_cors_configuration" "media" {
  bucket = aws_s3_bucket.media.id

  # The phone PUTs directly to a presigned URL, so the browser-equivalent
  # preflight applies to the web admin too. ETag is exposed because a
  # multipart upload cannot be completed without it.
  cors_rule {
    allowed_methods = ["PUT", "GET", "HEAD"]
    allowed_origins = var.cors_origins
    allowed_headers = ["*"]
    expose_headers  = ["ETag"]
    max_age_seconds = 3000
  }
}

variable "cors_origins" {
  description = "Origins allowed to PUT directly. The mobile apps do not send Origin; this is for the web admin."
  type        = list(string)
  default     = ["*"]
}

resource "aws_s3_bucket_lifecycle_configuration" "media" {
  bucket = aws_s3_bucket.media.id

  # The source file after it has been transcoded. Kept 30 days rather than
  # deleted on completion: a re-encode at a different ladder, or an
  # investigation into a moderation decision, both need the original, and both
  # happen within a month or not at all.
  rule {
    id     = "expire-sources"
    status = "Enabled"
    filter { prefix = "uploads/" }
    expiration { days = 30 }
  }

  # A presigned PUT that was never completed leaves a part behind, and parts
  # are billed. They are invisible in the console, which is why this is the
  # rule people discover from an invoice.
  rule {
    id     = "abort-incomplete-uploads"
    status = "Enabled"
    filter {}
    abort_incomplete_multipart_upload { days_after_initiation = 7 }
  }
}

# ------------------------------------------------------------------- cdn --

resource "aws_cloudfront_origin_access_control" "media" {
  name                              = "${local.prefix}-media"
  origin_access_control_origin_type = "s3"
  signing_behavior                  = "always"
  signing_protocol                  = "sigv4"
}

resource "aws_cloudfront_distribution" "media" {
  enabled = true
  comment = "${local.prefix} media"

  origin {
    domain_name              = aws_s3_bucket.media.bucket_regional_domain_name
    origin_id                = "media"
    origin_access_control_id = aws_cloudfront_origin_access_control.media.id
  }

  default_cache_behavior {
    target_origin_id       = "media"
    viewer_protocol_policy = "redirect-to-https"
    allowed_methods        = ["GET", "HEAD", "OPTIONS"]
    cached_methods         = ["GET", "HEAD"]
    compress               = true

    # CachingOptimized. HLS segments are immutable once written — the output
    # prefix is derived from the source key, so a re-encode of the same source
    # overwrites in place and a different source is a different path.
    cache_policy_id = "658327ea-f89d-4fab-a63d-7e88639e58f6"
  }

  restrictions {
    geo_restriction {
      # Not restricted. A UAE marketplace has viewers abroad — someone buying
      # a car before they land — and geo-blocking them is a product decision
      # nobody made.
      restriction_type = "none"
    }
  }

  viewer_certificate {
    cloudfront_default_certificate = true
  }

  price_class = "PriceClass_200"
  tags        = local.tags
}

# CloudFront reads the bucket; nothing else does anonymously.
data "aws_iam_policy_document" "bucket" {
  statement {
    sid       = "CloudFrontRead"
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.media.arn}/*"]

    principals {
      type        = "Service"
      identifiers = ["cloudfront.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "AWS:SourceArn"
      values   = [aws_cloudfront_distribution.media.arn]
    }
  }
}

resource "aws_s3_bucket_policy" "media" {
  bucket = aws_s3_bucket.media.id
  policy = data.aws_iam_policy_document.bucket.json
}

# ---------------------------------------------------------- mediaconvert --

data "aws_iam_policy_document" "mediaconvert_trust" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["mediaconvert.amazonaws.com"]
    }
  }
}

# The role MediaConvert assumes to read the upload and write the ladder.
# It needs the bucket, and nothing else.
resource "aws_iam_role" "mediaconvert" {
  name               = "${local.prefix}-mediaconvert"
  assume_role_policy = data.aws_iam_policy_document.mediaconvert_trust.json
  tags               = local.tags
}

data "aws_iam_policy_document" "mediaconvert" {
  statement {
    actions   = ["s3:GetObject", "s3:PutObject"]
    resources = ["${aws_s3_bucket.media.arn}/*"]
  }
  statement {
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.media.arn]
  }
}

resource "aws_iam_role_policy" "mediaconvert" {
  name   = "media"
  role   = aws_iam_role.mediaconvert.id
  policy = data.aws_iam_policy_document.mediaconvert.json
}

# ----------------------------------------------------------------- app --

# What the API and worker are allowed to do.
#
# Attached to whatever identity runs the containers — a task role, an IRSA
# role, an instance profile. Deliberately a policy rather than a user with
# static keys: objectstore falls through to the default credential chain when
# S3_ACCESS_KEY_ID is empty, which is why that variable is not required, and a
# long-lived access key in an environment file is the credential that leaks.
data "aws_iam_policy_document" "app" {
  statement {
    sid       = "Media"
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
    resources = ["${aws_s3_bucket.media.arn}/*"]
  }
  statement {
    sid       = "ListForPresign"
    actions   = ["s3:ListBucket"]
    resources = [aws_s3_bucket.media.arn]
  }
  statement {
    sid = "Transcode"
    actions = [
      "mediaconvert:CreateJob",
      "mediaconvert:GetJob",
      "mediaconvert:ListJobs",
      "mediaconvert:DescribeEndpoints",
    ]
    resources = ["*"]
  }
  statement {
    # The one that is always forgotten. Creating a job means handing
    # MediaConvert a role to assume, and without PassRole the call fails with
    # an AccessDenied that names the *caller*, not the role — which sends
    # people to debug the wrong policy for an afternoon.
    sid       = "PassTheMediaConvertRole"
    actions   = ["iam:PassRole"]
    resources = [aws_iam_role.mediaconvert.arn]
    condition {
      test     = "StringEquals"
      variable = "iam:PassedToService"
      values   = ["mediaconvert.amazonaws.com"]
    }
  }
}

resource "aws_iam_policy" "app" {
  name        = "${local.prefix}-app"
  description = "S3 and MediaConvert access for the API and worker"
  policy      = data.aws_iam_policy_document.app.json
  tags        = local.tags
}

# ------------------------------------------------------------- outputs --

# Named for the environment variables they satisfy, so the mapping needs no
# documentation and cannot drift from it.
output "S3_BUCKET" {
  value = aws_s3_bucket.media.id
}

output "S3_REGION" {
  value = var.region
}

output "MEDIA_CDN_BASE_URL" {
  value = "https://${aws_cloudfront_distribution.media.domain_name}"
}

output "S3_PUBLIC_ENDPOINT" {
  description = "Empty means presigned URLs name the bucket's own endpoint, which is correct here — uploads go straight to S3 and only playback goes through the CDN."
  value       = ""
}

output "MEDIACONVERT_ROLE_ARN" {
  value = aws_iam_role.mediaconvert.arn
}

output "MEDIACONVERT_DESTINATION" {
  value = "s3://${aws_s3_bucket.media.id}/hls"
}

output "app_policy_arn" {
  description = "Attach to the task/instance role that runs the API and worker."
  value       = aws_iam_policy.app.arn
}

output "still_required" {
  description = "Not creatable here. See the header."
  value = [
    "DATABASE_URL — managed Postgres with PostGIS and TLS",
    "JWT_SIGNING_KEY — openssl rand -base64 48",
    "ANTHROPIC_API_KEY — moderation; the API refuses to boot without it",
    "an SMS sender with a registered UAE sender ID",
  ]
}
