# Agent instructions

Read README.md first.

- Commit with the identity configured in this clone (`git config user.email`). Set it before the first commit if it is missing, and check `git log --format='%ae %ce'` before pushing.
- This repository is written as if it were public: no installation names, email addresses, hostnames, bucket names, keys or other personal details in files or commit messages. Installation-specific values live in each installation's config repository.
- Configuration belongs in git. The restic backup is for runtime state (libraries, history, users). When a setting can be declared in a config repository instead of only living in an app's database, declare it there.
- Any change to behavior updates the docs in the same commit. Docs describe the current system only, never its history.
- Never print or commit decrypted secrets. Show key names, not values.
- Every service that writes app state runs as uid and gid 1000; a test enforces it.
- Run `make test` after changing scripts or compose files.
- Follow-ups and ideas for later go to GitHub issues in this repository. Draft each one and get the owner's confirmation before creating it.
