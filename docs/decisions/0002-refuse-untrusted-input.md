# 2. Refuse input it can't trust

Date: 2026-11-16

## Decision
unscraped exits 2, with nothing on stdout, when any input can't be read completely: empty files, binary data, random text, the output of a failed `helm template`, `kubectl`, `go build`, `go test` or `curl`, HTML pages, Kubernetes API errors, empty Lists, YAML that isn't a manifest, unrendered Helm templates, files that don't end with a line break or end on a key with no value, error lines mixed into the YAML, duplicate objects, operator kinds outside `monitoring.coreos.com`, invalid selectors, and objects missing the parts unscraped reads. It does the same when the inputs hold no monitor or rule, and when not one link could be checked. `--no-fail` doesn't change this.

## Why
A checker that prints "No problems" for a `helm template` error, or for a manifest cut in half, is worse than none. Exit 2 can't be mistaken for success in CI or by an agent.

## Limits
A file cut exactly on a line break can be a valid shorter file. Such a cut is refused when it leaves a key without a value or an object without required parts; otherwise the shorter file is read. The summary says how many monitors, rules, checks and files were read, so a short read is visible.
