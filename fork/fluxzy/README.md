# fluxzy-console fork

This directory builds the console-maintained fluxzy CLI from the official
`haga-rak/fluxzy.core` source at a pinned release. It does not modify the
official CLI source layout beyond two patches:

- `0001`: adds `ConsoleTransformRequestBodyAction` and
  `ConsoleTransformResponseBodyAction` to Fluxzy.Core. Both are normal,
  YAML-serializable Actions.
- `0002`: watches the rule file and calls `Proxy.UpdateRules()` in-process,
  writing an atomic status file for the console.

Upstream source version and commit are pinned in `upstream.env`.

Build:

```bash
./fork/fluxzy/build.sh arm64   # dev, self-contained glibc
./fork/fluxzy/build.sh amd64   # prod, self-contained musl
```

Compatibility check against a new upstream release:

```bash
./fork/fluxzy/check-upstream.sh <version>          # patch + compile + e2e
./fork/fluxzy/check-upstream.sh <version> --full   # also upstream Rules tests
```

The check script produces `out/check-<version>/report.json`. Only merge and
publish a fork release after the report passes and the semantic diff has been
reviewed manually. The fork version intentionally keeps the upstream base
version. The console uses only the bundled CLI and upgrades it by rebuilding
the image; there is no runtime CLI update path.
