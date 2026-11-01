package version

// Version is injected at build time via -ldflags.
var Version = "1.0.0"

// BundledFluxzyRelease is the upstream fluxzy release the bundled CLI was
// built from. It is injected at build time and shown as metadata.
var BundledFluxzyRelease = ""

// Name is the product name shown in the UI and logs.
const Name = "fluxzy-console"
