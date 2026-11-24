# Rolling release schema guard

From the repository root, compare the release currently running on **both** instances against the exact deployment commit:

```powershell
go test ./deploy/schema-check
go run ./deploy/schema-check -current v1.0.29 -target HEAD
```

`-current` accepts a formal `vX.Y.Z` tag or a legacy `dev-<hash>` release directory name. Verify the production tag against the trusted release reference before running the checker. A missing reference, non-ancestor baseline, unchanged commit, or any schema-related change stops the automatic deployment. Run the same check again against the final release tag before building.

The guard compares production Go declarations on both sides of the diff, so deleting an existing migration can also block a release. It recognizes GORM model changes, migration symbols, SQL DDL in Go literals, migration/SQL files, and Go dependency manifest changes. Go `_test.go` fixtures are excluded; ordinary ORM calls such as `db.Create` are data writes, not DDL.

This check does **not** inspect a real database or follow all runtime behavior, such as DDL assembled from variables or indirect helper calls. It does not replace database compatibility tests or review of known schema changes. If a change is flagged or its schema behavior is uncertain, stop automatic deployment and review it separately. Do not set `CONFIRM_BACKWARD_COMPATIBLE=yes` on a failed check.
