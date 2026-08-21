import SwiftUI
import UniformTypeIdentifiers
import Shared

/// The last screen of the sell flow, and it says which of several things is
/// true.
///
/// The mock calls this "You're live in Al Quoz". It often is not: every author
/// on this path is new in `author_trust`, so all of it goes to a person before
/// publish. Telling the seller it is live produces the worst possible sequence —
/// they share the link, their friend sees nothing, and they conclude the
/// platform is broken.
struct LiveStep: View {
    let state: SellUiState
    let viewModel: SellViewModel
    let onSeeFeed: () -> Void
    let onDone: () -> Void

    @State private var pickingDocument = false

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    HStack(alignment: .top, spacing: Space.medium) {
                        // A tinted disc rather than a bare glyph. The state of
                        // the listing is the only thing on this screen the
                        // seller came to read, and a 20pt character against a
                        // dark background is not read from across a forecourt.
                        let tint = phase.bad ? Palette.bad : Palette.good
                        Image(systemName: phase.symbol)
                            .font(.system(size: 20))
                            .foregroundStyle(tint)
                            .frame(width: 40, height: 40)
                            .background(Circle().fill(tint.opacity(0.15)))
                        VStack(alignment: .leading, spacing: Space.tight) {
                            Text(phase.headline)
                                .font(.system(size: 19).weight(.semibold))
                                .foregroundStyle(Palette.text)
                            Text(phase.detail)
                                .font(.system(size: TypeScale.secondary))
                                .foregroundStyle(Palette.textMuted)
                        }
                    }

                    // Three blocks across, not three label-and-number rows.
                    // These are the numbers the seller reopens the app for;
                    // side by side they read as one result at a glance instead
                    // of a settings list.
                    HStack(alignment: .top, spacing: Space.large) {
                        stat("sell_stat_views", state.status?.counters.views)
                        stat("sell_stat_saves", state.status?.counters.saves)
                        stat("sell_stat_comments", state.status?.counters.comments)
                    }
                    .padding(.top, Space.xl)

                    // The button repairs whichever half actually broke.
                    // Retrying the upload after a refused publish would send the
                    // seller to fix the one part that worked.
                    if let repair {
                        Button(action: repair.action) {
                            Text(repair.title)
                                .font(.system(size: 14).weight(.medium))
                                .foregroundStyle(Palette.accent)
                                .frame(maxWidth: .infinity, minHeight: Space.touch)
                                .overlay(RoundedRectangle(cornerRadius: 10)
                                    .stroke(Palette.accent, lineWidth: 1))
                        }
                        .padding(.top, Space.xl)
                    }

                    // Dropped once the reel is actually in the feed. Leaving
                    // "reviewed by a person before they appear" under "You're
                    // live" contradicts the headline, and the seller has to
                    // decide which half to believe.
                    if !state.reviewNote.isEmpty && state.status?.state != "published" {
                        Text(state.reviewNote)
                            .font(.system(size: TypeScale.caption))
                            .foregroundStyle(Palette.textFaint)
                            .padding(.top, Space.xl)
                    }

