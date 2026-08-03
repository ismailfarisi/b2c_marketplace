# One Dockerfile, three binaries, selected by --build-arg.
#
# The api, worker and feed services share a module, a dependency graph and a
# base image; three near-identical Dockerfiles would drift, and the first
# symptom of drift is the worker running a different Go version than the API
# for a month before anyone notices.
#
# Build:
#   docker build -f services/api/Dockerfile --build-arg CMD=worker -t worker .
# Context is the *repository root*, not services/api, because the build needs
# the module and nothing above it — see .dockerignore for what is excluded.

# Matched to go.mod. The Alpine image pins GOTOOLCHAIN=local, so an older
# base does not quietly upgrade itself the way a developer machine does — it
# fails, which is the better behaviour and how the stale CI pin was found.
ARG GO_VERSION=1.26

# ---------------------------------------------------------------- build --
FROM golang:${GO_VERSION}-alpine AS build

# git for the module system; tzdata because Alpine ships none and the runtime
# stage copies /usr/share/zoneinfo out of here. Without the package that path
# does not exist and the COPY fails — which is a better outcome than the
# alternative, a container that starts and silently renders every settlement
# date in UTC.
RUN apk add --no-cache git tzdata

WORKDIR /src

# Dependencies first, as their own layer. They change on the order of once a
# month; the source changes every commit, and without this split every build
# re-downloads the whole graph.
COPY services/api/go.mod services/api/go.sum ./
RUN go mod download

COPY services/api/ ./

ARG CMD=api
ARG VERSION=dev

# CGO off so the result runs on a distroless/static base. The race detector
# needs cgo, but that belongs in CI, not in a shipped image.
#
# -trimpath keeps the build machine's directory layout out of the binary, and
# -s -w drop the symbol and DWARF tables — together roughly a third of the
# size, and none of it is usable without the matching source anyway.
ENV CGO_ENABLED=0 GOOS=linux
RUN go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
        -o /out/service ./cmd/${CMD}

# ---------------------------------------------------------------- runtime --
FROM gcr.io/distroless/static-debian12:nonroot

# The market timezone is a correctness dependency, not a nicety: settlement
# dates and the cutoff time mode are rendered in Asia/Dubai, and a container
# with no zoneinfo silently falls back to UTC. That produced a settlement row
# reading "07 Aug" against a notification reading "8 Aug" once already.
#
# The Go binary embeds tzdata via time/tzdata, so this is belt and braces for
# anything that shells out or reads /etc/localtime.
COPY --from=build /usr/share/zoneinfo /usr/share/zoneinfo
ENV TZ=Asia/Dubai

# Distroless ships CA roots; the S3, Anthropic and SMS clients all need them.
COPY --from=build /out/service /service

# nonroot (uid 65532) comes from the base tag, not a USER line, so it cannot be
# lost by an edit here.
EXPOSE 8080
ENTRYPOINT ["/service"]
