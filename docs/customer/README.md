# Customer-facing guides

Polished, self-contained guides intended to be handed to customers — and to be
rendered to **CircleCI-branded PDFs**.

| File | Audience / scope |
|---|---|
| [secrets-transfer-guide.md](secrets-transfer-guide.md) | Moving **context + project env vars** between two CircleCI orgs, in-pipeline, with no secrets on disk. |
| [oauth-to-oauth-migration-guide.md](oauth-to-oauth-migration-guide.md) | A full **GitHub OAuth → OAuth** org migration runbook with checklists and validation gates. |

Both target `circleci-migrate` **v0.17.1+** and assume GitHub OAuth orgs
(`gh/<name>` slugs).

---

## Building the branded PDFs

These Markdown files carry YAML front matter (title/subtitle/author) and use the
shared stylesheet [`assets/circleci-pdf.css`](assets/circleci-pdf.css). Brand
colours are variables at the top of that CSS — adjust them to the exact current
CircleCI brand kit, and drop the logo at `assets/circleci-logo.png` if you want
it in the header.

### Option A — pandoc + wkhtmltopdf (recommended)

```bash
# one-time: brew install pandoc; brew install --cask wkhtmltopdf
cd docs/customer

pandoc secrets-transfer-guide.md \
  --css assets/circleci-pdf.css \
  --pdf-engine wkhtmltopdf \
  --metadata title="Transferring Secrets Between CircleCI Organizations" \
  -o secrets-transfer-guide.pdf

pandoc oauth-to-oauth-migration-guide.md \
  --css assets/circleci-pdf.css \
  --pdf-engine wkhtmltopdf \
  -o oauth-to-oauth-migration-guide.pdf
```

### Option B — pandoc + weasyprint (better CSS `@page` support)

```bash
# one-time: brew install pandoc weasyprint
cd docs/customer
pandoc secrets-transfer-guide.md --css assets/circleci-pdf.css \
  --pdf-engine weasyprint -o secrets-transfer-guide.pdf
pandoc oauth-to-oauth-migration-guide.md --css assets/circleci-pdf.css \
  --pdf-engine weasyprint -o oauth-to-oauth-migration-guide.pdf
```

### Adding the logo to the header

With wkhtmltopdf you can inject a header image directly:

```bash
pandoc secrets-transfer-guide.md --css assets/circleci-pdf.css \
  --pdf-engine wkhtmltopdf \
  --pdf-engine-opt=--header-html --pdf-engine-opt=assets/header.html \
  -o secrets-transfer-guide.pdf
```

where `assets/header.html` is a tiny HTML snippet that references
`circleci-logo.png`. (Keep the logo ~120px wide, right-aligned.)

> **Tip:** review the rendered PDFs once with the real brand colours/logo before
> sending. The CSS is intentionally conservative so it converts cleanly across
> engines.
