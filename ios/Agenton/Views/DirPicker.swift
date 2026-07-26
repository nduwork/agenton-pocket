import SwiftUI

// Browse the daemon host's filesystem to pick a working directory. The app
// can't see the Mac's disk, so every step asks the daemon (list_dir) and
// renders its reply (dir_list). Tap a folder to descend, ↑ for the parent,
// "Use this folder" to select the current path.
struct DirPicker: View {
    @ObservedObject var client: AgentonClient
    let onPick: (String) -> Void
    @Environment(\.dismiss) private var dismiss

    private var path: String { client.dirListing?.path ?? "" }
    private var dirs: [String] { client.dirListing?.dirs ?? [] }
    private var atRoot: Bool { path == "/" || path.isEmpty }

    var body: some View {
        NavigationStack {
            VStack(spacing: 0) {
                pathBar
                ScrollView {
                    LazyVStack(spacing: 0) {
                        if client.dirListing == nil {
                            ProgressView().padding(.top, 40)
                        } else if dirs.isEmpty {
                            Text("no subfolders").foregroundStyle(Theme.dim)
                                .font(.subheadline).padding(.top, 40)
                        } else {
                            ForEach(dirs, id: \.self) { d in row(d) }
                        }
                    }
                }
            }
            .background(Theme.bg)
            .navigationTitle("Choose folder")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .cancellationAction) { Button("Cancel") { dismiss() } }
                ToolbarItem(placement: .topBarLeading) {
                    Button { client.listDir("") } label: { Image(systemName: "house") } // home
                }
                ToolbarItem(placement: .confirmationAction) {
                    Button("Use") { onPick(path); dismiss() }.disabled(path.isEmpty)
                }
            }
            .onAppear { client.dirListing = nil; client.listDir("") }
        }
    }

    private var pathBar: some View {
        HStack(spacing: 10) {
            Button { client.listDir(parent(of: path)) } label: {
                Image(systemName: "chevron.up").font(.system(size: 15, weight: .semibold))
            }
            .disabled(atRoot)
            .foregroundStyle(atRoot ? Theme.dim : Theme.accent)
            Text(path.isEmpty ? "…" : path)
                .font(.system(.footnote, design: .monospaced))
                .foregroundStyle(Theme.fg)
                .lineLimit(1).truncationMode(.head)
            Spacer()
        }
        .padding(.horizontal, 14).padding(.vertical, 10)
        .background(Theme.panel)
        .overlay(alignment: .bottom) { Rectangle().fill(Theme.rule).frame(height: 1) }
    }

    private func row(_ name: String) -> some View {
        Button {
            client.listDir((path as NSString).appendingPathComponent(name))
        } label: {
            HStack(spacing: 12) {
                Image(systemName: "folder.fill").foregroundStyle(Theme.accent)
                Text(name).foregroundStyle(Theme.fg)
                Spacer()
                Image(systemName: "chevron.right").font(.caption).foregroundStyle(Theme.dim)
            }
            .padding(.horizontal, 16).padding(.vertical, 13)
        }
        .buttonStyle(.plain)
        .overlay(alignment: .bottom) {
            Rectangle().fill(Theme.rule).frame(height: 1).padding(.leading, 44)
        }
    }

    private func parent(of p: String) -> String {
        let up = (p as NSString).deletingLastPathComponent
        return up.isEmpty ? "/" : up
    }
}
