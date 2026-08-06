import SwiftUI

// Where the daemon's web bridge lives. The browser client inherited this from
// the page origin; a standalone app must be told. Always a Tailscale MagicDNS
// host (e.g. mac.tailnet.ts.net) on port 9787, reached over plain HTTP — the
// tailnet is the security boundary.
struct ServerSettings: View {
    @ObservedObject var settings = AppSettings.shared
    @Environment(\.dismiss) private var dismiss
    @State private var probe = ""
    @State private var scanning = false

    var body: some View {
        NavigationStack {
            Form {
                Section("Daemon address") {
                    TextField("host (e.g. mac.tailnet.ts.net)", text: $settings.host)
                        .textInputAutocapitalization(.never)
                        .autocorrectionDisabled()
                        .keyboardType(.URL)
                    HStack {
                        Text("Port")
                        Spacer()
                        TextField("9787", value: $settings.port, format: .number.grouping(.never))
                            .keyboardType(.numberPad)
                            .multilineTextAlignment(.trailing)
                            .frame(width: 90)
                    }
                }
                Section {
                    Button { AppLinks.open(.daemonInstall) } label: {
                        Label("Install the daemon", systemImage: "arrow.down.circle")
                    }
                    Button {
                        scanning = true
                    } label: {
                        Label("Scan QR", systemImage: "qrcode.viewfinder")
                    }
                    Button("Test connection", action: test)
                    if !probe.isEmpty { Text(probe).font(.footnote).foregroundStyle(Theme.dim) }
                } footer: {
                    Text("Don't have it yet? Install the daemon, then run `agenton vpn` (or `agenton qr`) on the host — it prints a QR to scan, or reach it over your tailnet by hand.")
                }
            }
            .navigationTitle("Server")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .confirmationAction) { Button("Done") { dismiss() } }
            }
            .sheet(isPresented: $scanning) {
                NavigationStack {
                    QRScanner { code in
                        scanning = false
                        if settings.apply(url: code) { test() }
                        else { probe = "not an agenton QR" }
                    }
                    .ignoresSafeArea()
                    .navigationTitle("Scan QR")
                    .navigationBarTitleDisplayMode(.inline)
                    .toolbar {
                        ToolbarItem(placement: .cancellationAction) {
                            Button("Cancel") { scanning = false }
                        }
                    }
                }
            }
        }
    }

    // Hit /healthz and confirm the "agenton" identity string, so a wrong
    // host/port fails here instead of as a silent dead WebSocket.
    private func test() {
        guard let url = settings.healthURL else { probe = "invalid address"; return }
        probe = "testing…"
        URLSession.shared.dataTask(with: url) { data, resp, err in
            let body = data.flatMap { String(data: $0, encoding: .utf8) } ?? ""
            let ok = (resp as? HTTPURLResponse)?.statusCode == 200 && body.contains("agenton")
            DispatchQueue.main.async {
                probe = err != nil ? "unreachable: \(err!.localizedDescription)"
                    : ok ? "✓ reached agenton" : "responded, but not agenton"
            }
        }.resume()
    }
}
