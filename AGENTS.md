# Agent instructions

Read README.md and CONTRIBUTING.md first, and follow CONTRIBUTING.md's workflow and rules.

- Work on a branch and open a pull request; never push to `main`. Wait for the pull request's checks, then hand over the link: the owner merges.
- Commit with the identity configured in this clone (`git config user.email`). Set it before the first commit if it is missing, and check `git log --format='%ae %ce'` before pushing.
- Never print decrypted secrets. Show key names, not values.
- Draft each follow-up issue and get the owner's confirmation before creating it.
- Tag a release only when the owner asks for it.

When writing code, tests and docs:

- Comments say why, only where the code cannot; names say what.
- Docs describe the system as it is after the change, not its history. A pull request description says why; the diff already shows what.
- Tests state what they expect: mock a call at its boundary and assert on the request it makes and the answer it gets, rather than standing up a fake server.
- Go code follows the conventions in CONTRIBUTING.md: table-driven tests with Given/When/Then, dev tools through `tools/go.mod`, `make go-lint` clean.
- Read the coverage report as a list of questions, not a target. Test the behaviour a change adds, errors included, for inputs real configs and apps can produce. Simplify away a branch nothing can reach rather than testing it, and leave uncovered what is not worth a test.
