# AGENTS.md

This file defines the stable rules for AI agents working in `doc/`, which is
part of the parent project's Git repository. Read `README.md` for the detailed
workflow and document format.

## Repository Boundary

- `doc/` holds workspace plans and verification records in the parent Git repository.
- Do not initialize a separate Git repository under `doc/`.
- Run Git commands from the parent repository root.
- Keep documentation commits focused; do not include unrelated service source changes.
- Do not create or use the legacy `doc/finish/` directory.

## Documentation Lifecycle

- New plans must be created in `todo/planned/`.
- When implementation starts, move the plan to `todo/in-progress/`.
- When blocked, move the plan to `todo/blocked/` and record the blocker.
- When the blocker is resolved, move the plan back to `todo/in-progress/`.
- Move a plan to `done/` only after its acceptance criteria are satisfied and verification is complete.
- The plan's `status` frontmatter must match its directory.
- Move documents instead of creating duplicate copies, and update relative links when a document is moved.

## Validation

- From the parent repository root, run `npm --prefix doc run docs:check` after
  changing any plan document, especially its layout, metadata, or required sections.
- From the parent repository root, run `npm --prefix doc run docs:test` after
  changing the validation script.
- A `done/` document must include a `completed` frontmatter field and a non-empty `验证结果` or `Verification` section.
- A `blocked/` document must include a non-empty `阻塞原因` or `Blockers` section.

Do not place tokens, passwords, private keys, certificates, connection strings,
or environment-file contents in documentation.