                    // The trust document, offered here rather than on the form.
                    // It could not be anywhere else: a document attaches to a
                    // subject, and the subject does not exist until Publish
                    // creates the draft.
                    if let trust = state.form?.trust {
                        TrustRow(trust: trust, state: state.trust) { pickingDocument = true }
                            .padding(.top, Space.xl)
                    }
                }
                .padding(.horizontal, Space.edge)
                .padding(.top, Space.large)
            }

            VStack(spacing: Space.small) {
                // Only once the reel is actually in the feed. "See it in the
                // feed" on a reel awaiting review sends the seller to look for
                // something that is not there.
                if state.status?.state == "published" {
                    Button(action: onSeeFeed) {
                        Text("sell_see_in_feed")
                            .font(.system(size: TypeScale.body).weight(.semibold))
                            .foregroundStyle(.white)
                            .frame(maxWidth: .infinity, minHeight: 48)
                            .background(RoundedRectangle(cornerRadius: Radius.button)
                                .fill(Palette.accent))
                    }
                }
                Button(action: onDone) {
                    Text("sell_done")
                        .font(.system(size: TypeScale.body))
                        .foregroundStyle(Palette.text)
                        .frame(maxWidth: .infinity, minHeight: Space.touch)
                }
            }
            .padding(.horizontal, Space.edge)
            .padding(.bottom, Space.medium)
        }
        .fileImporter(
            isPresented: $pickingDocument,
            // Constrained to what the vertical accepts, which the server
            // declared. No wildcard: the reviewer's tooling cannot open a HEIC,
            // and letting one through produces a document nobody can look at
            // and a seller told, days later, to send it again.
            allowedContentTypes: acceptedTypes
        ) { result in
            guard case .success(let url) = result,
                  let kind = state.form?.trust?.key else { return }

            // A security-scoped URL from the document picker. Without the
            // start/stop pair the read fails on a real device and succeeds in
            // the simulator — which is the worst possible way for a bug to
            // behave.
            guard url.startAccessingSecurityScopedResource() else {
                viewModel.onDocumentUnreadable()
                return
            }
            defer { url.stopAccessingSecurityScopedResource() }

            guard let data = try? Data(contentsOf: url), !data.isEmpty else {
                viewModel.onDocumentUnreadable()
                return
            }
            viewModel.attachDocument(
                kind: kind,
                // The resolver's type rather than the extension. A document
                // from Files or a scanner app frequently has neither, and the
                // presign is signed against the content type — guessing it
                // wrong produces a 403 that reads like an auth failure.
                contentType: (try? url.resourceValues(forKeys: [.contentTypeKey]))?
                    .contentType?.preferredMIMEType ?? "application/octet-stream",
                bytes: toKotlinByteArray(data)
            )
        }
        .task {
            // A decision made while the seller is still on this screen should
            // show up on it. Cheap, and the only way an approval is ever
            // visible without a relaunch.
            viewModel.refreshDocuments()
        }
    }

    /// The MIME list the vertical declared, mapped to UTTypes.
    ///
    /// Falls back to the three the server accepts rather than to `.item`: a
    /// picker that offers everything invites a `.pages` file the endpoint will
    /// refuse after the upload.
    private var acceptedTypes: [UTType] {
        let mimes = state.form?.trust?.accept ?? []
        let mapped = mimes.compactMap { UTType(mimeType: $0) }
        return mapped.isEmpty ? [.jpeg, .png, .pdf] : mapped
    }

    private func stat(_ key: LocalizedStringKey, _ value: Int32?) -> some View {
        VStack(alignment: .leading, spacing: Space.hair) {
            // A dash, not a zero, before the first status lands. Zero is a
            // measurement; a dash is "not measured yet", and a seller who reads
            // an unfetched zero as a real one concludes their reel died.
            Text(value.map { grouped(Int64($0)) } ?? "—")
                .font(.system(size: TypeScale.heading).weight(.semibold))
                .foregroundStyle(Palette.text)
            Text(key)
                .font(.system(size: TypeScale.caption))
                .foregroundStyle(Palette.textFaint)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    private var repair: (title: LocalizedStringKey, action: () -> Void)? {
        switch state.upload {
        case .failed: return ("sell_retry_upload", viewModel.retryUpload)
        case .refused: return ("sell_retry_publish", viewModel.retryPublish)
        default: return nil
        }
    }

    /// The headline, chosen by what is actually true rather than by optimism.
    private var phase: (symbol: String, headline: String, detail: String, bad: Bool) {
        switch state.upload {
        case .uploading:
            return ("arrow.up.doc", String(localized: "sell_uploading"),
                    String(localized: "sell_uploading_detail"), false)
        case .failed:
            return ("exclamationmark.circle", String(localized: "sell_upload_failed"),
                    String(localized: "sell_upload_failed_detail"), true)
        case .refused:
            return ("exclamationmark.circle", String(localized: "sell_publish_refused"),
                    String(localized: "sell_publish_refused_detail"), true)
        default:
            break
        }

        switch state.status?.state {
        case "published":
            return ("checkmark.circle.fill", String(format: String(localized: "sell_live_in"),
                                                   displayPlace(state.place)),
                    String(localized: "sell_live_detail"), false)
        case "review_pending":
            return ("clock", String(localized: "sell_in_review"),
                    String(localized: "sell_in_review_detail"), false)
        case "rejected":
            return ("xmark", String(localized: "sell_rejected"),
                    String(localized: "sell_rejected_detail"), true)
        case "failed":
            return ("exclamationmark.circle", String(localized: "sell_processing_failed"),
                    String(localized: "sell_processing_failed_detail"), true)
        default:
            return ("arrow.triangle.2.circlepath", String(localized: "sell_processing"),
                    String(localized: "sell_processing_detail"), false)
        }
    }
}

/// "Add mulkiya to earn Verified", and a way to actually do it.
///
/// Five states, and none says "Verified" before a reviewer has granted it.
/// Announcing the badge when bytes land is the same mistake as calling a reel
/// live while it is with a moderator — worse, because the badge is the one
/// thing a buyer trusts.
private struct TrustRow: View {
    let trust: TrustUpload
    let state: TrustState
    let onPick: () -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: Space.small) {
            Text(localised(trust.label, trust.labelAr))
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Palette.text.opacity(0.75))

            switch state.phase {
            case .uploading:
                note("trust_uploading")
            case .submitted:
                note("trust_submitted")
            case .approved:
                Text(String(format: String(localized: "trust_approved"),
                            localised(trust.earns, trust.earnsAr)))
                    .font(.system(size: TypeScale.caption).weight(.semibold))
                    .foregroundStyle(Palette.good)
            case .rejected:
                // The reviewer's own words. A document refused with no
                // explanation is a seller who sends the same photograph again,
                // and then a third time.
                Text(state.reason ?? String(localized: "trust_rejected"))
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Palette.bad)
                button("trust_try_again")
            case .failed:
                Text(state.error?.words ?? String(localized: "err_unknown"))
                    .font(.system(size: TypeScale.caption))
                    .foregroundStyle(Palette.bad)
                button("trust_try_again")
            default:
                button("trust_choose")
            }
        }
        .padding(14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(RoundedRectangle(cornerRadius: 10).stroke(Palette.line, lineWidth: 1))
    }

    private func note(_ key: LocalizedStringKey) -> some View {
        Text(key)
            .font(.system(size: TypeScale.caption))
            .foregroundStyle(Palette.textFaint)
    }

    private func button(_ key: LocalizedStringKey) -> some View {
        Button(action: onPick) {
            Text(key)
                .font(.system(size: TypeScale.secondary))
                .foregroundStyle(Palette.accent)
                .padding(.horizontal, Space.large)
                .frame(minHeight: Space.touch)
                .overlay(RoundedRectangle(cornerRadius: 8)
                    .stroke(Palette.accent.opacity(0.6), lineWidth: 1))
        }
    }
}
