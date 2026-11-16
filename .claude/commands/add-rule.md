Add a rule to unscraped for: $ARGUMENTS

1. Find the Prometheus Operator API reference text that decides the behavior, and link it in the PR. Find a real chart or bug report where it matters, and say where it came from.
2. Add it in `plugin/scripts/check.go` with the next unused `USnnn` ID in its group, a message that says what Prometheus or the operator will do, and a fix line. Report it only when the inputs settle it; otherwise add an unchecked entry.
3. Add a case to `TestRules` in `plugin/scripts/unscraped_test.go`, plus a case that must stay quiet. Check that the quiet runs in `tests/real_test.go` stay quiet.
4. Add the rule to the table in `README.md` and to the skill's list in `plugin/skills/checking-prometheus-monitors/SKILL.md`.
5. Run `make test`.
