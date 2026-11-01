# Third-party notice

This project deploys and interoperates with [fluxzy](https://github.com/haga-rak/fluxzy.core).

- `fluxzy.core` / Fluxzy CLI is licensed under **GPLv3** (commercial licensing is
  available from the upstream project).
- The `fluxzy-console` image contains a modified fluxzy CLI built from pinned
  upstream source (currently `1.39.52.38106`). The modifications add
  declarative body-transform actions and in-process rule hot reload; their
  patches are in `fork/fluxzy/patches/`.
- The console supervises the CLI as a child process and uses it for rule
  dry-runs, CA export and archive packing.
- The CLI is upgraded by rebuilding the image and the bundled binary from
  source; see `fork/fluxzy/build.sh` and the compatibility checker.
- The console writes `rules.yaml`, reads the dump directory, indexes captured
  sessions, and controls the supervised process lifecycle.

If you distribute the `fluxzy-console` image or a modified CLI outside your
organization, you are responsible for complying with GPLv3 for the bundled
modified fluxzy binary, including providing the corresponding source: the
pinned upstream source, the patches in `fork/fluxzy/patches/`, the build
scripts, and the license text. Internal deployment is unaffected.

Upstream source: https://github.com/haga-rak/fluxzy.core
Upstream license text: `fork/fluxzy/UPSTREAM-LICENSE.md`
Upstream license URL: https://github.com/haga-rak/fluxzy.core/blob/main/LICENSE.md
