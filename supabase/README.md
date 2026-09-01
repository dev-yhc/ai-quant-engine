# Database migrations

`migrations/` is the canonical, version-controlled history of the shared
database used by `data-collector` and `valuation-engine`. Supabase is the
current PostgreSQL host; application code must connect through
`DATABASE_CONNECTION_URL`, never through Supabase-only APIs.

The baseline is intentionally plain PostgreSQL. It does not use Supabase Auth,
Storage, Row Level Security, or extensions, so a future PostgreSQL-compatible
host can apply the same migration history. If a non-PostgreSQL migration is
ever required, keep its equivalent under a database-specific migration runner
while retaining the logical schema and migration version.

## Rules

- Do not edit an applied migration, including the baseline. Add a new,
  lexically ordered file named `YYYYMMDDHHMMSS_description.sql` instead.
- Keep schema changes backward-compatible while both services may run different
  versions. Use an expand/migrate/contract sequence for breaking changes.
- Store timestamps in UTC. `observed_at` is a calendar date because the current
  FRED observations are daily values.
- Do not put credentials or project references in migration files.

## Supabase workflow

Install the Supabase CLI, authenticate once, then link this repository to the
existing project:

```bash
supabase login
supabase link --project-ref ylkpeuxzolwsuqloeahv
```

Review the pending changes locally and push the committed migration history:

```bash
supabase db diff --linked
supabase db push
```

`supabase db push` applies only migrations that are not recorded in the remote
Supabase migration history. Run it from the repository root and commit the SQL
file in the same pull request as the code that depends on it.

For a clean local database:

```bash
supabase start
supabase db reset
```
