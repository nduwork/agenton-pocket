# Releasing

Releases are automated. A daemon `feat:`/`fix:`/`perf:` commit on `main` makes
release-please open a Release PR; merging it tags `vX.Y.Z`, and the `release`
workflow builds the binaries, `.deb`/`.rpm`, and the Homebrew cask via
goreleaser. Nothing here needs to be run by hand.

## macOS signing & notarization

macOS 15+/26 Gatekeeper blocks unsigned binaries ("cannot verify … free of
malware"). goreleaser signs and notarizes the darwin binaries on the Linux
runner (via the embedded `quill`) **when five secrets are present**; without
them the step self-skips and the release still succeeds — just unsigned.

This is a one-time setup. Requires an Apple Developer account ($99/yr — the same
one behind the iOS app).

### 1. Developer ID Application certificate → `MACOS_SIGN_P12`, `MACOS_SIGN_PASSWORD`

This is a *different* certificate from the iOS "Apple Distribution" one — it is
what signs software distributed **outside** the App Store.

1. https://developer.apple.com/account/resources/certificates/list → **+** →
   **Developer ID Application** → follow the CSR steps (Keychain Access →
   Certificate Assistant → *Request a Certificate from a Certificate Authority*),
   upload the CSR, download the `.cer`, and double-click it to add it to your
   login keychain.
2. In **Keychain Access**, find *Developer ID Application: <your name>*, expand
   it to include its private key, select **both**, right-click → **Export 2
   items** → save as `agenton.p12` and set an export password.
3. Base64 the file (macOS `base64` has no `-w`, so it is single-line already):

   ```bash
   base64 -i agenton.p12 | pbcopy   # → paste as MACOS_SIGN_P12
   ```

   - `MACOS_SIGN_P12` = that base64 string
   - `MACOS_SIGN_PASSWORD` = the export password you chose

### 2. App Store Connect API key → `MACOS_NOTARY_*`

Used by the notary service to accept the upload.

1. https://appstoreconnect.apple.com/access/integrations/api → **Team Keys** →
   **+** → give it a name, role **Developer** (or Admin) → **Generate**.
2. Download the `AuthKey_XXXXXXXXXX.p8` **once** (it can't be re-downloaded).
3. From that page read:
   - **Key ID** — the `XXXXXXXXXX` in the filename / the Key ID column
     → `MACOS_NOTARY_KEY_ID`
   - **Issuer ID** — the UUID shown at the top of the Keys page
     → `MACOS_NOTARY_ISSUER_ID`
4. Base64 the key:

   ```bash
   base64 -i AuthKey_XXXXXXXXXX.p8 | pbcopy   # → paste as MACOS_NOTARY_KEY
   ```

### 3. Add the secrets

`agenton-pocket` → **Settings → Secrets and variables → Actions → New
repository secret**, add all five:

| Secret | Value |
|---|---|
| `MACOS_SIGN_P12` | base64 of `agenton.p12` |
| `MACOS_SIGN_PASSWORD` | the `.p12` export password |
| `MACOS_NOTARY_ISSUER_ID` | App Store Connect issuer UUID |
| `MACOS_NOTARY_KEY_ID` | API key ID |
| `MACOS_NOTARY_KEY` | base64 of the `.p8` |

Or via CLI (prompts for each value):

```bash
for s in MACOS_SIGN_P12 MACOS_SIGN_PASSWORD MACOS_NOTARY_ISSUER_ID \
         MACOS_NOTARY_KEY_ID MACOS_NOTARY_KEY; do
  gh secret set "$s" --repo nduwork/agenton-pocket
done
```

Once set, the **next** release is signed and notarized automatically — land any
daemon `fix:`/`feat:` on `main` and merge the Release PR. (Re-running the
workflow on an *old* tag won't help: it checks out that tag's tree, and tags cut
before this change don't include the notarize config.) Verify a built binary
with:

```bash
codesign -dv --verbose=4 ./agenton            # → Authority=Developer ID Application
spctl -a -vv -t install ./agenton 2>&1 | head # → accepted, source=Notarized ...
```

> The signing/notarization secrets never reach fork PRs (they run only in the
> `release` workflow, which forks can't trigger). Rotate them if leaked; the
> `.p8` and `.p12` can be regenerated in the Apple portals.
