#!/usr/bin/env bash
#
# Regenerate the committed ANTLR parser (internal/grammar) from a pinned version
# of the tsvsheet grammar (github.com/tsvsheet/tsvsheet).
#
# Standalone and reproducible: this fetches the pinned .g4 sources AND the pinned
# ANTLR Docker context from the grammar repo — the single source of both — builds
# the generator image, emits the Go lexer+parser straight into internal/grammar,
# gofmt's them for a stable diff, and drops the volatile ANTLR aux files. The
# generated parser is committed, so ordinary `go build`/`go test` stay
# Docker-free; only a grammar bump runs this. Bump GRAMMAR_REF to adopt a new
# grammar version.
set -euo pipefail

repo="${GRAMMAR_REPO:-tsvsheet/tsvsheet}"
ref="${GRAMMAR_REF:?set GRAMMAR_REF to the tsvsheet grammar commit SHA or tag to pin}"
out="${GRAMMAR_OUT:-internal/grammar}"
pkg="${GRAMMAR_PKG:-tsvsheetgrammar}"

root="$(git rev-parse --show-toplevel)"
work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

# Fetch the pinned grammar repo (authenticated, so private or public both work).
gh api "repos/${repo}/tarball/${ref}" | tar -xz -C "${work}" --strip-components=1

# Build the pinned ANTLR generator image from the fetched context.
image="tsvsheet-antlr:${ref}"
docker build -q -t "${image}" "${work}/docker/antlr" >/dev/null

# Generate directly into the committed package: workdir is /grammar (the fetched
# .g4, referenced by bare name so ANTLR's header stays path-stable), and /out is
# bind-mounted to internal/grammar so ANTLR can only write the parser there.
docker run --rm -v "${work}":/grammar -v "${root}/${out}":/out -w /grammar "${image}" \
  -Dlanguage=Go -package "${pkg}" -o /out TsvsheetLexer.g4
docker run --rm -v "${work}":/grammar -v "${root}/${out}":/out -w /grammar "${image}" \
  -Dlanguage=Go -visitor -package "${pkg}" -lib /out -o /out TsvsheetParser.g4

# Normalize for a reproducible diff and drop the volatile aux artifacts.
gofmt -w "${root}/${out}"
rm -f "${root}/${out}"/*.interp "${root}/${out}"/*.tokens

echo "regenerated ${out} from ${repo}@${ref}"
